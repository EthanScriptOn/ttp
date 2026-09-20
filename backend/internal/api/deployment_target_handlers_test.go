package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func TestDeploymentTargetCannotChangeDuringActiveRelease(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	targetsResponse := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-targets", token, "")
	if targetsResponse.Code != http.StatusOK {
		t.Fatalf("list deployment targets: expected 200, got %d: %s", targetsResponse.Code, targetsResponse.Body.String())
	}
	var targetBody struct {
		Items []struct {
			ID          string `json:"id"`
			Environment string `json:"environment"`
		} `json:"items"`
	}
	if err := json.Unmarshal(targetsResponse.Body.Bytes(), &targetBody); err != nil {
		t.Fatal(err)
	}
	var devID, uatID string
	for _, target := range targetBody.Items {
		switch target.Environment {
		case "dev":
			devID = target.ID
		case "uat":
			uatID = target.ID
		}
	}
	if devID == "" || uatID == "" {
		t.Fatalf("expected DEV and UAT targets, got %#v", targetBody.Items)
	}

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, fmt.Sprintf(`{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"target_ids":[%q,%q],"strategy":"rolling"}`, devID, uatID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Release.ID == "" {
		t.Fatal("release id is empty")
	}

	published := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases/"+createBody.Release.ID+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish release: expected 202, got %d: %s", published.Code, published.Body.String())
	}

	blocked := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/deployment-targets/"+uatID, token, `{"deploy_strategy":"canary"}`)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("update active target: expected 409, got %d: %s", blocked.Code, blocked.Body.String())
	}

	cancelled := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases/"+createBody.Release.ID+"/cancel", token, "")
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel release: expected 200, got %d: %s", cancelled.Code, cancelled.Body.String())
	}

	updated := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/deployment-targets/"+uatID, token, `{"deploy_strategy":"canary"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update target after release: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
}

func TestDeploymentTargetPersistsAndReusesNamespaceQuota(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	body := `{"resource_quota":{"cpu_request":"1","cpu_limit":"2","memory_request":"1Gi","memory_limit":"2Gi","ephemeral_storage_request":"5Gi","ephemeral_storage_limit":"10Gi","storage":"25Gi","pods":8,"persistent_volume_claims":4,"default_cpu_request":"100m","default_cpu_limit":"250m","default_memory_request":"128Mi","default_memory_limit":"256Mi","default_ephemeral_storage_request":"128Mi","default_ephemeral_storage_limit":"512Mi"}}`
	updated := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/deployment-targets/target-reverse-lab-uat", token, body)
	if updated.Code != http.StatusOK {
		t.Fatalf("update quota: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	var updatedBody struct {
		Target struct {
			ResourceQuota domain.NamespaceQuota `json:"resource_quota"`
		} `json:"target"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &updatedBody); err != nil {
		t.Fatal(err)
	}
	if updatedBody.Target.ResourceQuota.CPULimit != "2" || updatedBody.Target.ResourceQuota.Storage != "25Gi" || updatedBody.Target.ResourceQuota.Pods != 8 {
		t.Fatalf("updated target quota = %#v", updatedBody.Target.ResourceQuota)
	}

	withoutQuota := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/deployment-targets/target-reverse-lab-uat", token, `{"deploy_strategy":"canary"}`)
	if withoutQuota.Code != http.StatusOK {
		t.Fatalf("update without quota: expected 200, got %d: %s", withoutQuota.Code, withoutQuota.Body.String())
	}
	var reusedBody struct {
		Target struct {
			ResourceQuota domain.NamespaceQuota `json:"resource_quota"`
		} `json:"target"`
	}
	if err := json.Unmarshal(withoutQuota.Body.Bytes(), &reusedBody); err != nil {
		t.Fatal(err)
	}
	if reusedBody.Target.ResourceQuota != updatedBody.Target.ResourceQuota {
		t.Fatalf("quota was reset when omitted: got %#v want %#v", reusedBody.Target.ResourceQuota, updatedBody.Target.ResourceQuota)
	}
}

func TestDeploymentTargetSharesNamespaceQuotaAcrossProjects(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	body := `{"resource_quota":{"cpu_request":"1","cpu_limit":"2","memory_request":"1Gi","memory_limit":"2Gi","ephemeral_storage_request":"5Gi","ephemeral_storage_limit":"10Gi","storage":"25Gi","pods":8,"persistent_volume_claims":4,"default_cpu_request":"100m","default_cpu_limit":"250m","default_memory_request":"128Mi","default_memory_limit":"256Mi","default_ephemeral_storage_request":"128Mi","default_ephemeral_storage_limit":"512Mi"}}`
	updated := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/deployment-targets/target-reverse-lab-dev", token, body)
	if updated.Code != http.StatusOK {
		t.Fatalf("update shared quota: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}

	project, err := server.deps.Store.CreateProject(context.Background(), "space-lab", store.CreateProjectInput{
		Name:          "共享环境项目",
		RepositoryURL: "https://example.invalid/shared-environment",
		ClusterID:     "demo-cluster",
	})
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}
	created := doRequest(t, handler, http.MethodPost, "/api/projects/"+project.ID+"/deployment-targets", token, `{"name":"开发环境","environment":"DEV","stage":"dev","sort_order":1,"cluster_id":"demo-cluster","resource_quota":{"cpu_request":"2","cpu_limit":"4","memory_request":"2Gi","memory_limit":"4Gi","ephemeral_storage_request":"10Gi","ephemeral_storage_limit":"20Gi","storage":"50Gi","pods":20,"persistent_volume_claims":10,"default_cpu_request":"100m","default_cpu_limit":"500m","default_memory_request":"128Mi","default_memory_limit":"512Mi","default_ephemeral_storage_request":"256Mi","default_ephemeral_storage_limit":"1Gi"},"replicas":1,"container_port":8080,"deploy_strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create second project target: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Target struct {
			Namespace     string                `json:"namespace"`
			ResourceQuota domain.NamespaceQuota `json:"resource_quota"`
		} `json:"target"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Target.Namespace != "ttp-lab-dev" {
		t.Fatalf("shared target namespace = %q, want ttp-lab-dev", createdBody.Target.Namespace)
	}
	if createdBody.Target.ResourceQuota.CPULimit != "2" || createdBody.Target.ResourceQuota.Storage != "25Gi" || createdBody.Target.ResourceQuota.Pods != 8 {
		t.Fatalf("second project did not reuse shared quota: %#v", createdBody.Target.ResourceQuota)
	}
}
