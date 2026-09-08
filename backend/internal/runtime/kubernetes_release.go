package runtime

import (
	"context"
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
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("client-go cluster=%s namespace=%s", deployment.ClusterID, deployment.Namespace))
	client, err := p.clientFor(deployment.ClusterID)
	if err != nil {
		return err
	}
	if err := validateManifestFormat(deployment); err != nil {
		return err
	}
	if _, err := deploymentconfig.Validate(deployment.Manifest, deployment.Namespace); err != nil {
		return err
	}
	objects, err := decodeReleaseObjects(deployment.Manifest)
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
	appliedDeployments := make([]*appsv1.Deployment, 0, 1)
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
		return nil
	}
	for _, applied := range appliedDeployments {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("deployment/%s rollout status", applied.Name))
		if err := p.waitForDeploymentRollout(ctx, client, applied, log); err != nil {
			return err
		}
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("deployment/%s successfully rolled out", applied.Name))
	}
	return nil
}

func releaseLog(log ReleaseLogFunc, source, stream, level, line string) {
	if log != nil && strings.TrimSpace(line) != "" {
		log(source, stream, level, line)
	}
}

type releaseResource struct {
	kind       string
	deployment *appsv1.Deployment
	service    *corev1.Service
	configMap  *corev1.ConfigMap
	secret     *corev1.Secret
	ingress    *networkingv1.Ingress
	hpa        *autoscalingv2.HorizontalPodAutoscaler
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
	if deployment.Replicas <= 0 || deployment.Replicas > 100 {
		return fmt.Errorf("%w: replicas must be between 1 and 100", ErrInvalidKubernetesInput)
	}
	if strings.TrimSpace(deployment.Strategy) != "" && strings.TrimSpace(deployment.Strategy) != "rolling" {
		return fmt.Errorf("%w: kubernetes provider supports only rolling release strategy", ErrInvalidKubernetesInput)
	}
	if strings.TrimSpace(deployment.Manifest) == "" {
		return fmt.Errorf("%w: release manifest is required", ErrInvalidKubernetesInput)
	}
	return nil
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
	hasDeployment := false
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
			hasDeployment = true
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
	if !hasDeployment {
		return nil, fmt.Errorf("%w: release manifest must contain a Deployment", deploymentconfig.ErrInvalidManifest)
	}
	return resources, nil
}

func (p *KubernetesProvider) prepareReleaseDeployment(deployment *appsv1.Deployment, release ReleaseDeployment) error {
	if deployment.Name == "" {
		return errors.New("deployment name is required")
	}
	replicas := int32(release.Replicas)
	deployment.Spec.Replicas = &replicas
	p.prepareReleaseObjectMeta(&deployment.ObjectMeta, release)
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = make(map[string]string)
	}
	deployment.Labels[p.projectLabelKey] = release.ProjectID
	deployment.Spec.Template.Labels[p.projectLabelKey] = release.ProjectID
	deployment.Labels[releaseLabelKey] = release.ReleaseID
	deployment.Spec.Template.Labels[releaseLabelKey] = release.ReleaseID
	if release.TargetID != "" {
		deployment.Spec.Template.Labels[targetLabelKey] = release.TargetID
	}
	version := release.CommitSHA
	if len(version) > 10 {
		version = version[:10]
	}
	if validLabelValue(version) {
		deployment.Spec.Template.Labels[releaseVersionLabel] = version
	}
	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Annotations[releaseBranchAnnotKey] = release.Branch
	deployment.Spec.Template.Annotations[releaseBranchAnnotKey] = release.Branch
	deployment.Spec.Template.Annotations[rolloutVersionKey] = p.now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	containerIndex, err := p.targetContainerIndex(deployment.Spec.Template.Spec.Containers)
	if err != nil {
		return err
	}
	container := &deployment.Spec.Template.Spec.Containers[containerIndex]
	container.Image = release.Image
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
