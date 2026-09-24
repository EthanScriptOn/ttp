package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRemoteTimeout = 15 * time.Second
	defaultPageSize      = 100
	defaultCommitLimit   = 50
	maxCommitLimit       = 200
	defaultTagLimit      = 200
	maxTagLimit          = 500
	maxListItems         = 10000
	maxPaginationPages   = 1000
	maxResponseBytes     = 8 << 20
)

var (
	// ErrInvalidProviderConfig indicates that a provider was constructed with
	// an unsafe or incomplete URL/allowlist configuration.
	ErrInvalidProviderConfig = errors.New("invalid git provider configuration")
	ErrProviderHTTP          = errors.New("git provider returned non-success status")
	ErrProviderResponse      = errors.New("invalid git provider response")
)

// ProviderHTTPError reports a non-2xx response without retaining the response
// body. Provider responses can contain arbitrary user-controlled text, and
// keeping the body out of the error also ensures credentials are never echoed.
type ProviderHTTPError struct {
	StatusCode int
	Operation  string
	RetryAfter string
}

func (e *ProviderHTTPError) Error() string {
	if e == nil {
		return ErrProviderHTTP.Error()
	}
	if e.Operation == "" {
		return fmt.Sprintf("%s: status %d", ErrProviderHTTP, e.StatusCode)
	}
	return fmt.Sprintf("%s: status %d", e.Operation, e.StatusCode)
}

func (e *ProviderHTTPError) Unwrap() error { return ErrProviderHTTP }

// ProviderOption customizes an HTTP provider. The token is deliberately not
// an option: it is passed directly to the constructor and is kept private.
type ProviderOption func(*providerOptions)

type providerOptions struct {
	client            *http.Client
	timeout           time.Duration
	timeoutSet        bool
	allowedHosts      []string
	allowedHostsSet   bool
	apiBaseURL        string
	apiBaseURLSet     bool
	serviceAccount    ServiceAccount
	serviceAccountSet bool
}

// WithHTTPClient supplies the transport used for provider requests. The
// client is copied and its redirect policy is wrapped by the provider's host
// allowlist check.
func WithHTTPClient(client *http.Client) ProviderOption {
	return func(options *providerOptions) {
		options.client = client
	}
}

// WithTimeout sets the maximum duration for an individual HTTP request.
func WithTimeout(timeout time.Duration) ProviderOption {
	return func(options *providerOptions) {
		options.timeout = timeout
		options.timeoutSet = true
	}
}

// WithAllowedHosts replaces the default platform host allowlist. Entries are
// exact hostnames, optionally including a port; wildcard entries are rejected.
// Custom GitLab/GitHub Enterprise hosts and httptest servers must be supplied
// explicitly through this option.
func WithAllowedHosts(hosts ...string) ProviderOption {
	return func(options *providerOptions) {
		options.allowedHosts = append([]string(nil), hosts...)
		options.allowedHostsSet = true
	}
}

// WithAPIBaseURL overrides the platform API root. This is useful for
// self-hosted installations and tests; its host must also be allowlisted.
func WithAPIBaseURL(apiBaseURL string) ProviderOption {
	return func(options *providerOptions) {
		options.apiBaseURL = apiBaseURL
		options.apiBaseURLSet = true
	}
}

// WithServiceAccount supplies the non-secret identity expected for provider
// access checks. The access token remains a constructor argument and is never
// copied into this value.
func WithServiceAccount(account ServiceAccount) ProviderOption {
	return func(options *providerOptions) {
		options.serviceAccount = account
		options.serviceAccountSet = true
	}
}

type remoteKind uint8

const (
	remoteGitLab remoteKind = iota + 1
	remoteGitHub
)

type repositoryRef struct {
	rawURL       string
	canonicalURL string
	projectPath  string
	segments     []string
	name         string
	host         string
	aliases      map[string]struct{}
}

type remoteProvider struct {
	kind           remoteKind
	client         *http.Client
	token          string
	serviceAccount ServiceAccount
	allowlist      hostAllowlist
	apiBaseURL     *url.URL
	repository     repositoryRef

	mu       sync.RWMutex
	metadata Repository
}

