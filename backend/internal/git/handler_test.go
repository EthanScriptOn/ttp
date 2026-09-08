package git

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDemoProviderQueries(t *testing.T) {
	provider := NewDemoProvider()
	branches, err := provider.ListBranches(context.Background(), "demo-repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected two branches, got %d", len(branches))
	}
	commits, err := provider.ListCommits(context.Background(), "demo-repo", "main", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].SHA != "a1b2c3d4e5f6" {
		t.Fatalf("unexpected commits: %#v", commits)
	}
}

func TestHandlerRequiresBranchForCommitQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/repositories/demo-repo/commits", nil)
	recorder := httptest.NewRecorder()
	NewHandler(NewDemoProvider()).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestHandlerReturnsCommit(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/repositories/demo-repo/commits/a1b2c3d4e5f6", nil)
	recorder := httptest.NewRecorder()
	NewHandler(NewDemoProvider()).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
}

func TestDemoProviderListsTags(t *testing.T) {
	tags, err := NewDemoProvider().ListTags(context.Background(), "demo-repo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Name != "v1.0.0" || tags[0].SHA != "a1b2c3d4e5f6" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}
