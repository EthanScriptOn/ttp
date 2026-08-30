package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func testServer() *Server {
	data := store.NewMemoryWithAdminPassword("test-password")
	provider := git.NewDemoProvider()
	return New(Dependencies{
		Config:  config.Config{DemoMode: true, JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   data,
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(nil),
	})
}

func TestSpaceScopedReleaseAndRuntimeFlow(t *testing.T) {
	server := testServer()
	token := loginForTest(t, server.Router())

	projects := doRequest(t, server.Router(), http.MethodGet, "/api/projects", token, "")
	if projects.Code != http.StatusOK {
		t.Fatalf("list projects: expected 200, got %d: %s", projects.Code, projects.Body.String())
	}
	var projectResponse struct {
		Items []struct {
			ID       string `json:"id"`
			Health   string `json:"health"`
			PodCount int    `json:"pod_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(projects.Body.Bytes(), &projectResponse); err != nil {
		t.Fatal(err)
	}
	if len(projectResponse.Items) != 1 || projectResponse.Items[0].ID != "reverse-lab" {
		t.Fatalf("unexpected projects: %#v", projectResponse.Items)
	}
	if projectResponse.Items[0].Health != "healthy" || projectResponse.Items[0].PodCount != 2 {
		t.Fatalf("project runtime summary was not populated: %#v", projectResponse.Items[0])
	}

	commits := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/commits", token, "")
	if commits.Code != http.StatusOK {
		t.Fatalf("list commits: expected 200, got %d: %s", commits.Code, commits.Body.String())
	}

	created := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6","f6e5d4c3b2a1"],"strategy":"canary","stable_percent":90,"candidate_percent":10}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createResponse); err != nil {
		t.Fatal(err)
	}
	if createResponse.Release.ID == "" {
		t.Fatal("release id is empty")
	}

	removed := doRequest(t, server.Router(), http.MethodDelete, "/api/projects/reverse-lab/releases/"+createResponse.Release.ID+"/commits/f6e5d4c3b2a1", token, "")
	if removed.Code != http.StatusOK {
		t.Fatalf("remove commit: expected 200, got %d: %s", removed.Code, removed.Body.String())
	}

	published := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases/"+createResponse.Release.ID+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish: expected 202, got %d: %s", published.Code, published.Body.String())
	}

	pods := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/pods", token, "")
	if pods.Code != http.StatusOK {
		t.Fatalf("list pods: expected 200, got %d: %s", pods.Code, pods.Body.String())
	}
	metrics := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/metrics", token, "")
	if metrics.Code != http.StatusOK {
		t.Fatalf("metrics: expected 200, got %d: %s", metrics.Code, metrics.Body.String())
	}

	// A completed release may be run again; this is the repeat-publish workflow.
	time.Sleep(1100 * time.Millisecond)
	repeated := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases/"+createResponse.Release.ID+"/publish", token, "")
	if repeated.Code != http.StatusAccepted {
		t.Fatalf("repeat publish: expected 202, got %d: %s", repeated.Code, repeated.Body.String())
	}
}

func TestCreateReleaseUsesSelectedBranchAndCommit(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"release/2026.01","commit_shas":["112233445566"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release from selected branch: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		Release struct {
			ID      string `json:"id"`
			Branch  string `json:"branch"`
			Commits []struct {
				SHA string `json:"sha"`
			} `json:"commits"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createResponse); err != nil {
		t.Fatal(err)
	}
	if createResponse.Release.Branch != "release/2026.01" {
		t.Fatalf("release branch = %q, want %q", createResponse.Release.Branch, "release/2026.01")
	}
	if createResponse.Release.ID == "" || len(createResponse.Release.Commits) != 1 || createResponse.Release.Commits[0].SHA != "112233445566" {
		t.Fatalf("unexpected selected-branch release: %#v", createResponse.Release)
	}

	published := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases/"+createResponse.Release.ID+"/publish", token, "")
	if published.Code != http.StatusAccepted {
		t.Fatalf("publish selected branch: expected 202, got %d: %s", published.Code, published.Body.String())
	}
	var publishResponse struct {
		Release struct {
			Branch  string `json:"branch"`
			Commits []struct {
				SHA string `json:"sha"`
			} `json:"commits"`
		} `json:"release"`
	}
	if err := json.Unmarshal(published.Body.Bytes(), &publishResponse); err != nil {
		t.Fatal(err)
	}
	if publishResponse.Release.Branch != "release/2026.01" || len(publishResponse.Release.Commits) != 1 || publishResponse.Release.Commits[0].SHA != "112233445566" {
		t.Fatalf("publish response lost selected branch or commit: %#v", publishResponse.Release)
	}
}

func TestCreateReleaseFallsBackToProjectDefaultBranch(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release with default branch: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var response struct {
		Release struct {
			Branch string `json:"branch"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Release.Branch != "main" {
		t.Fatalf("default release branch = %q, want %q", response.Release.Branch, "main")
	}
}

func TestReleaseProgressCancelAndSingleReleaseEndpoints(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
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
	releasePath := "/api/projects/reverse-lab/releases/" + createBody.Release.ID

	started := doRequest(t, handler, http.MethodPost, releasePath+"/publish", token, "")
	if started.Code != http.StatusAccepted || !strings.Contains(started.Body.String(), `"status":"running"`) {
		t.Fatalf("publish response should be running: %d %s", started.Code, started.Body.String())
	}

	progress := doRequest(t, handler, http.MethodPatch, releasePath+"/progress", token, `{"progress":33,"stage":"testing","message":"正在检查"}`)
	if progress.Code != http.StatusOK || !strings.Contains(progress.Body.String(), `"progress":33`) {
		t.Fatalf("progress endpoint: %d %s", progress.Code, progress.Body.String())
	}

	read := doRequest(t, handler, http.MethodGet, releasePath, token, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"id":"`+createBody.Release.ID+`"`) {
		t.Fatalf("single release endpoint: %d %s", read.Code, read.Body.String())
	}

	cancelled := doRequest(t, handler, http.MethodPost, releasePath+"/cancel", token, `{"message":"停止本次演示"}`)
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel endpoint: %d %s", cancelled.Code, cancelled.Body.String())
	}

	// The asynchronous executor must not overwrite an operator cancellation.
	time.Sleep(800 * time.Millisecond)
	final := doRequest(t, handler, http.MethodGet, releasePath, token, "")
	if final.Code != http.StatusOK || !strings.Contains(final.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancelled release was overwritten: %d %s", final.Code, final.Body.String())
	}
}

func TestReleaseFailEndpoint(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)
	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
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
		t.Fatalf("publish: expected 202, got %d: %s", response.Code, response.Body.String())
	}
	failed := doRequest(t, handler, http.MethodPost, path+"/fail", token, `{"error":"演示构建失败"}`)
	if failed.Code != http.StatusOK || !strings.Contains(failed.Body.String(), `"status":"failed"`) || !strings.Contains(failed.Body.String(), "演示构建失败") {
		t.Fatalf("fail endpoint: %d %s", failed.Code, failed.Body.String())
	}
}

