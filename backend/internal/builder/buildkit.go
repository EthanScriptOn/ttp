package builder

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	InsecureRegistries      []string
	AllowedSourceHosts      []string
	ImageRepositoryPrefix   string
	RegistryCredentialRef   string
	Platforms               []string
	Timeout                 time.Duration
}

type RegistryCredential struct {
	Registry string `json:"registry"`
	AuthType string `json:"auth_type,omitempty"`
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
	insecureRegistries    map[string]struct{}
	imageRepositoryPrefix string
	registryCredentialRef string
	platforms             []string
	timeout               time.Duration
}

func (e *BuildKitExecutor) Preflight(ctx context.Context, request imagebuild.Request) error {
	if e == nil {
		return imagebuild.ErrNotConfigured
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if err := e.validateSourceHost(request.RepositoryURL); err != nil {
		return err
	}
	imageRepository, credential, err := e.resolveImageRepository(request)
	if err != nil {
		return err
	}
	host, err := imagebuild.RegistryHost(imageRepository)
	if err != nil {
		return err
	}
	// /v2/ verifies that the configured identity can authenticate to the
	// registry. Repository-level push policy is still confirmed by BuildKit's
	// actual upload and returned as a release error.
	requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	registryURL := "https://" + host + "/v2/"
	if e.isInsecureRegistry(host) {
		registryURL = "http://" + host + "/v2/"
	}
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, registryURL, nil)
	if err != nil {
		return fmt.Errorf("registry preflight request failed: %w", err)
	}
	setRegistryAuth(req, credential)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("registry %s is unreachable: %w", host, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized && strings.Contains(strings.ToLower(response.Header.Get("WWW-Authenticate")), "bearer") {
		if err := e.verifyRegistryBearer(requestContext, response.Header.Get("WWW-Authenticate"), host, imageRepository, credential); err != nil {
			return err
		}
		return nil
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("builder registry credential has no access to %s (HTTP %d)", host, response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("builder registry preflight failed for %s (HTTP %d)", host, response.StatusCode)
	}
	return nil
}

var bearerChallengeAttribute = regexp.MustCompile(`(?i)([a-z][a-z0-9_-]*)="([^"]*)"`)

func (e *BuildKitExecutor) verifyRegistryBearer(ctx context.Context, challenge, host, repository string, credential RegistryCredential) error {
	attributes := make(map[string]string)
	for _, match := range bearerChallengeAttribute.FindAllStringSubmatch(challenge, -1) {
		attributes[strings.ToLower(match[1])] = match[2]
	}
	realm := strings.TrimSpace(attributes["realm"])
	if realm == "" {
		return fmt.Errorf("registry %s returned an invalid bearer challenge", host)
	}
	parsed, err := url.Parse(realm)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return fmt.Errorf("registry %s returned an invalid bearer realm", host)
	}
	query := parsed.Query()
	if service := strings.TrimSpace(attributes["service"]); service != "" {
		query.Set("service", service)
	}
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("registry %s returned an invalid repository challenge", host)
	}
	query.Set("scope", "repository:"+parts[1]+":pull,push")
	parsed.RawQuery = query.Encode()
	tokenRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return fmt.Errorf("registry bearer token request failed: %w", err)
	}
	setRegistryAuth(tokenRequest, credential)
	tokenResponse, err := (&http.Client{Timeout: 15 * time.Second}).Do(tokenRequest)
	if err != nil {
		return fmt.Errorf("registry bearer token request failed: %w", err)
	}
	defer tokenResponse.Body.Close()
	if tokenResponse.StatusCode == http.StatusUnauthorized || tokenResponse.StatusCode == http.StatusForbidden {
		return fmt.Errorf("builder registry credential has no push access to %s (HTTP %d)", host, tokenResponse.StatusCode)
	}
	if tokenResponse.StatusCode < 200 || tokenResponse.StatusCode >= 300 {
		return fmt.Errorf("registry bearer token request failed for %s (HTTP %d)", host, tokenResponse.StatusCode)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResponse.Body, 1<<20)).Decode(&payload); err != nil {
		return fmt.Errorf("decode registry bearer token response: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" && strings.TrimSpace(payload.AccessToken) == "" {
		return fmt.Errorf("registry %s returned no bearer token", host)
	}
	return nil
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
	insecureRegistries := make(map[string]struct{}, len(config.InsecureRegistries))
	for _, value := range config.InsecureRegistries {
		host := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, "/")))
		normalized, normalizeErr := imagebuild.NormalizeImageRepositoryPrefix(host)
		if normalizeErr != nil || strings.Contains(normalized, "/") {
			return nil, errors.New("insecure registry host is invalid")
		}
		insecureRegistries[normalized] = struct{}{}
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
		allowedSourceHosts: allowedHosts, registryCredentials: credentials, insecureRegistries: insecureRegistries,
		imageRepositoryPrefix: imageRepositoryPrefix, registryCredentialRef: registryCredentialRef, platforms: platforms, timeout: timeout,
	}, nil
}

