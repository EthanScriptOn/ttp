package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
)

// newTestExecutor prepares a BuildKitExecutor with a fake buildctl on PATH
// and a private registry credential mapping on disk.
func newTestExecutor(t *testing.T, credentials string, ref, prefix string) *BuildKitExecutor {
	t.Helper()
	binDir := t.TempDir()
	buildctl := filepath.Join(binDir, "buildctl")
	if err := os.WriteFile(buildctl, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake buildctl: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	credentialsFile := filepath.Join(t.TempDir(), "registry-credentials.json")
	if err := os.WriteFile(credentialsFile, []byte(credentials), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	executor, err := NewBuildKitExecutor(BuildKitConfig{
		WorkDir:                 t.TempDir(),
		BuildctlPath:            buildctl,
		BuildkitAddr:            "unix:///tmp/ttp-buildkit-test.sock",
		RegistryCredentialsFile: credentialsFile,
		AllowedSourceHosts:      []string{"github.com"},
		ImageRepositoryPrefix:   prefix,
		RegistryCredentialRef:   ref,
		Platforms:               []string{"linux/amd64"},
	})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	return executor
}

func TestResolveImageRepositoryUsesProjectOverride(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"},"secondary":{"registry":"team.example.com","username":"u2","password":"p2"}}`,
		"primary", "platform.example.com/ttp")
	repository, credential, err := executor.resolveImageRepository(imagebuild.Request{ProjectID: "proj-1", ImageRepository: "team.example.com/team/app"})
	if err != nil {
		t.Fatalf("resolve override: %v", err)
	}
	if repository != "team.example.com/team/app" {
		t.Fatalf("repository = %q, want project override", repository)
	}
	if credential.Registry != "team.example.com" || credential.Username != "u2" {
		t.Fatalf("credential = %#v, want the matching host entry", credential)
	}
}

func TestResolveImageRepositoryRejectsUnknownRegistryHost(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`,
		"primary", "platform.example.com/ttp")
	_, _, err := executor.resolveImageRepository(imagebuild.Request{ProjectID: "proj-1", ImageRepository: "other.example.com/team/app"})
	if err == nil || !strings.Contains(err.Error(), "no builder credential is configured") {
		t.Fatalf("resolve error = %v, want missing credential failure", err)
	}
}

func TestResolveImageRepositoryFallsBackToPlatformNaming(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`,
		"primary", "platform.example.com/ttp")
	repository, credential, err := executor.resolveImageRepository(imagebuild.Request{ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("resolve fallback: %v", err)
	}
	if repository == "" || !strings.HasPrefix(repository, "platform.example.com/ttp/project-") {
		t.Fatalf("repository = %q, want platform prefix naming", repository)
	}
	if credential.Registry != "platform.example.com" {
		t.Fatalf("credential = %#v, want the configured reference", credential)
	}
}

func TestBuildRejectsInvalidProjectRepositoryBeforeCheckout(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`,
		"primary", "platform.example.com/ttp")
	_, err := executor.Build(context.Background(), imagebuild.Request{
		ProjectID: "proj-1", ReleaseID: "rel-1",
		RepositoryURL:   "https://github.com/example/repository",
		CommitSHA:       "0123456789abcdef0123456789abcdef01234567",
		ImageRepository: "platform.example.com/team/app:v1",
	})
	if err == nil || !strings.Contains(err.Error(), "must not include a tag") {
		t.Fatalf("build error = %v, want tag rejection", err)
	}
}
