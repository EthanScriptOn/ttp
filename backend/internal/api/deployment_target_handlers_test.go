package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
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