// newRemoteProvider constructs the shared, security-sensitive part of the
// GitLab and GitHub providers. Each concrete provider exposes only read-only
// methods from Provider.
func newRemoteProvider(kind remoteKind, repositoryURL, token string, options ...ProviderOption) (*remoteProvider, error) {
	if kind != remoteGitLab && kind != remoteGitHub {
		return nil, fmt.Errorf("%w: unsupported provider kind", ErrInvalidProviderConfig)
	}
	if strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("%w: token contains invalid characters", ErrInvalidProviderConfig)
	}

	settings := providerOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}

	repositoryURLValue, err := parseHTTPURL(repositoryURL)
	if err != nil {
		return nil, err
	}
	repository, err := parseRepositoryRef(repositoryURLValue, repositoryURL, kind)
	if err != nil {
		return nil, err
	}

	allowedHostValues := settings.allowedHosts
	if !settings.allowedHostsSet {
		allowedHostValues = defaultAllowedHosts(kind, repositoryURLValue.Hostname())
		if len(allowedHostValues) == 0 {
			return nil, fmt.Errorf("%w: custom repository host requires WithAllowedHosts", ErrInvalidProviderConfig)
		}
	}
	allowlist, err := newHostAllowlist(allowedHostValues)
	if err != nil {
		return nil, err
	}
	if err := allowlist.validate(repositoryURLValue); err != nil {
		return nil, fmt.Errorf("%w: repository URL host is not allowed", err)
	}

	apiBaseRaw := settings.apiBaseURL
	if !settings.apiBaseURLSet || strings.TrimSpace(apiBaseRaw) == "" {
		apiBaseRaw = defaultAPIBaseURL(kind, repositoryURLValue)
	}
	apiBaseURL, err := parseAPIBaseURL(apiBaseRaw)
	if err != nil {
		return nil, err
	}
	if err := allowlist.validate(apiBaseURL); err != nil {
		return nil, fmt.Errorf("%w: API base URL host is not allowed", ErrInvalidProviderConfig)
	}

	timeout := settings.timeout
	if settings.timeoutSet && timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout must be positive", ErrInvalidProviderConfig)
	}
	client := settings.client
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	if settings.timeoutSet {
		clientCopy.Timeout = timeout
	} else if clientCopy.Timeout <= 0 {
		clientCopy.Timeout = defaultRemoteTimeout
	}
	previousRedirect := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if err := allowlist.validate(request.URL); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return http.ErrUseLastResponse
		}
		return nil
	}

	metadata := Repository{ID: repository.canonicalURL, Name: repository.name, URL: repository.canonicalURL}
	serviceAccount := defaultServiceAccount(kind)
	if settings.serviceAccountSet {
		serviceAccount = normalizeServiceAccount(settings.serviceAccount, serviceAccount.Provider)
	}
	serviceAccount.Configured = strings.TrimSpace(token) != ""
	return &remoteProvider{
		kind:           kind,
		client:         &clientCopy,
		token:          token,
		serviceAccount: serviceAccount,
		allowlist:      allowlist,
		apiBaseURL:     apiBaseURL,
		repository:     repository,
		metadata:       metadata,
	}, nil
}

func parseHTTPURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.ContainsAny(trimmed, "\r\n") {
		return nil, fmt.Errorf("%w: repository URL is required", ErrInvalidProviderConfig)
	}
	value, err := url.ParseRequestURI(trimmed)
	if err != nil || value == nil {
		return nil, fmt.Errorf("%w: repository URL is invalid", ErrInvalidProviderConfig)
	}
	value.Scheme = strings.ToLower(value.Scheme)
	if value.Scheme != "http" && value.Scheme != "https" {
		return nil, fmt.Errorf("%w: only http and https URLs are supported", ErrInvalidProviderConfig)
	}
	if value.Host == "" || value.Hostname() == "" || value.User != nil || value.Opaque != "" {
		return nil, fmt.Errorf("%w: repository URL must contain a host and no credentials", ErrInvalidProviderConfig)
	}
	if value.RawQuery != "" || value.Fragment != "" {
		return nil, fmt.Errorf("%w: repository URL must not contain a query or fragment", ErrInvalidProviderConfig)
	}
	if strings.ContainsAny(value.Host, "\r\n") {
		return nil, fmt.Errorf("%w: repository URL host is invalid", ErrInvalidProviderConfig)
	}
	return value, nil
}

