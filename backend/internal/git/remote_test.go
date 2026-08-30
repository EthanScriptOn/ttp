package git

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitLabProviderReadsRepositoryBranchesAndPaginatedCommits(t *testing.T) {
	const token = "gitlab-test-token"
	var commitPageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != token {
			t.Errorf("PRIVATE-TOKEN = %q, want %q", got, token)
		}
		if strings.Contains(r.URL.RawQuery, token) {
			t.Errorf("token leaked into query: %q", r.URL.RawQuery)
		}
		switch {
		case r.URL.EscapedPath() == "/api/v4/projects/group%2Fteam%2Fwidget":
			writeTestJSON(t, w, map[string]any{"name": "widget", "default_branch": "main"})
		case r.URL.EscapedPath() == "/api/v4/projects/group%2Fteam%2Fwidget/repository/branches":
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "100" {
				t.Errorf("unexpected branch pagination: %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, []map[string]any{{
				"name":   "main",
				"commit": map[string]any{"id": "1234567890abcdef", "short_id": "1234567", "message": "initial", "author_name": "Ada", "authored_date": "2026-08-26T08:00:00Z"},
			}})
		case r.URL.EscapedPath() == "/api/v4/projects/group%2Fteam%2Fwidget/repository/commits":
			if r.URL.Query().Get("ref_name") != "release/v1" {
				t.Errorf("ref_name = %q, want release/v1", r.URL.Query().Get("ref_name"))
			}
			if r.URL.Query().Get("per_page") != "2" {
				t.Errorf("per_page = %q, want 2", r.URL.Query().Get("per_page"))
			}
			page := r.URL.Query().Get("page")
			commitPageRequests.Add(1)
			switch page {
			case "1":
				w.Header().Set("X-Next-Page", "2")
				writeTestJSON(t, w, []map[string]any{{"id": "aaaaaaaaaaaaaaaa", "short_id": "aaaaaaa", "message": "one", "author_name": "Ada", "authored_date": "2026-08-26T08:00:00Z"}})
			case "2":
				writeTestJSON(t, w, []map[string]any{{"id": "bbbbbbbbbbbbbbbb", "message": "two", "author_name": "Lin", "authored_date": "2026-08-26T09:00:00Z"}})
			default:
				t.Errorf("unexpected commit page %q", page)
				writeTestJSON(t, w, []any{})
			}
		case r.URL.EscapedPath() == "/api/v4/projects/group%2Fteam%2Fwidget/repository/commits/aaaaaaaaaaaaaaaa":
			writeTestJSON(t, w, map[string]any{"id": "aaaaaaaaaaaaaaaa", "message": "one", "author_name": "Ada", "authored_date": "2026-08-26T08:00:00Z"})
		case r.URL.EscapedPath() == "/api/v4/projects/group%2Fteam%2Fwidget/repository/tags":
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "2" {
				t.Errorf("unexpected tag pagination: %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, []map[string]any{{"name": "v1.0.0", "commit": map[string]string{"id": "aaaaaaaaaaaaaaaa"}}, {"name": "release", "commit": map[string]string{"id": "bbbbbbbbbbbbbbbb"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := NewGitLabProvider(
		server.URL+"/group/team/widget.git",
		token,
		WithAPIBaseURL(server.URL+"/api/v4"),
		WithAllowedHosts(server.Listener.Addr().String()),
	)
	if err != nil {
		t.Fatal(err)
	}

	repositories, err := provider.ListRepositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].DefaultBranch != "main" {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}

	branches, err := provider.ListBranches(context.Background(), repositories[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || !branches[0].IsHead || branches[0].Head.SHA != "1234567890abcdef" {
		t.Fatalf("unexpected branches: %#v", branches)
	}

	commits, err := provider.ListCommits(context.Background(), repositories[0].ID, "release/v1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].SHA != "aaaaaaaaaaaaaaaa" || commits[1].SHA != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("unexpected commits: %#v", commits)
	}
	if got := commitPageRequests.Load(); got != 2 {
		t.Fatalf("commit page requests = %d, want 2", got)
	}

	commit, err := provider.GetCommit(context.Background(), repositories[0].ID, commits[0].SHA)
	if err != nil {
		t.Fatal(err)
	}
	if commit.Author != "Ada" || commit.Message != "one" {
		t.Fatalf("unexpected commit: %#v", commit)
	}
	tags, err := provider.ListTags(context.Background(), repositories[0].ID, 2)
	if err != nil || len(tags) != 2 || tags[0].Name != "v1.0.0" || tags[0].SHA != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected GitLab tags: %#v, err=%v", tags, err)
	}
}

func TestGitHubProviderReadsBranchesAndLinkPaginatedCommits(t *testing.T) {
	const token = "github-test-token"
	var commitPageRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version = %q", got)
		}
		switch {
		case r.URL.EscapedPath() == "/api/v3/repos/acme/widget":
			writeTestJSON(t, w, map[string]any{"name": "widget", "default_branch": "main", "html_url": "https://github.com/acme/widget"})
		case r.URL.EscapedPath() == "/api/v3/repos/acme/widget/branches":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", "<"+server.URL+"/api/v3/repos/acme/widget/branches?page=2&per_page=100>; rel=\"next\"")
				writeTestJSON(t, w, []map[string]any{{"name": "main", "commit": map[string]string{"sha": "111111111111"}}})
				return
			}
			writeTestJSON(t, w, []map[string]any{{"name": "release/v1", "commit": map[string]string{"sha": "222222222222"}}})
		case r.URL.EscapedPath() == "/api/v3/repos/acme/widget/commits":
			commitPageRequests.Add(1)
			if r.URL.Query().Get("sha") != "release/v1" || r.URL.Query().Get("per_page") != "1" {
				t.Errorf("unexpected commit query: %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, []map[string]any{{
				"sha":    "333333333333",
				"commit": map[string]any{"message": "release", "author": map[string]string{"name": "Grace", "date": "2026-08-26T10:00:00Z"}},
			}})
		case r.URL.EscapedPath() == "/api/v3/repos/acme/widget/commits/333333333333":
			writeTestJSON(t, w, map[string]any{"sha": "333333333333", "commit": map[string]any{"message": "release", "author": map[string]string{"name": "Grace", "date": "2026-08-26T10:00:00Z"}}})
		case r.URL.EscapedPath() == "/api/v3/repos/acme/widget/tags":
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "2" {
				t.Errorf("unexpected tag pagination: %s", r.URL.RawQuery)
			}
			writeTestJSON(t, w, []map[string]any{{"name": "v2.0.0", "commit": map[string]string{"sha": "333333333333"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := NewGitHubProvider(
		server.URL+"/acme/widget.git",
		token,
		WithAPIBaseURL(server.URL+"/api/v3"),
		WithAllowedHosts(server.Listener.Addr().String()),
	)
	if err != nil {
		t.Fatal(err)
	}
	repositories, err := provider.ListRepositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	branches, err := provider.ListBranches(context.Background(), repositories[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || !branches[0].IsHead || branches[1].Head.SHA != "222222222222" {
		t.Fatalf("unexpected branches: %#v", branches)
	}
	commits, err := provider.ListCommits(context.Background(), repositories[0].ID, "release/v1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Author != "Grace" || commits[0].ShortSHA != "3333333" {
		t.Fatalf("unexpected commits: %#v", commits)
	}
	if commitPageRequests.Load() != 1 {
		t.Fatalf("commit page requests = %d, want 1", commitPageRequests.Load())
	}
	tags, err := provider.ListTags(context.Background(), repositories[0].ID, 2)
	if err != nil || len(tags) != 1 || tags[0].Name != "v2.0.0" || tags[0].SHA != "333333333333" {
		t.Fatalf("unexpected GitHub tags: %#v, err=%v", tags, err)
	}
}

func TestHTTPProviderRejectsUnsafeHostsAndRedirects(t *testing.T) {
	if _, err := NewGitHubProvider("file:///etc/passwd", "secret"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("unsafe scheme error = %v", err)
	}
	if _, err := NewGitHubProvider("https://example.test/acme/widget", "secret"); !errors.Is(err, ErrInvalidProviderConfig) {
		t.Fatalf("unallowlisted host error = %v", err)
	}

	var redirected atomic.Bool
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/api/v3/repos/acme/widget/commits/abc", http.StatusFound)
	}))
	defer first.Close()
	provider, err := NewGitHubProvider(
		first.URL+"/acme/widget",
		"redirect-secret",
		WithAPIBaseURL(first.URL+"/api/v3"),
		WithAllowedHosts(first.Listener.Addr().String()),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.GetCommit(context.Background(), first.URL+"/acme/widget", "abc")
	if err == nil || redirected.Load() {
		t.Fatalf("redirect was followed or unexpectedly succeeded: err=%v redirected=%v", err, redirected.Load())
	}
	if strings.Contains(err.Error(), "redirect-secret") {
		t.Fatalf("token leaked in redirect error: %v", err)
	}
}

func TestHTTPProviderMapsStatusAndTimeoutWithoutResponseBody(t *testing.T) {
	const secret = "body-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("this body must not appear: " + secret))
	}))
	defer server.Close()
	provider, err := NewGitLabProvider(
		server.URL+"/group/widget",
		secret,
		WithAPIBaseURL(server.URL+"/api/v4"),
		WithAllowedHosts(server.Listener.Addr().String()),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.ListRepositories(context.Background())
	var statusErr *ProviderHTTPError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status error = %T %v", err, err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "this body") {
		t.Fatalf("response contents leaked in error: %v", err)
	}

	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer timeoutServer.Close()
	timeoutProvider, err := NewGitLabProvider(
		timeoutServer.URL+"/group/widget",
		"",
		WithAPIBaseURL(timeoutServer.URL+"/api/v4"),
		WithAllowedHosts(timeoutServer.Listener.Addr().String()),
		WithTimeout(20*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = timeoutProvider.ListRepositories(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %T %v", err, err)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}
