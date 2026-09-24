package api

import (
	"context"
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
	environmentPathConflict := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-resource-overrides", token, jsonBody(map[string]any{
		"target_id": "target-reverse-lab-dev", "name": "source-settings.yaml", "path": "source-settings.yaml", "format": "yaml", "content": content,
	}))
	if environmentPathConflict.Code != http.StatusConflict {
		t.Fatalf("environment-only resource must not shadow a global path without an explicit override: %d: %s", environmentPathConflict.Code, environmentPathConflict.Body.String())
	}

	overridden := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-resource-overrides", token, jsonBody(map[string]any{
		"target_id": "target-reverse-lab-dev", "global_resource_id": createdBody.Resource.ID,
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: dev", 1),
	}))
	if overridden.Code != http.StatusCreated || !strings.Contains(overridden.Body.String(), `"scope":"overridden"`) {
		t.Fatalf("create environment override: %d: %s", overridden.Code, overridden.Body.String())
	}
	var overrideBody struct {
		Resource struct {
			OverrideID string `json:"override_id"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(overridden.Body.Bytes(), &overrideBody); err != nil {
		t.Fatal(err)
	}
	if overrideBody.Resource.OverrideID == "" {
		t.Fatalf("environment override id is missing: %s", overridden.Body.String())
	}
	relinked := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-resource-overrides/"+overrideBody.Resource.OverrideID, token, jsonBody(map[string]any{
		"target_id": "target-reverse-lab-dev", "name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: dev", 1),
	}))
	if relinked.Code != http.StatusConflict {
		t.Fatalf("an environment override must stay linked to its original global resource: %d: %s", relinked.Code, relinked.Body.String())
	}
	devFiles := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources?target_id=target-reverse-lab-dev", token, "")
	if devFiles.Code != http.StatusOK || !strings.Contains(devFiles.Body.String(), `"scope":"overridden"`) || !strings.Contains(devFiles.Body.String(), "mode: dev") {
		t.Fatalf("dev should use its override: %d: %s", devFiles.Code, devFiles.Body.String())
	}
	uatFiles := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources?target_id=target-reverse-lab-uat", token, "")
	if uatFiles.Code != http.StatusOK || !strings.Contains(uatFiles.Body.String(), `"scope":"inherited"`) || strings.Contains(uatFiles.Body.String(), "mode: dev") {
		t.Fatalf("uat should keep the global resource: %d: %s", uatFiles.Code, uatFiles.Body.String())
	}
	project, err := server.deps.Store.GetProject(context.Background(), "space-lab", "reverse-lab")
	if err != nil {
		t.Fatal(err)
	}
	devTarget, err := server.deps.Store.GetDeploymentTarget(context.Background(), project.SpaceID, project.ID, "target-reverse-lab-dev")
	if err != nil {
		t.Fatal(err)
	}
	uatTarget, err := server.deps.Store.GetDeploymentTarget(context.Background(), project.SpaceID, project.ID, "target-reverse-lab-uat")
	if err != nil {
		t.Fatal(err)
	}
	devConfig, _, err := server.resolveDeploymentConfigForTargetWithContext(context.Background(), project, devTarget)
	if err != nil || len(devConfig.Files) == 0 || !strings.Contains(devConfig.Files[0].Content, "mode: dev") {
		t.Fatalf("dev release config should use the environment override: %#v, %v", devConfig.Files, err)
	}
	uatConfig, _, err := server.resolveDeploymentConfigForTargetWithContext(context.Background(), project, uatTarget)
	if err != nil || len(uatConfig.Files) == 0 || !strings.Contains(uatConfig.Files[0].Content, "mode: prod") {
		t.Fatalf("uat release config should use the global resource: %#v, %v", uatConfig.Files, err)
	}

	globalChanged := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-resources/"+createdBody.Resource.ID, token, jsonBody(map[string]any{
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: global-new", 1),
	}))
	if globalChanged.Code != http.StatusOK || !strings.Contains(globalChanged.Body.String(), `"version":3`) {
		t.Fatalf("update global resource after override: %d: %s", globalChanged.Code, globalChanged.Body.String())
	}
	mergePending := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources?target_id=target-reverse-lab-dev", token, "")
	var mergePendingBody struct {
		Resources []struct {
			GlobalChanged bool `json:"global_changed"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(mergePending.Body.Bytes(), &mergePendingBody); err != nil {
		t.Fatal(err)
	}
	if mergePending.Code != http.StatusOK || len(mergePendingBody.Resources) == 0 || !mergePendingBody.Resources[0].GlobalChanged {
		t.Fatalf("global update should be informational while the environment override stays active: %d: %s", mergePending.Code, mergePending.Body.String())
	}
	merged := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-resource-overrides/"+overrideBody.Resource.OverrideID, token, jsonBody(map[string]any{
		"target_id": "target-reverse-lab-dev", "global_resource_id": createdBody.Resource.ID, "base_global_version": 3,
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: dev-merged", 1),
	}))
	if merged.Code != http.StatusOK {
		t.Fatalf("save environment override against the latest global version: %d: %s", merged.Code, merged.Body.String())
	}
	mergeResolved := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources?target_id=target-reverse-lab-dev", token, "")
	if mergeResolved.Code != http.StatusOK || strings.Contains(mergeResolved.Body.String(), `"global_changed":true`) || !strings.Contains(mergeResolved.Body.String(), "mode: dev-merged") {
		t.Fatalf("saving the environment override should acknowledge the latest global version: %d: %s", mergeResolved.Code, mergeResolved.Body.String())
	}
	convergedGlobal := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-resources/"+createdBody.Resource.ID, token, jsonBody(map[string]any{
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: dev-merged", 1),
	}))
	if convergedGlobal.Code != http.StatusOK || !strings.Contains(convergedGlobal.Body.String(), `"version":4`) {
		t.Fatalf("update global resource to the environment content: %d: %s", convergedGlobal.Code, convergedGlobal.Body.String())
	}
	converged := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources?target_id=target-reverse-lab-dev", token, "")
	if converged.Code != http.StatusOK || strings.Contains(converged.Body.String(), `"global_changed":true`) || !strings.Contains(converged.Body.String(), `"base_global_version":4`) {
		t.Fatalf("identical environment and global content must not require a merge: %d: %s", converged.Code, converged.Body.String())
	}
	convergedUpdate := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-resource-overrides/"+overrideBody.Resource.OverrideID, token, jsonBody(map[string]any{
		"target_id": "target-reverse-lab-dev", "global_resource_id": createdBody.Resource.ID, "base_global_version": 4,
		"name": "settings.yaml", "path": "settings.yaml", "format": "yaml", "content": strings.Replace(content, "mode: test", "mode: dev-after-convergence", 1),
	}))
	if convergedUpdate.Code != http.StatusOK {
		t.Fatalf("saving after identical versions should use the effective global base: %d: %s", convergedUpdate.Code, convergedUpdate.Body.String())
	}
	restored := doRequest(t, handler, http.MethodDelete, "/api/projects/reverse-lab/deployment-resource-overrides/"+overrideBody.Resource.OverrideID+"?target_id=target-reverse-lab-dev", token, "")
	if restored.Code != http.StatusNoContent {
		t.Fatalf("restore global resource: %d: %s", restored.Code, restored.Body.String())
	}
	inheritedAgain := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-resources?target_id=target-reverse-lab-dev", token, "")
	if inheritedAgain.Code != http.StatusOK || !strings.Contains(inheritedAgain.Body.String(), `"scope":"inherited"`) || !strings.Contains(inheritedAgain.Body.String(), "mode: dev-merged") {
		t.Fatalf("dev should inherit global after deleting override: %d: %s", inheritedAgain.Code, inheritedAgain.Body.String())
	}

	deleted := doRequest(t, handler, http.MethodDelete, "/api/projects/reverse-lab/deployment-resources/"+createdBody.Resource.ID, token, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete resource file: %d: %s", deleted.Code, deleted.Body.String())
	}
}
