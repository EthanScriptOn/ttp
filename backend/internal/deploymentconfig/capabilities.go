package deploymentconfig

import "sort"

// The direct Kubernetes publisher intentionally supports a small, useful set
// of namespaced resources. Keeping this list next to manifest validation gives
// the editor and the runtime one source of truth for the release boundary.
var releaseSupportedKinds = []string{
	"ConfigMap",
	"Deployment",
	"HorizontalPodAutoscaler",
	"Ingress",
	"Secret",
	"Service",
}

// ReleaseSupportedKinds returns a copy so callers cannot mutate the shared
// capability list.
func ReleaseSupportedKinds() []string {
	result := append([]string(nil), releaseSupportedKinds...)
	sort.Strings(result)
	return result
}

func IsReleaseSupportedKind(kind string) bool {
	for _, supported := range releaseSupportedKinds {
		if kind == supported {
			return true
		}
	}
	return false
}
