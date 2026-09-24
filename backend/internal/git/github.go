package git

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// GitHubProvider is a read-only Provider backed by the GitHub REST API.
// One instance represents one repository URL. GitHub Enterprise installations
// can supply their /api/v3 root with WithAPIBaseURL.
type GitHubProvider struct {
	remote *remoteProvider
}

var _ Provider = (*GitHubProvider)(nil)
var _ BranchMerger = (*GitHubProvider)(nil)

// GitHubHTTPProvider is an explicit alias for the net/http implementation.
type GitHubHTTPProvider = GitHubProvider

// NewGitHubProvider creates a read-only GitHub provider. Public repositories
// work without a token; private repositories should pass a token directly.
// Custom/self-hosted hosts must use WithAllowedHosts.
func NewGitHubProvider(repositoryURL, token string, options ...ProviderOption) (*GitHubProvider, error) {
	remote, err := newRemoteProvider(remoteGitHub, repositoryURL, token, options...)
	if err != nil {
		return nil, err
	}
	return &GitHubProvider{remote: remote}, nil
}

// NewGitHubHTTPProvider is an explicit-name alias for NewGitHubProvider.
func NewGitHubHTTPProvider(repositoryURL, token string, options ...ProviderOption) (*GitHubProvider, error) {
	return NewGitHubProvider(repositoryURL, token, options...)
}

func (p *GitHubProvider) ListRepositories(ctx context.Context) ([]Repository, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	var payload gitHubRepositoryResponse
	_, err := p.remote.getJSON(ctx, "list GitHub repository", p.remote.apiURL(gitHubRepositorySegments(p.remote.repository)...), &payload)
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
	if strings.TrimSpace(payload.HTMLURL) != "" {
		repository.URL = strings.TrimSpace(payload.HTMLURL)
	}
	p.remote.updateRepository(repository)
	return []Repository{p.remote.repositorySnapshot()}, nil
}

func (p *GitHubProvider) ListBranches(ctx context.Context, repositoryID string) ([]Branch, error) {
	if err := p.ensure(); err != nil {
		return nil, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return nil, err
	}
	endpoint := withQuery(p.remote.apiURL(append(gitHubRepositorySegments(p.remote.repository), "branches")...), url.Values{
		"page":     {"1"},
		"per_page": {fmt.Sprint(defaultPageSize)},
	})
	result := make([]Branch, 0)
	seen := make(map[string]struct{})
	for page := 1; page <= maxPaginationPages; page++ {
		var payload []gitHubBranchResponse
		meta, err := p.remote.getJSON(ctx, "list GitHub branches", endpoint, &payload)
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
				return nil, fmt.Errorf("list GitHub branches: %w", ErrProviderResponse)
			}
			head := item.Commit.toCommit()
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
			return nil, fmt.Errorf("list GitHub branches: %w", ErrProviderResponse)
		}
		seen[next.String()] = struct{}{}
		endpoint = next
	}
	return nil, fmt.Errorf("list GitHub branches: %w", ErrProviderResponse)
}

