package git

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestRegistryProviderRegistersRepositoriesAndDelegates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/acme/widget/branches" {
			writeTestJSON(t, w, []map[string]any{{"name": "main", "commit": map[string]string{"sha": "abc123456789"}}})
			return
		}
		if r.URL.Path == "/api/v3/repos/acme/widget" {
			writeTestJSON(t, w, map[string]any{"name": "widget", "default_branch": "main"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	registry, err := NewRegistry(RegistryConfig{
		Provider:     "github",
		Token:        "registry-token",
		APIBaseURL:   server.URL + "/api/v3",
		AllowedHosts: []string{server.Listener.Addr().String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	const repositoryID = "repo-registered"
	if err := registry.RegisterRepository(repositoryID, server.URL+"/acme/widget.git"); err != nil {
		t.Fatal(err)
	}
	branches, err := registry.ListBranches(context.Background(), repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || branches[0].Head.SHA != "abc123456789" {
		t.Fatalf("unexpected branches: %#v", branches)
	}
	if _, err := registry.ListBranches(context.Background(), "missing"); err != ErrRepositoryNotFound {
		t.Fatalf("missing repository error = %v", err)
	}
	if err := registry.RegisterRepository(repositoryID, server.URL+"/acme/widget.git"); err != nil {
		t.Fatalf("idempotent registration: %v", err)
	}
}

func TestRegistryProviderRejectsRepositoryIDRebinding(t *testing.T) {
	registry, err := NewRegistry(RegistryConfig{
		Provider:     "github",
		Token:        "registry-token",
		AllowedHosts: []string{"github.com", "api.github.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	const repositoryID = "stable-repository-id"
	firstURL := "https://github.com/acme/widget.git"
	secondURL := "https://github.com/acme/other.git"
	if err := registry.RegisterRepository(repositoryID, firstURL); err != nil {
		t.Fatal(err)
	}
	err = registry.RegisterRepository(repositoryID, secondURL)
	if !errors.Is(err, ErrRepositoryConflict) {
		t.Fatalf("rebind error = %v, want ErrRepositoryConflict", err)
	}
	registry.mu.RLock()
	registeredURL := registry.urls[repositoryID]
	registry.mu.RUnlock()
	if registeredURL != firstURL {
		t.Fatalf("registered URL changed after rejected rebind: %q", registeredURL)
	}
}

func TestRegistryProviderKeepsCallerRepositoryIDInAccessReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/user":
			writeTestJSON(t, w, map[string]string{"login": "cicd-bot"})
		case "/api/v3/repos/acme/widget":
			writeTestJSON(t, w, map[string]any{"permissions": map[string]bool{"pull": true, "push": true}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry, err := NewRegistry(RegistryConfig{
		Provider:     "github",
		Token:        "registry-token",
		APIBaseURL:   server.URL + "/api/v3",
		AllowedHosts: []string{server.Listener.Addr().String()},
		ServiceAccount: ServiceAccount{
			Username: "cicd-bot",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const repositoryID = "project-repository-id"
	if err := registry.RegisterRepository(repositoryID, server.URL+"/acme/widget.git"); err != nil {
		t.Fatal(err)
	}
	report, err := registry.CheckRepositoryAccess(context.Background(), repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if report.RepositoryID != repositoryID {
		t.Fatalf("repository ID = %q, want %q", report.RepositoryID, repositoryID)
	}
	if report.RepositoryURL != server.URL+"/acme/widget" {
		t.Fatalf("repository URL = %q, want canonical URL", report.RepositoryURL)
	}
}

func mustParseTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	value, err := parseHTTPURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRegistryProviderAutoDetectsPublicHosts(t *testing.T) {
	registry, err := NewRegistry(RegistryConfig{Provider: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.kindForURL(mustParseTestURL(t, "https://github.com/acme/widget")); err != nil {
		t.Fatalf("GitHub auto detection: %v", err)
	}
	if _, err := registry.kindForURL(mustParseTestURL(t, "https://gitlab.com/group/widget")); err != nil {
		t.Fatalf("GitLab auto detection: %v", err)
	}
	if _, err := registry.kindForURL(mustParseTestURL(t, "https://git.example.com/group/widget")); err == nil {
		t.Fatal("custom host should require an explicit provider")
	}
}
