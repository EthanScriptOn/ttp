package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestPrometheusResourcesInstallCompleteCollectionChain(t *testing.T) {
	resources := prometheusResources(7)
	if len(resources) == 0 {
		t.Fatal("expected monitoring resources")
	}

	seen := map[string]bool{}
	config := ""
	for _, item := range resources {
		if item.GetAPIVersion() == "apiregistration.k8s.io/v1" {
			t.Fatalf("legacy API aggregation resource was included: %s/%s", item.GetKind(), item.GetName())
		}
		key := item.GetKind() + "/" + item.GetName()
		seen[key] = true
		if item.GetKind() == "ConfigMap" && item.GetName() == prometheusName {
			config, _, _ = mapValue(item.Object, "data", "prometheus.yml")
		}
		if item.GetKind() == "Deployment" && item.GetName() == prometheusName {
			strategy, _, _ := mapValue(item.Object, "spec", "strategy", "type")
			if strategy != "Recreate" {
				t.Fatalf("prometheus must use Recreate strategy with its single-writer TSDB volume, got %q", strategy)
			}
		}
		if resourceForKind(item.GetKind()).Resource == "" {
			t.Fatalf("resource kind %q is not supported by the installer", item.GetKind())
		}
	}

	for _, expected := range []string{
		"Namespace/" + prometheusNamespace,
		"Deployment/" + prometheusName,
		"Deployment/" + kubeStateMetricsName,
		"DaemonSet/" + nodeExporterName,
		"ClusterRole/ttp-prometheus",
		"ClusterRole/ttp-kube-state-metrics",
	} {
		if !seen[expected] {
			t.Fatalf("missing monitoring resource %s", expected)
		}
	}
	for _, expected := range []string{"job_name: node-exporter", "job_name: kube-state-metrics", "job_name: kubernetes-nodes-cadvisor", "job_name: kubernetes-pods"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("Prometheus scrape config is missing %q", expected)
		}
	}
	var parsedConfig map[string]any
	if err := yaml.Unmarshal([]byte(config), &parsedConfig); err != nil {
		t.Fatalf("invalid Prometheus scrape config: %v", err)
	}
}

func TestMonitoringWaitingState(t *testing.T) {
	for reason, expected := range map[string]string{
		"ContainerCreating": "installing",
		"PodInitializing":   "installing",
		"ImagePullBackOff":  "failed",
		"CrashLoopBackOff":  "failed",
	} {
		if actual := monitoringWaitingState(reason); actual != expected {
			t.Fatalf("waiting reason %q: expected %q, got %q", reason, expected, actual)
		}
	}
}

func TestMonitoringImagesUseDomesticSources(t *testing.T) {
	allowedHosts := map[string]bool{
		"m.daocloud.io":                    true,
		"docker.1panel.live":               true,
		"swr.cn-north-4.myhuaweicloud.com": true,
		"quay.nju.edu.cn":                  true,
		"k8s.nju.edu.cn":                   true,
	}
	for _, component := range []string{prometheusName, nodeExporterName, kubeStateMetricsName} {
		sources := monitoringImageSources[component]
		if len(sources) < 3 {
			t.Fatalf("%s should have at least three image sources, got %d", component, len(sources))
		}
		for _, source := range sources {
			host := strings.SplitN(source.Image, "/", 2)[0]
			if !allowedHosts[host] {
				t.Fatalf("%s source %q does not use an approved domestic registry", component, source.Image)
			}
		}
	}

	for _, resource := range prometheusResources(7) {
		if resource.GetKind() != "Deployment" && resource.GetKind() != "DaemonSet" {
			continue
		}
		component := resource.GetName()
		if image := monitoringContainerImage(resource); image != primaryMonitoringImage(component) {
			t.Fatalf("%s primary image = %q, want %q", component, image, primaryMonitoringImage(component))
		}
		if resource.GetAnnotations()[monitoringImageSourceIndexAnnotation] != "0" {
			t.Fatalf("%s does not start from the first image source", component)
		}
	}
}

func TestMonitoringImageDecisionRetriesThenRotates(t *testing.T) {
	decision := nextMonitoringImageDecision(0, 0, 3)
	if decision.Rotate || decision.Exhausted || decision.RetryCount != 1 {
		t.Fatalf("first pull failure should retry the same source: %#v", decision)
	}
	decision = nextMonitoringImageDecision(0, 2, 3)
	if !decision.Rotate || decision.SourceIndex != 1 || decision.RetryCount != 0 {
		t.Fatalf("third pull failure should rotate to the next source: %#v", decision)
	}
	decision = nextMonitoringImageDecision(2, 2, 3)
	if !decision.Exhausted || decision.SourceIndex != 2 || decision.RetryCount != 3 {
		t.Fatalf("last source should report exhaustion after its retry budget: %#v", decision)
	}
}

