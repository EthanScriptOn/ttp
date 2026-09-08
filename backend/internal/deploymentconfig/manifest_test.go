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

func TestEmptyManifestIsNotPublishable(t *testing.T) {
	if _, err := Validate("", "lab"); err == nil {
		t.Fatal("empty manifest should be rejected")
	}
}

func TestValidateRejectsMalformedAndDuplicateResources(t *testing.T) {
	if _, err := Validate("apiVersion: [", "lab"); err == nil {
		t.Fatal("malformed YAML should be rejected")
	}

	duplicate := `apiVersion: v1
kind: Service
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
	if _, err := Validate(duplicate, "lab"); err == nil || !strings.Contains(err.Error(), "duplicate resource") {
		t.Fatalf("duplicate resource error = %v", err)
	}
}

func TestRetargetNamespaceLeavesClusterScopedResourcesUntouched(t *testing.T) {
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: old
---
apiVersion: v1
kind: ClusterRole
metadata:
  name: app-reader
`
	retargeted, err := RetargetNamespace(manifest, "new")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(retargeted, "namespace: new") || strings.Contains(retargeted, "name: app-reader\n  namespace:") {
		t.Fatalf("unexpected retargeted manifest: %s", retargeted)
	}
}

func TestValidateRejectsTooManyResources(t *testing.T) {
	var builder strings.Builder
	for index := 0; index < 101; index++ {
		if index > 0 {
			builder.WriteString("---\n")
		}
		builder.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: config-")
		builder.WriteString(string(rune('a' + index/26)))
		builder.WriteString(string(rune('a' + index%26)))
		builder.WriteString("\n")
	}
	if _, err := Validate(builder.String(), "lab"); err == nil || !strings.Contains(err.Error(), "no more than 100") {
		t.Fatalf("resource limit error = %v", err)
	}
}
