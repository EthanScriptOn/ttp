package api

import "strings"

func buildLogStartsImageBuild(line string) bool {
	line = strings.ToLower(strings.TrimSpace(line))
	return strings.Contains(line, "building and pushing image with buildkit") ||
		strings.Contains(line, "load build definition from dockerfile")
}

func buildLogSource(line string) string {
	value := strings.ToLower(strings.TrimSpace(line))
	gitMarkers := []string{
		"preparing source workspace",
		"fetching source revision",
		"checking out source revision",
		"source revision ready",
		"git network attempt",
		"fatal: unable to access",
		"remote:",
		"enumerating objects",
		"counting objects",
		"compressing objects",
		"receiving objects",
		"resolving deltas",
		"fetch_head",
		"from http://",
		"from https://",
	}
	for _, marker := range gitMarkers {
		if strings.Contains(value, marker) {
			return "git"
		}
	}
	if isCommitSHA(value) {
		return "git"
	}

	registryMarkers := []string{
		"pushing layers",
		"pushing manifest",
		"image build and push completed",
		"image=",
	}
	for _, marker := range registryMarkers {
		if strings.Contains(value, marker) {
			return "registry"
		}
	}
	return "build"
}

func isCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}
