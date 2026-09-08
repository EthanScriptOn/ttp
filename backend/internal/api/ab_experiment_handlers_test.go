package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type abExperimentResponse struct {
	Experiment struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		RoutingRule struct {
			Source          string `json:"source"`
			Path            string `json:"path"`
			MissingBehavior string `json:"missing_behavior"`
			Algorithm       string `json:"algorithm"`
		} `json:"routing_rule"`
		ATraffic int `json:"a_traffic"`
		BTraffic int `json:"b_traffic"`
		APods    []struct {
			Name string `json:"name"`
		} `json:"a_pods"`
		BPods []struct {
			Name string `json:"name"`
		} `json:"b_pods"`
	} `json:"experiment"`
}

func TestABExperimentAPIFlowAndEnvironmentConflict(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	aReleaseID := createCompletedABRelease(t, server, token, "main", "a1b2c3d4e5f6")
	bReleaseID := createCompletedABRelease(t, server, token, "feature/checkout", "112233445566")
	createPath := "/api/projects/reverse-lab/ab-experiments"
	body := fmt.Sprintf(`{"name":"结算流程实验","target_id":"target-reverse-lab-dev","a_release_id":"%s","b_release_id":"%s"}`, aReleaseID, bReleaseID)

	created := doRequest(t, handler, http.MethodPost, createPath, token, body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create experiment: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody abExperimentResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Experiment.ID == "" || createdBody.Experiment.Status != "running" || createdBody.Experiment.ATraffic != 99 || createdBody.Experiment.BTraffic != 1 || len(createdBody.Experiment.APods) != 2 || len(createdBody.Experiment.BPods) != 2 {
		t.Fatalf("unexpected created experiment: %#v", createdBody.Experiment)
	}
	experimentPath := createPath + "/" + createdBody.Experiment.ID

	conflict := doRequest(t, handler, http.MethodPost, createPath, token, fmt.Sprintf(`{"name":"重复实验","target_id":"target-reverse-lab-dev","a_release_id":"%s","b_release_id":"%s"}`, aReleaseID, bReleaseID))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second running experiment: expected 409, got %d: %s", conflict.Code, conflict.Body.String())
	}

	updated := doRequest(t, handler, http.MethodPatch, experimentPath+"/traffic", token, `{"a_traffic":80,"b_traffic":20}`)
	if updated.Code != http.StatusOK || !containsABTraffic(updated.Body.Bytes(), 80, 20) {
		t.Fatalf("update traffic: expected 200 with 80/20, got %d: %s", updated.Code, updated.Body.String())
	}

	invalidTraffic := doRequest(t, handler, http.MethodPatch, experimentPath+"/traffic", token, `{"a_traffic":70,"b_traffic":20}`)
	if invalidTraffic.Code != http.StatusBadRequest {
		t.Fatalf("invalid traffic: expected 400, got %d: %s", invalidTraffic.Code, invalidTraffic.Body.String())
	}

	stopped := doRequest(t, handler, http.MethodPost, experimentPath+"/stop", token, "")
	if stopped.Code != http.StatusOK {
		t.Fatalf("stop experiment: expected 200, got %d: %s", stopped.Code, stopped.Body.String())
	}
	var stoppedBody abExperimentResponse
	if err := json.Unmarshal(stopped.Body.Bytes(), &stoppedBody); err != nil {
		t.Fatal(err)
	}
	if stoppedBody.Experiment.Status != "stopped" || stoppedBody.Experiment.ATraffic != 100 || stoppedBody.Experiment.BTraffic != 0 || len(stoppedBody.Experiment.APods) != 2 || len(stoppedBody.Experiment.BPods) != 0 {
		t.Fatalf("unexpected stopped experiment: %#v", stoppedBody.Experiment)
	}

	stopAgain := doRequest(t, handler, http.MethodPost, experimentPath+"/stop", token, "")
	if stopAgain.Code != http.StatusConflict {
		t.Fatalf("stopping terminal experiment: expected 409, got %d: %s", stopAgain.Code, stopAgain.Body.String())
	}
}

