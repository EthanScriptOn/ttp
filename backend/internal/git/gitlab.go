package git

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// GitLabProvider is a read-only Provider backed by the GitLab REST API v4.
// One instance represents one repository URL. The repository URL is supplied
// at construction time so callers never need to build provider API paths.
type GitLabProvider struct {
	remote *remoteProvider
}

var _ Provider = (*GitLabProvider)(nil)

// GitLabHTTPProvider is kept as an explicit alias for callers that want the
// transport-backed nature of the implementation to be visible at call sites.
type GitLabHTTPProvider = GitLabProvider

// NewGitLabProvider creates a read-only GitLab provider. Public GitLab URLs
// work without a token; private repositories should pass a token directly.
// Custom/self-hosted hosts must use WithAllowedHosts.
func NewGitLabProvider(repositoryURL, token string, options ...ProviderOption) (*GitLabProvider, error) {
	remote, err := newRemoteProvider(remoteGitLab, repositoryURL, token, options...)
	if err != nil {
		return nil, err
	}
	return &GitLabProvider{remote: remote}, nil
}

// NewGitLabHTTPProvider is an explicit-name alias for NewGitLabProvider.
func NewGitLabHTTPProvider(repositoryURL, token string, options ...ProviderOption) (*GitLabProvider, error) {
	return NewGitLabProvider(repositoryURL, token, options...)
}

func (p *GitLabProvider) ListRepositories(ctx context.Context) ([]Repository, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	var payload gitLabRepositoryResponse
	_, err := p.remote.getJSON(ctx, "list GitLab repository", p.remote.apiURL("projects", p.remote.repository.projectPath), &payload)
	if err != nil {
		return nil, mapNotFound(err, ErrRepositoryNotFound)
	}
	repository := p.remote.repositorySnapshot()
	if strings.TrimSpace(payload.Name) != "" {
		repository.Name = strings.TrimSpace(payload.Name)
	}
	if strings.TrimSpace(payload.DefaultBranch) != "" {
		repository.DefaultBranch = strings.TrimSpace(payload.DefaultBranch)
	}
	p.remote.updateRepository(repository)
	return []Repository{p.remote.repositorySnapshot()}, nil
}

func (p *GitLabProvider) ListBranches(ctx context.Context, repositoryID string) ([]Branch, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return nil, err
	}
	endpoint := withQuery(p.remote.apiURL("projects", p.remote.repository.projectPath, "repository", "branches"), url.Values{
		"page":     {"1"},
		"per_page": {fmt.Sprint(defaultPageSize)},
	})
	result := make([]Branch, 0)
	seen := make(map[string]struct{})
	for page := 1; page <= maxPaginationPages; page++ {
		var payload []gitLabBranchResponse
		meta, err := p.remote.getJSON(ctx, "list GitLab branches", endpoint, &payload)
		if err != nil {
			return nil, mapNotFound(err, ErrRepositoryNotFound)
		}
		defaultBranch := p.remote.repositorySnapshot().DefaultBranch
		for _, item := range payload {
			if len(result) >= maxListItems {
				return result, nil
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				return nil, fmt.Errorf("list GitLab branches: %w", ErrProviderResponse)
			}
			head, err := item.Commit.toCommit()
			if err != nil {
				return nil, fmt.Errorf("list GitLab branches: %w", ErrProviderResponse)
			}
			result = append(result, Branch{Name: name, Head: head, IsHead: name == defaultBranch})
		}
		next, ok, err := nextPageURL(endpoint, meta.header, page, defaultPageSize, len(payload))
		if err != nil {
			return nil, err
		}
		if !ok {
			return result, nil
		}
		if _, exists := seen[next.String()]; exists {
			return nil, fmt.Errorf("list GitLab branches: %w", ErrProviderResponse)
		}
		seen[next.String()] = struct{}{}
		endpoint = next
	}
	return nil, fmt.Errorf("list GitLab branches: %w", ErrProviderResponse)
}

