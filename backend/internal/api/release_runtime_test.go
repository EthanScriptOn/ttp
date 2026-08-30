package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
)

func TestPublishExecutesRuntimeDeployment(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Release.ID == "" {
		t.Fatal("release id is empty")
	}
	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID

	published := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", published.Code, published.Body.String())
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
		if response.Code != http.StatusOK {
			t.Fatalf("read release: expected 200, got %d: %s", response.Code, response.Body.String())
		}
		var body struct {
			Release struct {
				Status string `json:"status"`
			} `json:"release"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Release.Status == "succeeded" {
			break
		}
		if body.Release.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("release did not succeed: %s", response.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	pods := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/pods", token, "")
	if pods.Code != http.StatusOK {
		t.Fatalf("list pods: expected 200, got %d: %s", pods.Code, pods.Body.String())
	}
	var podBody struct {
		Items []struct {
			Labels map[string]string `json:"labels"`
		} `json:"items"`
	}
	if err := json.Unmarshal(pods.Body.Bytes(), &podBody); err != nil {
		t.Fatal(err)
	}
	if len(podBody.Items) == 0 {
		t.Fatal("release left no pods")
	}
	for _, pod := range podBody.Items {
		if pod.Labels["release"] != createdBody.Release.ID || pod.Labels["version"] != "a1b2c3d4e5" {
			t.Fatalf("pod does not identify the deployed release: %#v", pod.Labels)
		}
	}
}

func TestPublishStopsAfterFirstEnvironmentUntilExplicitAdvance(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"target_ids":["target-reverse-lab-dev","target-reverse-lab-uat"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID
	blocked := doRequest(t, handler, http.MethodPost, releasePath+"/targets/target-reverse-lab-uat/publish", token, "")
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), `"code":"conflict"`) {
		t.Fatalf("out-of-order UAT publish: expected 409 conflict, got %d: %s", blocked.Code, blocked.Body.String())
	}
	if response := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, ""); response.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", response.Code, response.Body.String())
	}

	readRelease := func() struct {
		Status  string `json:"status"`
		Stage   string `json:"stage"`
		Targets []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"targets"`
	} {
		response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
		if response.Code != http.StatusOK {
			t.Fatalf("read release: expected 200, got %d: %s", response.Code, response.Body.String())
		}
		var body struct {
			Release struct {
				Status  string `json:"status"`
				Stage   string `json:"stage"`
				Targets []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"targets"`
			} `json:"release"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Release
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		item := readRelease()
		if len(item.Targets) != 2 {
			t.Fatalf("unexpected release targets: %#v", item.Targets)
		}
		if item.Targets[0].Status == "succeeded" {
			if item.Status != "running" || item.Stage != "waiting" || item.Targets[1].Status != "pending" {
				t.Fatalf("release should wait after DEV instead of auto-publishing UAT: %#v", item)
			}
			break
		}
		if item.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("DEV did not finish in time: %#v", item)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Give the old serial worker enough time to reveal itself. UAT must remain
	// pending until its dedicated endpoint is called.
	time.Sleep(900 * time.Millisecond)
	waiting := readRelease()
	if waiting.Targets[1].Status != "pending" {
		t.Fatalf("UAT started without an explicit advance: %#v", waiting)
	}

	advanced := doRequest(t, handler, http.MethodPost, releasePath+"/targets/target-reverse-lab-uat/publish", token, "")
	if advanced.Code != http.StatusAccepted || !strings.Contains(advanced.Body.String(), `"status":"running"`) {
		t.Fatalf("advance UAT: expected running 202, got %d: %s", advanced.Code, advanced.Body.String())
	}
}

func TestCreateReleaseRequiresDevAsTheFirstEnvironment(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	response := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"target_ids":["target-reverse-lab-uat"],"strategy":"rolling"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "发布必须包含 DEV 环境") {
		t.Fatalf("UAT-only release should be rejected at creation, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRepeatPublishResetsACompletedReleaseFromDev(t *testing.T) {
	service := release.NewService(nil)
	item, _, err := service.Create(context.Background(), release.CreateInput{
		ProjectID: "reverse-lab", RepositoryID: "demo-repo", Branch: "main",
		CommitSHAs: []string{"a1b2c3d4e5f6"}, Targets: []release.TargetInput{
			{ID: "dev", Name: "开发环境", Environment: "dev"},
			{ID: "uat", Name: "测试环境", Environment: "uat"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, targetID := range []string{"dev", "uat"} {
		if _, claimed, startErr := service.StartTarget(item.ID, targetID); startErr != nil || !claimed {
			t.Fatalf("start %s: claimed=%v err=%v", targetID, claimed, startErr)
		}
		if _, completeErr := service.CompleteTarget(item.ID, targetID); completeErr != nil {
			t.Fatalf("complete %s: %v", targetID, completeErr)
		}
	}
	completed, err := service.FinalizeTargets(item.ID)
	if err != nil || completed.Status != release.StatusSucceeded {
		t.Fatalf("finish release: status=%s err=%v", completed.Status, err)
	}

	restarted, claimed, err := service.StartTarget(item.ID, "dev")
	if err != nil || !claimed {
		t.Fatalf("repeat publish should restart DEV: claimed=%v err=%v release=%#v", claimed, err, restarted)
	}
	if restarted.Status != release.StatusRunning || restarted.Targets[0].Status != release.TargetRunning || restarted.Targets[1].Status != release.TargetWaiting {
		t.Fatalf("repeat publish did not reset the environment sequence: %#v", restarted)
	}
}

func TestTargetPublishFailureLeavesLaterTargetsWaiting(t *testing.T) {
	server := testServer()
	server.deps.Runtime = runtime.NewService(&failingDemoRuntime{DemoProvider: runtime.NewDemoProvider()})
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"target_ids":["target-reverse-lab-dev","target-reverse-lab-uat"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID

	started := doRequest(t, handler, http.MethodPost, releasePath+"/targets/target-reverse-lab-dev/publish", token, "")
	if started.Code != http.StatusAccepted || !strings.Contains(started.Body.String(), `"status":"running"`) {
		t.Fatalf("publish DEV: expected running 202, got %d: %s", started.Code, started.Body.String())
	}

	var failed struct {
		Release struct {
			Status  string `json:"status"`
			Targets []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"targets"`
		} `json:"release"`
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
		if response.Code != http.StatusOK {
			t.Fatalf("read failed release: expected 200, got %d: %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &failed); err != nil {
			t.Fatal(err)
		}
		if failed.Release.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("release did not fail in time: %s", response.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(failed.Release.Targets) != 2 || failed.Release.Targets[0].Status != "failed" || failed.Release.Targets[1].Status != "waiting" {
		t.Fatalf("later target was cancelled or promoted after DEV failure: %#v", failed.Release.Targets)
	}

	blocked := doRequest(t, handler, http.MethodPost, releasePath+"/targets/target-reverse-lab-uat/publish", token, "")
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "请先完成环境") {
		t.Fatalf("out-of-order UAT publish after DEV failure: expected 409 prerequisite error, got %d: %s", blocked.Code, blocked.Body.String())
	}

	retried := doRequest(t, handler, http.MethodPost, releasePath+"/targets/target-reverse-lab-dev/retry", token, "")
	if retried.Code != http.StatusAccepted || !strings.Contains(retried.Body.String(), `"status":"running"`) {
		t.Fatalf("retry DEV: expected running 202, got %d: %s", retried.Code, retried.Body.String())
	}

	deadline = time.Now().Add(6 * time.Second)
	for {
		response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
		if response.Code != http.StatusOK {
			t.Fatalf("read retried release: expected 200, got %d: %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &failed); err != nil {
			t.Fatal(err)
		}
		if failed.Release.Status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retried release did not fail in time: %s", response.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if failed.Release.Targets[0].Status != "failed" || failed.Release.Targets[1].Status != "waiting" {
		t.Fatalf("retry changed a later pending target: %#v", failed.Release.Targets)
	}
}

type recordingReleaseRuntime struct {
	*runtime.DemoProvider
	mu          sync.Mutex
	deployments []runtime.ReleaseDeployment
}

func (p *recordingReleaseRuntime) DeployRelease(ctx context.Context, deployment runtime.ReleaseDeployment) error {
	p.mu.Lock()
	p.deployments = append(p.deployments, deployment)
	p.mu.Unlock()
	return p.DemoProvider.DeployRelease(ctx, deployment)
}

func (p *recordingReleaseRuntime) latestDeployment() (runtime.ReleaseDeployment, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.deployments) == 0 {
		return runtime.ReleaseDeployment{}, false
	}
	return p.deployments[len(p.deployments)-1], true
}

func TestPublishPassesSavedManifestToRuntime(t *testing.T) {
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServer()
	server.deps.Runtime = runtime.NewService(recorder)
	handler := server.Router()
	token := loginForTest(t, handler)

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  namespace: lab
spec:
  replicas: 2
---
apiVersion: v1
kind: Service
metadata:
  name: checkout
  namespace: lab
`
	saved := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-config", token, jsonBody(map[string]any{
		"manifest": manifest,
		"format":   "yaml",
	}))
	if saved.Code != http.StatusOK {
		t.Fatalf("save deployment config: expected 200, got %d: %s", saved.Code, saved.Body.String())
	}

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Release.ID == "" {
		t.Fatal("release id is empty")
	}

	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID
	published := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", published.Code, published.Body.String())
	}

	deadline := time.Now().Add(7 * time.Second)
	for {
		deployment, ok := recorder.latestDeployment()
		if ok {
			if strings.TrimSpace(deployment.Manifest) != strings.TrimSpace(manifest) || deployment.ManifestFormat != "yaml" || deployment.ManifestVersion != 1 {
				t.Fatalf("runtime received the wrong deployment config: %#v", deployment)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime did not receive a deployment within 7 seconds")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestPublishUsesReleaseBatchSnapshotForRuntimeDeployment(t *testing.T) {
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServer()
	server.deps.Runtime = runtime.NewService(recorder)
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"release/2026.01","commit_shas":["112233445566"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Release.ID == "" {
		t.Fatal("release id is empty")
	}

	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID
	published := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", published.Code, published.Body.String())
	}
	var publishedBody struct {
		Release struct {
			BatchSnapshotSHA string `json:"batch_snapshot_sha"`
		} `json:"release"`
	}
	if err := json.Unmarshal(published.Body.Bytes(), &publishedBody); err != nil {
		t.Fatal(err)
	}
	snapshot := strings.TrimSpace(publishedBody.Release.BatchSnapshotSHA)
	if snapshot == "" {
		t.Fatalf("publish did not expose a batch snapshot: %s", published.Body.String())
	}
	if snapshot == "112233445566" {
		t.Fatalf("fixture did not create an integrated batch snapshot: %s", snapshot)
	}

	deadline := time.Now().Add(7 * time.Second)
	for {
		deployment, ok := recorder.latestDeployment()
		if ok {
			if deployment.ReleaseID != createdBody.Release.ID {
				t.Fatalf("runtime received an unexpected release: %#v", deployment)
			}
			if deployment.CommitSHA != snapshot {
				t.Fatalf("runtime used the selected commit instead of the immutable batch snapshot: got=%s want=%s", deployment.CommitSHA, snapshot)
			}
			if deployment.Image != "example.invalid/reverse-lab:"+snapshot[:10] {
				t.Fatalf("runtime image was not built from the batch snapshot: %s", deployment.Image)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime did not receive a deployment within 7 seconds")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type failingDemoRuntime struct {
	*runtime.DemoProvider
}

func (p *failingDemoRuntime) DeployRelease(_ context.Context, _ runtime.ReleaseDeployment) error {
	return errors.New("演示运行时部署失败")
}

func TestPublishMarksReleaseFailedWhenRuntimeDeploymentFails(t *testing.T) {
	server := testServer()
	server.deps.Runtime = runtime.NewService(&failingDemoRuntime{DemoProvider: runtime.NewDemoProvider()})
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID
	if response := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, ""); response.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", response.Code, response.Body.String())
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
		if response.Code != http.StatusOK {
			t.Fatalf("read release: expected 200, got %d: %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), `"status":"failed"`) {
			if !strings.Contains(response.Body.String(), "演示运行时部署失败") {
				t.Fatalf("failure reason was not recorded: %s", response.Body.String())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("release did not fail in time: %s", response.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
}
