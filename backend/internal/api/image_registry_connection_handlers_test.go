package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
)

type requiredImageRegistryProvider struct {
	*credentialRegistryProvider
}

func (*requiredImageRegistryProvider) RequiresImageRegistryConnection() bool { return true }

func TestProjectRequiresAnImageRegistryConnection(t *testing.T) {
	provider := &requiredImageRegistryProvider{credentialRegistryProvider: &credentialRegistryProvider{Provider: git.NewDemoProvider()}}
	server := testServerWithGitProvider(provider)
	token := loginForTest(t, server.Router())

	missing := doRequest(t, server.Router(), http.MethodPost, "/api/projects", token, `{"name":"无镜像仓库连接","repository_url":"https://github.com/acme/missing-registry","cluster_id":"demo-cluster","git_provider":"github","git_username":"release-bot","git_token":"project-token-secret"}`)
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), `"code":"image_registry_connection_required"`) {
		t.Fatalf("create without registry connection: expected 400/image_registry_connection_required, got %d: %s", missing.Code, missing.Body.String())
	}

	credential := doRequest(t, server.Router(), http.MethodPut, "/api/projects/reverse-lab/git/credential", token, `{"provider":"github","username":"release-bot","token":"project-token-secret"}`)
	if credential.Code != http.StatusOK {
		t.Fatalf("save project credential: expected 200, got %d: %s", credential.Code, credential.Body.String())
	}

	missingUpdate := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"description":"should not save"}`)
	if missingUpdate.Code != http.StatusBadRequest || !strings.Contains(missingUpdate.Body.String(), `"code":"image_registry_connection_required"`) {
		t.Fatalf("update without registry connection: expected 400/image_registry_connection_required, got %d: %s", missingUpdate.Code, missingUpdate.Body.String())
	}
	emptyUpdate := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"registry_connection_id":""}`)
	if emptyUpdate.Code != http.StatusBadRequest || !strings.Contains(emptyUpdate.Body.String(), `"code":"image_registry_connection_required"`) {
		t.Fatalf("clear registry connection: expected 400/image_registry_connection_required, got %d: %s", emptyUpdate.Code, emptyUpdate.Body.String())
	}
	unknownUpdate := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"registry_connection_id":"missing-connection"}`)
	if unknownUpdate.Code != http.StatusNotFound || !strings.Contains(unknownUpdate.Body.String(), `"code":"not_found"`) {
		t.Fatalf("unknown registry connection: expected 404/not_found, got %d: %s", unknownUpdate.Code, unknownUpdate.Body.String())
	}

	created := doRequest(t, server.Router(), http.MethodPost, "/api/image-registry-connections", token, `{"name":"主镜像仓库","registry":"registry.example.com","auth_type":"basic","username":"robot","secret":"registry-secret"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create registry connection: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var body struct {
		Connection struct {
			ID string `json:"id"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Connection.ID == "" {
		t.Fatal("created registry connection has no id")
	}

	selected := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"registry_connection_id":"`+body.Connection.ID+`","description":"saved with registry"}`)
	if selected.Code != http.StatusOK || !strings.Contains(selected.Body.String(), `"registry_connection_id":"`+body.Connection.ID+`"`) {
		t.Fatalf("update with registry connection: expected 200, got %d: %s", selected.Code, selected.Body.String())
	}
	retained := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"description":"retain registry"}`)
	if retained.Code != http.StatusOK || !strings.Contains(retained.Body.String(), `"registry_connection_id":"`+body.Connection.ID+`"`) {
		t.Fatalf("update retaining existing registry connection: expected 200, got %d: %s", retained.Code, retained.Body.String())
	}

	createdProject := doRequest(t, server.Router(), http.MethodPost, "/api/projects", token, `{"name":"绑定镜像仓库","repository_url":"https://github.com/acme/with-registry","cluster_id":"demo-cluster","registry_connection_id":"`+body.Connection.ID+`","git_provider":"github","git_username":"release-bot","git_token":"project-token-secret"}`)
	if createdProject.Code != http.StatusCreated {
		t.Fatalf("create with registry connection: expected 201, got %d: %s", createdProject.Code, createdProject.Body.String())
	}
}

func TestImageRegistryConnectionEndpointsEncryptCredentialsAndTestStatus(t *testing.T) {
	const secret = "acr-password-secret"
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if r.URL.Path != "/v2/" || !ok || username != "robot" || password != secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer registry.Close()
	host := strings.TrimPrefix(registry.URL, "http://")
	server := testServer()
	token := loginForTest(t, server.Router())
	created := doRequest(t, server.Router(), http.MethodPost, "/api/image-registry-connections", token, `{"name":"ACR","registry":"`+host+`","auth_type":"basic","username":"robot","secret":"`+secret+`"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create connection: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), secret) || strings.Contains(created.Body.String(), "ciphertext") {
		t.Fatalf("connection response exposed secret material: %s", created.Body.String())
	}
	var createBody struct {
		Connection struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Connection.ID == "" || createBody.Connection.Status != "unverified" {
		t.Fatalf("created connection metadata = %#v", createBody.Connection)
	}
	stored, err := server.deps.Store.GetImageRegistryConnection(context.Background(), "space-lab", createBody.Connection.ID)
	if err != nil || stored.CredentialCiphertext == "" || stored.CredentialCiphertext == secret {
		t.Fatalf("stored connection credential = %#v, err=%v", stored, err)
	}
	tested := doRequest(t, server.Router(), http.MethodPost, "/api/image-registry-connections/"+createBody.Connection.ID+"/test", token, "")
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), `"status":"active"`) {
		t.Fatalf("test connection: expected active 200, got %d: %s", tested.Code, tested.Body.String())
	}
	if strings.Contains(tested.Body.String(), secret) {
		t.Fatalf("test response exposed secret: %s", tested.Body.String())
	}
	bound := doRequest(t, server.Router(), http.MethodPatch, "/api/projects/reverse-lab", token, `{"registry_connection_id":"`+createBody.Connection.ID+`"}`)
	if bound.Code != http.StatusOK {
		t.Fatalf("bind project connection: expected 200, got %d: %s", bound.Code, bound.Body.String())
	}
	project, err := server.deps.Store.GetProject(context.Background(), "space-lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	buildRequest, err := server.imageBuildRequest(context.Background(), project, release.Release{ID: "release-1"}, "abcdef123456")
	if err != nil {
		t.Fatalf("build request with registry connection: %v", err)
	}
	if buildRequest.ImageRepository != "" {
		t.Fatalf("managed registry should derive the image repository path, got %q", buildRequest.ImageRepository)
	}
	if err := buildRequest.Validate(); err != nil {
		t.Fatalf("build request with derived registry repository: %v", err)
	}
	if buildRequest.RegistryCredential.Secret != secret || buildRequest.RegistryCredential.Registry != host || buildRequest.RegistryCredential.AuthType != "basic" {
		t.Fatalf("build registry credential = %#v", buildRequest.RegistryCredential)
	}
	pullCredential, err := server.imagePullCredentialForProject(context.Background(), project)
	if err != nil || pullCredential == nil || pullCredential.Secret != secret || pullCredential.SecretName == "" {
		t.Fatalf("pull credential = %#v, err=%v", pullCredential, err)
	}
	deleted := doRequest(t, server.Router(), http.MethodDelete, "/api/image-registry-connections/"+createBody.Connection.ID, token, "")
	if deleted.Code != http.StatusConflict {
		t.Fatalf("delete bound connection: expected 409, got %d: %s", deleted.Code, deleted.Body.String())
	}
}

