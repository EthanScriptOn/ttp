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

	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
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
			ID            string `json:"id"`
			CreatedBy     uint64 `json:"created_by"`
			CreatedByName string `json:"created_by_name"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Release.ID == "" {
		t.Fatal("release id is empty")
	}
	if createdBody.Release.CreatedBy != 1 || createdBody.Release.CreatedByName != "平台管理员" {
		t.Fatalf("release creator = %d/%q, want 1/平台管理员", createdBody.Release.CreatedBy, createdBody.Release.CreatedByName)
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

func TestRollingReleaseTrafficUpdateReturnsClearConflict(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"target_ids":["target-reverse-lab-dev"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var body struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	response := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/releases/"+body.Release.ID+"/targets/target-reverse-lab-dev/traffic", token, `{"stable_percent":90,"candidate_percent":10}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "当前发布单实际采用滚动发布，不支持调整流量") {
		t.Fatalf("rolling traffic update: expected clear 409 conflict, got %d: %s", response.Code, response.Body.String())
	}
}

func TestPublishTriggersConfiguredEnvironmentAutoMerge(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)
	updated := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab", token, `{"auto_merge_enabled":true,"auto_merge_target_id":"target-reverse-lab-dev"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("configure auto merge: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"release/2026.01","commit_shas":["112233445566"],"target_ids":["target-reverse-lab-dev"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var body struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases/"+body.Release.ID+"/publish", token, ""); response.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", response.Code, response.Body.String())
	}
	item := waitForSucceededRelease(t, server, body.Release.ID)
	if len(item.Targets) != 1 || len(item.Targets[0].Logs) == 0 {
		t.Fatalf("release did not retain execution logs: %#v", item.Targets)
	}
	found := false
	for _, log := range item.Targets[0].Logs {
		if strings.Contains(log.Line, "自动 merge 成功") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("auto merge success was not logged: %#v", item.Targets[0].Logs)
	}
}

func TestReleaseFlowEndpointExposesCurrentReleaseAndParticipants(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)
	for _, branch := range []string{"main", "release/2026.01"} {
		created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"`+branch+`","strategy":"rolling"}`)
		if created.Code != http.StatusCreated {
			t.Fatalf("create %s release: expected 201, got %d: %s", branch, created.Code, created.Body.String())
		}
	}

	response := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/release-flow", token, "")
	if response.Code != http.StatusOK {
		t.Fatalf("get release flow: expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Flow struct {
			ID               string `json:"id"`
			BaseBranch       string `json:"base_branch"`
			CurrentReleaseID string `json:"current_release_id"`
			Version          int    `json:"version"`
			Participants     []struct {
				Branch string `json:"branch"`
				Active bool   `json:"active"`
			} `json:"participants"`
		} `json:"flow"`
		CurrentRelease     release.Release   `json:"current_release"`
		ParticipantRelease []release.Release `json:"participant_releases"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Flow.ID == "" || body.Flow.BaseBranch != "main" || body.Flow.Version != 2 || body.Flow.CurrentReleaseID == "" {
		t.Fatalf("unexpected flow snapshot: %#v", body.Flow)
	}
	active := map[string]bool{}
	for _, participant := range body.Flow.Participants {
		active[participant.Branch] = participant.Active
	}
	if !active["main"] || !active["release/2026.01"] {
		t.Fatalf("flow did not expose both active participants: %#v", active)
	}
	if body.CurrentRelease.ID != body.Flow.CurrentReleaseID || len(body.ParticipantRelease) != 2 {
		t.Fatalf("flow release projections are incomplete: current=%s/%s participants=%d", body.CurrentRelease.ID, body.Flow.CurrentReleaseID, len(body.ParticipantRelease))
	}
}

