package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDeploymentResourceFileEndpoints(t *testing.T) {
	server := testServerWithoutDeploymentConfig()
	handler := server.Router()
	token := loginForTest(t, handler)

	content := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  mode: test\n"
	created := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-resources", token, jsonBody(map[string]any{
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": content,
	}))
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"kind":"ConfigMap"`) {
		t.Fatalf("create resource file: %d: %s", created.Code, created.Body.String())
	}

	sourceNamespaceContent := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: source-settings\n  namespace: a\ndata:\n  mode: test\n"
	sourceNamespace := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-resources", token, jsonBody(map[string]any{
		"name": "source-settings.yaml", "path": "source-settings.yaml", "format": "yaml", "content": sourceNamespaceContent,
	}))
	if sourceNamespace.Code != http.StatusCreated || !strings.Contains(sourceNamespace.Body.String(), `"namespace":"ttp-lab-dev"`) {
		t.Fatalf("resource namespace should be normalized to the TTP environment: %d: %s", sourceNamespace.Code, sourceNamespace.Body.String())
	}

	updatedTarget := doRequest(t, handler, http.MethodPatch, "/api/projects/reverse-lab/deployment-targets/target-reverse-lab-dev", token, jsonBody(map[string]any{
		"environment": "dev",
	}))
	if updatedTarget.Code != http.StatusOK || !strings.Contains(updatedTarget.Body.String(), `"namespace":"ttp-lab-dev"`) {
		t.Fatalf("environment namespace should be generated: %d: %s", updatedTarget.Code, updatedTarget.Body.String())
	}
	resolved := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-config", token, "")
	if resolved.Code != http.StatusOK || !strings.Contains(resolved.Body.String(), `"namespace":"ttp-lab-dev"`) {
		t.Fatalf("resource namespace should be retargeted at release resolution: %d: %s", resolved.Code, resolved.Body.String())
	}
	var createdBody struct {
		Resource struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Resource.ID == "" || createdBody.Resource.Version != 1 {
		t.Fatalf("unexpected created resource: %#v", createdBody.Resource)
	}

	invalid := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-resources", token, jsonBody(map[string]any{
		"name": "invalid.yaml", "path": "invalid.yaml", "format": "yaml", "content": content + "---\napiVersion: v1\nkind: Service\nmetadata:\n  name: settings\n",
	}))
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "exactly one") {
		t.Fatalf("multi-document resource should be rejected: %d: %s", invalid.Code, invalid.Body.String())
	}

	listed := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources", token, "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "settings.yaml") {
		t.Fatalf("list resource files: %d: %s", listed.Code, listed.Body.String())
	}

	updated := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-resources/"+createdBody.Resource.ID, token, jsonBody(map[string]any{
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: prod", 1),
	}))
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"version":2`) {
		t.Fatalf("update resource file: %d: %s", updated.Code, updated.Body.String())
	}

	deleted := doRequest(t, handler, http.MethodDelete, "/api/projects/reverse-lab/deployment-resources/"+createdBody.Resource.ID, token, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete resource file: %d: %s", deleted.Code, deleted.Body.String())
	}
}