func (p *GitHubProvider) ListCommits(ctx context.Context, repositoryID, branch string, limit int) ([]Commit, error) {
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
	endpoint := withQuery(p.remote.apiURL(append(gitHubRepositorySegments(p.remote.repository), "commits")...), url.Values{
		"page":     {"1"},
		"per_page": {fmt.Sprint(pageSize)},
		"sha":      {branch},
	})
	result := make([]Commit, 0, limit)
	seen := make(map[string]struct{})
	for page := 1; page <= maxPaginationPages && len(result) < limit; page++ {
		var payload []gitHubCommitResponse
		meta, err := p.remote.getJSON(ctx, "list GitHub commits", endpoint, &payload)
		if err != nil {
			return nil, mapNotFound(err, ErrBranchNotFound)
		}
		for _, item := range payload {
			if len(result) >= limit {
				break
			}
			commit, err := item.toCommit()
			if err != nil {
				return nil, fmt.Errorf("list GitHub commits: %w", ErrProviderResponse)
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
			return nil, fmt.Errorf("list GitHub commits: %w", ErrProviderResponse)
		}
		seen[next.String()] = struct{}{}
		endpoint = next
	}
	return result, nil
}

// ListTags returns lightweight and annotated repository tags. GitHub exposes
// the target commit SHA for both forms through the same endpoint.
func (p *GitHubProvider) ListTags(ctx context.Context, repositoryID string, limit int) ([]Tag, error) {
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
	endpoint := withQuery(p.remote.apiURL(append(gitHubRepositorySegments(p.remote.repository), "tags")...), url.Values{
		"page":     {"1"},
		"per_page": {fmt.Sprint(pageSize)},
	})
	result := make([]Tag, 0, limit)
	seen := make(map[string]struct{})
	for page := 1; page <= maxPaginationPages && len(result) < limit; page++ {
		var payload []gitHubTagResponse
		meta, err := p.remote.getJSON(ctx, "list GitHub tags", endpoint, &payload)
		if err != nil {
			return nil, mapNotFound(err, ErrRepositoryNotFound)
		}
		for _, item := range payload {
			if len(result) >= limit {
				break
			}
			name := strings.TrimSpace(item.Name)
			sha := strings.TrimSpace(item.Commit.SHA)
			if name == "" || sha == "" {
				return nil, fmt.Errorf("list GitHub tags: %w", ErrProviderResponse)
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
			return nil, fmt.Errorf("list GitHub tags: %w", ErrProviderResponse)
		}
		seen[next.String()] = struct{}{}
		endpoint = next
	}
	return result, nil
}

func (p *GitHubProvider) GetCommit(ctx context.Context, repositoryID, sha string) (Commit, error) {
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
	var payload gitHubCommitResponse
	segments := append(gitHubRepositorySegments(p.remote.repository), "commits", sha)
	_, err := p.remote.getJSON(ctx, "get GitHub commit", p.remote.apiURL(segments...), &payload)
	if err != nil {
		return Commit{}, mapCommitNotFound(err)
	}
	commit, err := payload.toCommit()
	if err != nil {
		return Commit{}, fmt.Errorf("get GitHub commit: %w", ErrProviderResponse)
	}
	return commit, nil
}

// MergeBranch uses GitHub's repository merge endpoint. GitHub returns 204
// when the head is already fully contained in the base; that is a successful
// idempotent outcome for an automatic post-release merge.
func (p *GitHubProvider) MergeBranch(ctx context.Context, repositoryID, sourceBranch, targetBranch string) (BranchMergeResult, error) {
	if err := p.ensure(); err != nil {
		return BranchMergeResult{}, err
	}
	if err := p.remote.checkRepository(repositoryID); err != nil {
		return BranchMergeResult{}, err
	}
	sourceBranch, targetBranch = strings.TrimSpace(sourceBranch), strings.TrimSpace(targetBranch)
	if sourceBranch == "" || targetBranch == "" {
		return BranchMergeResult{}, ErrBranchNotFound
	}
	var payload struct {
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}
	endpoint := p.remote.apiURL(append(gitHubRepositorySegments(p.remote.repository), "merges")...)
	_, err := p.remote.sendJSON(ctx, "merge GitHub branches", http.MethodPost, endpoint, map[string]string{
		"head": sourceBranch, "base": targetBranch, "commit_message": fmt.Sprintf("Merge %s into %s", sourceBranch, targetBranch),
	}, &payload)
	if err != nil {
		return BranchMergeResult{}, err
	}
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = fmt.Sprintf("已将 %s 合并到 %s", sourceBranch, targetBranch)
	}
	return BranchMergeResult{CommitSHA: strings.TrimSpace(payload.SHA), Message: message}, nil
}

func (p *GitHubProvider) ensure() error {
	if p == nil || p.remote == nil {
		return fmt.Errorf("%w: provider is not initialized", ErrInvalidProviderConfig)
	}
	return nil
}

func gitHubRepositorySegments(repository repositoryRef) []string {
	segments := make([]string, 0, len(repository.segments)+1)
	segments = append(segments, "repos")
	segments = append(segments, repository.segments...)
	return segments
}

type gitHubRepositoryResponse struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

type gitHubBranchResponse struct {
	Name   string             `json:"name"`
	Commit gitHubBranchCommit `json:"commit"`
}

type gitHubBranchCommit struct {
	SHA string `json:"sha"`
}

type gitHubTagResponse struct {
	Name   string             `json:"name"`
	Commit gitHubBranchCommit `json:"commit"`
}

func (value gitHubBranchCommit) toCommit() Commit {
	sha := strings.TrimSpace(value.SHA)
	return Commit{SHA: sha, ShortSHA: shortSHA(sha)}
}

type gitHubCommitResponse struct {
	SHA    string              `json:"sha"`
	Commit gitHubCommitDetails `json:"commit"`
}

type gitHubCommitDetails struct {
	Message   string                `json:"message"`
	Author    *gitHubCommitIdentity `json:"author"`
	Committer *gitHubCommitIdentity `json:"committer"`
}

type gitHubCommitIdentity struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

func (value gitHubCommitResponse) toCommit() (Commit, error) {
	sha := strings.TrimSpace(value.SHA)
	if sha == "" {
		return Commit{}, ErrProviderResponse
	}
	author := ""
	authoredAt := value.Commit.Author
	if authoredAt != nil {
		author = strings.TrimSpace(authoredAt.Name)
	}
	if author == "" && value.Commit.Committer != nil {
		author = strings.TrimSpace(value.Commit.Committer.Name)
		if strings.TrimSpace(value.Commit.Committer.Date) != "" {
			authoredAt = value.Commit.Committer
		}
	}
	authoredTime := ""
	if authoredAt != nil {
		authoredTime = authoredAt.Date
	}
	parsedTime, err := parseProviderTime(authoredTime)
	if err != nil {
		return Commit{}, err
	}
	return Commit{SHA: sha, ShortSHA: shortSHA(sha), Message: value.Commit.Message, Author: author, AuthoredAt: parsedTime}, nil
}