func parseAPIBaseURL(raw string) (*url.URL, error) {
	value, err := parseHTTPURL(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: API base URL is invalid", ErrInvalidProviderConfig)
	}
	decodedPath, err := url.PathUnescape(value.EscapedPath())
	if err != nil || strings.Contains(decodedPath, "..") {
		return nil, fmt.Errorf("%w: API base URL path is invalid", ErrInvalidProviderConfig)
	}
	value.Path = strings.TrimRight(decodedPath, "/")
	value.RawPath = ""
	return value, nil
}

func parseRepositoryRef(value *url.URL, raw string, kind remoteKind) (repositoryRef, error) {
	decodedPath, err := url.PathUnescape(value.EscapedPath())
	if err != nil {
		return repositoryRef{}, fmt.Errorf("%w: repository path is invalid", ErrInvalidProviderConfig)
	}
	trimmedPath := strings.Trim(decodedPath, "/")
	if trimmedPath == "" {
		return repositoryRef{}, fmt.Errorf("%w: repository path is required", ErrInvalidProviderConfig)
	}
	parts := strings.Split(trimmedPath, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\x00\r\n") {
			return repositoryRef{}, fmt.Errorf("%w: repository path is invalid", ErrInvalidProviderConfig)
		}
	}
	last := parts[len(parts)-1]
	if strings.HasSuffix(strings.ToLower(last), ".git") {
		last = last[:len(last)-len(".git")]
		if last == "" {
			return repositoryRef{}, fmt.Errorf("%w: repository name is required", ErrInvalidProviderConfig)
		}
		parts[len(parts)-1] = last
	}
	if kind == remoteGitHub && len(parts) != 2 {
		return repositoryRef{}, fmt.Errorf("%w: GitHub repository URL must be /owner/repository", ErrInvalidProviderConfig)
	}
	if kind == remoteGitLab && len(parts) < 2 {
		return repositoryRef{}, fmt.Errorf("%w: GitLab repository URL must include a group and repository", ErrInvalidProviderConfig)
	}

	canonicalURL := *value
	canonicalURL.Path = "/" + strings.Join(parts, "/")
	canonicalURL.RawPath = ""
	canonicalURL.RawQuery = ""
	canonicalURL.Fragment = ""
	canonical := canonicalURL.String()
	rawTrimmed := strings.TrimSpace(raw)
	aliases := map[string]struct{}{
		canonical:                {},
		rawTrimmed:               {},
		strings.Join(parts, "/"): {},
	}
	// The API layer currently derives repository IDs from the URL's SHA-256
	// prefix. Accepting that value keeps this provider usable without coupling
	// the provider interface to the API package.
	for _, candidate := range []string{rawTrimmed, canonical} {
		sum := sha256.Sum256([]byte(candidate))
		aliases["repo-"+hex.EncodeToString(sum[:8])] = struct{}{}
	}
	aliases[strings.ToLower(canonical)] = struct{}{}
	return repositoryRef{
		rawURL:       rawTrimmed,
		canonicalURL: canonical,
		projectPath:  strings.Join(parts, "/"),
		segments:     append([]string(nil), parts...),
		name:         last,
		host:         value.Host,
		aliases:      aliases,
	}, nil
}

func defaultAllowedHosts(kind remoteKind, repositoryHost string) []string {
	host := strings.ToLower(strings.TrimSuffix(repositoryHost, "."))
	switch kind {
	case remoteGitHub:
		if host == "github.com" || host == "www.github.com" {
			return []string{"github.com", "www.github.com", "api.github.com"}
		}
	case remoteGitLab:
		if host == "gitlab.com" || host == "www.gitlab.com" {
			return []string{"gitlab.com", "www.gitlab.com"}
		}
	}
	return nil
}

func defaultAPIBaseURL(kind remoteKind, repositoryURL *url.URL) string {
	scheme := strings.ToLower(repositoryURL.Scheme)
	if kind == remoteGitHub && (strings.EqualFold(repositoryURL.Hostname(), "github.com") || strings.EqualFold(repositoryURL.Hostname(), "www.github.com")) {
		return "https://api.github.com"
	}
	if kind == remoteGitHub {
		return fmt.Sprintf("%s://%s/api/v3", scheme, repositoryURL.Host)
	}
	return fmt.Sprintf("%s://%s/api/v4", scheme, repositoryURL.Host)
}

