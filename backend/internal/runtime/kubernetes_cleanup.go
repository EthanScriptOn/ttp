package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// CleanupEnvironment deletes only resources carrying the project, target and
// TTP ownership labels. A resource from an older release without a target
// label blocks deletion instead of being silently orphaned.
func (p *KubernetesProvider) CleanupEnvironment(ctx context.Context, clusterID, namespace, projectID, targetID string) error {
	clusterID = strings.TrimSpace(clusterID)
	namespace = strings.TrimSpace(namespace)
	projectID = strings.TrimSpace(projectID)
	targetID = strings.TrimSpace(targetID)
	if err := validateClusterID(clusterID); err != nil {
		return err
	}
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	if err := validateProjectID(projectID); err != nil {
		return err
	}
	if targetID == "" || !validLabelValue(targetID) {
		return fmt.Errorf("%w: target ID is required and must be a valid Kubernetes label value", ErrInvalidKubernetesInput)
	}
	client, err := p.clientFor(clusterID)
	if err != nil {
		return err
	}

	requestContext, cancel := p.requestContext(ctx)
	defer cancel()
	collections := p.environmentCollections(requestContext, client, namespace, projectID)
	objects := make([]environmentObject, 0)
	for _, collection := range collections {
		items, listErr := collection.list()
		if listErr != nil {
			return fmt.Errorf("%w: list %s resources: %v", ErrEnvironmentCleanupFailed, collection.kind, listErr)
		}
		objects = append(objects, items...)
	}

	owned := make([]environmentObject, 0, len(objects))
	for _, object := range objects {
		if object.labels[targetLabelKey] == "" {
			return fmt.Errorf("%w: %s/%s 没有环境归属标签，请先重新发布后再删除环境", ErrEnvironmentCleanupFailed, object.kind, object.name)
		}
		if object.labels[targetLabelKey] != targetID {
			continue
		}
		if object.labels[managedByLabelKey] != managedByLabelValue {
			return fmt.Errorf("%w: %s/%s 不是 TTP 管理的资源", ErrEnvironmentCleanupFailed, object.kind, object.name)
		}
		owned = append(owned, object)
	}

	// Remove dependants first. Foreground deletion on the Deployment is still
	// used as a second line of defence for ReplicaSets and Pods created by it.
	sort.SliceStable(owned, func(i, j int) bool {
		return cleanupKindRank(owned[i].kind) < cleanupKindRank(owned[j].kind)
	})
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: propagationPolicy(metav1.DeletePropagationForeground)}
	for _, object := range owned {
		if err := object.delete(deleteOptions); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: delete %s/%s: %v", ErrEnvironmentCleanupFailed, object.kind, object.name, err)
		}
	}

	if len(owned) == 0 {
		return nil
	}
	return p.waitForEnvironmentCleanup(requestContext, client, namespace, projectID, targetID)
}

type environmentObject struct {
	kind   string
	name   string
	labels map[string]string
	delete func(metav1.DeleteOptions) error
}

type environmentCollection struct {
	kind string
	list func() ([]environmentObject, error)
}

