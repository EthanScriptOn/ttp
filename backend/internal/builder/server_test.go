package builder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
)

type fakeExecutor struct {
	result imagebuild.Result
	err    error
}

func (f fakeExecutor) Build(context.Context, imagebuild.Request) (imagebuild.Result, error) {
	return f.result, f.err
}

func TestServerAuthenticatesBuildRequests(t *testing.T) {
	server, err := NewServer("builder-token", fakeExecutor{result: imagebuild.Result{Image: "registry.example.com/team/service@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", strings.NewReader(`{"request":{"project_id":"project-1","release_id":"release-1","repository_url":"https://git.example.com/team/service.git","commit_sha":"0123456789abcdef0123456789abcdef01234567"}}`))
	request.Header.Set("Authorization", "Bearer builder-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestServerReturnsSanitizedFailureLogs(t *testing.T) {
	logs := []imagebuild.LogEntry{{Stream: "stderr", Level: "ERROR", Line: "BuildKit failed"}}
	server, err := NewServer("builder-token", fakeExecutor{err: &imagebuild.Failure{Err: errors.New("BuildKit failed"), Logs: logs}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/builds", strings.NewReader(`{"request":{"project_id":"project-1","release_id":"release-1","repository_url":"https://git.example.com/team/service.git","commit_sha":"0123456789abcdef0123456789abcdef01234567"}}`))
	request.Header.Set("Authorization", "Bearer builder-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Logs []imagebuild.LogEntry `json:"logs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Logs) != 1 || response.Logs[0].Line != "BuildKit failed" {
		t.Fatalf("unexpected logs: %#v", response.Logs)
	}
}