func (p *GitLabProvider) ListCommits(ctx context.Context, repositoryID, branch string, limit int) ([]Commit, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, ErrBranchNotFound
	}
	limit = normalizeCommitLimit(limit)
	pageSize := limit
	if pageSize > defaultPageSize {
		pageSize = defaultPageSize
	}
	endpoint := withQuery(p.remote.apiURL("projects", p.remote.repository.projectPath, "repository", "commits"), url.Values{
		"page":     {"1"},
		"per_page": {fmt.Sprint(pageSize)},
		"ref_name": {branch},
	})
	result := make([]Commit, 0, limit)
	seen := make(map[string]struct{})
	for page := 1; page <= maxPaginationPages && len(result) < limit; page++ {
		var payload []gitLabCommitResponse
		meta, err := p.remote.getJSON(ctx, "list GitLab commits", endpoint, &payload)
		if err != nil {
			return nil, mapNotFound(err, ErrBranchNotFound)
		}
		for _, item := range payload {
			if len(result) >= limit {
				break
			}
			commit, err := item.toCommit()
			if err != nil {
				return nil, fmt.Errorf("list GitLab commits: %w", ErrProviderResponse)
			}
			result = append(result, commit)
		}
		if len(result) >= limit {
			break
		}
		next, ok, err := nextPageURL(endpoint, meta.header, page, pageSize, len(payload))
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if _, exists := seen[next.String()]; exists {
			return nil, fmt.Errorf("list GitLab commits: %w", ErrProviderResponse)
		}
		seen[next.String()] = struct{}{}
		endpoint = next
	}
	return result, nil
}

// ListTags returns repository tags and their target commit IDs.
func (p *GitLabProvider) ListTags(ctx context.Context, repositoryID string, limit int) ([]Tag, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return nil, err
	}
	limit = normalizeTagLimit(limit)
	pageSize := limit
	if pageSize > defaultPageSize {
		pageSize = defaultPageSize
	}
	endpoint := withQuery(p.remote.apiURL("projects", p.remote.repository.projectPath, "repository", "tags"), url.Values{
		"page":     {"1"},
		"per_page": {fmt.Sprint(pageSize)},
	})
	result := make([]Tag, 0, limit)
	seen := make(map[string]struct{})
	for page := 1; page <= maxPaginationPages && len(result) < limit; page++ {
		var payload []gitLabTagResponse
		meta, err := p.remote.getJSON(ctx, "list GitLab tags", endpoint, &payload)
		if err != nil {
			return nil, mapNotFound(err, ErrRepositoryNotFound)
		}
		for _, item := range payload {
			if len(result) >= limit {
				break
			}
			name := strings.TrimSpace(item.Name)
			sha := strings.TrimSpace(item.Commit.ID)
			if name == "" || sha == "" {
				return nil, fmt.Errorf("list GitLab tags: %w", ErrProviderResponse)
			}
			result = append(result, Tag{Name: name, SHA: sha})
		}
		if len(result) >= limit {
			break
		}
		next, ok, err := nextPageURL(endpoint, meta.header, page, pageSize, len(payload))
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if _, exists := seen[next.String()]; exists {
			return nil, fmt.Errorf("list GitLab tags: %w", ErrProviderResponse)
		}
		seen[next.String()] = struct{}{}
		endpoint = next
	}
	return result, nil
}

func (p *GitLabProvider) GetCommit(ctx context.Context, repositoryID, sha string) (Commit, error) {
	if err := p.ensure(); err != nil {
		return Commit{}, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return Commit{}, err
	}
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return Commit{}, ErrCommitNotFound
	}
	var payload gitLabCommitResponse
	_, err := p.remote.getJSON(ctx, "get GitLab commit", p.remote.apiURL("projects", p.remote.repository.projectPath, "repository", "commits", sha), &payload)
	if err != nil {
		return Commit{}, mapNotFound(err, ErrCommitNotFound)
	}
	commit, err := payload.toCommit()
	if err != nil {
		return Commit{}, fmt.Errorf("get GitLab commit: %w", ErrProviderResponse)
	}
	return commit, nil
}

func (p *GitLabProvider) ensure() error {
	if p == nil || p.remote == nil {
		return fmt.Errorf("%w: provider is not initialized", ErrInvalidProviderConfig)
	}
	return nil
}

type gitLabRepositoryResponse struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
}

type gitLabBranchResponse struct {
	Name   string               `json:"name"`
	Commit gitLabCommitResponse `json:"commit"`
}

type gitLabCommitResponse struct {
	ID          string `json:"id"`
	ShortID     string `json:"short_id"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	AuthoredAt  string `json:"authored_date"`
}

type gitLabTagResponse struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

func (value gitLabCommitResponse) toCommit() (Commit, error) {
	sha := strings.TrimSpace(value.ID)
	if sha == "" {
		return Commit{}, ErrProviderResponse
	}
	authoredAt, err := parseProviderTime(value.AuthoredAt)
	if err != nil {
		return Commit{}, err
	}
	author := strings.TrimSpace(value.AuthorName)
	if author == "" {
		author = strings.TrimSpace(value.AuthorEmail)
	}
	message := value.Message
	if strings.TrimSpace(message) == "" {
		message = value.Title
	}
	short := strings.TrimSpace(value.ShortID)
	if short == "" {
		short = shortSHA(sha)
	}
	return Commit{SHA: sha, ShortSHA: short, Message: message, Author: author, AuthoredAt: authoredAt}, nil
}
