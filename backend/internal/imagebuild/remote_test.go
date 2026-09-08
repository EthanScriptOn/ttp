package imagebuild

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
