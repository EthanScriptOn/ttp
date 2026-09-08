package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func TestPrepareReleaseReturnsBaselineCheck(t *testing.T) {
	server := testServer()
	token := loginForTest(t, server.Router())

	ready := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/release-preparation", token, `{"source_branch":"main","base_branch":"main","selected_sha":"a1b2c3d4e5f6"}`)
	if ready.Code != http.StatusOK {
		t.Fatalf("same-branch preparation: expected 200, got %d: %s", ready.Code, ready.Body.String())
	}
	var readyBody struct {
		Preparation struct {
			Status     string `json:"status"`
			CanPublish bool   `json:"can_publish"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(ready.Body.Bytes(), &readyBody); err != nil {
		t.Fatal(err)
	}
	if readyBody.Preparation.Status != "ready" || !readyBody.Preparation.CanPublish {
		t.Fatalf("unexpected ready preparation: %#v", readyBody.Preparation)
	}

	needsMerge := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/release-preparation", token, `{"source_branch":"release/2026.01","base_branch":"main","selected_sha":"112233445566"}`)
	if needsMerge.Code != http.StatusOK {
		t.Fatalf("diverged preparation: expected 200, got %d: %s", needsMerge.Code, needsMerge.Body.String())
	}
	var mergeBody struct {
		Preparation struct {
			Status          string `json:"status"`
			CanPublish      bool   `json:"can_publish"`
			RequiresMerge   bool   `json:"requires_merge"`
			TemporaryBranch string `json:"temporary_branch"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(needsMerge.Body.Bytes(), &mergeBody); err != nil {
		t.Fatal(err)
	}
	if mergeBody.Preparation.Status != "needs_merge" || mergeBody.Preparation.CanPublish || !mergeBody.Preparation.RequiresMerge || mergeBody.Preparation.TemporaryBranch == "" {
		t.Fatalf("unexpected merge preparation: %#v", mergeBody.Preparation)
	}

	temporaryBranch := mergeBody.Preparation.TemporaryBranch
	open := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/release-preparation", token, `{"action":"merge","source_branch":"release/2026.01","base_branch":"main","selected_sha":"112233445566","temporary_branch":"`+temporaryBranch+`"}`)
	if open.Code != http.StatusOK {
		t.Fatalf("open merge workspace: expected 200, got %d: %s", open.Code, open.Body.String())
	}
	var conflictBody struct {
		Preparation struct {
			Status          string `json:"status"`
			CanPublish      bool   `json:"can_publish"`
			TemporaryBranch string `json:"temporary_branch"`
			ConflictFiles   []struct {
				Path string `json:"path"`
			} `json:"conflict_files"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(open.Body.Bytes(), &conflictBody); err != nil {
		t.Fatal(err)
	}
	if conflictBody.Preparation.Status != "conflict" || conflictBody.Preparation.CanPublish || conflictBody.Preparation.TemporaryBranch != temporaryBranch || len(conflictBody.Preparation.ConflictFiles) != 1 || conflictBody.Preparation.ConflictFiles[0].Path == "" {
		t.Fatalf("unexpected conflict workspace: %#v", conflictBody.Preparation)
	}

	resolve := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/release-preparation", token, `{"action":"resolve","source_branch":"release/2026.01","base_branch":"main","selected_sha":"112233445566","temporary_branch":"`+temporaryBranch+`","resolutions":[{"path":"deploy/application.yaml","content":"replicas: 2\nimageTag: release\n"}]}`)
	if resolve.Code != http.StatusOK {
		t.Fatalf("resolve merge workspace: expected 200, got %d: %s", resolve.Code, resolve.Body.String())
	}
	var resolvedBody struct {
		Preparation struct {
			Status        string `json:"status"`
			CanPublish    bool   `json:"can_publish"`
			ReleaseBranch string `json:"release_branch"`
			ReleaseSHA    string `json:"release_sha"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(resolve.Body.Bytes(), &resolvedBody); err != nil {
		t.Fatal(err)
	}
	if resolvedBody.Preparation.Status != "ready" || !resolvedBody.Preparation.CanPublish || resolvedBody.Preparation.ReleaseBranch != temporaryBranch || resolvedBody.Preparation.ReleaseSHA == "" {
		t.Fatalf("unexpected resolved preparation: %#v", resolvedBody.Preparation)
	}

	release := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"branch":"`+temporaryBranch+`","commit_shas":["`+resolvedBody.Preparation.ReleaseSHA+`"],"strategy":"rolling"}`)
	if release.Code != http.StatusCreated {
		t.Fatalf("create release from prepared branch: expected 201, got %d: %s", release.Code, release.Body.String())
	}
}

func TestPrepareMergeRequiresMergeAccess(t *testing.T) {
	base := git.NewDemoProvider()
	provider := mergePermissionProvider{Provider: base}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())

	response := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/release-preparation", token, `{"action":"merge","source_branch":"release/2026.01","base_branch":"main","selected_sha":"112233445566"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("merge without merge access: expected 403, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"git_write_access_denied"`) {
		t.Fatalf("unexpected merge denial: %s", response.Body.String())
	}
}

func TestPrepareMergeReadyBaselineDoesNotRequireMergeAccess(t *testing.T) {
	base := git.NewDemoProvider()
	provider := mergePermissionProvider{Provider: base}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())

	response := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/release-preparation", token, `{"action":"merge","source_branch":"main","base_branch":"main","selected_sha":"a1b2c3d4e5f6"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("ready merge preparation: expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Preparation struct {
			Status        string `json:"status"`
			CanPublish    bool   `json:"can_publish"`
			RequiresMerge bool   `json:"requires_merge"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Preparation.Status != "ready" || !body.Preparation.CanPublish || body.Preparation.RequiresMerge {
		t.Fatalf("unexpected ready merge preparation: %#v", body.Preparation)
	}
}

type mergePermissionProvider struct {
	git.Provider
}

func (mergePermissionProvider) CheckRepositoryAccess(_ context.Context, _ string) (git.RepositoryAccess, error) {
	return git.RepositoryAccess{
		RepositoryID:             "demo-repo",
		Supported:                true,
		Authenticated:            true,
		RepositoryFound:          true,
		AccountMatches:           true,
		CanWrite:                 true,
		CanCreateTemporaryBranch: true,
		CanMerge:                 false,
		RequiredMergePermission:  "Maintainer",
	}, nil
}
