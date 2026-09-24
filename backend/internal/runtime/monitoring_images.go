package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

const (
	monitoringImageSourceIndexAnnotation = "cicd.yuebuy.com/monitoring-image-source-index"
	monitoringImageRetryCountAnnotation  = "cicd.yuebuy.com/monitoring-image-retry-count"
	monitoringImageLastFailureAnnotation = "cicd.yuebuy.com/monitoring-image-last-failure"
	monitoringImageRetryLimit            = 3
	monitoringImageRetryInterval         = 15 * time.Second
)

type monitoringImageSource struct {
	Name  string
	Image string
}

// These images are installed and maintained by TTP itself. User workload
// images always keep the registry configured by the project.
var monitoringImageSources = map[string][]monitoringImageSource{
	prometheusName: {
		{Name: "DaoCloud", Image: "m.daocloud.io/docker.io/prom/prometheus:" + prometheusVersion},
		{Name: "1Panel", Image: "docker.1panel.live/prom/prometheus:" + prometheusVersion},
		{Name: "华为云 DDN", Image: "swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/prom/prometheus:" + prometheusVersion},
	},
	nodeExporterName: {
		{Name: "DaoCloud", Image: "m.daocloud.io/quay.io/prometheus/node-exporter:" + nodeExporterVersion},
		{Name: "南京大学镜像站", Image: "quay.nju.edu.cn/prometheus/node-exporter:" + nodeExporterVersion},
		{Name: "华为云 DDN", Image: "swr.cn-north-4.myhuaweicloud.com/ddn-k8s/quay.io/prometheus/node-exporter:" + nodeExporterVersion},
	},
	kubeStateMetricsName: {
		{Name: "DaoCloud", Image: "m.daocloud.io/registry.k8s.io/kube-state-metrics/kube-state-metrics:" + kubeStateMetricsVersion},
		{Name: "南京大学镜像站", Image: "k8s.nju.edu.cn/kube-state-metrics/kube-state-metrics:" + kubeStateMetricsVersion},
		{Name: "华为云 DDN", Image: "swr.cn-north-4.myhuaweicloud.com/ddn-k8s/registry.k8s.io/kube-state-metrics/kube-state-metrics:" + kubeStateMetricsVersion},
	},
}

type monitoringImageProgress struct {
	SourceName  string
	SourceIndex int
	SourceCount int
	RetryCount  int
	RetryLimit  int
}

func primaryMonitoringImage(component string) string {
	sources := monitoringImageSources[component]
	if len(sources) == 0 {
		return ""
	}
	return sources[0].Image
}

func monitoringImageAnnotations() map[string]interface{} {
	return map[string]interface{}{
		monitoringImageSourceIndexAnnotation: "0",
		monitoringImageRetryCountAnnotation:  "0",
	}
}

func monitoringPodImageAnnotations(component string) map[string]interface{} {
	sources := monitoringImageSources[component]
	if len(sources) == 0 {
		return map[string]interface{}{}
	}
	return map[string]interface{}{"cicd.yuebuy.com/monitoring-image-source": sources[0].Name}
}

func applyMonitoringImageProgress(dependency *MonitoringDependency, progress monitoringImageProgress) {
	dependency.ImageSource = progress.SourceName
	dependency.SourceIndex = progress.SourceIndex
	dependency.SourceCount = progress.SourceCount
	dependency.RetryCount = progress.RetryCount
	dependency.RetryLimit = progress.RetryLimit
}

func installingMonitoringDependency(component, displayName, version string) MonitoringDependency {
	dependency := MonitoringDependency{
		Component:      component,
		DisplayName:    displayName,
		State:          "installing",
		Installable:    true,
		Installed:      true,
		InstallVersion: version,
		Message:        "等待 Pod 就绪",
	}
	sources := monitoringImageSources[component]
	progress := monitoringImageProgress{SourceIndex: 1, SourceCount: len(sources), RetryLimit: monitoringImageRetryLimit}
	if len(sources) > 0 {
		progress.SourceName = sources[0].Name
	}
	applyMonitoringImageProgress(&dependency, progress)
	return dependency
}

func monitoringImageProgressFor(workload *unstructured.Unstructured, component string) monitoringImageProgress {
	sources := monitoringImageSources[component]
	index := monitoringImageSourceIndex(workload, sources)
	retries := annotationInt(workload, monitoringImageRetryCountAnnotation)
	progress := monitoringImageProgress{
		SourceIndex: index + 1,
		SourceCount: len(sources),
		RetryCount:  retries,
		RetryLimit:  monitoringImageRetryLimit,
	}
	if index >= 0 && index < len(sources) {
		progress.SourceName = sources[index].Name
	}
	return progress
}

func monitoringImageSourceIndex(workload *unstructured.Unstructured, sources []monitoringImageSource) int {
	if len(sources) == 0 {
		return -1
	}
	if index, found := annotationIntFound(workload, monitoringImageSourceIndexAnnotation); found && index < len(sources) {
		return index
	}
	image := monitoringContainerImage(workload)
	for index, source := range sources {
		if image == source.Image {
			return index
		}
	}
	return -1
}

func annotationInt(workload *unstructured.Unstructured, key string) int {
	number, _ := annotationIntFound(workload, key)
	return number
}

