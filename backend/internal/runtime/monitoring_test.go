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