func TestUpdateProjectPersistsRepositoryAndRuntimeSettings(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	updated := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab", token, `{"repository_url":"https://git.example.com/team/release-api.git","default_branch":"release","namespace":"staging","replicas":3,"container_port":9090}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update project: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	var body struct {
		RepositoryURL string `json:"repository_url"`
		DefaultBranch string `json:"default_branch"`
		Namespace     string `json:"namespace"`
		Replicas      int    `json:"replicas"`
		ContainerPort int    `json:"container_port"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RepositoryURL != "https://git.example.com/team/release-api.git" || body.DefaultBranch != "release" || body.Namespace != "staging" || body.Replicas != 3 || body.ContainerPort != 9090 {
		t.Fatalf("updated project fields were not persisted: %#v", body)
	}
	listed := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab", token, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "release-api.git") {
		t.Fatalf("updated project was not readable: %d %s", listed.Code, listed.Body.String())
	}
}

func TestCreateProjectDerivesRepositoryIDServerSide(t *testing.T) {
	provider := &recordingRepositoryProvider{Provider: git.NewDemoProvider()}
	server := New(Dependencies{
		Config:  config.Config{DemoMode: false, JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithAdminPassword("test-password"),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(nil),
	})
	token := loginForTest(t, server.Router())
	repositoryURL := "https://github.com/acme/server-owned-id"
	response := doRequest(t, server.Router(), http.MethodPost, "/api/projects", token, `{"name":"Server Owned Repository","repository_id":"client-supplied-id","repository_url":"https://github.com/acme/server-owned-id","default_branch":"main"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", response.Code, response.Body.String())
	}
	var body domain.Project
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	expectedID := repositoryID(repositoryURL)
	if body.RepositoryID != expectedID || body.RepositoryID == "client-supplied-id" {
		t.Fatalf("repository ID = %q, want server-derived %q", body.RepositoryID, expectedID)
	}
	if provider.registeredID != expectedID || provider.registeredURL != repositoryURL {
		t.Fatalf("registrar received ID=%q URL=%q", provider.registeredID, provider.registeredURL)
	}
}

type recordingRepositoryProvider struct {
	git.Provider
	registeredID  string
	registeredURL string
}

func (p *recordingRepositoryProvider) RegisterRepository(repositoryID, repositoryURL string) error {
	p.registeredID = repositoryID
	p.registeredURL = repositoryURL
	return nil
}