func (e *BuildKitExecutor) isInsecureRegistry(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if _, ok := e.insecureRegistries[host]; ok {
		return true
	}
	parsed, err := url.Parse("https://" + host)
	if err != nil {
		return false
	}
	hostname := strings.Trim(parsed.Hostname(), "[]")
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
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
		if strings.TrimSpace(reference) == "" || strings.TrimSpace(value.Registry) == "" || strings.TrimSpace(value.Password) == "" {
			return nil, errors.New("registry credential entry is incomplete")
		}
		if strings.ContainsAny(value.Registry, "\x00\r\n/ ") {
			return nil, errors.New("registry credential host is invalid")
		}
		value.Registry = strings.ToLower(strings.TrimSpace(value.Registry))
		if strings.TrimSpace(value.AuthType) == "" {
			value.AuthType = "basic"
		}
		value.AuthType = strings.ToLower(strings.TrimSpace(value.AuthType))
		if value.AuthType != "basic" && value.AuthType != "token" {
			return nil, errors.New("registry credential auth type is invalid")
		}
		if value.AuthType == "basic" && strings.TrimSpace(value.Username) == "" {
			return nil, errors.New("registry credential username is required for basic auth")
		}
		result[reference] = value
	}
	return result, nil
}

func (e *BuildKitExecutor) Build(parent context.Context, request imagebuild.Request) (imagebuild.Result, error) {
	return e.build(parent, request, nil)
}

// BuildWithLogs runs the same BuildKit workflow while forwarding each
// sanitized output line as soon as the underlying command emits it.
func (e *BuildKitExecutor) BuildWithLogs(parent context.Context, request imagebuild.Request, onLog imagebuild.LogFunc) (imagebuild.Result, error) {
	if onLog == nil {
		return e.Build(parent, request)
	}
	return e.build(parent, request, onLog)
}

func (e *BuildKitExecutor) build(parent context.Context, request imagebuild.Request, onLog imagebuild.LogFunc) (imagebuild.Result, error) {
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
	var sink imagebuild.LogFunc
	if onLog != nil {
		var logMu sync.Mutex
		sink = func(entry imagebuild.LogEntry) {
			logMu.Lock()
			defer logMu.Unlock()
			if len(result.Logs) < 1200 {
				result.Logs = append(result.Logs, entry)
			}
			onLog(entry)
		}
	}
	if err := e.checkout(ctx, workDir, sourceDir, request, env, &result, credential.Password, sink); err != nil {
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
	if sink != nil {
		sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: "building and pushing image with BuildKit"})
	}
	output, buildErr := e.runLogged(ctx, workDir, buildEnv, e.buildctlPath, sink, []string{request.SourceCredential.Token, credential.Password},
		"--addr", e.buildkitAddr,
		"build",
		"--progress", "plain",
		"--frontend", "dockerfile.v0",
		"--local", "context="+contextAbsolute,
		"--local", "dockerfile="+filepath.Dir(dockerfileAbsolute),
		"--opt", "filename="+filepath.Base(dockerfileAbsolute),
		"--opt", "platform="+strings.Join(e.platforms, ","),
		"--output", "type=image,name="+imageTag+",push=true",
		"--metadata-file", metadataPath,
	)
	if sink == nil {
		appendOutput(&result, output, buildErr != nil, request.SourceCredential.Token, credential.Password)
	}
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
	if sink != nil {
		sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: "image build and push completed"})
	}
	return result, nil
}

