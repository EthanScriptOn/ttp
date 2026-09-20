// Package imagebuild contains the provider-neutral contract between the TTP
// control plane and an image build executor.
package imagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotConfigured               = errors.New("image builder is not configured")
	ErrUnauthorized                = errors.New("image builder authentication failed")
	ErrRegistryPreflightFailed     = errors.New("image registry preflight failed")
	ErrPlatformConfigNotConfigured = errors.New("platform image build configuration is not configured")
)

// Builder turns one immutable source revision into an immutable OCI image.
// Implementations must never treat a Git SHA as an image reference.
type Builder interface {
	Build(context.Context, Request) (Result, error)
}

// PreflightChecker verifies builder-side credentials before a release enters
// the running state. Build remains the final authority because registries may
// apply repository-specific policy only when an upload starts.
type PreflightChecker interface {
	Preflight(context.Context, Request) error
}

// SourceCredential is sent only for the lifetime of a single build request.
// It must not be persisted or returned in a Result.
type SourceCredential struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

// PushCredential is supplied for one build only. The detached builder uses it
// to create a private, short-lived Docker config and never persists it.
type PushCredential struct {
	ConnectionID string `json:"connection_id,omitempty"`
	Registry     string `json:"registry"`
	AuthType     string `json:"auth_type"`
	Username     string `json:"username,omitempty"`
	Secret       string `json:"secret"`
}

// Request contains the source information a detached builder needs to build
// an exact revision, plus the optional project-owned push target. Dockerfile,
// context and platform settings remain platform-owned and never come from a
// project or release request.
type Request struct {
	ProjectID        string           `json:"project_id"`
	ReleaseID        string           `json:"release_id"`
	RepositoryURL    string           `json:"repository_url"`
	CommitSHA        string           `json:"commit_sha"`
	SourceCredential SourceCredential `json:"source_credential"`
	// ImageRepository is an optional legacy/project-specific push target in the
	// form "registry.host/namespace/repository" without a tag or digest. When
	// a managed RegistryCredential is present and this is empty, the builder
	// derives a stable repository path from that connection and ProjectID.
	ImageRepository    string         `json:"image_repository,omitempty"`
	RegistryCredential PushCredential `json:"registry_credential,omitempty"`
}

// LogEntry intentionally contains no command line or environment value. A
// builder may return sanitized raw tool output in Line.
type LogEntry struct {
	Stream string `json:"stream"`
	Level  string `json:"level"`
	Line   string `json:"line"`
}

// Result contains the immutable deployable artifact. Image always has the
// form registry/path@sha256:... when Build succeeds.
type Result struct {
	Image      string     `json:"image"`
	Digest     string     `json:"digest"`
	Logs       []LogEntry `json:"logs,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
}

// Failure carries sanitized build output back to the control plane without
// exposing credentials through error strings.
type Failure struct {
	Err  error
	Logs []LogEntry
}

func (e *Failure) Error() string {
	if e == nil || e.Err == nil {
		return "image build failed"
	}
	return e.Err.Error()
}

func (e *Failure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var (
	commitPattern   = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	platformPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*(?:/[a-z0-9][a-z0-9._-]*)?$`)
	credentialRef   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Validate checks request fields before any network, filesystem, or command
// execution happens in the build worker.
func (r Request) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" || strings.TrimSpace(r.ReleaseID) == "" || strings.ContainsAny(r.ProjectID+r.ReleaseID, "\x00\r\n") {
		return fmt.Errorf("project_id and release_id are required")
	}
	parsed, err := url.Parse(strings.TrimSpace(r.RepositoryURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("repository_url must be an http or https URL without credentials")
	}
	if !commitPattern.MatchString(strings.TrimSpace(r.CommitSHA)) {
		return fmt.Errorf("commit_sha must be a Git object ID")
	}
	if len(r.SourceCredential.Username) > 120 || len(r.SourceCredential.Token) > 4096 || strings.ContainsAny(r.SourceCredential.Username, "\x00\r\n") || strings.ContainsAny(r.SourceCredential.Token, "\x00\r\n") {
		return fmt.Errorf("source credential is invalid")
	}
	if strings.TrimSpace(r.ImageRepository) != "" {
		if err := ValidateImageRepository(r.ImageRepository); err != nil {
			return err
		}
	}
	if r.RegistryCredential != (PushCredential{}) {
		credential := r.RegistryCredential
		registry := strings.ToLower(strings.TrimSpace(credential.Registry))
		if _, err := NormalizeImageRepositoryPrefix(registry); err != nil || strings.Contains(registry, "/") {
			return fmt.Errorf("registry credential registry is invalid")
		}
		if strings.TrimSpace(r.ImageRepository) != "" {
			host, err := RegistryHost(r.ImageRepository)
			if err != nil || registry != host {
				return fmt.Errorf("registry credential does not match image_repository")
			}
		}
		if credential.AuthType != "basic" && credential.AuthType != "token" {
			return fmt.Errorf("registry credential auth_type must be basic or token")
		}
		if strings.TrimSpace(credential.Secret) == "" || len(credential.Secret) > 4096 || strings.ContainsAny(credential.Secret, "\x00\r\n") {
			return fmt.Errorf("registry credential secret is invalid")
		}
		if len(credential.Username) > 120 || strings.ContainsAny(credential.Username, "\x00\r\n") {
			return fmt.Errorf("registry credential username is invalid")
		}
		if credential.AuthType == "basic" && strings.TrimSpace(credential.Username) == "" {
			return fmt.Errorf("registry credential username is required for basic auth")
		}
		if credential.ConnectionID != "" && !credentialRef.MatchString(strings.TrimSpace(credential.ConnectionID)) {
			return fmt.Errorf("registry credential connection_id is invalid")
		}
	}
	return nil
}