func TestImageRegistryConnectionTestMarksInvalidWithoutLeakingCredential(t *testing.T) {
	registry := httptest.NewServer(http.NotFoundHandler())
	defer registry.Close()
	host := strings.TrimPrefix(registry.URL, "http://")
	server := testServer()
	token := loginForTest(t, server.Router())
	created := doRequest(t, server.Router(), http.MethodPost, "/api/image-registry-connections", token, `{"name":"invalid-registry","registry":"`+host+`","auth_type":"token","secret":"token-value"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create token connection: %d %s", created.Code, created.Body.String())
	}
	var body struct {
		Connection struct {
			ID string `json:"id"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	tested := doRequest(t, server.Router(), http.MethodPost, "/api/image-registry-connections/"+body.Connection.ID+"/test", token, "")
	if tested.Code != http.StatusUnprocessableEntity || !strings.Contains(tested.Body.String(), "registry_connection_test_failed") {
		t.Fatalf("invalid test response = %d %s", tested.Code, tested.Body.String())
	}
	if strings.Contains(tested.Body.String(), "token-value") {
		t.Fatalf("invalid test response leaked token: %s", tested.Body.String())
	}
	stored, err := server.deps.Store.GetImageRegistryConnection(context.Background(), "space-lab", body.Connection.ID)
	if err != nil || stored.Status != "invalid" {
		t.Fatalf("invalid test status = %#v, err=%v", stored, err)
	}
}
