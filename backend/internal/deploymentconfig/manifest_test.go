package deploymentconfig

import (
	"strings"
	"testing"
)

func TestValidateMultiDocumentManifest(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  namespace: lab
---
apiVersion: v1
kind: Service
metadata:
  name: checkout
  namespace: lab
`
	result, err := Validate(manifest, "lab")
	if err != nil {
		t.Fatalf("validate manifest: %v", err)
	}
	if result.Format != "yaml" || len(result.Resources) != 2 || result.Resources[0].Kind != "Deployment" {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestValidateJSONResourceArrayAndConvert(t *testing.T) {
	manifest := `[{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"lab"}},{"apiVersion":"v1","kind":"Service","metadata":{"name":"checkout","namespace":"lab"}}]`
	result, err := Validate(manifest, "lab")
	if err != nil {
		t.Fatalf("validate JSON: %v", err)
	}
	if result.Format != "json" || len(result.Resources) != 2 {
		t.Fatalf("unexpected JSON validation result: %#v", result)
	}
	yamlText, converted, err := Convert(manifest, "yaml", "lab")
	if err != nil {
		t.Fatalf("convert JSON to YAML: %v", err)
	}
	if converted.Format != "yaml" || strings.Count(yamlText, "apiVersion:") != 2 {
		t.Fatalf("unexpected converted YAML: %q", yamlText)
	}
}

func TestValidateRejectsNamespaceOutsideProject(t *testing.T) {
	manifest := `apiVersion: v1
kind: Service
metadata:
  name: checkout
  namespace: production
`
	if _, err := Validate(manifest, "lab"); err == nil || !strings.Contains(err.Error(), "must match project namespace") {
		t.Fatalf("expected namespace validation error, got %v", err)
	}
}

func TestDefaultManifestIsPublishable(t *testing.T) {
	manifest := DefaultManifest("Checkout API", "lab", 2, 8080)
	result, err := Validate(manifest, "lab")
	if err != nil {
		t.Fatalf("default manifest should validate: %v", err)
	}
	if len(result.Resources) != 3 {
		t.Fatalf("default resource count = %d, want 3", len(result.Resources))
	}
}
