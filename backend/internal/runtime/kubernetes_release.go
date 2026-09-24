package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/deploymentconfig"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

const (
	releaseLabelKey       = "cicd.yuebuy.com/release"
	releaseVersionLabel   = "cicd.yuebuy.com/version"
	releaseBranchAnnotKey = "cicd.yuebuy.com/branch"
	targetLabelKey        = "cicd.yuebuy.com/target"
	managedByLabelKey     = "cicd.yuebuy.com/managed-by"
	managedByLabelValue   = "ttp"
)

// DeployRelease applies the namespaced resources supported by the current
// deployment manifest contract. All resources are parsed and checked before
// the first API write so unsupported input cannot result in a partial rollout.
func (p *KubernetesProvider) DeployRelease(ctx context.Context, deployment ReleaseDeployment) (err error) {
	log := deployment.Log
	defer func() {
		if err != nil {
			releaseLog(log, "k8s", "stderr", "ERROR", err.Error())
		}
	}()
	deployment.ClusterID = strings.TrimSpace(deployment.ClusterID)
	deployment.Namespace = strings.TrimSpace(deployment.Namespace)
	deployment.ProjectID = strings.TrimSpace(deployment.ProjectID)
	deployment.ReleaseID = strings.TrimSpace(deployment.ReleaseID)
	deployment.CommitSHA = strings.TrimSpace(deployment.CommitSHA)
	deployment.Image = strings.TrimSpace(deployment.Image)
	deployment.Strategy = strings.TrimSpace(deployment.Strategy)
	if err := validateReleaseDeployment(deployment); err != nil {
		return err
	}
	var imagePullSecret *corev1.Secret
	if deployment.ImagePullCredential != nil {
		imagePullSecret, err = p.imagePullSecretForDeployment(deployment)
		if err != nil {
			return err
		}
	}
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("client-go cluster=%s namespace=%s", deployment.ClusterID, deployment.Namespace))
	client, err := p.clientFor(deployment.ClusterID)
	if err != nil {
		return err
	}
	objects, err := decodeReleaseResourceFiles(deployment)
	if err != nil {
		return err
	}
	resources, err := p.prepareReleaseResources(objects, deployment)
	if err != nil {
		return err
	}
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("decoded %d Kubernetes resources from manifest", len(resources)))

	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	if imagePullSecret != nil {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply image pull Secret/%s namespace=%s", imagePullSecret.Name, imagePullSecret.Namespace))
		if existing, getErr := client.CoreV1().Secrets(imagePullSecret.Namespace).Get(requestContext, imagePullSecret.Name, metav1.GetOptions{}); getErr == nil {
			if existing.Labels[managedByLabelKey] != managedByLabelValue || existing.Labels[p.projectLabelKey] != deployment.ProjectID {
				return fmt.Errorf("%w: image pull Secret/%s is not managed by this project", ErrInvalidKubernetesInput, imagePullSecret.Name)
			}
		} else if !apierrors.IsNotFound(getErr) {
			return getErr
		}
		if err := p.applyReleaseSecret(requestContext, client, imagePullSecret); err != nil {
			return err
		}
	}
	appliedDeployments := make([]*appsv1.Deployment, 0, 1)
	appliedStatefulSets := make([]*appsv1.StatefulSet, 0, 1)
	appliedDaemonSets := make([]*appsv1.DaemonSet, 0, 1)
	appliedJobs := make([]*batchv1.Job, 0, 1)
	for _, resource := range resources {
		switch resource.kind {
		case "Deployment":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply Deployment/%s namespace=%s", resource.deployment.Name, resource.deployment.Namespace))
			applied, err := p.applyReleaseDeployment(requestContext, client, resource.deployment)
			if err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("deployment.apps/%s configured resourceVersion=%s generation=%d", applied.Name, applied.ResourceVersion, applied.Generation))
			appliedDeployments = append(appliedDeployments, applied)
		case "StatefulSet":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply StatefulSet/%s namespace=%s", resource.statefulSet.Name, resource.statefulSet.Namespace))
			applied, err := p.applyReleaseStatefulSet(requestContext, client, resource.statefulSet)
			if err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("statefulset.apps/%s configured resourceVersion=%s generation=%d", applied.Name, applied.ResourceVersion, applied.Generation))
			appliedStatefulSets = append(appliedStatefulSets, applied)
		case "DaemonSet":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply DaemonSet/%s namespace=%s", resource.daemonSet.Name, resource.daemonSet.Namespace))
			applied, err := p.applyReleaseDaemonSet(requestContext, client, resource.daemonSet)
			if err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("daemonset.apps/%s configured resourceVersion=%s generation=%d", applied.Name, applied.ResourceVersion, applied.Generation))
			appliedDaemonSets = append(appliedDaemonSets, applied)
		case "Job":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply Job/%s namespace=%s", resource.job.Name, resource.job.Namespace))
			applied, err := p.applyReleaseJob(requestContext, client, resource.job, deployment)
			if err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("job.batch/%s configured resourceVersion=%s generation=%d", applied.Name, applied.ResourceVersion, applied.Generation))
			appliedJobs = append(appliedJobs, applied)
		case "CronJob":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply CronJob/%s namespace=%s", resource.cronJob.Name, resource.cronJob.Namespace))
			if err := p.applyReleaseCronJob(requestContext, client, resource.cronJob); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("cronjob.batch/%s configured", resource.cronJob.Name))
		case "Service":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply Service/%s namespace=%s", resource.service.Name, resource.service.Namespace))
			if err := p.applyReleaseService(requestContext, client, resource.service); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("service/%s configured", resource.service.Name))
		case "ConfigMap":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply ConfigMap/%s namespace=%s", resource.configMap.Name, resource.configMap.Namespace))
			if err := p.applyReleaseConfigMap(requestContext, client, resource.configMap); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("configmap/%s configured", resource.configMap.Name))
		case "Secret":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply Secret/%s namespace=%s", resource.secret.Name, resource.secret.Namespace))
			if err := p.applyReleaseSecret(requestContext, client, resource.secret); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("secret/%s configured", resource.secret.Name))
		case "PersistentVolumeClaim":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply PersistentVolumeClaim/%s namespace=%s", resource.pvc.Name, resource.pvc.Namespace))
			if err := p.applyReleasePersistentVolumeClaim(requestContext, client, resource.pvc, deployment); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("persistentvolumeclaim/%s configured", resource.pvc.Name))
		case "Ingress":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply Ingress/%s namespace=%s", resource.ingress.Name, resource.ingress.Namespace))
			if err := p.applyReleaseIngress(requestContext, client, resource.ingress); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("ingress.networking.k8s.io/%s configured", resource.ingress.Name))
		case "HorizontalPodAutoscaler":
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("apply HorizontalPodAutoscaler/%s namespace=%s", resource.hpa.Name, resource.hpa.Namespace))
			if err := p.applyReleaseHPA(requestContext, client, resource.hpa); err != nil {
				return err
			}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("horizontalpodautoscaler.autoscaling/%s configured", resource.hpa.Name))
		}
	}
	if !p.rolloutWait {
		return p.reconcileReleaseResources(ctx, client, deployment, resources, imagePullSecret, log)
	}
	for _, applied := range appliedDeployments {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("deployment/%s rollout status", applied.Name))
		if err := p.waitForDeploymentRollout(ctx, client, applied, log); err != nil {
			return err
		}
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("deployment/%s successfully rolled out", applied.Name))
	}
	for _, applied := range appliedStatefulSets {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("statefulset/%s rollout status", applied.Name))
		if err := p.waitForStatefulSetRollout(ctx, client, applied, log); err != nil {
			return err
		}
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("statefulset/%s successfully rolled out", applied.Name))
	}
	for _, applied := range appliedDaemonSets {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("daemonset/%s rollout status", applied.Name))
		if err := p.waitForDaemonSetRollout(ctx, client, applied, log); err != nil {
			return err
		}
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("daemonset/%s successfully rolled out", applied.Name))
	}
	for _, applied := range appliedJobs {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("job/%s completion status", applied.Name))
		if err := p.waitForJobCompletion(ctx, client, applied, log); err != nil {
			return err
		}
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("job/%s successfully completed", applied.Name))
	}
	return p.reconcileReleaseResources(ctx, client, deployment, resources, imagePullSecret, log)
}