func (p *KubernetesProvider) environmentCollections(ctx context.Context, client kubernetes.Interface, namespace, projectID string) []environmentCollection {
	selector := labels.Set{p.projectLabelKey: projectID}.AsSelector().String()
	return []environmentCollection{
		{
			kind: "Deployment",
			list: func() ([]environmentObject, error) {
				list, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "Deployment", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.AppsV1().Deployments(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "StatefulSet",
			list: func() ([]environmentObject, error) {
				list, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "StatefulSet", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.AppsV1().StatefulSets(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "DaemonSet",
			list: func() ([]environmentObject, error) {
				list, err := client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "DaemonSet", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.AppsV1().DaemonSets(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "Pod",
			list: func() ([]environmentObject, error) {
				list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "Pod", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.CoreV1().Pods(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "ReplicaSet",
			list: func() ([]environmentObject, error) {
				list, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "ReplicaSet", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.AppsV1().ReplicaSets(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "Job",
			list: func() ([]environmentObject, error) {
				list, err := client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "Job", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.BatchV1().Jobs(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "CronJob",
			list: func() ([]environmentObject, error) {
				list, err := client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "CronJob", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.BatchV1().CronJobs(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "HorizontalPodAutoscaler",
			list: func() ([]environmentObject, error) {
				list, err := client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "HorizontalPodAutoscaler", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "Ingress",
			list: func() ([]environmentObject, error) {
				list, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "Ingress", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.NetworkingV1().Ingresses(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "Service",
			list: func() ([]environmentObject, error) {
				list, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "Service", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.CoreV1().Services(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "ConfigMap",
			list: func() ([]environmentObject, error) {
				list, err := client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "ConfigMap", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.CoreV1().ConfigMaps(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "Secret",
			list: func() ([]environmentObject, error) {
				list, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "Secret", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.CoreV1().Secrets(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
		{
			kind: "PersistentVolumeClaim",
			list: func() ([]environmentObject, error) {
				list, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
				if err != nil {
					return nil, err
				}
				items := make([]environmentObject, 0, len(list.Items))
				for _, item := range list.Items {
					item := item
					items = append(items, environmentObject{kind: "PersistentVolumeClaim", name: item.Name, labels: item.Labels, delete: func(options metav1.DeleteOptions) error {
						return client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, item.Name, options)
					}})
				}
				return items, nil
			},
		},
	}
}

func cleanupKindRank(kind string) int {
	switch kind {
	case "Pod":
		return 1
	case "ReplicaSet":
		return 2
	case "CronJob":
		return 2
	case "Job":
		return 3
	case "HorizontalPodAutoscaler", "Ingress", "Service", "ConfigMap", "Secret":
		return 3
	case "Deployment", "StatefulSet", "DaemonSet":
		return 4
	case "PersistentVolumeClaim":
		return 5
	default:
		return 6
	}
}

func propagationPolicy(policy metav1.DeletionPropagation) *metav1.DeletionPropagation {
	return &policy
}

func (p *KubernetesProvider) waitForEnvironmentCleanup(ctx context.Context, client kubernetes.Interface, namespace, projectID, targetID string) error {
	interval := p.rolloutPollInterval
	if interval <= 0 || interval > 2*time.Second {
		interval = 200 * time.Millisecond
	}
	selector := labels.Set{p.projectLabelKey: projectID, targetLabelKey: targetID, managedByLabelKey: managedByLabelValue}.AsSelector().String()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return fmt.Errorf("%w: verify Pod cleanup: %v", ErrEnvironmentCleanupFailed, err)
		}
		if len(list.Items) == 0 {
			// Check the remaining resource kinds as well. A Deployment may be
			// gone while a Service or Secret is still terminating.
			remaining, err := p.countEnvironmentResources(ctx, client, namespace, selector)
			if err != nil {
				return err
			}
			if remaining == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *KubernetesProvider) countEnvironmentResources(ctx context.Context, client kubernetes.Interface, namespace, selector string) (int, error) {
	count := 0
	countList := func(kind string, length int, err error) error {
		if err != nil {
			return fmt.Errorf("%w: verify %s cleanup: %v", ErrEnvironmentCleanupFailed, kind, err)
		}
		count += length
		return nil
	}
	list, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("ReplicaSet", 0, err)
	}
	if err := countList("ReplicaSet", len(list.Items), nil); err != nil {
		return 0, err
	}
	listJobs, err := client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("Job", 0, err)
	}
	if err := countList("Job", len(listJobs.Items), nil); err != nil {
		return 0, err
	}
	listCronJobs, err := client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("CronJob", 0, err)
	}
	if err := countList("CronJob", len(listCronJobs.Items), nil); err != nil {
		return 0, err
	}
	listStatefulSets, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("StatefulSet", 0, err)
	}
	if err := countList("StatefulSet", len(listStatefulSets.Items), nil); err != nil {
		return 0, err
	}
	listDaemonSets, err := client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("DaemonSet", 0, err)
	}
	if err := countList("DaemonSet", len(listDaemonSets.Items), nil); err != nil {
		return 0, err
	}
	listHPA, err := client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("HorizontalPodAutoscaler", 0, err)
	}
	if err := countList("HorizontalPodAutoscaler", len(listHPA.Items), nil); err != nil {
		return 0, err
	}
	listIngress, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("Ingress", 0, err)
	}
	if err := countList("Ingress", len(listIngress.Items), nil); err != nil {
		return 0, err
	}
	listServices, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("Service", 0, err)
	}
	if err := countList("Service", len(listServices.Items), nil); err != nil {
		return 0, err
	}
	listConfigMaps, err := client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("ConfigMap", 0, err)
	}
	if err := countList("ConfigMap", len(listConfigMaps.Items), nil); err != nil {
		return 0, err
	}
	listSecrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("Secret", 0, err)
	}
	if err := countList("Secret", len(listSecrets.Items), nil); err != nil {
		return 0, err
	}
	listClaims, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("PersistentVolumeClaim", 0, err)
	}
	if err := countList("PersistentVolumeClaim", len(listClaims.Items), nil); err != nil {
		return 0, err
	}
	listDeployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return 0, countList("Deployment", 0, err)
	}
	if err := countList("Deployment", len(listDeployments.Items), nil); err != nil {
		return 0, err
	}
	return count, nil
}
