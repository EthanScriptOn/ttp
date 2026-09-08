package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDeploymentConfigEndpoints(t *testing.T) {
	server := testServerWithoutDeploymentConfig()
	handler := server.Router()
	token := loginForTest(t, handler)

	emptyResponse := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-config", token, "")
	if emptyResponse.Code != http.StatusOK {
		t.Fatalf("get empty deployment config: expected 200, got %d: %s", emptyResponse.Code, emptyResponse.Body.String())
	}
	var defaultBody struct {
		Config struct {
			Version       int  `json:"version"`
			IsDefault     bool `json:"is_default"`
			ResourceCount int  `json:"resource_count"`
		} `json:"config"`
	}
	if err := json.Unmarshal(emptyResponse.Body.Bytes(), &defaultBody); err != nil {
		t.Fatal(err)
	}
	if defaultBody.Config.IsDefault || defaultBody.Config.Version != 0 || defaultBody.Config.ResourceCount != 0 {
		t.Fatalf("unexpected empty deployment config: %#v", defaultBody.Config)
	}
	if strings.Contains(emptyResponse.Body.String(), "example.invalid") {
		t.Fatalf("empty config response contains fabricated data: %s", emptyResponse.Body.String())
	}

	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  namespace: lab
spec:
  replicas: 2
---
apiVersion: v1
kind: Service
metadata:
  name: checkout
  namespace: lab
spec:
  selector:
    app: checkout
`
	savedResponse := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-config", token, jsonBody(map[string]any{
		"manifest": manifest,
		"format":   "yaml",
	}))
	if savedResponse.Code != http.StatusOK {
		t.Fatalf("save deployment config: expected 200, got %d: %s", savedResponse.Code, savedResponse.Body.String())
	}
	var savedBody struct {
		Config struct {
			Version       int    `json:"version"`
			IsDefault     bool   `json:"is_default"`
			Format        string `json:"format"`
			Manifest      string `json:"manifest"`
			ResourceCount int    `json:"resource_count"`
		} `json:"config"`
	}
	if err := json.Unmarshal(savedResponse.Body.Bytes(), &savedBody); err != nil {
		t.Fatal(err)
	}
	if savedBody.Config.Version != 1 || savedBody.Config.IsDefault || savedBody.Config.Format != "yaml" || savedBody.Config.ResourceCount != 2 || strings.TrimSpace(savedBody.Config.Manifest) != strings.TrimSpace(manifest) {
		t.Fatalf("unexpected saved deployment config: %#v", savedBody.Config)
	}

	readResponse := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/deployment-config", token, "")
	if readResponse.Code != http.StatusOK || !strings.Contains(readResponse.Body.String(), `"version":1`) || !strings.Contains(readResponse.Body.String(), "checkout") {
		t.Fatalf("saved deployment config was not readable: %d: %s", readResponse.Code, readResponse.Body.String())
	}

	convertedResponse := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-config/convert", token, jsonBody(map[string]any{
		"manifest": manifest,
		"format":   "json",
	}))
	if convertedResponse.Code != http.StatusOK || !strings.Contains(convertedResponse.Body.String(), `"format":"json"`) || !strings.Contains(convertedResponse.Body.String(), `"resource_count":2`) {
		t.Fatalf("convert deployment config: %d: %s", convertedResponse.Code, convertedResponse.Body.String())
	}

	invalidResponse := doRequest(t, handler, http.MethodPut, "/api/projects/reverse-lab/deployment-config", token, jsonBody(map[string]any{
		"manifest": "apiVersion: v1\nkind: Service\nmetadata:\n  name: outside\n  namespace: production\n",
		"format":   "yaml",
	}))
	if invalidResponse.Code != http.StatusBadRequest || !strings.Contains(invalidResponse.Body.String(), "namespace") {
		t.Fatalf("namespace violation: expected 400, got %d: %s", invalidResponse.Code, invalidResponse.Body.String())
	}

	capabilityResponse := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/deployment-config/validate", token, jsonBody(map[string]any{
		"manifest": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: checkout\n",
		"format":   "yaml",
	}))
	if capabilityResponse.Code != http.StatusOK || !strings.Contains(capabilityResponse.Body.String(), `"release_supported":false`) || !strings.Contains(capabilityResponse.Body.String(), `"unsupported_resources"`) {
		t.Fatalf("unsupported resource capability was not reported: %d: %s", capabilityResponse.Code, capabilityResponse.Body.String())
	}
}

func jsonBody(value map[string]any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
