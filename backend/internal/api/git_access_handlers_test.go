package api

import (
	"context"
	"encoding/json"
	"errors"
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

func TestGitAccessEndpointsExposeProjectCredentialMetadata(t *testing.T) {
	server := testServer()
	token := loginForTest(t, server.Router())

	credentialResponse := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/credential", token, "")
	if credentialResponse.Code != http.StatusOK {
		t.Fatalf("project credential: expected 200, got %d: %s", credentialResponse.Code, credentialResponse.Body.String())
	}
	var credentialBody struct {
		Credential struct {
			Configured bool   `json:"configured"`
			Username   string `json:"username"`
		} `json:"credential"`
	}
	if err := json.Unmarshal(credentialResponse.Body.Bytes(), &credentialBody); err != nil {
		t.Fatal(err)
	}
	if credentialBody.Credential.Configured || credentialBody.Credential.Username != "" {
		t.Fatalf("unexpected project credential: %#v", credentialBody.Credential)
	}
	if strings.Contains(credentialResponse.Body.String(), "token") || strings.Contains(credentialResponse.Body.String(), "ciphertext") {
		t.Fatalf("project credential response exposed a secret field: %s", credentialResponse.Body.String())
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

func TestProjectGitCredentialIsVerifiedStoredEncryptedAndBoundToProject(t *testing.T) {
	provider := &credentialRegistryProvider{Provider: git.NewDemoProvider()}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", GitCredentialKey: "credential-test-key", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())
	const secret = "project-token-secret"
	saved := doRequest(t, server.Router(), http.MethodPut, "/api/projects/reverse-lab/git/credential", token, `{"provider":"github","username":"release-bot","token":"`+secret+`"}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save project credential: expected 200, got %d: %s", saved.Code, saved.Body.String())
	}
	if strings.Contains(saved.Body.String(), secret) || strings.Contains(saved.Body.String(), "token_ciphertext") {
		t.Fatalf("project credential response exposed secret material: %s", saved.Body.String())
	}
	stored, err := server.deps.Store.GetProjectGitCredential(context.Background(), "space-lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if stored.TokenCiphertext == "" || stored.TokenCiphertext == secret {
		t.Fatalf("credential was not encrypted: %#v", stored)
	}
	if provider.configuredUsername != "release-bot" || provider.configuredToken != secret {
		t.Fatalf("credential was not bound to the project provider: username=%q token configured=%v", provider.configuredUsername, provider.configuredToken != "")
	}

	branches := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/branches", token, "")
	if branches.Code != http.StatusOK || provider.configureCalls < 2 {
		t.Fatalf("project Git request did not reload its credential: status=%d configureCalls=%d", branches.Code, provider.configureCalls)
	}

	removed := doRequest(t, server.Router(), http.MethodDelete, "/api/projects/reverse-lab/git/credential", token, "")
	if removed.Code != http.StatusOK || provider.clearCalls != 1 {
		t.Fatalf("remove project credential: status=%d clearCalls=%d body=%s", removed.Code, provider.clearCalls, removed.Body.String())
	}
	if strings.Contains(removed.Body.String(), secret) || strings.Contains(removed.Body.String(), "token_ciphertext") {
		t.Fatalf("remove response exposed secret material: %s", removed.Body.String())
	}
}

func TestProjectGitCredentialCanReplaceCiphertextFromPreviousKey(t *testing.T) {
	provider := &credentialRegistryProvider{Provider: git.NewDemoProvider()}
	projectStore := store.NewMemoryWithFixtures()
	oldServer := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", GitCredentialKey: "old-credential-key", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   projectStore,
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	oldCiphertext, err := oldServer.credentialCipher.seal("old-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectStore.SaveProjectGitCredential(context.Background(), "space-lab", "reverse-lab", store.SaveProjectGitCredentialInput{
		Provider: "github", Username: "old-bot", TokenCiphertext: oldCiphertext,
	}); err != nil {
		t.Fatal(err)
	}

	// A restarted server has a different key and cannot restore the old token.
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", GitCredentialKey: "new-credential-key", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   projectStore,
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())
	metadata := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/credential", token, "")
	if metadata.Code != http.StatusOK {
		t.Fatalf("stale project credential metadata: expected 200, got %d: %s", metadata.Code, metadata.Body.String())
	}
	var metadataBody struct {
		Credential struct {
			Configured bool `json:"configured"`
			Invalid    bool `json:"invalid"`
		} `json:"credential"`
	}
	if err := json.Unmarshal(metadata.Body.Bytes(), &metadataBody); err != nil {
		t.Fatal(err)
	}
	if metadataBody.Credential.Configured || !metadataBody.Credential.Invalid {
		t.Fatalf("stale credential was not exposed as replaceable: %s", metadata.Body.String())
	}
	saved := doRequest(t, server.Router(), http.MethodPut, "/api/projects/reverse-lab/git/credential", token, `{"provider":"github","username":"release-bot","token":"project-token-secret"}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("replace project credential: expected 200, got %d: %s", saved.Code, saved.Body.String())
	}
	stored, err := projectStore.GetProjectGitCredential(context.Background(), "space-lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Username != "release-bot" || stored.TokenCiphertext == oldCiphertext {
		t.Fatalf("old credential was not replaced: %#v", stored)
	}
	if provider.configuredUsername != "release-bot" || provider.configuredToken != "project-token-secret" {
		t.Fatalf("new credential was not configured: username=%q tokenConfigured=%v", provider.configuredUsername, provider.configuredToken != "")
	}
}

func TestChangingProjectRepositoryRemovesCredentialAndOldBinding(t *testing.T) {
	provider := &trackingRepositoryProvider{Provider: git.NewDemoProvider()}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", GitCredentialKey: "credential-test-key", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())
	newURL := "https://github.com/acme/another-repository"
	secret := "old-project-token"

	saved := doRequest(t, server.Router(), http.MethodPut, "/api/projects/reverse-lab/git/credential", token, `{"provider":"github","username":"release-bot","token":"`+secret+`"}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save project credential: expected 200, got %d: %s", saved.Code, saved.Body.String())
	}
	oldID := "demo-repo"
	newID := repositoryID("reverse-lab", newURL)

	updated := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"repository_url":"`+newURL+`"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("change repository: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	if _, err := server.deps.Store.GetProjectGitCredential(context.Background(), "space-lab", "reverse-lab"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old project credential survived repository change: %v", err)
	}
	if !provider.unregistered[oldID] || provider.registeredURL[newID] != newURL {
		t.Fatalf("repository bindings were not rotated: registered=%#v unregistered=%#v", provider.registeredURL, provider.unregistered)
	}
	if provider.configuredToken[newID] != "" || provider.configuredToken[oldID] != "" {
		t.Fatalf("repository change retained a project token in the provider: %#v", provider.configuredToken)
	}

	access := doRequest(t, server.Router(), http.MethodGet, "/api/projects/reverse-lab/git/access", token, "")
	if access.Code != http.StatusOK {
		t.Fatalf("access check after repository change: expected 200, got %d: %s", access.Code, access.Body.String())
	}
	if provider.configuredToken[newID] != "" {
		t.Fatalf("new repository inherited old project token during access check")
	}
}

func TestUpdateProjectRequiresVerifiedCredential(t *testing.T) {
	provider := &credentialRegistryProvider{Provider: git.NewDemoProvider()}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", GitCredentialKey: "credential-test-key", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())

	response := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"description":"should not save"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("update project without credential: expected 400, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"git_credential_required"`) {
		t.Fatalf("unexpected missing credential response: %s", response.Body.String())
	}

	saved := doRequest(t, server.Router(), http.MethodPut, "/api/projects/reverse-lab/git/credential", token, `{"provider":"github","username":"release-bot","token":"project-token-secret"}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save project credential: expected 200, got %d: %s", saved.Code, saved.Body.String())
	}
	response = doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"description":"saved after credential"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("update project with credential: expected 200, got %d: %s", response.Code, response.Body.String())
	}
}

func TestProjectsUsingSameRepositoryKeepIndependentCredentials(t *testing.T) {
	provider := &trackingRepositoryProvider{Provider: git.NewDemoProvider()}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", GitCredentialKey: "credential-test-key", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
	})
	token := loginForTest(t, server.Router())
	repositoryURL := "https://github.com/acme/shared-repository"
	projects := make([]struct {
		ID           string `json:"id"`
		RepositoryID string `json:"repository_id"`
	}, 2)
	for index, name := range []string{"Shared Repository One", "Shared Repository Two"} {
		created := doRequest(t, server.Router(), http.MethodPost, "/api/projects", token, `{"name":"`+name+`","repository_url":"`+repositoryURL+`","cluster_id":"demo-cluster","git_provider":"auto","git_username":"create-bot","git_token":"create-token"}`)
		if created.Code != http.StatusCreated {
			t.Fatalf("create project %d: expected 201, got %d: %s", index, created.Code, created.Body.String())
		}
		if err := json.Unmarshal(created.Body.Bytes(), &projects[index]); err != nil {
			t.Fatal(err)
		}
	}
	if projects[0].RepositoryID == projects[1].RepositoryID {
		t.Fatalf("projects using one repository received the same provider binding: %#v", projects)
	}

	credentials := []struct {
		username string
		token    string
	}{
		{username: "project-bot-1", token: "project-token-1"},
		{username: "project-bot-2", token: "project-token-2"},
	}
	for index, item := range projects {
		body := `{"provider":"github","username":"` + credentials[index].username + `","token":"` + credentials[index].token + `"}`
		saved := doRequest(t, server.Router(), http.MethodPut, "/api/projects/"+item.ID+"/git/credential", token, body)
		if saved.Code != http.StatusOK {
			t.Fatalf("save project %d credential: expected 200, got %d: %s", index, saved.Code, saved.Body.String())
		}
	}
	if provider.configuredToken[projects[0].RepositoryID] != "project-token-1" || provider.configuredToken[projects[1].RepositoryID] != "project-token-2" {
		t.Fatalf("project credentials were not isolated: %#v", provider.configuredToken)
	}
}

func TestPublishIsBlockedWhenProviderCannotVerifyServiceAccount(t *testing.T) {
	base := git.NewDemoProvider()
	provider := readOnlyProvider{Provider: base}
	server := New(Dependencies{
		Config:  config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
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
		Config:  config.Config{JWTSecret: "test-secret", JWTMinutes: 60, AllowedOrigin: "*"},
		Store:   store.NewMemoryWithFixtures(),
		Auth:    auth.NewManager("test-secret", 60),
		Git:     provider,
		Release: release.NewService(provider),
		Runtime: runtime.NewService(runtime.NewDemoProvider()),
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

type credentialRegistryProvider struct {
	git.Provider
	configuredUsername string
	configuredToken    string
	configureCalls     int
	clearCalls         int
}

type trackingRepositoryProvider struct {
	git.Provider
	registeredURL   map[string]string
	configuredToken map[string]string
	unregistered    map[string]bool
}

func (p *trackingRepositoryProvider) RegisterRepository(repositoryID, repositoryURL string) error {
	if p.registeredURL == nil {
		p.registeredURL = make(map[string]string)
	}
	p.registeredURL[repositoryID] = repositoryURL
	return nil
}

func (p *trackingRepositoryProvider) UnregisterRepository(repositoryID, _ string) error {
	if p.unregistered == nil {
		p.unregistered = make(map[string]bool)
	}
	p.unregistered[repositoryID] = true
	delete(p.registeredURL, repositoryID)
	delete(p.configuredToken, repositoryID)
	return nil
}

func (p *trackingRepositoryProvider) CheckRepositoryCredential(_ context.Context, repositoryID, repositoryURL string, credential git.RepositoryCredential) (git.RepositoryAccess, error) {
	return git.RepositoryAccess{
		RepositoryID: repositoryID, RepositoryURL: repositoryURL, Provider: credential.Provider,
		Supported: true, Authenticated: true, AuthenticatedUsername: credential.Username,
		RepositoryFound: true, AccountMatches: true, CanRead: true, CanWrite: true,
		CanCreateTemporaryBranch: true, Usable: true, RequiredPermission: "Write",
		Message: "授权通过",
	}, nil
}

func (p *trackingRepositoryProvider) ConfigureRepositoryCredential(repositoryID, repositoryURL string, credential git.RepositoryCredential) error {
	if p.configuredToken == nil {
		p.configuredToken = make(map[string]string)
	}
	if p.registeredURL == nil {
		p.registeredURL = make(map[string]string)
	}
	p.registeredURL[repositoryID] = repositoryURL
	p.configuredToken[repositoryID] = credential.Token
	return nil
}

func (p *trackingRepositoryProvider) ClearRepositoryCredential(repositoryID, _ string) error {
	delete(p.configuredToken, repositoryID)
	return nil
}

func (p *trackingRepositoryProvider) CheckRepositoryAccess(_ context.Context, repositoryID string) (git.RepositoryAccess, error) {
	return git.RepositoryAccess{
		RepositoryID: repositoryID, Supported: true, Message: "尚未配置项目仓库机器人",
	}, nil
}

func (p *credentialRegistryProvider) CheckRepositoryCredential(_ context.Context, _, _ string, credential git.RepositoryCredential) (git.RepositoryAccess, error) {
	if credential.Username != "release-bot" || credential.Token != "project-token-secret" {
		return git.RepositoryAccess{Message: "机器人凭证无效"}, nil
	}
	return git.RepositoryAccess{
		Provider:                 credential.Provider,
		Supported:                true,
		Authenticated:            true,
		AuthenticatedUsername:    credential.Username,
		RepositoryFound:          true,
		AccountMatches:           true,
		CanRead:                  true,
		CanWrite:                 true,
		CanCreateTemporaryBranch: true,
		Usable:                   true,
		Message:                  "授权通过",
	}, nil
}

func (p *credentialRegistryProvider) ConfigureRepositoryCredential(_ string, _ string, credential git.RepositoryCredential) error {
	p.configuredUsername = credential.Username
	p.configuredToken = credential.Token
	p.configureCalls++
	return nil
}

func (p *credentialRegistryProvider) ClearRepositoryCredential(_, _ string) error {
	p.clearCalls++
	p.configuredUsername = ""
	p.configuredToken = ""
	return nil
}