type hostRule struct {
	hostname string
	port     string
}

type hostAllowlist []hostRule

func newHostAllowlist(values []string) (hostAllowlist, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: host allowlist is required", ErrInvalidProviderConfig)
	}
	result := make(hostAllowlist, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.Contains(trimmed, "*") || strings.Contains(trimmed, "/") || strings.Contains(trimmed, "?") || strings.Contains(trimmed, "#") || strings.Contains(trimmed, "://") {
			return nil, fmt.Errorf("%w: host allowlist entry is invalid", ErrInvalidProviderConfig)
		}
		value, err := url.Parse("//" + trimmed)
		if err != nil || value.Host != trimmed || value.User != nil || value.Path != "" || value.RawQuery != "" || value.Fragment != "" || value.Hostname() == "" {
			return nil, fmt.Errorf("%w: host allowlist entry is invalid", ErrInvalidProviderConfig)
		}
		port := value.Port()
		if port != "" {
			parsedPort, parseErr := strconv.Atoi(port)
			if parseErr != nil || parsedPort < 1 || parsedPort > 65535 {
				return nil, fmt.Errorf("%w: host allowlist port is invalid", ErrInvalidProviderConfig)
			}
			port = strconv.Itoa(parsedPort)
		}
		result = append(result, hostRule{hostname: strings.ToLower(strings.TrimSuffix(value.Hostname(), ".")), port: port})
	}
	return result, nil
}

func (a hostAllowlist) validate(value *url.URL) error {
	if value == nil || (value.Scheme != "http" && value.Scheme != "https") || value.Hostname() == "" || value.User != nil {
		return fmt.Errorf("%w: URL must use http or https and contain no credentials", ErrInvalidProviderConfig)
	}
	hostname := strings.ToLower(strings.TrimSuffix(value.Hostname(), "."))
	port := value.Port()
	if port == "" {
		switch value.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	for _, rule := range a {
		if rule.hostname != hostname {
			continue
		}
		if rule.port == "" || rule.port == port {
			return nil
		}
	}
	return fmt.Errorf("%w: URL host is not allowlisted", ErrInvalidProviderConfig)
}

func (p *remoteProvider) checkRepository(repositoryID string) error {
	trimmed := strings.TrimSpace(repositoryID)
	if trimmed == "" {
		return ErrRepositoryNotFound
	}
	if _, ok := p.repository.aliases[trimmed]; ok {
		return nil
	}
	if _, ok := p.repository.aliases[strings.ToLower(trimmed)]; ok {
		return nil
	}
	return ErrRepositoryNotFound
}

func (p *remoteProvider) repositorySnapshot() Repository {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.metadata
}

func (p *remoteProvider) updateRepository(update Repository) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if update.ID == "" {
		update.ID = p.metadata.ID
	}
	if update.URL == "" {
		update.URL = p.metadata.URL
	}
	if update.Name == "" {
		update.Name = p.metadata.Name
	}
	if update.DefaultBranch == "" {
		update.DefaultBranch = p.metadata.DefaultBranch
	}
	p.metadata = update
}

func (p *remoteProvider) apiURL(segments ...string) *url.URL {
	value := *p.apiBaseURL
	escapedPath := strings.TrimRight(value.EscapedPath(), "/")
	decodedPath, _ := url.PathUnescape(escapedPath)
	for _, segment := range segments {
		escapedSegment := url.PathEscape(segment)
		decodedSegment, _ := url.PathUnescape(escapedSegment)
		escapedPath += "/" + escapedSegment
		decodedPath += "/" + decodedSegment
	}
	value.Path = decodedPath
	value.RawPath = escapedPath
	value.RawQuery = ""
	return &value
}

func withQuery(value *url.URL, query url.Values) *url.URL {
	copyValue := *value
	copyValue.RawQuery = query.Encode()
	return &copyValue
}

type responseMeta struct {
	header http.Header
}

func (p *remoteProvider) getJSON(ctx context.Context, operation string, endpoint *url.URL, target any) (responseMeta, error) {
	return p.sendJSON(ctx, operation, http.MethodGet, endpoint, nil, target)
}

