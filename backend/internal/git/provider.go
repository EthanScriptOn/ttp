package git

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProviderNotConfigured = errors.New("git provider is not configured")
	ErrRepositoryNotFound    = errors.New("git repository not found")
	ErrRepositoryConflict    = errors.New("git repository ID is already bound to another repository")
	ErrBranchNotFound        = errors.New("git branch not found")
	ErrCommitNotFound        = errors.New("git commit not found")
	ErrWriteUnsupported      = errors.New("git write operations are not supported")
)

// BranchMergeResult is the provider-neutral outcome of merging a release
// branch into the project's default branch. Providers may report an empty
// commit SHA when the target was already up to date.
type BranchMergeResult struct {
	CommitSHA string `json:"commit_sha,omitempty"`
	Message   string `json:"message,omitempty"`
}

// BranchMerger is optional so read-only/custom providers remain source
// compatible. The production GitHub/GitLab providers implement it.
type BranchMerger interface {
	MergeBranch(ctx context.Context, repositoryID, sourceBranch, targetBranch string) (BranchMergeResult, error)
}

// Repository is the provider-neutral identity of a Git repository.
type Repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
}

type Branch struct {
	Name   string `json:"name"`
	Head   Commit `json:"head"`
	IsHead bool   `json:"is_head"`
}

type Commit struct {
	SHA        string    `json:"sha"`
	ShortSHA   string    `json:"short_sha"`
	Message    string    `json:"message"`
	Author     string    `json:"author"`
	AuthoredAt time.Time `json:"authored_at"`
}

// Tag is a repository tag and the commit it points to. Tags are queried
// separately from branch history because a tag can point at a commit that is
// not present in the currently selected branch page.
type Tag struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

// RepositoryCredential is the project-owned machine identity used for Git
// operations. Token is kept in memory only while a request configures a
// provider and is never included in API responses.
type RepositoryCredential struct {
	Provider string
	Username string
	Token    string
}

// RepositoryCredentialRegistry lets the API bind a project's encrypted
// credential to the provider used for that project's repository.
type RepositoryCredentialRegistry interface {
	ConfigureRepositoryCredential(repositoryID, repositoryURL string, credential RepositoryCredential) error
	CheckRepositoryCredential(ctx context.Context, repositoryID, repositoryURL string, credential RepositoryCredential) (RepositoryAccess, error)
	ClearRepositoryCredential(repositoryID, repositoryURL string) error
}

// ImageRegistryRequirement identifies providers whose projects must use a
// space-scoped image registry connection. It is optional so demo and read-only
// providers can retain their legacy behavior.
type ImageRegistryRequirement interface {
	RequiresImageRegistryConnection() bool
}

// TagProvider is optional so existing custom read-only providers remain
// source-compatible. Built-in GitHub and GitLab providers implement it.
type TagProvider interface {
	ListTags(ctx context.Context, repositoryID string, limit int) ([]Tag, error)
}

// MergeRequest describes the immutable inputs to a release preparation merge.
// Providers may use the temporary branch as an isolated workspace; callers
// must never assume that the protected base branch is changed by this call.
type MergeRequest struct {
	RepositoryID    string
	SourceBranch    string
	BaseBranch      string
	SelectedSHA     string
	TemporaryBranch string
}

// MergeConflict is deliberately provider-neutral so the UI can present the
// same conflict editor for GitHub or GitLab.
type MergeConflict struct {
	Path            string
	BaseContent     string
	SourceContent   string
	ResolvedContent string
	Status          string
}

type MergeResolution struct {
	Path    string
	Content string
}

// MergeResult reports the outcome of an isolated merge attempt. A provider
// that implements MergeOperator is allowed to create an isolated temporary
// ref, but it must not update the protected base ref as a side effect.
type MergeResult struct {
	TemporaryBranch string
	Status          string
	Conflicts       []MergeConflict
	HeadSHA         string
	Message         string
}

// MergeOperator is optional. The default remote providers remain read-only;
// tests may provide an in-memory implementation without touching a repository.
type MergeOperator interface {
	PrepareMerge(ctx context.Context, request MergeRequest) (MergeResult, error)
	ResolveMerge(ctx context.Context, request MergeRequest, resolutions []MergeResolution) (MergeResult, error)
}

// Provider is intentionally read-only. A GitHub/GitLab implementation can be
// added later without changing release domain code.
type Provider interface {
	ListRepositories(ctx context.Context) ([]Repository, error)
	ListBranches(ctx context.Context, repositoryID string) ([]Branch, error)
	ListCommits(ctx context.Context, repositoryID, branch string, limit int) ([]Commit, error)
	GetCommit(ctx context.Context, repositoryID, sha string) (Commit, error)
}
