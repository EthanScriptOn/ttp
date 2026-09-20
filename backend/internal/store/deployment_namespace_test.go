package store

import (
	"strings"
	"testing"
)

func TestBuildDeploymentNamespace(t *testing.T) {
	tests := []struct {
		name        string
		space       string
		environment string
		want        string
		wantHasHash bool
	}{
		{name: "readable pair", space: "lab", environment: "dev", want: "ttp-lab-dev"},
		{name: "sanitizes and disambiguates", space: "team_one", environment: "qa/test", wantHasHash: true},
		{name: "long values stay within kubernetes limit", space: "a-very-long-space-slug-that-would-overflow-the-kubernetes-namespace-limit", environment: "a-very-long-environment-name-that-would-overflow", wantHasHash: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := BuildDeploymentNamespace(test.space, test.environment)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) > deploymentNamespaceMaxLen {
				t.Fatalf("namespace length = %d, want <= %d: %q", len(got), deploymentNamespaceMaxLen, got)
			}
			if test.want != "" && got != test.want {
				t.Fatalf("namespace = %q, want %q", got, test.want)
			}
			if test.wantHasHash {
				parts := strings.Split(got, "-")
				if len(parts) == 0 || len(parts[len(parts)-1]) != deploymentNamespaceHashLen {
					t.Fatalf("namespace does not contain a disambiguating suffix: %q", got)
				}
			}
			again, err := BuildDeploymentNamespace(test.space, test.environment)
			if err != nil {
				t.Fatal(err)
			}
			if got != again {
				t.Fatalf("namespace is not stable: %q != %q", got, again)
			}
		})
	}
}

func TestBuildDeploymentNamespaceRejectsMissingParts(t *testing.T) {
	for _, test := range []struct {
		space, environment string
	}{
		{space: "", environment: "dev"},
		{space: "lab", environment: ""},
	} {
		if _, err := BuildDeploymentNamespace(test.space, test.environment); err == nil {
			t.Fatalf("BuildDeploymentNamespace(%q, %q) unexpectedly succeeded", test.space, test.environment)
		}
	}
}