func releaseLog(log ReleaseLogFunc, source, stream, level, line string) {
	if log != nil && strings.TrimSpace(line) != "" {
		log(source, stream, level, line)
	}
}

type releaseResource struct {
	kind        string
	deployment  *appsv1.Deployment
	statefulSet *appsv1.StatefulSet
	daemonSet   *appsv1.DaemonSet
	job         *batchv1.Job
	cronJob     *batchv1.CronJob
	service     *corev1.Service
	configMap   *corev1.ConfigMap
	secret      *corev1.Secret
	pvc         *corev1.PersistentVolumeClaim
	ingress     *networkingv1.Ingress
	hpa         *autoscalingv2.HorizontalPodAutoscaler
}

func validateReleaseDeployment(deployment ReleaseDeployment) error {
	if err := validateClusterID(strings.TrimSpace(deployment.ClusterID)); err != nil {
		return err
	}
	if err := validateNamespace(strings.TrimSpace(deployment.Namespace)); err != nil {
		return err
	}
	if err := validateProjectID(strings.TrimSpace(deployment.ProjectID)); err != nil {
		return err
	}
	if strings.TrimSpace(deployment.ProjectID) == "" {
		return fmt.Errorf("%w: project ID is required", ErrInvalidKubernetesInput)
	}
	for name, value := range map[string]string{
		"release ID": deployment.ReleaseID,
		"commit SHA": deployment.CommitSHA,
		"image":      deployment.Image,
	} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("%w: %s is required", ErrInvalidKubernetesInput, name)
		}
	}
	if !validLabelValue(deployment.ReleaseID) {
		return fmt.Errorf("%w: release ID is not a valid Kubernetes label value", ErrInvalidKubernetesInput)
	}
	if strings.ContainsAny(deployment.Branch, "\x00\r\n") {
		return fmt.Errorf("%w: branch contains invalid control characters", ErrInvalidKubernetesInput)
	}
	if strings.TrimSpace(deployment.Strategy) != "" && strings.TrimSpace(deployment.Strategy) != "rolling" {
		return fmt.Errorf("%w: kubernetes provider supports only rolling release strategy", ErrInvalidKubernetesInput)
	}
	if len(deployment.ResourceFiles) == 0 && strings.TrimSpace(deployment.Manifest) == "" {
		return fmt.Errorf("%w: release resource files are required", ErrInvalidKubernetesInput)
	}
	return nil
}

func (p *KubernetesProvider) imagePullSecretForDeployment(deployment ReleaseDeployment) (*corev1.Secret, error) {
	credential := deployment.ImagePullCredential
	if credential == nil {
		return nil, nil
	}
	if strings.TrimSpace(credential.Registry) == "" || strings.TrimSpace(credential.Secret) == "" || strings.TrimSpace(credential.SecretName) == "" {
		return nil, fmt.Errorf("%w: image pull credential is incomplete", ErrInvalidKubernetesInput)
	}
	if credential.AuthType != "basic" && credential.AuthType != "token" {
		return nil, fmt.Errorf("%w: image pull credential auth type is invalid", ErrInvalidKubernetesInput)
	}
	if credential.AuthType == "basic" && strings.TrimSpace(credential.Username) == "" {
		return nil, fmt.Errorf("%w: image pull credential username is required", ErrInvalidKubernetesInput)
	}
	if strings.ContainsAny(credential.Registry+credential.Username+credential.Secret+credential.SecretName, "\x00\r\n") {
		return nil, fmt.Errorf("%w: image pull credential contains control characters", ErrInvalidKubernetesInput)
	}
	if len(credential.Registry) > 255 || len(credential.Username) > 120 || len(credential.Secret) > 4096 {
		return nil, fmt.Errorf("%w: image pull credential is too long", ErrInvalidKubernetesInput)
	}
	imageHost := strings.ToLower(strings.SplitN(strings.TrimSpace(deployment.Image), "/", 2)[0])
	registryHost := strings.ToLower(strings.TrimSpace(credential.Registry))
	if imageHost == "" || imageHost != registryHost {
		return nil, fmt.Errorf("%w: image pull credential does not match image registry", ErrInvalidKubernetesInput)
	}
	if len(validation.IsDNS1123Subdomain(strings.ToLower(credential.SecretName))) != 0 {
		return nil, fmt.Errorf("%w: image pull Secret name is invalid", ErrInvalidKubernetesInput)
	}
	entry := map[string]string{}
	if credential.AuthType == "token" {
		entry["identitytoken"] = credential.Secret
	} else {
		entry["auth"] = base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Secret))
	}
	config, err := json.Marshal(map[string]any{"auths": map[string]any{strings.ToLower(strings.TrimSpace(credential.Registry)): entry}})
	if err != nil {
		return nil, fmt.Errorf("%w: encode image pull credential", ErrInvalidKubernetesInput)
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: strings.TrimSpace(credential.SecretName), Namespace: deployment.Namespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: config},
	}
	p.prepareReleaseObjectMeta(&secret.ObjectMeta, deployment)
	return secret, nil
}