// checkout fetches the exact release commit. Fetching from GitHub or GitLab
// from networks with unstable international transit commonly fails once with
// "Error in the HTTP2 framing layer" or a reset connection, so the network
// steps force HTTP/1.1 and retry with backoff before the release is marked
// failed. Local-only steps never retry.
func (e *BuildKitExecutor) checkout(ctx context.Context, workDir, sourceDir string, request imagebuild.Request, env []string, result *imagebuild.Result, registryPassword string, sink imagebuild.LogFunc) error {
	localSteps := [][]string{
		{"init", "--quiet", sourceDir},
		{"-C", sourceDir, "remote", "add", "origin", request.RepositoryURL},
	}
	fetchStep := []string{"-C", sourceDir, "-c", "http.version=HTTP/1.1", "-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60", "fetch", "--progress", "--depth=1", "origin", request.CommitSHA}
	checkoutStep := []string{"-C", sourceDir, "checkout", "--quiet", "--detach", "FETCH_HEAD"}

	if sink != nil {
		sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: "preparing source workspace"})
	}
	for _, args := range localSteps {
		output, err := e.runLogged(ctx, workDir, env, e.gitPath, sink, []string{request.SourceCredential.Token, registryPassword}, args...)
		if sink == nil {
			appendOutput(result, output, err != nil, request.SourceCredential.Token, registryPassword)
		}
		if err != nil {
			_, failure := e.failure(*result, "source checkout failed")
			return failure
		}
	}
	if err := e.runGitWithRetry(ctx, workDir, env, fetchStep, request.SourceCredential.Token, registryPassword, result, sink, "source fetch failed after retries"); err != nil {
		return err
	}
	if sink != nil {
		sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: "checking out source revision"})
	}
	output, err := e.runLogged(ctx, workDir, env, e.gitPath, sink, []string{request.SourceCredential.Token, registryPassword}, checkoutStep...)
	if sink == nil {
		appendOutput(result, output, err != nil, request.SourceCredential.Token, registryPassword)
	}
	if err != nil {
		_, failure := e.failure(*result, "source checkout failed")
		return failure
	}
	revOutput, err := e.runLogged(ctx, workDir, env, e.gitPath, sink, []string{request.SourceCredential.Token, registryPassword}, "-C", sourceDir, "rev-parse", "HEAD")
	if sink == nil {
		appendOutput(result, revOutput, err != nil, request.SourceCredential.Token, registryPassword)
	}
	if err != nil || !strings.EqualFold(strings.TrimSpace(revOutput), strings.TrimSpace(request.CommitSHA)) {
		_, failure := e.failure(*result, "checked-out source does not match the requested commit")
		return failure
	}
	if sink != nil {
		sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: "source revision ready"})
	}
	return nil
}