func TestAuditLogsAreSpaceScopedAndWrittenForMutations(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/projects", token, `{"name":"Audit Project","repository_url":"https://github.com/example/audit","default_branch":"main","namespace":"lab"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	read := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab", token, "")
	if read.Code != http.StatusOK {
		t.Fatalf("read project: expected 200, got %d: %s", read.Code, read.Body.String())
	}

	logs := doRequest(t, handler, http.MethodGet, "/api/audit-logs?limit=20", token, "")
	if logs.Code != http.StatusOK {
		t.Fatalf("list audit logs: expected 200, got %d: %s", logs.Code, logs.Body.String())
	}
	var body struct {
		Items []struct {
			SpaceID  string `json:"space_id"`
			UserName string `json:"user_name"`
			Action   string `json:"action"`
			Target   string `json:"target"`
		} `json:"items"`
	}
	if err := json.Unmarshal(logs.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].SpaceID != "space-lab" || body.Items[0].Action != "创建项目" || body.Items[0].Target != "Audit Project" || body.Items[0].UserName != "平台管理员" {
		t.Fatalf("unexpected audit logs: %#v", body.Items)
	}

	invalidLimit := doRequest(t, handler, http.MethodGet, "/api/audit-logs?limit=201", token, "")
	if invalidLimit.Code != http.StatusBadRequest {
		t.Fatalf("invalid audit limit: expected 400, got %d", invalidLimit.Code)
	}
}

func TestCreateReleaseRejectsUnknownBranchAndCommit(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	unknownBranch := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"release/missing","strategy":"rolling"}`)
	if unknownBranch.Code != http.StatusNotFound || !strings.Contains(unknownBranch.Body.String(), "not_found") {
		t.Fatalf("unknown branch: expected 404 not_found, got %d: %s", unknownBranch.Code, unknownBranch.Body.String())
	}

	unknownCommit := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"release/2026.01","commit_shas":["missing-commit"],"strategy":"rolling"}`)
	if unknownCommit.Code != http.StatusNotFound || !strings.Contains(unknownCommit.Body.String(), "not_found") {
		t.Fatalf("unknown commit: expected 404 not_found, got %d: %s", unknownCommit.Code, unknownCommit.Body.String())
	}
}

func TestProtectedRoutesRequireLogin(t *testing.T) {
	response := doRequest(t, testServer().Router(), http.MethodGet, "/api/projects", "", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestHTTPFlowCoversSpaceProjectGitReleaseAndPodRoutes(t *testing.T) {
	server := testServer()
	handler := server.Router()
	token := loginForTest(t, handler)

	checks := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/auth/me", ""},
		{http.MethodGet, "/api/spaces", ""},
		{http.MethodGet, "/api/clusters", ""},
		{http.MethodGet, "/api/projects/reverse-lab/git/branches", ""},
		{http.MethodGet, "/api/projects/reverse-lab/git/commits?branch=main", ""},
		{http.MethodGet, "/api/projects/reverse-lab/git/tags", ""},
		{http.MethodGet, "/api/projects/reverse-lab/pods", ""},
		{http.MethodGet, "/api/projects/reverse-lab/pods/reverse-lab-api-7d9f8c6d4b-x2k9m", ""},
		{http.MethodGet, "/api/projects/reverse-lab/pods/reverse-lab-api-7d9f8c6d4b-x2k9m/logs?container=api", ""},
		{http.MethodGet, "/api/projects/reverse-lab/metrics", ""},
		{http.MethodGet, "/api/clusters/demo-cluster/metrics", ""},
	}
	for _, check := range checks {
		response := doRequest(t, handler, check.method, check.path, token, check.body)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s: expected 200, got %d: %s", check.method, check.path, response.Code, response.Body.String())
		}
	}

	created := doRequest(t, handler, http.MethodPost, "/api/projects", token, `{"name":"Smoke Project","repository_url":"https://github.com/example/smoke","default_branch":"main","namespace":"lab"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	duplicate := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling","stable_percent":100,"candidate_percent":0}`)
	if duplicate.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
	var payload struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(duplicate.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Release.ID == "" {
		t.Fatal("release id is empty")
	}

	repeated := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling","stable_percent":100,"candidate_percent":0}`)
	if repeated.Code != http.StatusOK || !strings.Contains(repeated.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate release: expected 200 with duplicate=true, got %d: %s", repeated.Code, repeated.Body.String())
	}

	config := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/pods/reverse-lab-api-7d9f8c6d4b-x2k9m/config", token, `{"config":{"LOG_LEVEL":"debug"},"environment":{"TRACE":"1"}}`)
	if config.Code != http.StatusOK {
		t.Fatalf("update pod config: expected 200, got %d: %s", config.Code, config.Body.String())
	}
}

func loginForTest(t *testing.T, handler http.Handler) string {
	response := doRequest(t, handler, http.MethodPost, "/api/auth/login", "", `{"username":"admin","password":"test-password"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Fatal("login token is empty")
	}
	return body.Token
}

func doRequest(t *testing.T, handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