func TestRepublishRemovesBranchFromFlowAndKeepsImmutableHistory(t *testing.T) {
	provider := &movingHeadProvider{Provider: git.NewDemoProvider()}
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServerWithGitProvider(provider)
	server.deps.Runtime = runtime.NewService(recorder)
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"release/2026.01","strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		Release struct {
			ID      string `json:"id"`
			Commits []struct {
				SHA string `json:"sha"`
			} `json:"commits"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if len(createdBody.Release.Commits) != 1 || createdBody.Release.Commits[0].SHA != "112233445566" {
		t.Fatalf("created release did not capture the original branch head: %#v", createdBody.Release.Commits)
	}
	releasePath := "/api/projects/reverse-lab/releases/" + createdBody.Release.ID
	published := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", published.Code, published.Body.String())
	}
	waitForSucceededRelease(t, server, createdBody.Release.ID)

	provider.moveHead()
	removed := doRequest(t, handler, http.MethodDelete, releasePath, token, "")
	if removed.Code != http.StatusConflict || !strings.Contains(removed.Body.String(), "已发布发布单不能直接移除") {
		t.Fatalf("remove published release: expected 409 conflict, got %d: %s", removed.Code, removed.Body.String())
	}

	unchanged, err := server.deps.Release.Get(createdBody.Release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Removed || unchanged.Status != release.StatusSucceeded {
		t.Fatalf("direct removal changed the old release: %#v", unchanged)
	}

	republished := doRequest(t, handler, http.MethodPost, releasePath+"/republish", token, "")
	if republished.Code != http.StatusAccepted {
		t.Fatalf("republish: expected 202, got %d: %s", republished.Code, republished.Body.String())
	}
	var republishedBody struct {
		Release struct {
			ID      string `json:"id"`
			Branch  string `json:"branch"`
			Commits []struct {
				SHA string `json:"sha"`
			} `json:"commits"`
		} `json:"release"`
		ReplacingReleaseID string `json:"replacing_release_id"`
		Replacing          bool   `json:"replacing"`
	}
	if err := json.Unmarshal(republished.Body.Bytes(), &republishedBody); err != nil {
		t.Fatal(err)
	}
	if republishedBody.ReplacingReleaseID != createdBody.Release.ID || !republishedBody.Replacing {
		t.Fatalf("republish response did not identify the replaced release: %s", republished.Body.String())
	}
	if republishedBody.Release.ID == "" || republishedBody.Release.ID == createdBody.Release.ID || republishedBody.Release.Branch != "main" {
		t.Fatalf("republish did not create a new release: %#v", republishedBody.Release)
	}
	if len(republishedBody.Release.Commits) != 1 || republishedBody.Release.Commits[0].SHA != "a1b2c3d4e5f6" {
		t.Fatalf("replacement did not use the flow base revision: %#v", republishedBody.Release.Commits)
	}

	replacement := waitForSucceededRelease(t, server, republishedBody.Release.ID)
	if len(replacement.Commits) != 1 || replacement.Commits[0].SHA != "a1b2c3d4e5f6" {
		t.Fatalf("replacement release did not retain the flow base revision: %#v", replacement.Commits)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		old, getErr := server.deps.Release.Get(createdBody.Release.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if old.ReplacedByReleaseID == republishedBody.Release.ID && old.ReplacementState == "replaced" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("old release was not marked as replaced after replacement succeeded: %#v", old)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if deployment, ok := recorder.latestDeployment(); !ok || deployment.CommitSHA != "a1b2c3d4e5f6" {
		t.Fatalf("replacement deployment did not use the flow base revision: %#v", deployment)
	}
	listed := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/releases", token, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), createdBody.Release.ID) || !strings.Contains(listed.Body.String(), republishedBody.Release.ID) {
		t.Fatalf("release list did not retain immutable history and replacement: %d %s", listed.Code, listed.Body.String())
	}
}

func TestPublishBuildsOneArtifactForAllTargetsAndRebuildsOnRepublish(t *testing.T) {
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServer()
	server.deps.Runtime = runtime.NewService(recorder)
	builder, ok := server.deps.ImageBuilder.(*testImageBuilder)
	if !ok {
		t.Fatal("test server did not install a build contract fake")
	}
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"target_ids":["target-reverse-lab-dev","target-reverse-lab-uat"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create multi-target release: %d %s", created.Code, created.Body.String())
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
		t.Fatal("created release has no id")
	}
	path := "/api/projects/reverse-lab/releases/" + body.Release.ID
	if response := doRequest(t, handler, http.MethodPost, path+"/publish", token, ""); response.Code != http.StatusAccepted {
		t.Fatalf("publish multi-target release: %d %s", response.Code, response.Body.String())
	}
	first := waitForSucceededRelease(t, server, body.Release.ID)
	if builder.callCount() != 1 {
		t.Fatalf("one multi-target execution built %d images, want 1", builder.callCount())
	}
	if first.Artifact == nil || first.Artifact.Digest != testImageDigest {
		t.Fatalf("release did not persist its immutable artifact: %#v", first.Artifact)
	}
	if recorder.deploymentCount() != 2 {
		t.Fatalf("expected deployments for both selected environments, got %d", recorder.deploymentCount())
	}
	for _, deployment := range recorder.deploymentsSnapshot() {
		if deployment.Image != first.Artifact.Image {
			t.Fatalf("environment deployed a different image: %#v, want %q", deployment, first.Artifact.Image)
		}
	}

	if response := doRequest(t, handler, http.MethodPost, path+"/publish", token, ""); response.Code != http.StatusAccepted {
		t.Fatalf("re-publish release: %d %s", response.Code, response.Body.String())
	}
	_ = waitForSucceededRelease(t, server, body.Release.ID)
	if builder.callCount() != 2 {
		t.Fatalf("re-publish should rebuild the saved commit once, got %d builds", builder.callCount())
	}
	if recorder.deploymentCount() != 4 {
		t.Fatalf("re-publish should deploy both environments again, got %d deployments", recorder.deploymentCount())
	}
}

func TestBuildFailureDoesNotCallRuntimeAndRetainsBuildLogs(t *testing.T) {
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServer()
	server.deps.Runtime = runtime.NewService(recorder)
	builder := server.deps.ImageBuilder.(*testImageBuilder)
	builder.err = &imagebuild.Failure{
		Err:  errors.New("BuildKit failed to build or push the image"),
		Logs: []imagebuild.LogEntry{{Stream: "stderr", Level: "ERROR", Line: "failed to resolve Dockerfile"}},
	}
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: %d %s", created.Code, created.Body.String())
	}
	var body struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	path := "/api/projects/reverse-lab/releases/" + body.Release.ID
	if response := doRequest(t, handler, http.MethodPost, path+"/publish", token, ""); response.Code != http.StatusAccepted {
		t.Fatalf("publish release: %d %s", response.Code, response.Body.String())
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		item, err := server.deps.Release.Get(body.Release.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Status == release.StatusFailed {
			if recorder.deploymentCount() != 0 {
				t.Fatalf("runtime was called after build failure: %d deployment(s)", recorder.deploymentCount())
			}
			if len(item.Targets) == 0 || !releaseLogsContain(item.Targets[0].Logs, "failed to resolve Dockerfile") {
				t.Fatalf("sanitized builder log was not retained: %#v", item.Targets)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("release did not fail after builder failure: %#v", item)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForSucceededRelease(t *testing.T, server *Server, releaseID string) release.Release {
	t.Helper()
	deadline := time.Now().Add(7 * time.Second)
	for {
		item, err := server.deps.Release.Get(releaseID)
		if err != nil {
			t.Fatal(err)
		}
		switch item.Status {
		case release.StatusSucceeded:
			return item
		case release.StatusFailed, release.StatusCancelled:
			t.Fatalf("release %s did not succeed: %#v", releaseID, item)
		}
		if time.Now().After(deadline) {
			t.Fatalf("release %s did not finish: %#v", releaseID, item)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func releaseLogsContain(logs []release.ExecutionLog, expected string) bool {
	for _, entry := range logs {
		if strings.Contains(entry.Line, expected) {
			return true
		}
	}
	return false
}

func TestPublishUsesSavedCommitWhenSourceBranchMoves(t *testing.T) {
	base := git.NewDemoProvider()
	provider := &movingHeadProvider{Provider: base}
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServerWithGitProvider(provider)
	server.deps.Runtime = runtime.NewService(recorder)
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	provider.moveHead()

	var body struct {
		Release struct {
			ID      string `json:"id"`
			Commits []struct {
				SHA string `json:"sha"`
			} `json:"commits"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Release.Commits) != 1 || body.Release.Commits[0].SHA != "a1b2c3d4e5f6" {
		t.Fatalf("release did not save the original branch head: %#v", body.Release.Commits)
	}

	releasePath := "/api/projects/reverse-lab/releases/" + body.Release.ID
	published := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", published.Code, published.Body.String())
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		deployment, ok := recorder.latestDeployment()
		if ok {
			if deployment.CommitSHA != "a1b2c3d4e5f6" {
				t.Fatalf("publish used the moved branch head instead of the saved commit: %#v", deployment)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime did not receive a deployment within 6 seconds")
		}
		time.Sleep(100 * time.Millisecond)
	}
	for {
		response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
		if response.Code != http.StatusOK {
			t.Fatalf("read release: expected 200, got %d: %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), `"status":"succeeded"`) {
			break
		}
		if strings.Contains(response.Body.String(), `"status":"failed"`) || time.Now().After(deadline) {
			t.Fatalf("first release did not succeed: %s", response.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	provider.moveHead()
	repeated := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if repeated.Code != http.StatusAccepted {
		t.Fatalf("repeat publish: expected 202, got %d: %s", repeated.Code, repeated.Body.String())
	}

	deadline = time.Now().Add(6 * time.Second)
	for {
		if recorder.deploymentCount() >= 2 {
			deployment, _ := recorder.latestDeployment()
			if deployment.CommitSHA != "a1b2c3d4e5f6" {
				t.Fatalf("repeat publish used the moved branch head instead of the saved commit: %#v", deployment)
			}
			if provider.listCommitsCalls() != 1 {
				t.Fatalf("publish re-read the source branch: list commits called %d times", provider.listCommitsCalls())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("repeat publish did not reach the runtime within 6 seconds")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestProductionPublishDoesNotInventImage(t *testing.T) {
	provider := &movingHeadProvider{Provider: git.NewDemoProvider()}
	recorder := &recordingReleaseRuntime{DemoProvider: runtime.NewDemoProvider()}
	server := testServerWithGitProvider(provider)
	server.deps.ImageBuilder = nil
	server.deps.Runtime = runtime.NewService(recorder)
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"main","commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var body struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	releasePath := "/api/projects/reverse-lab/releases/" + body.Release.ID
	started := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if started.Code != http.StatusNotImplemented || !strings.Contains(started.Body.String(), `"code":"image_build_unsupported"`) {
		t.Fatalf("publish should be blocked before queueing without a builder, got %d: %s", started.Code, started.Body.String())
	}
	response := doRequest(t, handler, http.MethodGet, releasePath, token, "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"status":"running"`) {
		t.Fatalf("blocked publish must not mutate release state: %s", response.Body.String())
	}
	if recorder.deploymentCount() != 0 {
		t.Fatalf("runtime was called without a real image: %d call(s)", recorder.deploymentCount())
	}
}

type movingHeadProvider struct {
	git.Provider
	mu          sync.Mutex
	commitCalls int
	branchMoved bool
}

func (p *movingHeadProvider) ListCommits(ctx context.Context, repositoryID, branch string, limit int) ([]git.Commit, error) {
	p.mu.Lock()
	p.commitCalls++
	moved := p.branchMoved
	p.mu.Unlock()
	commits, err := p.Provider.ListCommits(ctx, repositoryID, branch, limit)
	if err != nil || !moved || len(commits) == 0 {
		return commits, err
	}
	commits[0].SHA = "newbranchhead0000"
	commits[0].ShortSHA = "newbranch"
	return commits, nil
}

func (p *movingHeadProvider) moveHead() {
	p.mu.Lock()
	p.branchMoved = true
	p.mu.Unlock()
}

func (p *movingHeadProvider) listCommitsCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.commitCalls
}

func (p *movingHeadProvider) CheckRepositoryAccess(_ context.Context, repositoryID string) (git.RepositoryAccess, error) {
	return git.RepositoryAccess{
		RepositoryID:             repositoryID,
		Supported:                true,
		Authenticated:            true,
		RepositoryFound:          true,
		AccountMatches:           true,
		CanRead:                  true,
		CanWrite:                 true,
		CanCreateTemporaryBranch: true,
		CanMerge:                 true,
		Usable:                   true,
		Permission:               "write",
	}, nil
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

func (p *recordingReleaseRuntime) deploymentCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.deployments)
}

func (p *recordingReleaseRuntime) deploymentsSnapshot() []runtime.ReleaseDeployment {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]runtime.ReleaseDeployment(nil), p.deployments...)
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
			if strings.TrimSpace(deployment.Manifest) != strings.TrimSpace(manifest) || deployment.ManifestFormat != "yaml" || deployment.ManifestVersion != 2 {
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
