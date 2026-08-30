package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func TestClusterManagementFlowKeepsConnectionMaterialPrivate(t *testing.T) {
	server := testServer()
	token := loginForTest(t, server.Router())

	created := doRequest(t, server.Router(), http.MethodPost, "/api/clusters", token, `{"name":"自定义测试集群","api_endpoint":"https://k8s.example.test:6443","connection_mode":"kubeconfig","kubeconfig_path":"/etc/cicd/test.config","kube_context":"test-context"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create cluster: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "/etc/cicd/test.config") {
		t.Fatal("create response leaked kubeconfig path")
	}
	var createBody struct {
		Cluster   store.Cluster `json:"cluster"`
		Connected bool          `json:"connected"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Cluster.ID == "" || !createBody.Connected || createBody.Cluster.APIEndpoint != "https://k8s.example.test:6443" {
		t.Fatalf("unexpected create response: %#v; raw=%s", createBody, created.Body.String())
	}

	read := doRequest(t, server.Router(), http.MethodGet, "/api/clusters/"+createBody.Cluster.ID, token, "")
	if read.Code != http.StatusOK || strings.Contains(read.Body.String(), "/etc/cicd/test.config") {
		t.Fatalf("read cluster: %d %s", read.Code, read.Body.String())
	}

	updated := doRequest(t, server.Router(), http.MethodPatch, "/api/clusters/"+createBody.Cluster.ID, token, `{"name":"自定义测试集群-更新","api_endpoint":"https://k8s.example.test:7443"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update cluster: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	if !strings.Contains(updated.Body.String(), "自定义测试集群-更新") || !strings.Contains(updated.Body.String(), "https://k8s.example.test:7443") {
		t.Fatalf("updated fields missing: %s", updated.Body.String())
	}

	tested := doRequest(t, server.Router(), http.MethodPost, "/api/clusters/"+createBody.Cluster.ID+"/test", token, "")
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), `"connected":true`) {
		t.Fatalf("test cluster: %d %s", tested.Code, tested.Body.String())
	}
}
