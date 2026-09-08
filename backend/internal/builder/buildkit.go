package builder

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
)

const (
	maxToolOutputBytes = 256 << 10
	// fetchAttempts is how often a network-bound git step (GitHub/GitLab
	// fetch) is retried with exponential backoff before a release fails.
	fetchAttempts = 4
)

type BuildKitConfig struct {
	WorkDir                 string
	GitPath                 string
	BuildctlPath            string
	BuildkitAddr            string
	RegistryCredentialsFile string
	AllowedSourceHosts      []string
	ImageRepositoryPrefix   string
	RegistryCredentialRef   string
	Platforms               []string
	Timeout                 time.Duration
}

type RegistryCredential struct {
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type BuildKitExecutor struct {
	workDir               string
	gitPath               string
	buildctlPath          string
	buildkitAddr          string
	allowedSourceHosts    map[string]struct{}
	registryCredentials   map[string]RegistryCredential
	imageRepositoryPrefix string
	registryCredentialRef string
	platforms             []string
	timeout               time.Duration
}

func NewBuildKitExecutor(config BuildKitConfig) (*BuildKitExecutor, error) {
	workDir := strings.TrimSpace(config.WorkDir)
	if workDir == "" {
		return nil, errors.New("builder work directory is required")
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, fmt.Errorf("create builder work directory: %w", err)
	}
	gitPath, err := executablePath(config.GitPath, "git")
	if err != nil {
		return nil, err
	}
	buildctlPath, err := executablePath(config.BuildctlPath, "buildctl")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.BuildkitAddr) == "" {
		return nil, errors.New("BuildKit address is required")
	}
	allowedHosts := make(map[string]struct{}, len(config.AllowedSourceHosts))
	for _, value := range config.AllowedSourceHosts {
		host := strings.ToLower(strings.TrimSpace(value))
		if host != "" {
			allowedHosts[host] = struct{}{}
		}
	}
	if len(allowedHosts) == 0 {
		return nil, errors.New("at least one allowed source host is required")
	}
	imageRepositoryPrefix, err := imagebuild.NormalizeImageRepositoryPrefix(config.ImageRepositoryPrefix)
	if err != nil {
		return nil, err
	}
	registryCredentialRef := strings.TrimSpace(config.RegistryCredentialRef)
	if !imagebuild.ValidateCredentialReference(registryCredentialRef) {
		return nil, errors.New("registry credential reference is required")
	}
	platforms, err := imagebuild.NormalizePlatforms(config.Platforms)
	if err != nil {
		return nil, err
	}
	credentials, err := loadRegistryCredentials(config.RegistryCredentialsFile)
	if err != nil {
		return nil, err
	}
	credential, ok := credentials[registryCredentialRef]
	if !ok {
		return nil, errors.New("configured registry credential reference was not found")
	}
	registryHost, err := imagebuild.RegistryHost(imageRepositoryPrefix + "/project-check")
	if err != nil || credential.Registry != registryHost {
		return nil, errors.New("configured registry credential does not match the image repository")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	return &BuildKitExecutor{
		workDir: workDir, gitPath: gitPath, buildctlPath: buildctlPath, buildkitAddr: strings.TrimSpace(config.BuildkitAddr),
		allowedSourceHosts: allowedHosts, registryCredentials: credentials,
		imageRepositoryPrefix: imageRepositoryPrefix, registryCredentialRef: registryCredentialRef, platforms: platforms, timeout: timeout,
	}, nil
}

func executablePath(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	path, err := exec.LookPath(value)
	if err != nil {
		return "", fmt.Errorf("required executable %q was not found", value)
	}
	return path, nil
}

func loadRegistryCredentials(path string) (map[string]RegistryCredential, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("registry credentials file is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat registry credentials file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("registry credentials file must be a private regular file (0600)")
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry credentials file: %w", err)
	}
	if len(bytes) > 1<<20 {
		return nil, errors.New("registry credentials file is too large")
	}
	var values map[string]RegistryCredential
	if err := json.Unmarshal(bytes, &values); err != nil {
		return nil, fmt.Errorf("decode registry credentials file: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("registry credentials file has no credentials")
	}
	result := make(map[string]RegistryCredential, len(values))
	for reference, value := range values {
		if strings.TrimSpace(reference) == "" || strings.TrimSpace(value.Registry) == "" || strings.TrimSpace(value.Username) == "" || strings.TrimSpace(value.Password) == "" {
			return nil, errors.New("registry credential entry is incomplete")
		}
		if strings.ContainsAny(value.Registry, "\x00\r\n/ ") {
			return nil, errors.New("registry credential host is invalid")
		}
		value.Registry = strings.ToLower(strings.TrimSpace(value.Registry))
		result[reference] = value
	}
	return result, nil
}

func (e *BuildKitExecutor) Build(parent context.Context, request imagebuild.Request) (imagebuild.Result, error) {
	startedAt := time.Now().UTC()
	result := imagebuild.Result{StartedAt: startedAt}
	if e == nil {
		return result, imagebuild.ErrNotConfigured
	}
	if err := request.Validate(); err != nil {
		return result, err
	}
	if err := e.validateSourceHost(request.RepositoryURL); err != nil {
		return result, err
	}
	imageRepository, credential, err := e.resolveImageRepository(request)
	if err != nil {
		return e.failure(result, err.Error())
	}
	ctx, cancel := context.WithTimeout(parent, e.timeout)
	defer cancel()
	workDir, err := os.MkdirTemp(e.workDir, "build-")
	if err != nil {
		return result, fmt.Errorf("create build workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	sourceDir := filepath.Join(workDir, "source")
	env, cleanupAskpass, err := gitEnvironment(workDir, request.SourceCredential)
	if err != nil {
		return result, err
	}
	defer cleanupAskpass()
	if err := e.checkout(ctx, workDir, sourceDir, request, env, &result, credential.Password); err != nil {
		result.FinishedAt = time.Now().UTC()
		return result, err
	}
	dockerfilePath := "Dockerfile"
	contextPath := "."
	dockerfileAbsolute := filepath.Join(sourceDir, filepath.FromSlash(dockerfilePath))
	if info, statErr := os.Stat(dockerfileAbsolute); statErr != nil || info.IsDir() {
		return e.failure(result, "Dockerfile does not exist in the checked-out commit")
	}
	contextAbsolute := filepath.Join(sourceDir, filepath.FromSlash(contextPath))
	if info, statErr := os.Stat(contextAbsolute); statErr != nil || !info.IsDir() {
		return e.failure(result, "build context does not exist in the checked-out commit")
	}
	if err := writeDockerConfig(workDir, credential); err != nil {
		return e.failure(result, "prepare registry authentication failed")
	}
	metadataPath := filepath.Join(workDir, "metadata.json")
	imageTag := imageRepository + ":sha-" + strings.ToLower(strings.TrimSpace(request.CommitSHA))
	buildEnv := append(append([]string(nil), env...), "HOME="+workDir, "DOCKER_CONFIG="+filepath.Join(workDir, ".docker"))
	output, buildErr := e.run(ctx, workDir, buildEnv, e.buildctlPath,
		"--addr", e.buildkitAddr,
		"build",
		"--frontend", "dockerfile.v0",
		"--local", "context="+contextAbsolute,
		"--local", "dockerfile="+filepath.Dir(dockerfileAbsolute),
		"--opt", "filename="+filepath.Base(dockerfileAbsolute),
		"--opt", "platform="+strings.Join(e.platforms, ","),
		"--output", "type=image,name="+imageTag+",push=true",
		"--metadata-file", metadataPath,
	)
	appendOutput(&result, output, buildErr != nil, request.SourceCredential.Token, credential.Password)
	if buildErr != nil {
		return e.failure(result, "BuildKit failed to build or push the image")
	}
	digest, err := readImageDigest(metadataPath)
	if err != nil {
		return e.failure(result, "BuildKit did not return an immutable image digest")
	}
	result.Digest = digest
	result.Image = imageRepository + "@" + digest
	result.FinishedAt = time.Now().UTC()
	return result, nil
}

// checkout fetches the exact release commit. Fetching from GitHub or GitLab
// from networks with unstable international transit commonly fails once with
// "Error in the HTTP2 framing layer" or a reset connection, so the network
// steps force HTTP/1.1 and retry with backoff before the release is marked
// failed. Local-only steps never retry.
func (e *BuildKitExecutor) checkout(ctx context.Context, workDir, sourceDir string, request imagebuild.Request, env []string, result *imagebuild.Result, registryPassword string) error {
	localSteps := [][]string{
		{"init", "--quiet", sourceDir},
		{"-C", sourceDir, "remote", "add", "origin", request.RepositoryURL},
	}
	fetchStep := []string{"-C", sourceDir, "-c", "http.version=HTTP/1.1", "-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60", "fetch", "--depth=1", "origin", request.CommitSHA}
	checkoutStep := []string{"-C", sourceDir, "checkout", "--quiet", "--detach", "FETCH_HEAD"}

	for _, args := range localSteps {
		output, err := e.run(ctx, workDir, env, e.gitPath, args...)
		appendOutput(result, output, err != nil, request.SourceCredential.Token, registryPassword)
		if err != nil {
			_, failure := e.failure(*result, "source checkout failed")
			return failure
		}
	}
	if err := e.runGitWithRetry(ctx, workDir, env, fetchStep, request.SourceCredential.Token, registryPassword, result, "source fetch failed after retries"); err != nil {
		return err
	}
	output, err := e.run(ctx, workDir, env, e.gitPath, checkoutStep...)
	appendOutput(result, output, err != nil, request.SourceCredential.Token, registryPassword)
	if err != nil {
		_, failure := e.failure(*result, "source checkout failed")
		return failure
	}
	revOutput, err := e.run(ctx, workDir, env, e.gitPath, "-C", sourceDir, "rev-parse", "HEAD")
	appendOutput(result, revOutput, err != nil, request.SourceCredential.Token, registryPassword)
	if err != nil || !strings.EqualFold(strings.TrimSpace(revOutput), strings.TrimSpace(request.CommitSHA)) {
		_, failure := e.failure(*result, "checked-out source does not match the requested commit")
		return failure
	}
	return nil
}

// runGitWithRetry executes a network-bound git step up to fetchAttempts times
// with exponential backoff. Every attempt's sanitized output is retained so
// operators can see the flaky failure they retried through.
func (e *BuildKitExecutor) runGitWithRetry(ctx context.Context, workDir string, env []string, args []string, token, registryPassword string, result *imagebuild.Result, failureMessage string) error {
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		output, err := e.run(ctx, workDir, env, e.gitPath, args...)
		appendOutput(result, output, err != nil, token, registryPassword)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			break
		}
		if attempt < fetchAttempts {
			wait := time.Duration(1<<(attempt-1)) * 3 * time.Second
			appendOutput(result, fmt.Sprintf("git network attempt %d/%d failed, retrying in %s", attempt, fetchAttempts, wait), false, token, registryPassword)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	_, failure := e.failure(*result, failureMessage)
	return failure
}

// resolveImageRepository picks the push target for one build. A project may
// specify its own repository; the registry host must then match one of the
// credentials mapped on this builder. Without a project override the
// platform-owned prefix naming and its configured credential reference are
// used. Registry passwords never leave this process in either path.
func (e *BuildKitExecutor) resolveImageRepository(request imagebuild.Request) (string, RegistryCredential, error) {
	empty := RegistryCredential{}
	if override := strings.TrimSpace(request.ImageRepository); override != "" {
		if err := imagebuild.ValidateImageRepository(override); err != nil {
			return "", empty, errors.New("image_repository is invalid")
		}
		host, err := imagebuild.RegistryHost(override)
		if err != nil {
			return "", empty, errors.New("image_repository is invalid")
		}
		for _, candidate := range e.registryCredentials {
			if candidate.Registry == host {
				return override, candidate, nil
			}
		}
		return "", empty, fmt.Errorf("no builder credential is configured for registry %s", host)
	}
	imageRepository, err := imagebuild.ImageRepositoryForProject(e.imageRepositoryPrefix, request.ProjectID)
	if err != nil {
		return "", empty, err
	}
	credential, ok := e.registryCredentials[e.registryCredentialRef]
	if !ok {
		return "", empty, errors.New("configured registry credential reference was not found")
	}
	return imageRepository, credential, nil
}

func (e *BuildKitExecutor) validateSourceHost(repositoryURL string) error {
	parsed, err := url.Parse(repositoryURL)
	if err != nil {
		return errors.New("repository URL is invalid")
	}
	host := strings.ToLower(parsed.Hostname())
	if _, ok := e.allowedSourceHosts[host]; !ok {
		return fmt.Errorf("repository host is not allowed by this builder")
	}
	return nil
}

func (e *BuildKitExecutor) failure(result imagebuild.Result, message string) (imagebuild.Result, error) {
	result.FinishedAt = time.Now().UTC()
	return result, &imagebuild.Failure{Err: errors.New(message), Logs: result.Logs}
}

func (e *BuildKitExecutor) run(ctx context.Context, directory string, environment []string, path string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, path, args...)
	command.Dir = directory
	command.Env = environment
	output := &cappedBuffer{limit: maxToolOutputBytes}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if output.truncated {
		output.Write([]byte("\n[tool output truncated]\n"))
	}
	return output.String(), err
}

func gitEnvironment(workDir string, credential imagebuild.SourceCredential) ([]string, func(), error) {
	home := filepath.Join(workDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create Git home: %w", err)
	}
	environment := append([]string(nil), os.Environ()...)
	environment = append(environment, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if strings.TrimSpace(credential.Token) == "" {
		return environment, func() {}, nil
	}
	if strings.TrimSpace(credential.Username) == "" {
		return nil, nil, errors.New("source credential username is required")
	}
	askpass := filepath.Join(workDir, "git-askpass")
	const script = "#!/bin/sh\ncase \"$1\" in\n  *Username*|*username*) printf '%s\\n' \"$TTP_BUILDER_GIT_USERNAME\" ;;\n  *) printf '%s\\n' \"$TTP_BUILDER_GIT_TOKEN\" ;;\nesac\n"
	if err := os.WriteFile(askpass, []byte(script), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create Git credential helper: %w", err)
	}
	environment = append(environment,
		"GIT_ASKPASS="+askpass,
		"TTP_BUILDER_GIT_USERNAME="+credential.Username,
		"TTP_BUILDER_GIT_TOKEN="+credential.Token,
	)
	return environment, func() { _ = os.Remove(askpass) }, nil
}

func writeDockerConfig(workDir string, credential RegistryCredential) error {
	directory := filepath.Join(workDir, ".docker")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password))
	payload, err := json.Marshal(map[string]any{"auths": map[string]any{credential.Registry: map[string]string{"auth": auth}}})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "config.json"), payload, 0o600)
}

func readImageDigest(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(content, &metadata); err != nil {
		return "", err
	}
	var digest string
	if value, ok := metadata["containerimage.digest"]; ok {
		_ = json.Unmarshal(value, &digest)
	}
	digest = strings.ToLower(strings.TrimSpace(digest))
	if !imagebuild.IsDigest(digest) {
		return "", errors.New("missing image digest")
	}
	return digest, nil
}

func appendOutput(result *imagebuild.Result, output string, failed bool, secrets ...string) {
	output = redact(output, secrets...)
	stream, level := "stdout", "INFO"
	if failed {
		stream, level = "stderr", "ERROR"
	}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(result.Logs) >= 1200 {
			return
		}
		result.Logs = append(result.Logs, imagebuild.LogEntry{Stream: stream, Level: level, Line: line})
	}
}

func redact(value string, secrets ...string) string {
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buffer.Len() >= b.limit {
		b.truncated = true
		return len(value), nil
	}
	remaining := b.limit - b.buffer.Len()
	if len(value) > remaining {
		_, _ = b.buffer.Write(value[:remaining])
		b.truncated = true
		return len(value), nil
	}
	_, _ = b.buffer.Write(value)
	return len(value), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