func validateManifestFormat(deployment ReleaseDeployment) error {
	format, err := deploymentconfig.NormalizeFormat(deployment.ManifestFormat)
	if err != nil {
		return err
	}
	if strings.TrimSpace(deployment.ManifestFormat) != "" && format != deploymentconfig.DetectFormat(deployment.Manifest) {
		return fmt.Errorf("%w: manifest format does not match content", deploymentconfig.ErrInvalidManifest)
	}
	return nil
}

func decodeReleaseResourceFiles(deployment ReleaseDeployment) ([]map[string]any, error) {
	if len(deployment.ResourceFiles) == 0 {
		if err := validateManifestFormat(deployment); err != nil {
			return nil, err
		}
		if _, err := deploymentconfig.Validate(deployment.Manifest, deployment.Namespace); err != nil {
			return nil, err
		}
		return decodeReleaseObjects(deployment.Manifest)
	}
	objects := make([]map[string]any, 0, len(deployment.ResourceFiles))
	for index, content := range deployment.ResourceFiles {
		validated, err := deploymentconfig.ValidateResourceFile(content, "", deployment.Namespace)
		if err != nil {
			return nil, fmt.Errorf("resource file %d: %w", index+1, err)
		}
		decoded, err := decodeReleaseObjects(content)
		if err != nil {
			return nil, fmt.Errorf("resource file %d: %w", index+1, err)
		}
		if len(decoded) != 1 || len(validated.Resources) != 1 {
			return nil, fmt.Errorf("%w: resource file %d must contain exactly one object", deploymentconfig.ErrInvalidManifest, index+1)
		}
		objects = append(objects, decoded[0])
	}
	return objects, nil
}

