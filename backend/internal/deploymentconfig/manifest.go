package deploymentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// MaxManifestBytes keeps the editor endpoint bounded even when it is backed
	// by the in-memory store.
	MaxManifestBytes = 512 << 10
	maxResources     = 100
)

var ErrInvalidManifest = errors.New("invalid kubernetes deployment manifest")

type Resource struct {
	APIVersion string `json:"api_version"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

type Validation struct {
	Format    string
	Resources []Resource
}

// ValidateResourceFile validates one independently applicable Kubernetes file.
// Unlike Validate, it deliberately rejects YAML document streams and JSON
// arrays: one file must contain exactly one object that kubectl can apply.
func ValidateResourceFile(content, format, expectedNamespace string) (Validation, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Validation{}, fmt.Errorf("%w: resource file cannot be empty", ErrInvalidManifest)
	}
	requestedFormat, err := NormalizeFormat(format)
	if err != nil {
		return Validation{}, err
	}
	if strings.TrimSpace(format) == "" {
		requestedFormat = DetectFormat(content)
	} else if requestedFormat != DetectFormat(content) {
		return Validation{}, fmt.Errorf("%w: resource file format does not match content", ErrInvalidManifest)
	}
	values, err := decodeDocuments(content)
	if err != nil {
		return Validation{}, err
	}
	if len(values) != 1 {
		return Validation{}, fmt.Errorf("%w: each resource file must contain exactly one Kubernetes object", ErrInvalidManifest)
	}
	if _, ok := values[0].([]any); ok {
		return Validation{}, fmt.Errorf("%w: JSON arrays are not allowed in a resource file", ErrInvalidManifest)
	}
	validated, err := Validate(content, expectedNamespace)
	if err != nil {
		return Validation{}, err
	}
	if len(validated.Resources) != 1 {
		return Validation{}, fmt.Errorf("%w: each resource file must contain exactly one Kubernetes object", ErrInvalidManifest)
	}
	resource := validated.Resources[0]
	if clusterScopedKinds[resource.Kind] {
		return Validation{}, fmt.Errorf("%w: cluster-scoped resource %s is not allowed in a project resource file", ErrInvalidManifest, resource.Kind)
	}
	validated.Format = requestedFormat
	return validated, nil
}

func NormalizeFormat(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "yaml", "yml":
		return "yaml", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("%w: format must be yaml or json", ErrInvalidManifest)
	}
}

func DetectFormat(manifest string) string {
	trimmed := strings.TrimSpace(manifest)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return "json"
	}
	return "yaml"
}

// Validate parses a Kubernetes YAML/JSON stream and checks the identity fields
// needed by the publisher. It intentionally does not try to reproduce every
// Kubernetes OpenAPI rule; the target cluster remains the final validator.
func Validate(manifest, expectedNamespace string) (Validation, error) {
	if len([]byte(manifest)) > MaxManifestBytes {
		return Validation{}, fmt.Errorf("%w: manifest is larger than %d bytes", ErrInvalidManifest, MaxManifestBytes)
	}
	values, err := decodeDocuments(manifest)
	if err != nil {
		return Validation{}, err
	}
	objects, err := flattenObjects(values)
	if err != nil {
		return Validation{}, err
	}
	if len(objects) == 0 {
		return Validation{}, fmt.Errorf("%w: at least one resource is required", ErrInvalidManifest)
	}
	if len(objects) > maxResources {
		return Validation{}, fmt.Errorf("%w: no more than %d resources are allowed", ErrInvalidManifest, maxResources)
	}

	expectedNamespace = strings.TrimSpace(expectedNamespace)
	if expectedNamespace != "" && len(validation.IsDNS1123Label(expectedNamespace)) > 0 {
		return Validation{}, fmt.Errorf("%w: project namespace %q is invalid", ErrInvalidManifest, expectedNamespace)
	}

	result := Validation{Format: DetectFormat(manifest), Resources: make([]Resource, 0, len(objects))}
	seen := make(map[string]struct{}, len(objects))
	for index, object := range objects {
		apiVersion, ok := stringField(object, "apiVersion")
		if !ok || apiVersion == "" {
			return Validation{}, resourceError(index, "apiVersion is required")
		}
		kind, ok := stringField(object, "kind")
		if !ok || kind == "" {
			return Validation{}, resourceError(index, "kind is required")
		}
		metadata, ok := mapField(object, "metadata")
		if !ok {
			return Validation{}, resourceError(index, "metadata is required")
		}
		name, ok := stringField(metadata, "name")
		if !ok || name == "" {
			return Validation{}, resourceError(index, "metadata.name is required")
		}
		if len(validation.IsDNS1123Subdomain(name)) > 0 {
			return Validation{}, resourceError(index, fmt.Sprintf("metadata.name %q is not a valid Kubernetes name", name))
		}
		namespace, _ := stringField(metadata, "namespace")
		if expectedNamespace != "" && namespace != "" && namespace != expectedNamespace {
			return Validation{}, resourceError(index, fmt.Sprintf("namespace %q must match project namespace %q", namespace, expectedNamespace))
		}
		key := strings.Join([]string{apiVersion, kind, namespace, name}, "\x00")
		if _, exists := seen[key]; exists {
			return Validation{}, resourceError(index, fmt.Sprintf("duplicate resource %s/%s", kind, name))
		}
		seen[key] = struct{}{}
		result.Resources = append(result.Resources, Resource{APIVersion: apiVersion, Kind: kind, Name: name, Namespace: namespace})
	}
	return result, nil
}

// Convert validates first and then renders the same resource documents in the
// requested format. Multiple YAML documents become a JSON array so no resource
// is silently lost when the user switches the editor mode.
func Convert(manifest, targetFormat, expectedNamespace string) (string, Validation, error) {
	format, err := NormalizeFormat(targetFormat)
	if err != nil {
		return "", Validation{}, err
	}
	validated, err := Validate(manifest, expectedNamespace)
	if err != nil {
		return "", Validation{}, err
	}
	values, err := decodeDocuments(manifest)
	if err != nil {
		return "", Validation{}, err
	}
	objects, err := flattenObjects(values)
	if err != nil {
		return "", Validation{}, err
	}
	if format == "json" {
		var value any = objects[0]
		if len(objects) > 1 {
			value = objects
		}
		encoded, marshalErr := json.MarshalIndent(value, "", "  ")
		if marshalErr != nil {
			return "", Validation{}, fmt.Errorf("%w: cannot render JSON: %v", ErrInvalidManifest, marshalErr)
		}
		validated.Format = "json"
		return string(encoded) + "\n", validated, nil
	}

	var builder strings.Builder
	for index, object := range objects {
		if index > 0 {
			builder.WriteString("---\n")
		}
		encoded, marshalErr := yaml.Marshal(object)
		if marshalErr != nil {
			return "", Validation{}, fmt.Errorf("%w: cannot render YAML: %v", ErrInvalidManifest, marshalErr)
		}
		builder.Write(encoded)
	}
	validated.Format = "yaml"
	return builder.String(), validated, nil
}

// RetargetNamespace rewrites namespaced Kubernetes resources for an
// environment target. The project keeps one native manifest, while the
// selected target supplies the namespace at release time.
func RetargetNamespace(manifest, namespace string) (string, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || len(validation.IsDNS1123Label(namespace)) != 0 {
		return "", fmt.Errorf("%w: target namespace is invalid", ErrInvalidManifest)
	}
	values, err := decodeDocuments(manifest)
	if err != nil {
		return "", err
	}
	objects, err := flattenObjects(values)
	if err != nil {
		return "", err
	}
	changed := false
	for _, object := range objects {
		kind, _ := stringField(object, "kind")
		if clusterScopedKinds[strings.TrimSpace(kind)] {
			continue
		}
		metadata, ok := mapField(object, "metadata")
		if !ok {
			continue
		}
		currentNamespace, _ := stringField(metadata, "namespace")
		if currentNamespace != namespace {
			metadata["namespace"] = namespace
			changed = true
		}
	}
	if !changed {
		// Keep the user's original formatting when the target namespace is
		// already correct. Re-render only when TTP actually has to retarget a
		// namespaced object.
		return strings.TrimSpace(manifest), nil
	}
	format := DetectFormat(manifest)
	if format == "json" {
		var value any = objects[0]
		if len(objects) > 1 {
			value = objects
		}
		encoded, marshalErr := json.MarshalIndent(value, "", "  ")
		if marshalErr != nil {
			return "", fmt.Errorf("%w: cannot render JSON: %v", ErrInvalidManifest, marshalErr)
		}
		return string(encoded) + "\n", nil
	}
	var builder strings.Builder
	for index, object := range objects {
		if index > 0 {
			builder.WriteString("---\n")
		}
		encoded, marshalErr := yaml.Marshal(object)
		if marshalErr != nil {
			return "", fmt.Errorf("%w: cannot render YAML: %v", ErrInvalidManifest, marshalErr)
		}
		builder.Write(encoded)
	}
	return builder.String(), nil
}

var clusterScopedKinds = map[string]bool{
	"APIService":                     true,
	"ClusterRole":                    true,
	"ClusterRoleBinding":             true,
	"CustomResourceDefinition":       true,
	"Namespace":                      true,
	"Node":                           true,
	"PersistentVolume":               true,
	"PersistentVolumeClaim":          false,
	"PodSecurityPolicy":              true,
	"PriorityClass":                  true,
	"StorageClass":                   true,
	"MutatingWebhookConfiguration":   true,
	"ValidatingWebhookConfiguration": true,
}

func decodeDocuments(manifest string) ([]any, error) {
	if strings.TrimSpace(manifest) == "" {
		return nil, fmt.Errorf("%w: manifest cannot be empty", ErrInvalidManifest)
	}
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	values := make([]any, 0, 4)
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: YAML syntax error: %v", ErrInvalidManifest, err)
		}
		if len(node.Content) == 0 {
			continue
		}
		root := node.Content[0]
		var value any
		if err := root.Decode(&value); err != nil {
			return nil, fmt.Errorf("%w: document cannot be decoded: %v", ErrInvalidManifest, err)
		}
		values = append(values, value)
	}
	return values, nil
}

func flattenObjects(values []any) ([]map[string]any, error) {
	objects := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if list, ok := value.([]any); ok {
			for _, item := range list {
				object, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%w: every JSON array item must be an object", ErrInvalidManifest)
				}
				objects = append(objects, object)
			}
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: each document must be an object", ErrInvalidManifest)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func mapField(source map[string]any, key string) (map[string]any, bool) {
	value, ok := source[key]
	if !ok {
		return nil, false
	}
	result, ok := value.(map[string]any)
	return result, ok
}

func stringField(source map[string]any, key string) (string, bool) {
	value, ok := source[key]
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	return strings.TrimSpace(text), ok
}

func resourceError(index int, message string) error {
	return fmt.Errorf("%w: resource %d: %s", ErrInvalidManifest, index+1, message)
}