func TestABExperimentRejectsInvalidFinishBeforeRuntimeMutation(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)
	aReleaseID := createCompletedABRelease(t, server, token, "main", "a1b2c3d4e5f6")
	bReleaseID := createCompletedABRelease(t, server, token, "feature/checkout", "112233445566")

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/ab-experiments", token, fmt.Sprintf(`{"name":"非法结束测试","target_id":"target-reverse-lab-dev","a_release_id":"%s","b_release_id":"%s"}`, aReleaseID, bReleaseID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create experiment: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody abExperimentResponse
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}

	path := "/api/projects/reverse-lab/ab-experiments/" + createdBody.Experiment.ID
	invalid := doRequest(t, handler, http.MethodPost, path+"/finish", token, `{"result":"unknown"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid finish result: expected 400, got %d: %s", invalid.Code, invalid.Body.String())
	}

	current := doRequest(t, handler, http.MethodGet, path, token, "")
	if current.Code != http.StatusOK {
		t.Fatalf("read after invalid finish: expected 200, got %d: %s", current.Code, current.Body.String())
	}
	var currentBody abExperimentResponse
	if err := json.Unmarshal(current.Body.Bytes(), &currentBody); err != nil {
		t.Fatal(err)
	}
	if currentBody.Experiment.Status != "running" || len(currentBody.Experiment.APods) != 2 || len(currentBody.Experiment.BPods) != 2 {
		t.Fatalf("invalid finish changed the experiment runtime: %#v", currentBody.Experiment)
	}

	finished := doRequest(t, handler, http.MethodPost, path+"/finish", token, `{"result":"promote_b"}`)
	if finished.Code != http.StatusOK {
		t.Fatalf("finish with B: expected 200, got %d: %s", finished.Code, finished.Body.String())
	}
	var finishedBody abExperimentResponse
	if err := json.Unmarshal(finished.Body.Bytes(), &finishedBody); err != nil {
		t.Fatal(err)
	}
	if finishedBody.Experiment.Status != "finished" || finishedBody.Experiment.ATraffic != 0 || finishedBody.Experiment.BTraffic != 100 || len(finishedBody.Experiment.APods) != 0 || len(finishedBody.Experiment.BPods) != 2 {
		t.Fatalf("unexpected promoted experiment: %#v", finishedBody.Experiment)
	}
}

func TestABExperimentAcceptsUnfinishedBReleaseAsCandidate(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)
	aReleaseID := createCompletedABRelease(t, server, token, "main", "a1b2c3d4e5f6")

	draft := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"feature/checkout","commit_shas":["112233445566"],"target_ids":["target-reverse-lab-dev"],"strategy":"rolling"}`)
	if draft.Code != http.StatusCreated {
		t.Fatalf("create draft B release: expected 201, got %d: %s", draft.Code, draft.Body.String())
	}
	var draftBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(draft.Body.Bytes(), &draftBody); err != nil {
		t.Fatal(err)
	}

	response := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/ab-experiments", token, fmt.Sprintf(`{"name":"未完成版本实验","target_id":"target-reverse-lab-dev","a_release_id":"%s","b_release_id":"%s"}`, aReleaseID, draftBody.Release.ID))
	if response.Code != http.StatusCreated {
		t.Fatalf("unfinished B release candidate: expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var responseBody abExperimentResponse
	if err := json.Unmarshal(response.Body.Bytes(), &responseBody); err != nil {
		t.Fatal(err)
	}
	if responseBody.Experiment.BPods == nil || len(responseBody.Experiment.BPods) != 2 {
		t.Fatalf("candidate B release was not deployed alongside A: %#v", responseBody.Experiment)
	}
}

func TestABExperimentAcceptsJSONBodyRoutingRule(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)
	aReleaseID := createCompletedABRelease(t, server, token, "main", "a1b2c3d4e5f6")
	bReleaseID := createCompletedABRelease(t, server, token, "feature/wx-routing", "112233445566")

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/ab-experiments", token, fmt.Sprintf(`{"name":"微信用户分流","target_id":"target-reverse-lab-dev","a_release_id":"%s","b_release_id":"%s","assignment":"user_id","routing_rule":{"source":"json_body","path":"$.wx_id","missing_behavior":"stable","algorithm":"consistent_hash"}}`, aReleaseID, bReleaseID))
	if created.Code != http.StatusCreated {
		t.Fatalf("create JSON body experiment: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var body abExperimentResponse
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	rule := body.Experiment.RoutingRule
	if rule.Source != "json_body" || rule.Path != "$.wx_id" || rule.MissingBehavior != "stable" || rule.Algorithm != "consistent_hash" {
		t.Fatalf("unexpected routing rule: %#v", rule)
	}

	invalid := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/ab-experiments", token, fmt.Sprintf(`{"name":"不安全字段","target_id":"target-reverse-lab-dev","a_release_id":"%s","b_release_id":"%s","assignment":"user_id","routing_rule":{"source":"json_body","path":"$.headers['x-ttp-variant']"}}`, aReleaseID, bReleaseID))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unsafe JSON path: expected 400, got %d: %s", invalid.Code, invalid.Body.String())
	}
}

func createCompletedABRelease(t *testing.T, server *Server, token, branch, sha string) string {
	t.Helper()
	created := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases", token, fmt.Sprintf(`{"branch":"%s","commit_shas":["%s"],"target_ids":["target-reverse-lab-dev"],"strategy":"rolling"}`, branch, sha))
	if created.Code != http.StatusCreated {
		t.Fatalf("create release %s: expected 201, got %d: %s", branch, created.Code, created.Body.String())
	}
	var body struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Release.ID == "" {
		t.Fatal("release id is empty")
	}
	if _, err := server.deps.Release.Transition(body.Release.ID, "queued"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.deps.Release.Transition(body.Release.ID, "running"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.deps.Release.CompleteTarget(body.Release.ID, "target-reverse-lab-dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.deps.Release.FinalizeTargets(body.Release.ID); err != nil {
		t.Fatal(err)
	}
	return body.Release.ID
}

func containsABTraffic(body []byte, a, b int) bool {
	var response abExperimentResponse
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	return response.Experiment.ATraffic == a && response.Experiment.BTraffic == b
}
