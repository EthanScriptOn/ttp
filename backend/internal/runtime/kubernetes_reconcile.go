package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type releaseObjectKey struct {
	kind string
	name string
}

// reconcileReleaseResources removes resources that belonged to an older
// release of the same project target after the new workloads are healthy.
// PVCs and controller-created objects are deliberately outside this pruning
// boundary.
func (p *KubernetesProvider) reconcileReleaseResources(
	ctx context.Context,
	client kubernetes.Interface,
	deployment ReleaseDeployment,
	resources []releaseResource,
	imagePullSecret *corev1.Secret,
	log ReleaseLogFunc,
) error {
	if strings.TrimSpace(deployment.TargetID) == "" {
		return nil
	}

	desired := make(map[releaseObjectKey]struct{}, len(resources)+1)
	for _, resource := range resources {
		if key, ok := releaseResourceObjectKey(resource); ok {
			desired[key] = struct{}{}
		}
	}
	if imagePullSecret != nil && strings.TrimSpace(imagePullSecret.GetName()) != "" {
		desired[releaseObjectKey{kind: "Secret", name: imagePullSecret.GetName()}] = struct{}{}
	}
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("reconcile desired resources=%d", len(desired)))

	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	stale, err := p.staleReleaseResources(requestContext, client, deployment, desired)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		releaseLog(log, "k8s", "stdout", "INFO", "stale resources removed=0")
		return nil
	}

	sort.SliceStable(stale, func(i, j int) bool {
		return reconcileKindRank(stale[i].kind) < reconcileKindRank(stale[j].kind)
	})
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: propagationPolicy(metav1.DeletePropagationForeground)}
	for _, object := range stale {
		releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("delete stale %s/%s namespace=%s", object.kind, object.name, deployment.Namespace))
		if err := object.delete(deleteOptions); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: delete stale %s/%s: %v", ErrEnvironmentCleanupFailed, object.kind, object.name, err)
		}
	}
	cancel()
	if err := p.waitForStaleReleaseResources(ctx, client, deployment, stale); err != nil {
		return err
	}
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("stale resources removed=%d", len(stale)))
	return nil
}

func (p *KubernetesProvider) staleReleaseResources(
	ctx context.Context,
	client kubernetes.Interface,
	deployment ReleaseDeployment,
	desired map[releaseObjectKey]struct{},
) ([]environmentObject, error) {
	stale := make([]environmentObject, 0)
	for _, collection := range p.environmentCollections(ctx, client, deployment.Namespace, deployment.ProjectID) {
		objects, err := collection.list()
		if err != nil {
			return nil, fmt.Errorf("%w: list %s resources for release reconciliation: %v", ErrEnvironmentCleanupFailed, collection.kind, err)
		}
		for _, object := range objects {
			if !isPrunableReleaseObject(object, deployment.TargetID) {
				continue
			}
			if _, ok := desired[releaseObjectKey{kind: object.kind, name: object.name}]; ok {
				continue
			}
			stale = append(stale, object)
		}
	}
	return stale, nil
}

func isPrunableReleaseObject(object environmentObject, targetID string) bool {
	if object.kind == "Pod" || object.kind == "ReplicaSet" || object.kind == "PersistentVolumeClaim" {
		return false
	}
	return object.labels[targetLabelKey] == targetID &&
		object.labels[managedByLabelKey] == managedByLabelValue &&
		strings.TrimSpace(object.labels[releaseLabelKey]) != ""
}

func (p *KubernetesProvider) waitForStaleReleaseResources(
	ctx context.Context,
	client kubernetes.Interface,
	deployment ReleaseDeployment,
	stale []environmentObject,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := p.rolloutTimeout
	if timeout <= 0 {
		timeout = defaultRolloutTimeout
	}
	reconcileContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	waiting := make(map[releaseObjectKey]string, len(stale))
	for _, object := range stale {
		waiting[releaseObjectKey{kind: object.kind, name: object.name}] = object.labels[releaseLabelKey]
	}
	interval := p.rolloutPollInterval
	if interval <= 0 || interval > time.Second {
		interval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		remaining := 0
		requestContext, requestCancel := p.requestContext(reconcileContext)
		for _, collection := range p.environmentCollections(requestContext, client, deployment.Namespace, deployment.ProjectID) {
			objects, err := collection.list()
			if err != nil {
				requestCancel()
				return fmt.Errorf("%w: verify stale %s cleanup: %v", ErrEnvironmentCleanupFailed, collection.kind, err)
			}
			for _, object := range objects {
				key := releaseObjectKey{kind: object.kind, name: object.name}
				if releaseID, ok := waiting[key]; ok && object.labels[releaseLabelKey] == releaseID {
					remaining++
				}
			}
		}
		requestCancel()
		if remaining == 0 {
			return nil
		}
		select {
		case <-reconcileContext.Done():
			return fmt.Errorf("%w: wait for %d stale resources: %v", ErrEnvironmentCleanupFailed, remaining, reconcileContext.Err())
		case <-ticker.C:
		}
	}
}

func releaseResourceObjectKey(resource releaseResource) (releaseObjectKey, bool) {
	switch resource.kind {
	case "Deployment":
		return objectKey(resource.kind, resource.deployment)
	case "StatefulSet":
		return objectKey(resource.kind, resource.statefulSet)
	case "DaemonSet":
		return objectKey(resource.kind, resource.daemonSet)
	case "Job":
		return objectKey(resource.kind, resource.job)
	case "CronJob":
		return objectKey(resource.kind, resource.cronJob)
	case "Service":
		return objectKey(resource.kind, resource.service)
	case "ConfigMap":
		return objectKey(resource.kind, resource.configMap)
	case "Secret":
		return objectKey(resource.kind, resource.secret)
	case "PersistentVolumeClaim":
		return objectKey(resource.kind, resource.pvc)
	case "Ingress":
		return objectKey(resource.kind, resource.ingress)
	case "HorizontalPodAutoscaler":
		return objectKey(resource.kind, resource.hpa)
	default:
		return releaseObjectKey{}, false
	}
}

func objectKey(kind string, object interface{ GetName() string }) (releaseObjectKey, bool) {
	if object == nil || strings.TrimSpace(object.GetName()) == "" {
		return releaseObjectKey{}, false
	}
	return releaseObjectKey{kind: kind, name: object.GetName()}, true
}

func reconcileKindRank(kind string) int {
	switch kind {
	case "HorizontalPodAutoscaler", "Ingress", "Service", "ConfigMap", "Secret", "CronJob", "Job":
		return 1
	case "Deployment", "StatefulSet", "DaemonSet":
		return 2
	default:
		return 3
	}
}
