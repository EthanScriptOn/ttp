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

func TestGitAccessEndpointsExposeSafeServiceAccount(t *testing.T) {
	server := testServer()
	token := loginForTest(t, server.Router())

	accountResponse := doRequest(t, server.Router(), http.MethodGet, "/api/git/service-account", token, "")
	if accountResponse.Code != http.StatusOK {
		t.Fatalf("service account: expected 200, got %d: %s", accountResponse.Code, accountResponse.Body.String())
	}
	var accountBody struct {
		Account git.ServiceAccount `json:"account"`
	}
	if err := json.Unmarshal(accountResponse.Body.Bytes(), &accountBody); err != nil {
		t.Fatal(err)
	}
	if accountBody.Account.Username != "cicd-bot" || accountBody.Account.Provider != "demo" || !accountBody.Account.Configured {
		t.Fatalf("unexpected service account: %#v", accountBody.Account)
	}

	accessResponse := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/access", token, "")
	if accessResponse.Code != http.StatusOK {
		t.Fatalf("repository access: expected 200, got %d: %s", accessResponse.Code, accessResponse.Body.String())
	}
	var accessBody struct {
		Access git.RepositoryAccess `json:"access"`
	}
	if err := json.Unmarshal(accessResponse.Body.Bytes(), &accessBody); err != nil {
		t.Fatal(err)
	}
	if !accessBody.Access.Usable || !accessBody.Access.CanWrite || accessBody.Access.AuthenticatedUsername != "cicd-bot" {
		t.Fatalf("unexpected repository access: %#v", accessBody.Access)
	}
}

func TestPublishIsBlockedWhenProviderCannotVerifyServiceAccount(t *testing.T) {
	base := git.NewDemoProvider()
	provider := readOnlyProvider{Provider: base}
	server := New(Dependencies{
		Config:  config.Config{DemoMode: true, JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithAdminPassword("test-password"),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(nil),
	})
	token := loginForTest(t, server.Router())

	created := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases", token, `{"commit_shas":["a1b2c3d4e5f6"],"strategy":"rolling"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create release: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var body struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	published := doRequest(t, server.Router(), http.MethodPost, "/api/projects/reverse-lab/releases/"+body.Release.ID+"/publish", token, "")
	if published.Code != http.StatusForbidden {
		t.Fatalf("publish: expected 403, got %d: %s", published.Code, published.Body.String())
	}
	if published.Body.String() == "" || !strings.Contains(published.Body.String(), `"code":"git_write_access_denied"`) {
		t.Fatalf("unexpected publish denial: %s", published.Body.String())
	}
}

func TestGitAccessEndpointReportsTimeoutSeparatelyFromPermissionDenial(t *testing.T) {
	base := git.NewDemoProvider()
	provider := timeoutAccessProvider{Provider: base}
	server := New(Dependencies{
		Config:  config.Config{DemoMode: true, JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithAdminPassword("test-password"),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(nil),
	})
	token := loginForTest(t, server.Router())
	response := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/access", token, "")
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("access check: expected 504, got %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "git_write_access_denied") {
		t.Fatalf("timeout was reported as permission denial: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "git_access_check_timeout") {
		t.Fatalf("unexpected timeout response: %s", response.Body.String())
	}
}

// Embedding the Provider interface deliberately does not promote optional
// access-checking methods from the concrete demo provider.
type readOnlyProvider struct {
	git.Provider
}

type timeoutAccessProvider struct {
	git.Provider
}

func (timeoutAccessProvider) CheckRepositoryAccess(context.Context, string) (git.RepositoryAccess, error) {
	return git.RepositoryAccess{}, context.DeadlineExceeded
}
