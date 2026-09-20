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

func TestResolveImageRepositoryUsesRequestCredential(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`,
		"primary", "platform.example.com/ttp")
	repository, credential, err := executor.resolveImageRepository(imagebuild.Request{
		ProjectID: "proj-1", ImageRepository: "team.example.com/team/app",
		RegistryCredential: imagebuild.PushCredential{ConnectionID: "acr-main", Registry: "team.example.com", AuthType: "token", Secret: "token-secret"},
	})
	if err != nil {
		t.Fatalf("resolve request credential: %v", err)
	}
	if repository != "team.example.com/team/app" || credential.AuthType != "token" || credential.Password != "token-secret" || credential.Registry != "team.example.com" {
		t.Fatalf("resolved credential = %#v, repository=%q", credential, repository)
	}
}

func TestResolveImageRepositoryDerivesPathFromRequestCredential(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`,
		"primary", "platform.example.com/ttp")
	repository, credential, err := executor.resolveImageRepository(imagebuild.Request{
		ProjectID:          "proj-1",
		RegistryCredential: imagebuild.PushCredential{ConnectionID: "acr-main", Registry: "team.example.com", AuthType: "token", Secret: "token-secret"},
	})
	if err != nil {
		t.Fatalf("resolve derived repository: %v", err)
	}
	if !strings.HasPrefix(repository, "team.example.com/project-") || credential.Registry != "team.example.com" || credential.Password != "token-secret" {
		t.Fatalf("resolved repository=%q credential=%#v", repository, credential)
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

func TestInsecureRegistrySelection(t *testing.T) {
	executor := newTestExecutor(t,
		`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`,
		"primary", "platform.example.com/ttp")
	executor.insecureRegistries = map[string]struct{}{"registry.local:5000": {}}

	for _, host := range []string{"localhost:5000", "127.0.0.1:5000", "[::1]:5000", "registry.local:5000"} {
		if !executor.isInsecureRegistry(host) {
			t.Fatalf("registry %q should use HTTP", host)
		}
	}
	if executor.isInsecureRegistry("registry.example.com") {
		t.Fatal("unconfigured remote registry should require HTTPS")
	}
}

func TestRejectsInvalidInsecureRegistry(t *testing.T) {
	binDir := t.TempDir()
	buildctl := filepath.Join(binDir, "buildctl")
	if err := os.WriteFile(buildctl, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake buildctl: %v", err)
	}
	credentialsFile := filepath.Join(t.TempDir(), "registry-credentials.json")
	if err := os.WriteFile(credentialsFile, []byte(`{"primary":{"registry":"platform.example.com","username":"u1","password":"p1"}}`), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	_, err := NewBuildKitExecutor(BuildKitConfig{
		WorkDir: t.TempDir(), BuildctlPath: buildctl, BuildkitAddr: "unix:///tmp/buildkit.sock",
		RegistryCredentialsFile: credentialsFile, AllowedSourceHosts: []string{"github.com"},
		ImageRepositoryPrefix: "platform.example.com/ttp", RegistryCredentialRef: "primary",
		Platforms: []string{"linux/amd64"}, InsecureRegistries: []string{"http://registry.local:5000"},
	})
	if err == nil || !strings.Contains(err.Error(), "insecure registry host is invalid") {
		t.Fatalf("error = %v, want invalid insecure registry", err)
	}
}
