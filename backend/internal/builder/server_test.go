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

type streamingFakeExecutor struct {
	result imagebuild.Result
	logs   []imagebuild.LogEntry
}

func (f streamingFakeExecutor) Build(context.Context, imagebuild.Request) (imagebuild.Result, error) {
	return f.result, nil
}

func (f streamingFakeExecutor) BuildWithLogs(_ context.Context, _ imagebuild.Request, onLog imagebuild.LogFunc) (imagebuild.Result, error) {
	for _, entry := range f.logs {
		onLog(entry)
	}
	f.result.Logs = append([]imagebuild.LogEntry(nil), f.logs...)
	return f.result, nil
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

func TestServerStreamsBuildLogsBeforeResult(t *testing.T) {
	logs := []imagebuild.LogEntry{
		{Stream: "stdout", Level: "INFO", Line: "step 1"},
		{Stream: "stdout", Level: "INFO", Line: "step 2"},
	}
	server, err := NewServer("builder-token", streamingFakeExecutor{
		result: imagebuild.Result{Image: "registry.example.com/team/service@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		logs:   logs,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/builds/stream", strings.NewReader(`{"request":{"project_id":"project-1","release_id":"release-1","repository_url":"https://git.example.com/team/service.git","commit_sha":"0123456789abcdef0123456789abcdef01234567"}}`))
	request.Header.Set("Authorization", "Bearer builder-token")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected two log events and one result, got %d: %s", len(lines), recorder.Body.String())
	}
	if !strings.Contains(lines[0], `"type":"log"`) || !strings.Contains(lines[0], `"line":"step 1"`) {
		t.Fatalf("unexpected first stream event: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"line":"step 2"`) {
		t.Fatalf("unexpected second stream event: %s", lines[1])
	}
	if !strings.Contains(lines[2], `"type":"result"`) {
		t.Fatalf("unexpected terminal stream event: %s", lines[2])
	}
}