func ValidateCredentialReference(value string) bool {
	return credentialRef.MatchString(strings.TrimSpace(value))
}

// NormalizePlatforms rejects duplicate and malformed platform values while
// preserving their order for BuildKit's multi-platform output setting.
func NormalizePlatforms(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 8 {
		return nil, fmt.Errorf("at least one and at most eight build platforms are required")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !platformPattern.MatchString(value) {
			return nil, fmt.Errorf("build platform is invalid")
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("build platform is duplicated")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

// ValidateImageRepository accepts an explicit OCI registry host and a
// repository path. Tags and digests are deliberately excluded because TTP
// creates a traceable tag and deploys the returned digest.
func ValidateImageRepository(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 500 || strings.ContainsAny(value, "\x00\r\n@ ") || strings.Contains(value, "://") {
		return fmt.Errorf("image_repository is invalid")
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		return fmt.Errorf("image_repository must include an explicit registry host")
	}
	host := strings.ToLower(parts[0])
	if host != "localhost" && !strings.ContainsAny(host, ".:") {
		return fmt.Errorf("image_repository must include an explicit registry host")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.ContainsAny(part, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			return fmt.Errorf("image_repository is invalid")
		}
	}
	if strings.Contains(parts[len(parts)-1], ":") {
		return fmt.Errorf("image_repository must not include a tag")
	}
	return nil
}

// NormalizeImageRepositoryPrefix validates the registry path owned by TTP.
// Projects never provide this value; TTP appends a stable project key before
// asking the builder to push an image.
func NormalizeImageRepositoryPrefix(value string) (string, error) {
	value = strings.TrimSuffix(strings.TrimSpace(value), "/")
	if value == "" || len(value) > 460 || strings.ContainsAny(value, "\x00\r\n@ ") || strings.Contains(value, "://") {
		return "", fmt.Errorf("image repository prefix is invalid")
	}
	parts := strings.Split(value, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("image repository prefix must include an explicit registry host")
	}
	host := strings.ToLower(parts[0])
	if host != parts[0] || (host != "localhost" && !strings.ContainsAny(host, ".:")) {
		return "", fmt.Errorf("image repository prefix must include an explicit registry host")
	}
	for _, part := range parts[1:] {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.ContainsAny(part, "ABCDEFGHIJKLMNOPQRSTUVWXYZ:@") {
			return "", fmt.Errorf("image repository prefix is invalid")
		}
	}
	return value, nil
}

// ImageRepositoryForProject derives an opaque, stable repository name from a
// TTP project identity. This keeps project names and user input out of OCI
// paths while allowing every project to share the platform registry.
func ImageRepositoryForProject(prefix, projectID string) (string, error) {
	prefix, err := NormalizeImageRepositoryPrefix(prefix)
	if err != nil {
		return "", err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || strings.ContainsAny(projectID, "\x00\r\n") {
		return "", fmt.Errorf("project id is invalid")
	}
	digest := sha256.Sum256([]byte(projectID))
	repository := prefix + "/project-" + hex.EncodeToString(digest[:12])
	if err := ValidateImageRepository(repository); err != nil {
		return "", err
	}
	return repository, nil
}

func RegistryHost(imageRepository string) (string, error) {
	if err := ValidateImageRepository(imageRepository); err != nil {
		return "", err
	}
	return strings.ToLower(strings.Split(strings.TrimSpace(imageRepository), "/")[0]), nil
}

func IsDigest(value string) bool {
	return digestPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}
