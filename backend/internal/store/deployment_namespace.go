package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	deploymentNamespacePrefix  = "ttp-"
	deploymentNamespaceMaxLen  = 63
	deploymentNamespaceHashLen = 10
)

// BuildDeploymentNamespace returns the stable Kubernetes namespace assigned to
// a TTP space/environment pair. The space slug is preferred by callers over a
// display name because it is already intended to be URL/DNS friendly.
//
// Kubernetes namespaces are limited to 63 lowercase DNS subdomain characters.
// When either component needs sanitising, or the readable form is too long, a
// stable hash is appended so different source values cannot silently collapse
// onto the same namespace.
func BuildDeploymentNamespace(spaceSlug, environment string) (string, error) {
	rawSpace := strings.TrimSpace(spaceSlug)
	rawEnvironment := strings.TrimSpace(environment)
	if rawSpace == "" || rawEnvironment == "" {
		return "", fmt.Errorf("%w: space slug and environment are required", ErrInvalidInput)
	}

	spacePart, spaceChanged := normalizeNamespacePart(rawSpace)
	environmentPart, environmentChanged := normalizeNamespacePart(rawEnvironment)
	if spacePart == "" {
		spacePart = "space"
		spaceChanged = true
	}
	if environmentPart == "" {
		environmentPart = "env"
		environmentChanged = true
	}

	base := deploymentNamespacePrefix + spacePart + "-" + environmentPart
	if !spaceChanged && !environmentChanged && len(base) <= deploymentNamespaceMaxLen {
		return base, nil
	}

	digest := sha256.Sum256([]byte(strings.ToLower(rawSpace) + "\x00" + strings.ToLower(rawEnvironment)))
	suffix := hex.EncodeToString(digest[:])[:deploymentNamespaceHashLen]
	maxReadableLen := deploymentNamespaceMaxLen - len(suffix) - 1
	if maxReadableLen < 1 {
		return "", fmt.Errorf("%w: generated namespace is too long", ErrInvalidInput)
	}
	if len(base) > maxReadableLen {
		base = strings.TrimRight(base[:maxReadableLen], "-")
	}
	if base == "" {
		base = "ttp"
	}
	return base + "-" + suffix, nil
}

func normalizeNamespacePart(value string) (string, bool) {
	var builder strings.Builder
	changed := false
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		case char == '-':
			if builder.Len() == 0 || lastDash {
				changed = true
				continue
			}
			builder.WriteRune(char)
			lastDash = true
		default:
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
			changed = true
		}
	}
	part := strings.Trim(builder.String(), "-")
	if part != strings.TrimSpace(strings.ToLower(value)) {
		changed = true
	}
	return part, changed
}
