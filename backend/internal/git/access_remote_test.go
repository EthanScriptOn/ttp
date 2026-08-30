package git

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubServiceAccountAccessCheck(t *testing.T) {
	const token = "github-access-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.EscapedPath() {
		case "/api/v3/user":
			writeTestJSON(t, w, map[string]string{"login": "cicd-bot"})
		case "/api/v3/repos/acme/widget":
			writeTestJSON(t, w, map[string]any{"permissions": map[string]bool{"pull": true, "push": true}})
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
		WithServiceAccount(ServiceAccount{Username: "cicd-bot", DisplayName: "发布机器人"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := provider.CheckRepositoryAccess(context.Background(), provider.remote.repository.canonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Supported || !report.Authenticated || !report.RepositoryFound || !report.AccountMatches || !report.CanWrite || !report.CanCreateTemporaryBranch || !report.Usable {
		t.Fatalf("unexpected GitHub access report: %#v", report)
	}
	if report.Permission != "Write" || report.CanMerge {
		t.Fatalf("unexpected GitHub permissions: %#v", report)
	}
	if report.AuthenticatedUsername != "cicd-bot" {
		t.Fatalf("authenticated username = %q", report.AuthenticatedUsername)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatalf("token leaked in access report: %s", encoded)
	}
}

func TestGitHubAccessCheckRejectsMismatchedAccountAndReadOnlyPermission(t *testing.T) {
	tests := []struct {
		name          string
		login         string
		permissions   map[string]bool
		wantRepoCheck bool
		wantMessage   string
	}{
		{name: "account mismatch", login: "developer", permissions: map[string]bool{"pull": true, "push": true}, wantRepoCheck: false, wantMessage: "当前 Token 属于"},
		{name: "read only", login: "cicd-bot", permissions: map[string]bool{"pull": true}, wantRepoCheck: true, wantMessage: "至少需要 Write"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repoChecked := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.EscapedPath() {
				case "/api/v3/user":
					writeTestJSON(t, w, map[string]string{"login": test.login})
				case "/api/v3/repos/acme/widget":
					repoChecked = true
					writeTestJSON(t, w, map[string]any{"permissions": test.permissions})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			provider, err := NewGitHubProvider(
				server.URL+"/acme/widget",
				"access-token",
				WithAPIBaseURL(server.URL+"/api/v3"),
				WithAllowedHosts(server.Listener.Addr().String()),
				WithServiceAccount(ServiceAccount{Username: "cicd-bot"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			report, err := provider.CheckRepositoryAccess(context.Background(), provider.remote.repository.canonicalURL)
			if err != nil {
				t.Fatal(err)
			}
			if report.Usable || report.CanWrite || report.AccountMatches != (test.login == "cicd-bot") {
				t.Fatalf("unexpected rejected report: %#v", report)
			}
			if repoChecked != test.wantRepoCheck || !strings.Contains(report.Message, test.wantMessage) {
				t.Fatalf("repoChecked=%v report=%#v", repoChecked, report)
			}
		})
	}
}

func TestGitLabServiceAccountAccessCheckAcceptsDeveloper(t *testing.T) {
	const token = "gitlab-access-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != token {
			t.Errorf("PRIVATE-TOKEN = %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/user":
			writeTestJSON(t, w, map[string]string{"username": "cicd-bot"})
		case "/api/v4/projects/group%2Fteam%2Fwidget":
			writeTestJSON(t, w, map[string]any{"permissions": map[string]any{"project_access": map[string]int{"access_level": 30}}})
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
		WithServiceAccount(ServiceAccount{Username: "cicd-bot"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := provider.CheckRepositoryAccess(context.Background(), provider.remote.repository.canonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	if report.Permission != "Developer" || !report.CanWrite || !report.CanCreateTemporaryBranch || report.CanMerge || !report.Usable {
		t.Fatalf("unexpected GitLab access report: %#v", report)
	}
}

func TestRequireRepositoryWriteAccessRejectsProviderWithoutChecker(t *testing.T) {
	provider := readOnlyTestProvider{}
	if err := RequireRepositoryWriteAccess(context.Background(), provider, "repo"); err == nil || !strings.Contains(err.Error(), ErrGitWriteAccessDenied.Error()) {
		t.Fatalf("expected write access denial, got %v", err)
	}
}

func TestRequireRepositoryMergeAccessRequiresEveryMergeCapability(t *testing.T) {
	base := RepositoryAccess{
		RepositoryID:             "repo",
		Supported:                true,
		Authenticated:            true,
		RepositoryFound:          true,
		AccountMatches:           true,
		CanWrite:                 true,
		CanCreateTemporaryBranch: true,
		CanMerge:                 true,
	}
	tests := []struct {
		name   string
		mutate func(*RepositoryAccess)
	}{
		{name: "unsupported", mutate: func(report *RepositoryAccess) { report.Supported = false }},
		{name: "unauthenticated", mutate: func(report *RepositoryAccess) { report.Authenticated = false }},
		{name: "repository missing", mutate: func(report *RepositoryAccess) { report.RepositoryFound = false }},
		{name: "account mismatch", mutate: func(report *RepositoryAccess) { report.AccountMatches = false }},
		{name: "read only", mutate: func(report *RepositoryAccess) { report.CanWrite = false }},
		{name: "cannot create temporary branch", mutate: func(report *RepositoryAccess) { report.CanCreateTemporaryBranch = false }},
		{name: "cannot merge", mutate: func(report *RepositoryAccess) { report.CanMerge = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := base
			test.mutate(&report)
			provider := accessReportProvider{Provider: readOnlyTestProvider{}, report: report}
			err := RequireRepositoryMergeAccess(context.Background(), provider, "repo")
			var denied *AccessDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected AccessDeniedError, got %v", err)
			}
			if !errors.Is(err, ErrGitWriteAccessDenied) {
				t.Fatalf("expected write access sentinel, got %v", err)
			}
		})
	}
}

func TestRequireRepositoryMergeAccessAcceptsCompleteReport(t *testing.T) {
	provider := accessReportProvider{
		Provider: readOnlyTestProvider{},
		report: RepositoryAccess{
			RepositoryID:             "repo",
			Supported:                true,
			Authenticated:            true,
			RepositoryFound:          true,
			AccountMatches:           true,
			CanWrite:                 true,
			CanCreateTemporaryBranch: true,
			CanMerge:                 true,
		},
	}
	if err := RequireRepositoryMergeAccess(context.Background(), provider, "repo"); err != nil {
		t.Fatalf("complete merge access report rejected: %v", err)
	}
}

func TestRequireRepositoryWriteAccessSeparatesAccessCheckFailure(t *testing.T) {
	provider := accessCheckErrorProvider{cause: context.DeadlineExceeded}
	err := RequireRepositoryWriteAccess(context.Background(), provider, "repo")
	if err == nil {
		t.Fatal("expected access check error")
	}
	if !errors.Is(err, ErrGitAccessCheckFailed) {
		t.Fatalf("error does not identify access check failure: %v", err)
	}
	if errors.Is(err, ErrGitWriteAccessDenied) {
		t.Fatalf("transport failure must not be reported as permission denial: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline cause was not preserved: %v", err)
	}
}

func TestRequireRepositoryWriteAccessRejectsUnconfiguredRemoteAccount(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	serverURL := server.URL
	server.Close()
	provider, err := NewGitHubProvider(
		serverURL+"/acme/widget",
		"",
		WithAllowedHosts(strings.TrimPrefix(serverURL, "http://")),
		WithAPIBaseURL(serverURL+"/api/v3"),
		WithServiceAccount(ServiceAccount{Username: "cicd-bot"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = RequireRepositoryWriteAccess(context.Background(), provider, provider.remote.repository.canonicalURL)
	if err == nil || !errors.Is(err, ErrGitWriteAccessDenied) {
		t.Fatalf("expected unconfigured account denial, got %v", err)
	}
	if errors.Is(err, ErrGitAccessCheckFailed) {
		t.Fatalf("missing token is a permission/configuration denial, got access-check failure: %v", err)
	}
}

type accessCheckErrorProvider struct {
	Provider
	cause error
}

func (p accessCheckErrorProvider) CheckRepositoryAccess(context.Context, string) (RepositoryAccess, error) {
	return RepositoryAccess{}, p.cause
}

type accessReportProvider struct {
	Provider
	report RepositoryAccess
}

func (p accessReportProvider) CheckRepositoryAccess(context.Context, string) (RepositoryAccess, error) {
	return p.report, nil
}

type readOnlyTestProvider struct{}

func (readOnlyTestProvider) ListRepositories(context.Context) ([]Repository, error) { return nil, nil }
func (readOnlyTestProvider) ListBranches(context.Context, string) ([]Branch, error) { return nil, nil }
func (readOnlyTestProvider) ListCommits(context.Context, string, string, int) ([]Commit, error) {
	return nil, nil
}
func (readOnlyTestProvider) GetCommit(context.Context, string, string) (Commit, error) {
	return Commit{}, nil
}