func annotationIntFound(workload *unstructured.Unstructured, key string) (int, bool) {
	value, found := workload.GetAnnotations()[key]
	if !found {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}

func monitoringContainerImage(workload *unstructured.Unstructured) string {
	containers, _, _ := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
	for _, raw := range containers {
		container, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		image, _, _ := unstructured.NestedString(container, "image")
		if image != "" {
			return image
		}
	}
	return ""
}

type monitoringImageDecision struct {
	SourceIndex int
	RetryCount  int
	Rotate      bool
	Exhausted   bool
}

func nextMonitoringImageDecision(currentIndex, retryCount, sourceCount int) monitoringImageDecision {
	if sourceCount == 0 {
		return monitoringImageDecision{Exhausted: true}
	}
	if currentIndex < 0 || currentIndex >= sourceCount {
		return monitoringImageDecision{SourceIndex: 0, Rotate: true}
	}
	retryCount++
	if retryCount < monitoringImageRetryLimit {
		return monitoringImageDecision{SourceIndex: currentIndex, RetryCount: retryCount}
	}
	if currentIndex+1 >= sourceCount {
		return monitoringImageDecision{SourceIndex: currentIndex, RetryCount: retryCount, Exhausted: true}
	}
	return monitoringImageDecision{SourceIndex: currentIndex + 1, Rotate: true}
}

func reconcileMonitoringImagePull(ctx context.Context, client dynamic.Interface, resource schema.GroupVersionResource, component string) (monitoringImageProgress, string, error) {
	sources := monitoringImageSources[component]
	if len(sources) == 0 {
		return monitoringImageProgress{}, "", fmt.Errorf("no image sources configured for %s", component)
	}

	var progress monitoringImageProgress
	var message string
	now := time.Now().UTC()
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		workload, err := client.Resource(resource).Namespace(prometheusNamespace).Get(ctx, component, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if lastFailure, parseErr := time.Parse(time.RFC3339Nano, workload.GetAnnotations()[monitoringImageLastFailureAnnotation]); parseErr == nil && now.Sub(lastFailure) < monitoringImageRetryInterval {
			progress = monitoringImageProgressFor(workload, component)
			message = fmt.Sprintf("镜像拉取失败，等待 Kubernetes 使用 %s 重试", progress.SourceName)
			return nil
		}
		currentIndex := monitoringImageSourceIndex(workload, sources)
		decision := nextMonitoringImageDecision(currentIndex, annotationInt(workload, monitoringImageRetryCountAnnotation), len(sources))
		if decision.Exhausted {
			progress = monitoringImageProgressFor(workload, component)
			progress.RetryCount = decision.RetryCount
			message = fmt.Sprintf("%d 个国内镜像源均拉取失败，最后尝试：%s", len(sources), progress.SourceName)
			return nil
		}

		annotations := workload.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[monitoringImageSourceIndexAnnotation] = strconv.Itoa(decision.SourceIndex)
		annotations[monitoringImageRetryCountAnnotation] = strconv.Itoa(decision.RetryCount)
		annotations[monitoringImageLastFailureAnnotation] = now.Format(time.RFC3339Nano)
		workload.SetAnnotations(annotations)

		if decision.Rotate {
			if err := setMonitoringContainerImage(workload, component, sources[decision.SourceIndex].Image); err != nil {
				return err
			}
			if err := setMonitoringPodSourceAnnotation(workload, sources[decision.SourceIndex].Name); err != nil {
				return err
			}
		}
		if _, err := client.Resource(resource).Namespace(prometheusNamespace).Update(ctx, workload, metav1.UpdateOptions{}); err != nil {
			return err
		}

		progress = monitoringImageProgress{
			SourceName:  sources[decision.SourceIndex].Name,
			SourceIndex: decision.SourceIndex + 1,
			SourceCount: len(sources),
			RetryCount:  decision.RetryCount,
			RetryLimit:  monitoringImageRetryLimit,
		}
		if decision.Rotate {
			message = fmt.Sprintf("已切换至国内镜像源 %s（%d/%d），等待节点重新拉取", progress.SourceName, progress.SourceIndex, progress.SourceCount)
		} else {
			message = fmt.Sprintf("镜像拉取失败，%s 正在重试（%d/%d）", progress.SourceName, progress.RetryCount, progress.RetryLimit)
		}
		return nil
	})
	return progress, message, err
}

func setMonitoringContainerImage(workload *unstructured.Unstructured, component, image string) error {
	containers, found, err := unstructured.NestedSlice(workload.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return fmt.Errorf("monitoring workload %s has no containers", component)
	}
	for index, raw := range containers {
		container, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(container, "name")
		if name != component {
			continue
		}
		container["image"] = image
		containers[index] = container
		return unstructured.SetNestedSlice(workload.Object, containers, "spec", "template", "spec", "containers")
	}
	return fmt.Errorf("monitoring container %s was not found", component)
}

func setMonitoringPodSourceAnnotation(workload *unstructured.Unstructured, sourceName string) error {
	annotations, _, err := unstructured.NestedStringMap(workload.Object, "spec", "template", "metadata", "annotations")
	if err != nil {
		return err
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["cicd.yuebuy.com/monitoring-image-source"] = strings.TrimSpace(sourceName)
	return unstructured.SetNestedStringMap(workload.Object, annotations, "spec", "template", "metadata", "annotations")
}
