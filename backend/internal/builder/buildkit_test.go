package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestRunStreamEmitsBeforeCommandCompletes(t *testing.T) {
	executor := &BuildKitExecutor{}
	firstLine := make(chan imagebuild.LogEntry, 1)
	done := make(chan error, 1)
	go func() {
		_, err := executor.runStream(context.Background(), t.TempDir(), os.Environ(), "/bin/sh", func(entry imagebuild.LogEntry) {
			select {
			case firstLine <- entry:
			default:
			}
		}, nil, "-c", "printf 'first\\n'; sleep 1; printf 'second\\n'")
		done <- err
	}()

	select {
	case entry := <-firstLine:
		if entry.Line != "first" {
			t.Fatalf("first streamed line = %q, want first", entry.Line)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive the first log line while the command was still running")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runStream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamed command did not finish")
	}
}

func TestBuildKitStderrProgressUsesSemanticLevels(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: "#1 [internal] load build definition from Dockerfile", want: "INFO"},
		{line: "#1 DONE 0.0s", want: "INFO"},
		{line: "warning: legacy frontend is deprecated", want: "WARN"},
		{line: "ERROR: failed to solve: denied", want: "ERROR"},
	}
	for _, test := range tests {
		if got := classifyBuildKitStderr(test.line); got != test.want {
			t.Errorf("classifyBuildKitStderr(%q) = %q, want %q", test.line, got, test.want)
		}
	}
}

func TestRunStreamClassifiesBuildKitAndGitFetchStderr(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "buildctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '#1 DONE 0.0s\\n' >&2\nprintf 'failed to solve: denied\\n' >&2\n"), 0o755); err != nil {
		t.Fatalf("write fake buildctl: %v", err)
	}
	var got []imagebuild.LogEntry
	if _, err := (&BuildKitExecutor{}).runStream(context.Background(), t.TempDir(), os.Environ(), bin, func(entry imagebuild.LogEntry) {
		got = append(got, entry)
	}, nil, "build"); err != nil {
		t.Fatalf("runStream buildctl: %v", err)
	}
	if len(got) != 2 || got[0].Level != "INFO" || got[1].Level != "ERROR" {
		t.Fatalf("BuildKit stderr levels = %#v, want INFO then ERROR", got)
	}

	gitBin := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nprintf 'remote: Enumerating objects: 12, done.\\n' >&2\nprintf 'Receiving objects: 50%% (6/12)\\r' >&2\nprintf 'From https://github.com/example/repo\\n' >&2\nprintf '* branch deadbeef -> FETCH_HEAD\\n' >&2\nprintf 'fatal: unable to access repository\\n' >&2\n"), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	got = nil
	if _, err := (&BuildKitExecutor{}).runStream(context.Background(), t.TempDir(), os.Environ(), gitBin, func(entry imagebuild.LogEntry) {
		got = append(got, entry)
	}, nil, "fetch"); err != nil {
		t.Fatalf("runStream git: %v", err)
	}
	if len(got) != 5 || got[0].Level != "INFO" || got[1].Level != "INFO" || got[2].Level != "INFO" || got[3].Level != "INFO" || got[4].Level != "ERROR" {
		t.Fatalf("Git fetch stderr levels = %#v, want progress INFO and fatal ERROR", got)
	}
}

func TestCheckoutStreamsStagesAndRequestsGitProgress(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	workDir := t.TempDir()
	gitBin := filepath.Join(workDir, "git")
	script := `#!/bin/sh
case "$*" in
  *"fetch"*)
    case "$*" in
      *"--progress"*) printf 'Receiving objects: 50%% (1/2)\r' >&2 ;;
      *) exit 42 ;;
    esac
    ;;
  *"rev-parse HEAD"*) printf '%s\n' "$TEST_COMMIT" ;;
esac
`
	if err := os.WriteFile(gitBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	executor := &BuildKitExecutor{gitPath: gitBin}
	var logs []imagebuild.LogEntry
	request := imagebuild.Request{RepositoryURL: "https://github.com/example/repository.git", CommitSHA: commit}
	environment := append(os.Environ(), "TEST_COMMIT="+commit)
	if err := executor.checkout(context.Background(), workDir, filepath.Join(workDir, "source"), request, environment, &imagebuild.Result{}, "", func(entry imagebuild.LogEntry) {
		logs = append(logs, entry)
	}); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	joined := make([]string, 0, len(logs))
	for _, entry := range logs {
		joined = append(joined, entry.Level+" "+entry.Line)
	}
	output := strings.Join(joined, "\n")
	for _, want := range []string{
		"INFO preparing source workspace",
		"INFO fetching source revision (attempt 1/4)",
		"INFO Receiving objects: 50% (1/2)",
		"INFO checking out source revision",
		"INFO source revision ready",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("checkout logs = %q, want %q", output, want)
		}
	}
}