func (p *remoteProvider) sendJSON(ctx context.Context, operation, method string, endpoint *url.URL, payload any, target any) (responseMeta, error) {
	if err := ctx.Err(); err != nil {
		return responseMeta{}, err
	}
	if err := p.allowlist.validate(endpoint); err != nil {
		return responseMeta{}, err
	}
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return responseMeta{}, fmt.Errorf("%s: request could not be encoded: %w", operation, err)
		}
		requestBody = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return responseMeta{}, fmt.Errorf("%s: request could not be created: %w", operation, err)
	}
	request.Header.Set("User-Agent", "cicd-platform-git-provider/1")
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if p.kind == remoteGitHub {
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	} else if p.token != "" {
		request.Header.Set("PRIVATE-TOKEN", p.token)
	}
	if p.kind == remoteGitHub && p.token != "" {
		request.Header.Set("Authorization", "Bearer "+p.token)
	}

	response, err := p.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return responseMeta{}, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return responseMeta{}, err
		}
		return responseMeta{}, fmt.Errorf("%s: request failed: %w", operation, err)
	}
	defer response.Body.Close()
	meta := responseMeta{header: response.Header.Clone()}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return meta, &ProviderHTTPError{StatusCode: response.StatusCode, Operation: operation, RetryAfter: response.Header.Get("Retry-After")}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return meta, fmt.Errorf("%s: response could not be read: %w", operation, err)
	}
	if len(body) > maxResponseBytes {
		return meta, fmt.Errorf("%s: %w", operation, ErrProviderResponse)
	}
	if target == nil || len(body) == 0 {
		return meta, nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return meta, fmt.Errorf("%s: %w", operation, ErrProviderResponse)
	}
	return meta, nil
}

func mapNotFound(err error, notFound error) error {
	var statusErr *ProviderHTTPError
	if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
		return notFound
	}
	return err
}

// GitHub uses 422 for an otherwise valid commit endpoint when the SHA cannot
// be resolved. Treat it like the 404 returned by GitLab so callers get one
// provider-neutral not-found contract.
func mapCommitNotFound(err error) error {
	var statusErr *ProviderHTTPError
	if errors.As(err, &statusErr) && (statusErr.StatusCode == http.StatusNotFound || statusErr.StatusCode == http.StatusUnprocessableEntity) {
		return ErrCommitNotFound
	}
	return err
}

func normalizeCommitLimit(limit int) int {
	if limit <= 0 {
		return defaultCommitLimit
	}
	if limit > maxCommitLimit {
		return maxCommitLimit
	}
	return limit
}

func normalizeTagLimit(limit int) int {
	if limit <= 0 {
		return defaultTagLimit
	}
	if limit > maxTagLimit {
		return maxTagLimit
	}
	return limit
}

func nextPageURL(current *url.URL, headers http.Header, page, pageSize, itemCount int) (*url.URL, bool, error) {
	if nextPage := strings.TrimSpace(headers.Get("X-Next-Page")); nextPage != "" {
		parsed, err := strconv.Atoi(nextPage)
		if err != nil || parsed < 0 {
			return nil, false, fmt.Errorf("pagination: %w", ErrProviderResponse)
		}
		if parsed == 0 || parsed <= page {
			return nil, false, nil
		}
		return pageURL(current, parsed), true, nil
	}
	if link := nextLink(headers.Get("Link")); link != "" {
		value, err := url.Parse(link)
		if err != nil {
			return nil, false, fmt.Errorf("pagination: %w", ErrProviderResponse)
		}
		if !value.IsAbs() {
			value = current.ResolveReference(value)
		}
		return value, true, nil
	}
	if itemCount == pageSize && page < maxPaginationPages {
		return pageURL(current, page+1), true, nil
	}
	return nil, false, nil
}

func pageURL(current *url.URL, page int) *url.URL {
	query := current.Query()
	query.Set("page", strconv.Itoa(page))
	return withQuery(current, query)
}

func nextLink(value string) string {
	for _, part := range strings.Split(value, ",") {
		pieces := strings.SplitN(part, ">", 2)
		if len(pieces) != 2 {
			continue
		}
		relation := strings.ToLower(pieces[1])
		if !strings.Contains(relation, "rel=\"next\"") && !strings.Contains(relation, "rel=next") {
			continue
		}
		candidate := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(pieces[0]), "<"))
		return candidate
	}
	return ""
}

func parseProviderTime(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: timestamp is invalid", ErrProviderResponse)
	}
	return parsed, nil
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}