func decodeReleaseObjects(manifest string) ([]map[string]any, error) {
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	objects := make([]map[string]any, 0, 4)
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: YAML syntax error: %v", deploymentconfig.ErrInvalidManifest, err)
		}
		if len(node.Content) == 0 {
			continue
		}
		var value any
		if err := node.Content[0].Decode(&value); err != nil {
			return nil, fmt.Errorf("%w: document cannot be decoded: %v", deploymentconfig.ErrInvalidManifest, err)
		}
		if list, ok := value.([]any); ok {
			for _, item := range list {
				object, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%w: every manifest array item must be an object", deploymentconfig.ErrInvalidManifest)
				}
				objects = append(objects, object)
			}
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: each manifest document must be an object", deploymentconfig.ErrInvalidManifest)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func (p *KubernetesProvider) prepareReleaseResources(objects []map[string]any, deployment ReleaseDeployment) ([]releaseResource, error) {
	if len(objects) == 0 {
		return nil, fmt.Errorf("%w: at least one resource is required", deploymentconfig.ErrInvalidManifest)
	}
	resources := make([]releaseResource, 0, len(objects))
	hasWorkload := false
	for index, object := range objects {
		kind, ok := object["kind"].(string)
		if !ok {
			return nil, fmt.Errorf("%w: resource %d kind is required", deploymentconfig.ErrInvalidManifest, index+1)
		}
		kind = strings.TrimSpace(kind)
		if !deploymentconfig.IsReleaseSupportedKind(kind) {
			return nil, fmt.Errorf("%w: resource %d kind %q is not supported by the Kubernetes release provider", deploymentconfig.ErrInvalidManifest, index+1, kind)
		}
		metadata, ok := object["metadata"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: resource %d metadata is required", deploymentconfig.ErrInvalidManifest, index+1)
		}
		if kind == "Secret" && deployment.ImagePullCredential != nil {
			if name, _ := metadata["name"].(string); strings.TrimSpace(name) == strings.TrimSpace(deployment.ImagePullCredential.SecretName) {
				return nil, fmt.Errorf("%w: manifest Secret conflicts with the managed image pull Secret", deploymentconfig.ErrInvalidManifest)
			}
		}
		metadata["namespace"] = deployment.Namespace
		object["metadata"] = metadata
		encoded, err := json.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("%w: resource %d cannot be encoded: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
		}
		unstructuredObject := &unstructured.Unstructured{}
		if err := json.Unmarshal(encoded, &unstructuredObject.Object); err != nil {
			return nil, fmt.Errorf("%w: resource %d cannot be decoded: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
		}
		resource := releaseResource{kind: kind}
		switch kind {
		case "Deployment":
			resource.deployment = &appsv1.Deployment{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.deployment); err != nil {
				return nil, fmt.Errorf("%w: resource %d deployment is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := p.prepareReleaseDeployment(resource.deployment, deployment); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			hasWorkload = true
		case "StatefulSet":
			resource.statefulSet = &appsv1.StatefulSet{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.statefulSet); err != nil {
				return nil, fmt.Errorf("%w: resource %d stateful set is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := p.prepareReleaseStatefulSet(resource.statefulSet, deployment); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			hasWorkload = true
		case "DaemonSet":
			resource.daemonSet = &appsv1.DaemonSet{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.daemonSet); err != nil {
				return nil, fmt.Errorf("%w: resource %d daemon set is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := p.prepareReleaseDaemonSet(resource.daemonSet, deployment); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			hasWorkload = true
		case "Job":
			resource.job = &batchv1.Job{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.job); err != nil {
				return nil, fmt.Errorf("%w: resource %d job is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := p.prepareReleaseJob(resource.job, deployment); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			hasWorkload = true
		case "CronJob":
			resource.cronJob = &batchv1.CronJob{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.cronJob); err != nil {
				return nil, fmt.Errorf("%w: resource %d cron job is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := p.prepareReleaseCronJob(resource.cronJob, deployment); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			hasWorkload = true
		case "Service":
			resource.service = &corev1.Service{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.service); err != nil {
				return nil, fmt.Errorf("%w: resource %d service is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			p.prepareReleaseObjectMeta(&resource.service.ObjectMeta, deployment)
		case "ConfigMap":
			resource.configMap = &corev1.ConfigMap{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.configMap); err != nil {
				return nil, fmt.Errorf("%w: resource %d config map is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			p.prepareReleaseObjectMeta(&resource.configMap.ObjectMeta, deployment)
		case "Secret":
			resource.secret = &corev1.Secret{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.secret); err != nil {
				return nil, fmt.Errorf("%w: resource %d secret is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			p.prepareReleaseObjectMeta(&resource.secret.ObjectMeta, deployment)
		case "PersistentVolumeClaim":
			resource.pvc = &corev1.PersistentVolumeClaim{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.pvc); err != nil {
				return nil, fmt.Errorf("%w: resource %d persistent volume claim is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := validateReleasePersistentVolumeClaim(resource.pvc); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			p.prepareReleaseObjectMeta(&resource.pvc.ObjectMeta, deployment)
		case "Ingress":
			resource.ingress = &networkingv1.Ingress{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.ingress); err != nil {
				return nil, fmt.Errorf("%w: resource %d ingress is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			p.prepareReleaseObjectMeta(&resource.ingress.ObjectMeta, deployment)
		case "HorizontalPodAutoscaler":
			resource.hpa = &autoscalingv2.HorizontalPodAutoscaler{}
			if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.Object, resource.hpa); err != nil {
				return nil, fmt.Errorf("%w: resource %d horizontal pod autoscaler is invalid: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			if err := validateReleaseHPA(resource.hpa); err != nil {
				return nil, fmt.Errorf("%w: resource %d: %v", deploymentconfig.ErrInvalidManifest, index+1, err)
			}
			p.prepareReleaseObjectMeta(&resource.hpa.ObjectMeta, deployment)
		}
		resources = append(resources, resource)
	}
	if !hasWorkload {
		return nil, fmt.Errorf("%w: release manifest must contain a Deployment, StatefulSet, DaemonSet, Job, or CronJob", deploymentconfig.ErrInvalidManifest)
	}
	return resources, nil
}

func (p *KubernetesProvider) prepareReleaseDeployment(deployment *appsv1.Deployment, release ReleaseDeployment) error {
	if deployment.Name == "" {
		return errors.New("deployment name is required")
	}
	p.prepareReleaseWorkloadMeta(&deployment.ObjectMeta, release)
	return p.prepareReleasePodTemplate(&deployment.Spec.Template, release)
}

func (p *KubernetesProvider) prepareReleaseStatefulSet(statefulSet *appsv1.StatefulSet, release ReleaseDeployment) error {
	if statefulSet.Name == "" {
		return errors.New("stateful set name is required")
	}
	p.prepareReleaseWorkloadMeta(&statefulSet.ObjectMeta, release)
	for index := range statefulSet.Spec.VolumeClaimTemplates {
		p.prepareReleaseObjectMeta(&statefulSet.Spec.VolumeClaimTemplates[index].ObjectMeta, release)
	}
	return p.prepareReleasePodTemplate(&statefulSet.Spec.Template, release)
}

func (p *KubernetesProvider) prepareReleaseDaemonSet(daemonSet *appsv1.DaemonSet, release ReleaseDeployment) error {
	if daemonSet.Name == "" {
		return errors.New("daemon set name is required")
	}
	p.prepareReleaseWorkloadMeta(&daemonSet.ObjectMeta, release)
	return p.prepareReleasePodTemplate(&daemonSet.Spec.Template, release)
}

func (p *KubernetesProvider) prepareReleaseJob(job *batchv1.Job, release ReleaseDeployment) error {
	if job.Name == "" {
		return errors.New("job name is required")
	}
	p.prepareReleaseWorkloadMeta(&job.ObjectMeta, release)
	return p.prepareReleasePodTemplate(&job.Spec.Template, release)
}

func (p *KubernetesProvider) prepareReleaseCronJob(cronJob *batchv1.CronJob, release ReleaseDeployment) error {
	if cronJob.Name == "" {
		return errors.New("cron job name is required")
	}
	p.prepareReleaseWorkloadMeta(&cronJob.ObjectMeta, release)
	p.prepareReleaseWorkloadMeta(&cronJob.Spec.JobTemplate.ObjectMeta, release)
	return p.prepareReleasePodTemplate(&cronJob.Spec.JobTemplate.Spec.Template, release)
}

func (p *KubernetesProvider) prepareReleaseWorkloadMeta(meta *metav1.ObjectMeta, release ReleaseDeployment) {
	p.prepareReleaseObjectMeta(meta, release)
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[releaseBranchAnnotKey] = release.Branch
}

func (p *KubernetesProvider) prepareReleasePodTemplate(template *corev1.PodTemplateSpec, release ReleaseDeployment) error {
	p.prepareReleaseObjectMeta(&template.ObjectMeta, release)
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}
	template.Annotations[releaseBranchAnnotKey] = release.Branch
	template.Annotations[rolloutVersionKey] = p.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	version := release.CommitSHA
	if len(version) > 10 {
		version = version[:10]
	}
	if validLabelValue(version) {
		template.Labels[releaseVersionLabel] = version
	}
	containerIndex, err := p.targetContainerIndex(template.Spec.Containers)
	if err != nil {
		return err
	}
	container := &template.Spec.Containers[containerIndex]
	container.Image = release.Image
	if release.ImagePullCredential != nil {
		secretName := strings.TrimSpace(release.ImagePullCredential.SecretName)
		found := false
		for _, reference := range template.Spec.ImagePullSecrets {
			if reference.Name == secretName {
				found = true
				break
			}
		}
		if !found {
			template.Spec.ImagePullSecrets = append(template.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: secretName})
		}
	}
	container.Env = setEnvironment(container.Env, map[string]string{
		"CICD_RELEASE_ID":        release.ReleaseID,
		"CICD_COMMIT_SHA":        release.CommitSHA,
		"CICD_BRANCH":            release.Branch,
		"CICD_STRATEGY":          release.Strategy,
		"CICD_STABLE_PERCENT":    fmt.Sprint(release.StablePercent),
		"CICD_CANDIDATE_PERCENT": fmt.Sprint(release.CandidatePercent),
		"CICD_BLUE_PERCENT":      fmt.Sprint(release.BluePercent),
		"CICD_GREEN_PERCENT":     fmt.Sprint(release.GreenPercent),
	})
	return nil
}

func (p *KubernetesProvider) prepareReleaseObjectMeta(meta *metav1.ObjectMeta, release ReleaseDeployment) {
	if meta == nil {
		return
	}
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels[p.projectLabelKey] = release.ProjectID
	meta.Labels[managedByLabelKey] = managedByLabelValue
	meta.Labels[releaseLabelKey] = release.ReleaseID
	if release.TargetID != "" {
		meta.Labels[targetLabelKey] = release.TargetID
	}
}

func validLabelValue(value string) bool {
	return len(validation.IsValidLabelValue(value)) == 0
}

func (p *KubernetesProvider) applyReleaseDeployment(ctx context.Context, client kubernetes.Interface, desired *appsv1.Deployment) (*appsv1.Deployment, error) {
	deployments := client.AppsV1().Deployments(desired.Namespace)
	existing, err := deployments.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := deployments.Create(ctx, desired, metav1.CreateOptions{})
		return created, createErr
	}
	if err != nil {
		return nil, err
	}
	desired.ResourceVersion = existing.ResourceVersion
	updated, updateErr := deployments.Update(ctx, desired, metav1.UpdateOptions{})
	return updated, updateErr
}

func (p *KubernetesProvider) applyReleaseStatefulSet(ctx context.Context, client kubernetes.Interface, desired *appsv1.StatefulSet) (*appsv1.StatefulSet, error) {
	statefulSets := client.AppsV1().StatefulSets(desired.Namespace)
	existing, err := statefulSets.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := statefulSets.Create(ctx, desired, metav1.CreateOptions{})
		return created, createErr
	}
	if err != nil {
		return nil, err
	}
	desired.ResourceVersion = existing.ResourceVersion
	desired.Status = existing.Status
	updated, updateErr := statefulSets.Update(ctx, desired, metav1.UpdateOptions{})
	return updated, updateErr
}

func (p *KubernetesProvider) applyReleaseDaemonSet(ctx context.Context, client kubernetes.Interface, desired *appsv1.DaemonSet) (*appsv1.DaemonSet, error) {
	daemonSets := client.AppsV1().DaemonSets(desired.Namespace)
	existing, err := daemonSets.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := daemonSets.Create(ctx, desired, metav1.CreateOptions{})
		return created, createErr
	}
	if err != nil {
		return nil, err
	}
	desired.ResourceVersion = existing.ResourceVersion
	desired.Status = existing.Status
	updated, updateErr := daemonSets.Update(ctx, desired, metav1.UpdateOptions{})
	return updated, updateErr
}

func (p *KubernetesProvider) applyReleaseJob(ctx context.Context, client kubernetes.Interface, desired *batchv1.Job, release ReleaseDeployment) (*batchv1.Job, error) {
	jobs := client.BatchV1().Jobs(desired.Namespace)
	existing, err := jobs.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		created, createErr := jobs.Create(ctx, desired, metav1.CreateOptions{})
		return created, createErr
	}
	if err != nil {
		return nil, err
	}
	if !isManagedWorkload(existing.Labels, p.projectLabelKey, release.ProjectID, release.TargetID) {
		return nil, fmt.Errorf("%w: Job/%s is not managed by this project and environment", ErrInvalidKubernetesInput, desired.Name)
	}
	if existing.Status.Active > 0 {
		return nil, fmt.Errorf("%w: Job/%s is still running and cannot be replaced", ErrInvalidKubernetesInput, desired.Name)
	}
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: propagationPolicy(metav1.DeletePropagationForeground)}
	if err := jobs.Delete(ctx, desired.Name, deleteOptions); err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}
	if err := p.waitForJobDeletion(ctx, desired.Name, func(requestContext context.Context) (*batchv1.Job, error) {
		return jobs.Get(requestContext, desired.Name, metav1.GetOptions{})
	}); err != nil {
		return nil, err
	}
	created, err := jobs.Create(ctx, desired, metav1.CreateOptions{})
	return created, err
}

func (p *KubernetesProvider) applyReleaseCronJob(ctx context.Context, client kubernetes.Interface, desired *batchv1.CronJob) error {
	cronJobs := client.BatchV1().CronJobs(desired.Namespace)
	existing, err := cronJobs.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = cronJobs.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels[p.projectLabelKey] != desired.Labels[p.projectLabelKey] ||
		existing.Labels[targetLabelKey] != desired.Labels[targetLabelKey] ||
		existing.Labels[managedByLabelKey] != managedByLabelValue {
		return fmt.Errorf("%w: CronJob/%s is not managed by this project and environment", ErrInvalidKubernetesInput, desired.Name)
	}
	desired.ResourceVersion = existing.ResourceVersion
	desired.Status = existing.Status
	_, err = cronJobs.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func isManagedWorkload(labels map[string]string, projectLabelKey, projectID, targetID string) bool {
	return labels[projectLabelKey] == projectID && labels[targetLabelKey] == targetID && labels[managedByLabelKey] == managedByLabelValue
}

func (p *KubernetesProvider) waitForJobDeletion(ctx context.Context, name string, get func(context.Context) (*batchv1.Job, error)) error {
	interval := p.rolloutPollInterval
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_, err := get(ctx)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: Job/%s deletion timed out", ErrDeploymentRolloutTimeout, name)
		case <-ticker.C:
		}
	}
}

func (p *KubernetesProvider) applyReleaseService(ctx context.Context, client kubernetes.Interface, desired *corev1.Service) error {
	services := client.CoreV1().Services(desired.Namespace)
	existing, err := services.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = services.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	desired.Spec.ClusterIPs = existing.Spec.ClusterIPs
	desired.Spec.IPFamilies = existing.Spec.IPFamilies
	desired.Spec.IPFamilyPolicy = existing.Spec.IPFamilyPolicy
	desired.Spec.HealthCheckNodePort = existing.Spec.HealthCheckNodePort
	desired.Spec.AllocateLoadBalancerNodePorts = existing.Spec.AllocateLoadBalancerNodePorts
	desired.Spec.LoadBalancerClass = existing.Spec.LoadBalancerClass
	_, err = services.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func (p *KubernetesProvider) applyReleaseConfigMap(ctx context.Context, client kubernetes.Interface, desired *corev1.ConfigMap) error {
	configMaps := client.CoreV1().ConfigMaps(desired.Namespace)
	existing, err := configMaps.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = configMaps.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	_, err = configMaps.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func (p *KubernetesProvider) applyReleaseSecret(ctx context.Context, client kubernetes.Interface, desired *corev1.Secret) error {
	secrets := client.CoreV1().Secrets(desired.Namespace)
	existing, err := secrets.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	_, err = secrets.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func validateReleasePersistentVolumeClaim(claim *corev1.PersistentVolumeClaim) error {
	if claim == nil || strings.TrimSpace(claim.Name) == "" {
		return errors.New("persistent volume claim name is required")
	}
	if len(claim.Spec.AccessModes) == 0 {
		return errors.New("persistent volume claim accessModes is required")
	}
	requests := claim.Spec.Resources.Requests
	storage, ok := requests[corev1.ResourceStorage]
	if !ok || storage.Sign() <= 0 {
		return errors.New("persistent volume claim storage request must be greater than zero")
	}
	for name := range requests {
		if name != corev1.ResourceStorage {
			return fmt.Errorf("persistent volume claim resource request %q is not supported", name)
		}
	}
	return nil
}

func validatePersistentVolumeClaimImmutableFields(desired, existing *corev1.PersistentVolumeClaim) error {
	if desired == nil || existing == nil {
		return errors.New("persistent volume claim is required")
	}
	if !samePersistentVolumeAccessModes(desired.Spec.AccessModes, existing.Spec.AccessModes) {
		return errors.New("persistent volume claim accessModes cannot be changed after creation")
	}
	if desired.Spec.Selector != nil && !apiequality.Semantic.DeepEqual(desired.Spec.Selector, existing.Spec.Selector) {
		return errors.New("persistent volume claim selector cannot be changed after creation")
	}
	if desired.Spec.StorageClassName != nil && !apiequality.Semantic.DeepEqual(desired.Spec.StorageClassName, existing.Spec.StorageClassName) {
		return errors.New("persistent volume claim storageClassName cannot be changed after creation")
	}
	if desired.Spec.VolumeMode != nil {
		existingMode := corev1.PersistentVolumeFilesystem
		if existing.Spec.VolumeMode != nil {
			existingMode = *existing.Spec.VolumeMode
		}
		if *desired.Spec.VolumeMode != existingMode {
			return errors.New("persistent volume claim volumeMode cannot be changed after creation")
		}
	}
	if desired.Spec.VolumeName != "" && desired.Spec.VolumeName != existing.Spec.VolumeName {
		return errors.New("persistent volume claim volumeName cannot be changed after creation")
	}
	if desired.Spec.DataSource != nil && !apiequality.Semantic.DeepEqual(desired.Spec.DataSource, existing.Spec.DataSource) {
		return errors.New("persistent volume claim dataSource cannot be changed after creation")
	}
	if desired.Spec.DataSourceRef != nil && !apiequality.Semantic.DeepEqual(desired.Spec.DataSourceRef, existing.Spec.DataSourceRef) {
		return errors.New("persistent volume claim dataSourceRef cannot be changed after creation")
	}
	return nil
}

func samePersistentVolumeAccessModes(left, right []corev1.PersistentVolumeAccessMode) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[corev1.PersistentVolumeAccessMode]int, len(left))
	for _, mode := range left {
		counts[mode]++
	}
	for _, mode := range right {
		counts[mode]--
		if counts[mode] < 0 {
			return false
		}
	}
	return true
}

func (p *KubernetesProvider) applyReleasePersistentVolumeClaim(ctx context.Context, client kubernetes.Interface, desired *corev1.PersistentVolumeClaim, release ReleaseDeployment) error {
	claims := client.CoreV1().PersistentVolumeClaims(desired.Namespace)
	existing, err := claims.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = claims.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels[p.projectLabelKey] != release.ProjectID ||
		existing.Labels[targetLabelKey] != release.TargetID ||
		existing.Labels[managedByLabelKey] != managedByLabelValue {
		return fmt.Errorf("%w: PersistentVolumeClaim/%s is not managed by this project and environment", ErrInvalidKubernetesInput, desired.Name)
	}
	if err := validatePersistentVolumeClaimImmutableFields(desired, existing); err != nil {
		return fmt.Errorf("%w: PersistentVolumeClaim/%s: %v", ErrInvalidKubernetesInput, desired.Name, err)
	}
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if existingStorage, ok := existing.Spec.Resources.Requests[corev1.ResourceStorage]; ok && desiredStorage.Cmp(existingStorage) < 0 {
		return fmt.Errorf("%w: PersistentVolumeClaim/%s storage cannot be reduced", ErrInvalidKubernetesInput, desired.Name)
	}
	if currentCapacity, ok := existing.Status.Capacity[corev1.ResourceStorage]; ok && desiredStorage.Cmp(currentCapacity) < 0 {
		return fmt.Errorf("%w: PersistentVolumeClaim/%s storage cannot be lower than current capacity", ErrInvalidKubernetesInput, desired.Name)
	}

	updated := desired.DeepCopy()
	updated.ResourceVersion = existing.ResourceVersion
	updated.Spec = *existing.Spec.DeepCopy()
	if updated.Spec.Resources.Requests == nil {
		updated.Spec.Resources.Requests = corev1.ResourceList{}
	}
	updated.Spec.Resources.Requests[corev1.ResourceStorage] = desiredStorage
	updated.Status = existing.Status
	_, err = claims.Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

func (p *KubernetesProvider) applyReleaseIngress(ctx context.Context, client kubernetes.Interface, desired *networkingv1.Ingress) error {
	ingresses := client.NetworkingV1().Ingresses(desired.Namespace)
	existing, err := ingresses.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = ingresses.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	// Status is owned by the ingress controller and must not be overwritten by
	// a release manifest.
	desired.Status = existing.Status
	_, err = ingresses.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func (p *KubernetesProvider) applyReleaseHPA(ctx context.Context, client kubernetes.Interface, desired *autoscalingv2.HorizontalPodAutoscaler) error {
	hpas := client.AutoscalingV2().HorizontalPodAutoscalers(desired.Namespace)
	existing, err := hpas.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = hpas.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	// Current replicas and conditions are controller-owned status fields.
	desired.Status = existing.Status
	_, err = hpas.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func validateReleaseHPA(hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	if hpa == nil || strings.TrimSpace(hpa.Name) == "" {
		return errors.New("horizontal pod autoscaler name is required")
	}
	if strings.TrimSpace(hpa.Spec.ScaleTargetRef.Name) == "" {
		return errors.New("horizontal pod autoscaler scaleTargetRef.name is required")
	}
	if hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas < 1 {
		return errors.New("horizontal pod autoscaler minReplicas must be at least 1")
	}
	if hpa.Spec.MaxReplicas < 1 {
		return errors.New("horizontal pod autoscaler maxReplicas must be at least 1")
	}
	if hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas > hpa.Spec.MaxReplicas {
		return errors.New("horizontal pod autoscaler minReplicas cannot exceed maxReplicas")
	}
	return nil
}

// waitForDeploymentRollout waits for the Deployment controller, rather than
// treating an accepted API write as a successful release. The caller's
// context remains the outer cancellation boundary; rolloutTimeout gives this
// provider its own, explicit readiness deadline.
func (p *KubernetesProvider) waitForDeploymentRollout(ctx context.Context, client kubernetes.Interface, applied *appsv1.Deployment, log ReleaseLogFunc) error {
	if applied == nil {
		return fmt.Errorf("%w: deployment response was empty", ErrDeploymentRolloutFailed)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := p.rolloutTimeout
	if timeout <= 0 {
		timeout = defaultRolloutTimeout
	}
	pollInterval := p.rolloutPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultRolloutPollInterval
	}
	rolloutContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	desiredReplicas := int32(1)
	if applied.Spec.Replicas != nil {
		desiredReplicas = *applied.Spec.Replicas
	}
	targetGeneration := applied.Generation
	lastSummary := "尚未读取 Deployment 状态"
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		current, err := p.getDeploymentForRollout(rolloutContext, client, applied.Namespace, applied.Name)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && rolloutContext.Err() != nil {
				return fmt.Errorf("%w: deployment %s/%s 等待状态时超时（%s）", ErrDeploymentRolloutTimeout, applied.Namespace, applied.Name, lastSummary)
			}
			return err
		}
		ready, failure, summary := deploymentRolloutState(current, targetGeneration, desiredReplicas)
		lastSummary = summary
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("deployment/%s: %s", applied.Name, summary))
		if pullFailure, pullSummary := p.imagePullFailure(rolloutContext, client, applied); pullFailure != nil {
			releaseLog(log, "k8s", "stderr", "ERROR", pullSummary)
			return pullFailure
		}
		if failure != nil {
			return failure
		}
		if ready {
			return nil
		}

		select {
		case <-rolloutContext.Done():
			if errors.Is(rolloutContext.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%w: deployment %s/%s 在 %s 内未就绪（%s）", ErrDeploymentRolloutTimeout, applied.Namespace, applied.Name, timeout, lastSummary)
			}
			return rolloutContext.Err()
		case <-ticker.C:
		}
	}
}

func (p *KubernetesProvider) waitForStatefulSetRollout(ctx context.Context, client kubernetes.Interface, applied *appsv1.StatefulSet, log ReleaseLogFunc) error {
	if applied == nil {
		return fmt.Errorf("%w: StatefulSet response was empty", ErrDeploymentRolloutFailed)
	}
	desiredReplicas := int32(1)
	if applied.Spec.Replicas != nil {
		desiredReplicas = *applied.Spec.Replicas
	}
	targetGeneration := applied.Generation
	return p.waitForWorkloadRollout(ctx, "StatefulSet", applied.Namespace, applied.Name, log, func(requestContext context.Context) (bool, error, string) {
		current, err := client.AppsV1().StatefulSets(applied.Namespace).Get(requestContext, applied.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, ErrDeploymentNotFound, "StatefulSet 不存在"
		}
		if err != nil {
			return false, err, "读取 StatefulSet 状态失败"
		}
		summary := fmt.Sprintf("期望副本 %d，当前副本 %d，就绪 %d，已更新 %d，已观察版本 %d/%d", desiredReplicas, current.Status.Replicas, current.Status.ReadyReplicas, current.Status.UpdatedReplicas, current.Status.ObservedGeneration, targetGeneration)
		ready := (targetGeneration == 0 || current.Status.ObservedGeneration >= targetGeneration) && current.Status.Replicas >= desiredReplicas && current.Status.ReadyReplicas >= desiredReplicas && current.Status.UpdatedReplicas >= desiredReplicas
		return ready, nil, summary
	})
}

func (p *KubernetesProvider) waitForDaemonSetRollout(ctx context.Context, client kubernetes.Interface, applied *appsv1.DaemonSet, log ReleaseLogFunc) error {
	if applied == nil {
		return fmt.Errorf("%w: DaemonSet response was empty", ErrDeploymentRolloutFailed)
	}
	targetGeneration := applied.Generation
	return p.waitForWorkloadRollout(ctx, "DaemonSet", applied.Namespace, applied.Name, log, func(requestContext context.Context) (bool, error, string) {
		current, err := client.AppsV1().DaemonSets(applied.Namespace).Get(requestContext, applied.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, ErrDeploymentNotFound, "DaemonSet 不存在"
		}
		if err != nil {
			return false, err, "读取 DaemonSet 状态失败"
		}
		summary := fmt.Sprintf("期望节点 %d，当前节点 %d，就绪 %d，可用 %d，已更新 %d，已观察版本 %d/%d", current.Status.DesiredNumberScheduled, current.Status.CurrentNumberScheduled, current.Status.NumberReady, current.Status.NumberAvailable, current.Status.UpdatedNumberScheduled, current.Status.ObservedGeneration, targetGeneration)
		ready := (targetGeneration == 0 || current.Status.ObservedGeneration >= targetGeneration) && current.Status.CurrentNumberScheduled == current.Status.DesiredNumberScheduled && current.Status.UpdatedNumberScheduled == current.Status.DesiredNumberScheduled && current.Status.NumberReady == current.Status.DesiredNumberScheduled && current.Status.NumberUnavailable == 0
		return ready, nil, summary
	})
}

func (p *KubernetesProvider) waitForJobCompletion(ctx context.Context, client kubernetes.Interface, applied *batchv1.Job, log ReleaseLogFunc) error {
	if applied == nil {
		return fmt.Errorf("%w: Job response was empty", ErrDeploymentRolloutFailed)
	}
	return p.waitForWorkloadRollout(ctx, "Job", applied.Namespace, applied.Name, log, func(requestContext context.Context) (bool, error, string) {
		current, err := client.BatchV1().Jobs(applied.Namespace).Get(requestContext, applied.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, ErrDeploymentNotFound, "Job 不存在"
		}
		if err != nil {
			return false, err, "读取 Job 状态失败"
		}
		for _, condition := range current.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				continue
			}
			if condition.Type == batchv1.JobFailed {
				message := strings.Join(strings.Fields(condition.Message), " ")
				if message == "" {
					message = string(condition.Type)
				}
				return false, fmt.Errorf("%w: Job %s/%s failed: %s", ErrDeploymentRolloutFailed, current.Namespace, current.Name, message), message
			}
			if condition.Type == batchv1.JobComplete {
				return true, nil, fmt.Sprintf("已完成 %d 个任务，失败 %d 个任务", current.Status.Succeeded, current.Status.Failed)
			}
		}
		return false, nil, fmt.Sprintf("运行中 %d 个任务，已完成 %d 个任务，失败 %d 个任务", current.Status.Active, current.Status.Succeeded, current.Status.Failed)
	})
}

type workloadRolloutCheck func(context.Context) (ready bool, failure error, summary string)

func (p *KubernetesProvider) waitForWorkloadRollout(ctx context.Context, kind, namespace, name string, log ReleaseLogFunc, check workloadRolloutCheck) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := p.rolloutTimeout
	if timeout <= 0 {
		timeout = defaultRolloutTimeout
	}
	pollInterval := p.rolloutPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultRolloutPollInterval
	}
	rolloutContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lastSummary := "尚未读取状态"
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		requestContext, requestCancel := p.requestContext(rolloutContext)
		ready, failure, summary := check(requestContext)
		requestCancel()
		lastSummary = summary
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("%s/%s: %s", strings.ToLower(kind), name, summary))
		if failure != nil {
			return failure
		}
		if ready {
			return nil
		}
		select {
		case <-rolloutContext.Done():
			if errors.Is(rolloutContext.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%w: %s %s/%s 在 %s 内未就绪（%s）", ErrDeploymentRolloutTimeout, kind, namespace, name, timeout, lastSummary)
			}
			return rolloutContext.Err()
		case <-ticker.C:
		}
	}
}

func (p *KubernetesProvider) imagePullFailure(ctx context.Context, client kubernetes.Interface, deployment *appsv1.Deployment) (error, string) {
	if deployment == nil || deployment.Spec.Selector == nil {
		return nil, ""
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, ""
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	pods, err := client.CoreV1().Pods(deployment.Namespace).List(requestContext, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, ""
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Status.InitContainerStatuses {
			if failure := waitingImagePullFailure(pod.Name, container.Name, container.State.Waiting); failure != "" {
				return fmt.Errorf("%w: %s", ErrDeploymentRolloutFailed, failure), failure
			}
		}
		for _, container := range pod.Status.ContainerStatuses {
			if failure := waitingImagePullFailure(pod.Name, container.Name, container.State.Waiting); failure != "" {
				return fmt.Errorf("%w: %s", ErrDeploymentRolloutFailed, failure), failure
			}
		}
	}
	return nil, ""
}

func waitingImagePullFailure(podName, containerName string, waiting *corev1.ContainerStateWaiting) string {
	if waiting == nil {
		return ""
	}
	reason := strings.TrimSpace(waiting.Reason)
	if reason != "ErrImagePull" && reason != "ImagePullBackOff" && reason != "InvalidImageName" && reason != "RegistryUnavailable" {
		return ""
	}
	detail := strings.Join(strings.Fields(waiting.Message), " ")
	if detail == "" {
		detail = reason
	}
	return fmt.Sprintf("Pod/%s container=%s 镜像拉取失败（%s）：%s；请检查目标集群 ServiceAccount/imagePullSecrets 对镜像仓库的权限", podName, containerName, reason, detail)
}

func (p *KubernetesProvider) getDeploymentForRollout(ctx context.Context, client kubernetes.Interface, namespace, name string) (*appsv1.Deployment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	deployment, err := client.AppsV1().Deployments(namespace).Get(requestContext, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, ErrDeploymentNotFound
	}
	return deployment, err
}

func deploymentRolloutState(deployment *appsv1.Deployment, targetGeneration int64, desiredReplicas int32) (bool, error, string) {
	if deployment == nil {
		return false, fmt.Errorf("%w: Deployment 状态为空", ErrDeploymentRolloutFailed), "状态为空"
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			return false, deploymentConditionError(deployment, condition), deploymentRolloutSummary(deployment, targetGeneration, desiredReplicas)
		}
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse {
			return false, deploymentConditionError(deployment, condition), deploymentRolloutSummary(deployment, targetGeneration, desiredReplicas)
		}
	}

	summary := deploymentRolloutSummary(deployment, targetGeneration, desiredReplicas)
	if targetGeneration > 0 && deployment.Status.ObservedGeneration < targetGeneration {
		return false, nil, summary
	}
	if deployment.Status.Replicas != desiredReplicas ||
		deployment.Status.UpdatedReplicas < desiredReplicas ||
		deployment.Status.ReadyReplicas < desiredReplicas ||
		deployment.Status.AvailableReplicas < desiredReplicas {
		return false, nil, summary
	}
	return true, nil, summary
}

func deploymentRolloutSummary(deployment *appsv1.Deployment, targetGeneration int64, desiredReplicas int32) string {
	return fmt.Sprintf("期望副本 %d，当前副本 %d，已更新 %d，就绪 %d，可用 %d，已观察版本 %d/%d",
		desiredReplicas,
		deployment.Status.Replicas,
		deployment.Status.UpdatedReplicas,
		deployment.Status.ReadyReplicas,
		deployment.Status.AvailableReplicas,
		deployment.Status.ObservedGeneration,
		targetGeneration,
	)
}

func deploymentConditionError(deployment *appsv1.Deployment, condition appsv1.DeploymentCondition) error {
	reason := strings.TrimSpace(condition.Reason)
	message := strings.Join(strings.Fields(condition.Message), " ")
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	detail := reason
	if message != "" {
		if detail != "" {
			detail += ": "
		}
		detail += message
	}
	if detail == "" {
		detail = "Deployment 控制器报告了失败"
	}
	return fmt.Errorf("%w: deployment %s/%s: %s", ErrDeploymentRolloutFailed, deployment.Namespace, deployment.Name, detail)
}
