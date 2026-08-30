package git

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RepositoryRegistrar lets the API bind a project repository URL to the
// provider used by the release service. It keeps the public Provider
// interface backwards-compatible while allowing one installation to manage
// repositories from more than one project.
type RepositoryRegistrar interface {
	RegisterRepository(repositoryID, repositoryURL string) error
}

// RegistryConfig controls the read-only GitHub/GitLab providers created by a
// RegistryProvider. Tokens remain private to the provider and are never part
// of repository IDs, URLs, or errors.
type RegistryConfig struct {
	Provider       string
	Token          string
	APIBaseURL     string
	AllowedHosts   []string
	Timeout        time.Duration
	HTTPClient     *http.Client
	ServiceAccount ServiceAccount
}

// RegistryProvider lazily holds one concrete provider per repository. The
// API registers a project when it is created and registers it again on first
// access after a restart.
type RegistryProvider struct {
	config    RegistryConfig
	mu        sync.RWMutex
	providers map[string]registryEntry
	urls      map[string]string
}

type registryEntry struct {
	provider  Provider
	backendID string
}

var _ Provider = (*RegistryProvider)(nil)
var _ RepositoryRegistrar = (*RegistryProvider)(nil)
var _ TagProvider = (*RegistryProvider)(nil)
var _ ServiceAccountProvider = (*RegistryProvider)(nil)
var _ AccessChecker = (*RegistryProvider)(nil)

func NewRegistry(config RegistryConfig) (*RegistryProvider, error) {
	kind := normalizeProviderKind(config.Provider)
	if kind != "auto" && kind != "github" && kind != "gitlab" {
		return nil, fmt.Errorf("%w: provider must be auto, github, or gitlab", ErrInvalidProviderConfig)
	}
	if strings.ContainsAny(config.Token, "\r\n") {
		return nil, fmt.Errorf("%w: token contains invalid characters", ErrInvalidProviderConfig)
	}
	serviceAccount := normalizeServiceAccount(config.ServiceAccount, kind)
	serviceAccount.Configured = strings.TrimSpace(config.Token) != ""
	return &RegistryProvider{config: RegistryConfig{
		Provider:       kind,
		Token:          config.Token,
		APIBaseURL:     strings.TrimSpace(config.APIBaseURL),
		AllowedHosts:   append([]string(nil), config.AllowedHosts...),
		Timeout:        config.Timeout,
		HTTPClient:     config.HTTPClient,
		ServiceAccount: serviceAccount,
	}, providers: make(map[string]registryEntry), urls: make(map[string]string)}, nil
}

func normalizeProviderKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "auto"
	}
	return value
}

func (r *RegistryProvider) RegisterRepository(repositoryID, repositoryURL string) error {
	if r == nil {
		return fmt.Errorf("%w: provider is not initialized", ErrInvalidProviderConfig)
	}
	repositoryID = strings.TrimSpace(repositoryID)
	repositoryURL = strings.TrimSpace(repositoryURL)
	if repositoryID == "" || repositoryURL == "" {
		return fmt.Errorf("%w: repository ID and URL are required", ErrInvalidProviderConfig)
	}

	r.mu.RLock()
	registeredURL, registered := r.urls[repositoryID]
	r.mu.RUnlock()
	if registered {
		if registeredURL == repositoryURL {
			return nil
		}
		return fmt.Errorf("%w: repository ID %q is already bound to another URL", ErrRepositoryConflict, repositoryID)
	}

	parsed, err := parseHTTPURL(repositoryURL)
	if err != nil {
		return err
	}
	kind, err := r.kindForURL(parsed)
	if err != nil {
		return err
	}
	options := make([]ProviderOption, 0, 5)
	if r.config.APIBaseURL != "" {
		options = append(options, WithAPIBaseURL(r.config.APIBaseURL))
	}
	if len(r.config.AllowedHosts) > 0 {
		options = append(options, WithAllowedHosts(r.config.AllowedHosts...))
	}
	if r.config.Timeout != 0 {
		options = append(options, WithTimeout(r.config.Timeout))
	}
	if r.config.HTTPClient != nil {
		options = append(options, WithHTTPClient(r.config.HTTPClient))
	}
	options = append(options, WithServiceAccount(r.config.ServiceAccount))

	var provider Provider
	switch kind {
	case "github":
		provider, err = NewGitHubProvider(repositoryURL, r.config.Token, options...)
	case "gitlab":
		provider, err = NewGitLabProvider(repositoryURL, r.config.Token, options...)
	}
	if err != nil {
		return err
	}
	parsedRepository, parseErr := parseRepositoryRef(parsed, repositoryURL, func() remoteKind {
		if kind == "github" {
			return remoteGitHub
		}
		return remoteGitLab
	}())
	if parseErr != nil {
		return parseErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if registeredURL, registered := r.urls[repositoryID]; registered {
		if registeredURL == repositoryURL {
			return nil
		}
		return fmt.Errorf("%w: repository ID %q is already bound to another URL", ErrRepositoryConflict, repositoryID)
	}
	r.providers[repositoryID] = registryEntry{provider: provider, backendID: parsedRepository.canonicalURL}
	r.urls[repositoryID] = repositoryURL
	return nil
}

func (r *RegistryProvider) kindForURL(repositoryURL *url.URL) (string, error) {
	configured := normalizeProviderKind(r.config.Provider)
	if configured != "auto" {
		return configured, nil
	}
	host := strings.ToLower(strings.TrimSuffix(repositoryURL.Hostname(), "."))
	switch host {
	case "github.com", "www.github.com":
		return "github", nil
	case "gitlab.com", "www.gitlab.com":
		return "gitlab", nil
	default:
		return "", fmt.Errorf("%w: custom Git host %q requires CICD_GIT_PROVIDER", ErrInvalidProviderConfig, host)
	}
}

func (r *RegistryProvider) providerFor(repositoryID string) (registryEntry, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	if repositoryID == "" {
		return registryEntry{}, ErrRepositoryNotFound
	}
	r.mu.RLock()
	entry := r.providers[repositoryID]
	r.mu.RUnlock()
	if entry.provider == nil {
		return registryEntry{}, ErrRepositoryNotFound
	}
	return entry, nil
}

func (r *RegistryProvider) ListRepositories(ctx context.Context) ([]Repository, error) {
	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider.provider)
	}
	r.mu.RUnlock()
	result := make([]Repository, 0, len(providers))
	for _, provider := range providers {
		items, err := provider.ListRepositories(ctx)
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
	}
	return result, nil
}