func TestMonitoringImagePullReconcileRotatesWorkload(t *testing.T) {
	workload := object(map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      prometheusName,
			"namespace": prometheusNamespace,
			"annotations": map[string]interface{}{
				monitoringImageSourceIndexAnnotation: "0",
				monitoringImageRetryCountAnnotation:  "2",
			},
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{},
				"spec": map[string]interface{}{
					"containers": []interface{}{map[string]interface{}{
						"name":  prometheusName,
						"image": primaryMonitoringImage(prometheusName),
					}},
				},
			},
		},
	})
	client := dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), workload)
	progress, message, err := reconcileMonitoringImagePull(context.Background(), client, appsDeploymentGVR, prometheusName)
	if err != nil {
		t.Fatal(err)
	}
	if progress.SourceIndex != 2 || !strings.Contains(message, "已切换") {
		t.Fatalf("unexpected rotation result: %#v, %q", progress, message)
	}
	updated, err := client.Resource(appsDeploymentGVR).Namespace(prometheusNamespace).Get(context.Background(), prometheusName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if image := monitoringContainerImage(updated); image != monitoringImageSources[prometheusName][1].Image {
		t.Fatalf("rotated image = %q, want %q", image, monitoringImageSources[prometheusName][1].Image)
	}
	if updated.GetAnnotations()[monitoringImageRetryCountAnnotation] != "0" {
		t.Fatalf("retry count was not reset after rotation: %#v", updated.GetAnnotations())
	}
}

func TestMonitoringPodStatusPrefersPullFailureAcrossPods(t *testing.T) {
	pod := func(name, reason string) *unstructured.Unstructured {
		return object(map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": prometheusNamespace,
				"labels":    map[string]interface{}{"app.kubernetes.io/name": nodeExporterName},
			},
			"status": map[string]interface{}{
				"containerStatuses": []interface{}{map[string]interface{}{
					"name":  nodeExporterName,
					"state": map[string]interface{}{"waiting": map[string]interface{}{"reason": reason}},
				}},
			},
		})
	}
	client := dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(),
		pod("creating", "ContainerCreating"),
		pod("pull-failed", "ImagePullBackOff"),
	)
	state, message, pullFailed := monitoringPodStatus(context.Background(), client, nodeExporterName)
	if state != "failed" || !pullFailed || !strings.Contains(message, "ImagePullBackOff") {
		t.Fatalf("unexpected pod status: state=%q pullFailed=%v message=%q", state, pullFailed, message)
	}
}

func TestMonitoringDependenciesUsePrometheusOnly(t *testing.T) {
	status := MonitoringStatus{
		Component:      prometheusName,
		DisplayName:    "Prometheus",
		InstallVersion: prometheusVersion,
		Dependencies: []MonitoringDependency{
			{Component: prometheusName},
			{Component: nodeExporterName},
			{Component: kubeStateMetricsName},
		},
	}
	if status.Component != prometheusName || len(status.Dependencies) != 3 {
		t.Fatalf("unexpected monitoring contract: %#v", status)
	}
	for _, dependency := range status.Dependencies {
		if dependency.Component != prometheusName && dependency.Component != nodeExporterName && dependency.Component != kubeStateMetricsName {
			t.Fatalf("unexpected monitoring dependency: %#v", dependency)
		}
	}
}

func TestNodeMetricsReadsPrometheus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/nodes":
			writeKubernetesJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "NodeList",
				"items": []any{map[string]any{
					"metadata": map[string]any{"name": "node-a"},
					"status":   map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
				}},
			})
		case "/api/v1/namespaces/ttp-monitoring/services/http:prometheus:9090/proxy/api/v1/query":
			value := "42"
			if strings.Contains(r.URL.Query().Get("query"), "MemAvailable") {
				value = "58"
			}
			writeKubernetesJSON(t, w, map[string]any{
				"status": "success",
				"data": map[string]any{
					"result": []any{map[string]any{
						"metric": map[string]any{"kubernetes_node": "node-a"},
						"value":  []any{float64(1700000000), value},
					}},
				},
			})
		default:
			if strings.HasPrefix(r.URL.Path, "/apis/metrics") {
				t.Errorf("unexpected legacy metrics API request: %s", r.URL.Path)
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: current
clusters:
- name: cluster
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: current
  context:
    cluster: cluster
users:
- name: test
  user: {}
`, server.URL)
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0600); err != nil {
		t.Fatal(err)
	}

	provider := NewKubernetesProvider()
	if err := provider.RegisterKubeconfig("cluster", path); err != nil {
		t.Fatal(err)
	}
	client, err := provider.clientFor("cluster")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := provider.nodeMetrics(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].Name != "node-a" || metrics[0].CPUUsedPercent != 42 || metrics[0].MemoryUsedPercent != 58 || !metrics[0].Ready {
		t.Fatalf("unexpected Prometheus node metrics: %#v", metrics)
	}
}

func mapValue(object map[string]interface{}, path ...string) (string, bool, error) {
	value := interface{}(object)
	for _, key := range path {
		typed, ok := value.(map[string]interface{})
		if !ok {
			return "", false, nil
		}
		value, ok = typed[key]
		if !ok {
			return "", false, nil
		}
	}
	text, ok := value.(string)
	return text, ok, nil
}
