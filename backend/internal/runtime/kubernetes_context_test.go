package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterKubeconfigWithContextSelectsRequestedContext(t *testing.T) {
	selected := make(chan string, 2)
	servers := make([]*httptest.Server, 0, 2)
	for _, podName := range []string{"from-context-a", "from-context-b"} {
		name := podName
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			selected <- name
			if r.URL.Path == "/apis/metrics.k8s.io/v1beta1/namespaces/lab/pods" {
				writeKubernetesJSON(t, w, map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetricsList", "items": []any{}})
				return
			}
			if r.URL.Path != "/api/v1/namespaces/lab/pods" {
				t.Errorf("unexpected Kubernetes path: %s", r.URL.Path)
			}
			writeKubernetesJSON(t, w, map[string]any{"apiVersion": "v1", "kind": "PodList", "items": []map[string]any{{"metadata": map[string]any{"name": name, "namespace": "lab"}, "status": map[string]any{"phase": "Running"}}}})
		}))
		servers = append(servers, server)
	}
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: context-a
clusters:
- name: cluster-a
  cluster:
    server: %s
    insecure-skip-tls-verify: true
- name: cluster-b
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: context-a
  context:
    cluster: cluster-a
- name: context-b
  context:
    cluster: cluster-b
users:
- name: test
  user: {}
`, servers[0].URL, servers[1].URL)
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(kubeconfig), 0600); err != nil {
		t.Fatal(err)
	}

	provider := NewKubernetesProvider()
	if err := provider.RegisterKubeconfigWithContext("cluster", path, "context-b"); err != nil {
		t.Fatal(err)
	}
	pods, err := provider.ListPodsInNamespace(context.Background(), "cluster", "lab", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "from-context-b" {
		t.Fatalf("selected context returned pods: %#v", pods)
	}
	if got := <-selected; got != "from-context-b" {
		t.Fatalf("request reached %q, want context-b server", got)
	}
}

func TestRegisterKubeconfigKeepsCurrentContextCompatibility(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/apis/metrics.k8s.io/") {
			writeKubernetesJSON(t, w, map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetricsList", "items": []any{}})
			return
		}
		writeKubernetesJSON(t, w, map[string]any{"apiVersion": "v1", "kind": "PodList", "items": []any{}})
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
	if _, err := provider.ListPodsInNamespace(context.Background(), "cluster", "lab", ""); err != nil {
		t.Fatal(err)
	}
}

func writeKubernetesJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode Kubernetes response: %v", err)
	}
}