func (r *RegistryProvider) ListBranches(ctx context.Context, repositoryID string) ([]Branch, error) {
	entry, err := r.providerFor(repositoryID)
	if err != nil {
		return nil, err
	}
	return entry.provider.ListBranches(ctx, entry.backendID)
}

func (r *RegistryProvider) ListCommits(ctx context.Context, repositoryID, branch string, limit int) ([]Commit, error) {
	entry, err := r.providerFor(repositoryID)
	if err != nil {
		return nil, err
	}
	return entry.provider.ListCommits(ctx, entry.backendID, branch, limit)
}

func (r *RegistryProvider) ListTags(ctx context.Context, repositoryID string, limit int) ([]Tag, error) {
	entry, err := r.providerFor(repositoryID)
	if err != nil {
		return nil, err
	}
	provider, ok := entry.provider.(TagProvider)
	if !ok {
		return []Tag{}, nil
	}
	return provider.ListTags(ctx, entry.backendID, limit)
}

func (r *RegistryProvider) GetCommit(ctx context.Context, repositoryID, sha string) (Commit, error) {
	entry, err := r.providerFor(repositoryID)
	if err != nil {
		return Commit{}, err
	}
	return entry.provider.GetCommit(ctx, entry.backendID, sha)
}

// ServiceAccount returns the installation-wide public identity. Credentials
// stay private to the concrete providers held by the registry.
func (r *RegistryProvider) ServiceAccount() ServiceAccount {
	if r == nil {
		return normalizeServiceAccount(ServiceAccount{Provider: "unknown"}, "unknown")
	}
	return normalizeServiceAccount(r.config.ServiceAccount, r.config.Provider)
}

// CheckRepositoryAccess delegates the permission check to the concrete
// provider while keeping the caller-facing repository ID stable.
func (r *RegistryProvider) CheckRepositoryAccess(ctx context.Context, repositoryID string) (RepositoryAccess, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	entry, err := r.providerFor(repositoryID)
	if err != nil {
		return RepositoryAccess{}, err
	}
	checker, ok := entry.provider.(AccessChecker)
	if !ok {
		report := RepositoryAccess{
			RepositoryID:  repositoryID,
			RepositoryURL: r.repositoryURL(repositoryID),
			Account:       r.ServiceAccount(),
			Supported:     false,
			Message:       "当前 Git 连接不支持验证平台服务账号，发布操作已被阻止。",
			CheckedAt:     time.Now().UTC(),
		}
		return report, nil
	}
	report, err := checker.CheckRepositoryAccess(ctx, entry.backendID)
	// Keep the caller's stable project-facing ID. The concrete provider uses
	// its canonical URL internally, but that value must not leak into API
	// records or replace the ID persisted with the project.
	report.RepositoryID = repositoryID
	if report.RepositoryURL == "" {
		report.RepositoryURL = r.repositoryURL(repositoryID)
	}
	return report, err
}

func (r *RegistryProvider) repositoryURL(repositoryID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.urls[repositoryID]
}