// runGitWithRetry executes a network-bound git step up to fetchAttempts times
// with exponential backoff. Every attempt's sanitized output is retained so
// operators can see the flaky failure they retried through.
func (e *BuildKitExecutor) runGitWithRetry(ctx context.Context, workDir string, env []string, args []string, token, registryPassword string, result *imagebuild.Result, sink imagebuild.LogFunc, failureMessage string) error {
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		if sink != nil {
			sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: fmt.Sprintf("fetching source revision (attempt %d/%d)", attempt, fetchAttempts)})
		}
		output, err := e.runLogged(ctx, workDir, env, e.gitPath, sink, []string{token, registryPassword}, args...)
		if sink == nil {
			appendOutput(result, output, err != nil, token, registryPassword)
		}
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			break
		}
		if attempt < fetchAttempts {
			wait := time.Duration(1<<(attempt-1)) * 3 * time.Second
			retryMessage := fmt.Sprintf("git network attempt %d/%d failed, retrying in %s", attempt, fetchAttempts, wait)
			if sink == nil {
				appendOutput(result, retryMessage, false, token, registryPassword)
			} else {
				sink(imagebuild.LogEntry{Stream: "stdout", Level: "INFO", Line: retryMessage})
			}
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
// retain an explicit repository for API compatibility; otherwise a managed
// connection derives a stable path from its registry host and project ID.
// Without a managed connection the platform-owned prefix naming and its
// configured credential reference are used. Registry passwords never leave
// this process in any path.
func (e *BuildKitExecutor) resolveImageRepository(request imagebuild.Request) (string, RegistryCredential, error) {
	empty := RegistryCredential{}
	if request.RegistryCredential != (imagebuild.PushCredential{}) {
		override := strings.TrimSpace(request.ImageRepository)
		host := strings.ToLower(strings.TrimSpace(request.RegistryCredential.Registry))
		if override == "" {
			var err error
			override, err = imagebuild.ImageRepositoryForProject(host, request.ProjectID)
			if err != nil {
				return "", empty, err
			}
		} else {
			if err := imagebuild.ValidateImageRepository(override); err != nil {
				return "", empty, errors.New("image_repository is invalid")
			}
			actualHost, err := imagebuild.RegistryHost(override)
			if err != nil || host != actualHost {
				return "", empty, errors.New("registry credential does not match image_repository")
			}
		}
		return override, RegistryCredential{Registry: host, AuthType: request.RegistryCredential.AuthType, Username: request.RegistryCredential.Username, Password: request.RegistryCredential.Secret}, nil
	}
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
				if candidate.AuthType == "" {
					candidate.AuthType = "basic"
				}
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
	if credential.AuthType == "" {
		credential.AuthType = "basic"
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

func (e *BuildKitExecutor) runLogged(ctx context.Context, directory string, environment []string, path string, sink imagebuild.LogFunc, secrets []string, args ...string) (string, error) {
	if sink == nil {
		return e.run(ctx, directory, environment, path, args...)
	}
	return e.runStream(ctx, directory, environment, path, sink, secrets, args...)
}

func (e *BuildKitExecutor) runStream(ctx context.Context, directory string, environment []string, path string, sink imagebuild.LogFunc, secrets []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, path, args...)
	command.Dir = directory
	command.Env = environment
	output := &cappedBuffer{limit: maxToolOutputBytes}
	stdout := &streamLineWriter{output: output, stream: "stdout", level: "INFO", sink: sink, secrets: secrets}
	stderr := &streamLineWriter{output: output, stream: "stderr", level: "ERROR", sink: sink, secrets: secrets}
	if isBuildKitCommand(path, args) {
		// buildctl --progress plain writes its normal progress stream to stderr.
		// Only actual failure/warning lines should be elevated in the release log.
		stderr.levelFunc = classifyBuildKitStderr
	} else if isGitFetchCommand(path, args) {
		// Git writes successful fetch progress ("From ..." and
		// "* branch ... -> FETCH_HEAD") to stderr as well. Keep those lines
		// informational so a successful release does not show an exception card.
		stderr.levelFunc = classifyGitFetchStderr
	}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	stdout.Flush()
	stderr.Flush()
	if output.truncated {
		output.Write([]byte("\n[tool output truncated]\n"))
		sink(imagebuild.LogEntry{Stream: "stderr", Level: "WARN", Line: "[tool output truncated]"})
	}
	return output.String(), err
}

type streamLineWriter struct {
	output    *cappedBuffer
	stream    string
	level     string
	levelFunc func(string) string
	sink      imagebuild.LogFunc
	secrets   []string
	mu        sync.Mutex
	pending   string
}

func (w *streamLineWriter) Write(value []byte) (int, error) {
	if w.output != nil {
		_, _ = w.output.Write(value)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(value)
	for {
		index := strings.IndexAny(w.pending, "\r\n")
		if index < 0 {
			break
		}
		delimiter := w.pending[index]
		line := w.pending[:index]
		w.pending = w.pending[index+1:]
		if delimiter == '\r' && strings.HasPrefix(w.pending, "\n") {
			w.pending = w.pending[1:]
		}
		w.emit(line)
	}
	return len(value), nil
}

func (w *streamLineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if strings.TrimSpace(w.pending) != "" {
		w.emit(w.pending)
	}
	w.pending = ""
}

func (w *streamLineWriter) emit(value string) {
	value = redact(strings.TrimSpace(value), w.secrets...)
	if value == "" || w.sink == nil {
		return
	}
	level := w.level
	if w.levelFunc != nil {
		level = w.levelFunc(value)
	}
	w.sink(imagebuild.LogEntry{Stream: w.stream, Level: level, Line: value})
}

func isBuildKitCommand(path string, args []string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	if base != "buildctl" && base != "buildctl-daemonless.sh" {
		return false
	}
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), "build") {
			return true
		}
	}
	return false
}

func isGitFetchCommand(path string, args []string) bool {
	if strings.ToLower(filepath.Base(strings.TrimSpace(path))) != "git" {
		return false
	}
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), "fetch") {
			return true
		}
	}
	return false
}

func classifyBuildKitStderr(line string) string {
	value := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(value, "warning"), strings.Contains(value, "deprecated"):
		return "WARN"
	case strings.Contains(value, "failed"), strings.Contains(value, "error"), strings.Contains(value, "fatal"),
		strings.Contains(value, "denied"), strings.Contains(value, "unauthorized"), strings.Contains(value, "not found"):
		return "ERROR"
	default:
		return "INFO"
	}
}

func classifyGitFetchStderr(line string) string {
	value := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(value, "warning"):
		return "WARN"
	case strings.Contains(value, "fatal"), strings.Contains(value, "error"), strings.Contains(value, "failed"),
		strings.Contains(value, "denied"), strings.Contains(value, "unauthorized"):
		return "ERROR"
	default:
		return "INFO"
	}
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
	entry := map[string]string{}
	if credential.AuthType == "token" {
		entry["identitytoken"] = credential.Password
	} else {
		auth := base64.StdEncoding.EncodeToString([]byte(credential.Username + ":" + credential.Password))
		entry["auth"] = auth
	}
	payload, err := json.Marshal(map[string]any{"auths": map[string]any{credential.Registry: entry}})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "config.json"), payload, 0o600)
}

func setRegistryAuth(request *http.Request, credential RegistryCredential) {
	if credential.AuthType == "token" {
		request.Header.Set("Authorization", "Bearer "+credential.Password)
		return
	}
	request.SetBasicAuth(credential.Username, credential.Password)
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
