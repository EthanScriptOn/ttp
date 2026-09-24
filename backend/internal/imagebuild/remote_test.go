package imagebuild

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func validRequest() Request {
	return Request{
		ProjectID: "project-1", ReleaseID: "release-1", RepositoryURL: "https://git.example.com/team/service.git",
		CommitSHA:        "0123456789abcdef0123456789abcdef01234567",
		SourceCredential: SourceCredential{Username: "build-bot", Token: "source-token"},
	}
}

func TestRemoteBuildPassesRequestAndRequiresDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer builder-token" {
			t.Fatalf("unexpected authorization header")
		}
		var payload struct {
			Request Request `json:"request"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Request.CommitSHA != validRequest().CommitSHA || payload.Request.SourceCredential.Token != "source-token" {
			t.Fatalf("unexpected build request: %#v", payload.Request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": Result{Image: "registry.example.com/team/service@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	}))
	defer server.Close()

	builder, err := NewRemote(RemoteConfig{URL: server.URL, Token: "builder-token"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := builder.Build(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest == "" || result.Image == "" {
		t.Fatalf("expected immutable result, got %#v", result)
	}
}

func TestRemoteRejectsNonLoopbackHTTP(t *testing.T) {
	if _, err := NewRemote(RemoteConfig{URL: "http://builder.example.com", Token: "token"}); err == nil {
		t.Fatal("expected non-loopback HTTP URL to be rejected")
	}
}

func TestRemoteBuildWithLogsForwardsIncrementalEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/builds/stream" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		encoder := json.NewEncoder(w)
		_ = encoder.Encode(map[string]any{"type": "log", "log": LogEntry{Stream: "stdout", Level: "INFO", Line: "step 1"}})
		flusher.Flush()
		_ = encoder.Encode(map[string]any{"type": "log", "log": LogEntry{Stream: "stdout", Level: "INFO", Line: "step 2"}})
		flusher.Flush()
		_ = encoder.Encode(map[string]any{"type": "result", "result": Result{Image: "registry.example.com/team/service@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
		flusher.Flush()
	}))
	defer server.Close()

	builder, err := NewRemote(RemoteConfig{URL: server.URL, Token: "builder-token"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var logs []LogEntry
	result, err := builder.BuildWithLogs(context.Background(), validRequest(), func(entry LogEntry) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, entry)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest == "" || len(logs) != 2 {
		t.Fatalf("expected immutable result and two streamed logs, result=%#v logs=%#v", result, logs)
	}
	if logs[0].Line != "step 1" || logs[1].Line != "step 2" {
		t.Fatalf("unexpected streamed logs: %#v", logs)
	}
}

func TestRemoteBuildWithLogsFallsBackToLegacyBuilder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/builds/stream" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": Result{
				Image:  "registry.example.com/team/service@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				Logs:   []LogEntry{{Stream: "stdout", Level: "INFO", Line: "legacy output"}},
			},
		})
	}))
	defer server.Close()

	builder, err := NewRemote(RemoteConfig{URL: server.URL, Token: "builder-token"})
	if err != nil {
		t.Fatal(err)
	}
	var logs []LogEntry
	result, err := builder.BuildWithLogs(context.Background(), validRequest(), func(entry LogEntry) {
		logs = append(logs, entry)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest == "" || len(logs) != 1 || logs[0].Line != "legacy output" {
		t.Fatalf("unexpected legacy fallback result=%#v logs=%#v", result, logs)
	}
}
