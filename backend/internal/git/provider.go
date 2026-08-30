package git

import (
	"context"
	"errors"
	"time"
)

var (
	ErrRepositoryNotFound = errors.New("git repository not found")
	ErrRepositoryConflict = errors.New("git repository ID is already bound to another repository")
	ErrBranchNotFound     = errors.New("git branch not found")
	ErrCommitNotFound     = errors.New("git commit not found")
	ErrWriteUnsupported   = errors.New("git write operations are not supported")
)

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

// TagProvider is optional so existing custom read-only providers remain
// source-compatible. Built-in GitHub, GitLab, and demo providers implement it.
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
// same conflict editor for GitHub, GitLab, or the local demo provider.
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

// MainMergeRequest describes the only Git write that can finish a TTP
// release. The batch branch is intentionally informational here: callers
// merge the selected release commits into the current main branch, never the
// whole batch branch.
type MainMergeRequest struct {
	RepositoryID   string
	SourceBranch   string
	BaseBranch     string
	BatchBranch    string
	CurrentMainSHA string
	SelectedSHAs   []string
}

// MainMergeResult is returned by a provider that can perform the final,
// per-release merge. Read-only providers can omit this optional interface; the
// API will then refuse the operation instead of claiming that main changed.
type MainMergeResult struct {
	Status    string
	HeadSHA   string
	Conflicts []MergeConflict
	Message   string
}

// BatchBranchRequest describes the shared integration branch used while
// several developers validate their own release items. The batch branch is a
// test workspace only; it is never a replacement for the protected main
// branch.
type BatchBranchRequest struct {
	RepositoryID   string
	BatchBranch    string
	BaseBranch     string
	BaseSHA        string
	SourceBranch   string
	CurrentHeadSHA string
	SelectedSHAs   []string
}

// BatchBranchResult reports the branch created or updated by a batch
// integration operation. Providers may omit this optional interface when
// they are intentionally read-only.
type BatchBranchResult struct {
	Status             string
	BatchBranch        string
	HeadSHA            string
	ReleaseSnapshotSHA string
	Message            string
}

// BatchOperator is optional so existing read-only providers remain source
// compatible. Implementations must create the batch branch from BaseBranch
// when it does not exist and then integrate only SelectedSHAs from the
// individual release. They must not change BaseBranch.
type BatchOperator interface {
	IntegrateReleaseToBatch(ctx context.Context, request BatchBranchRequest) (BatchBranchResult, error)
}

// MainMergeOperator is optional so existing read-only Git providers remain
// source-compatible. Implementations must merge only SelectedSHAs into the
// current base and must not fast-forward main to the shared batch branch.
type MainMergeOperator interface {
	// Implementations must compare CurrentMainSHA with the live BaseBranch head
	// as part of the remote merge operation and return a conflict when they do
	// not match. This keeps a main update that happens after the service's
	// pre-check from being silently overwritten.
	MergeReleaseToMain(ctx context.Context, request MainMergeRequest) (MainMergeResult, error)
}

// MergeOperator is optional. The default remote providers remain read-only;
// the local demo provider implements this interface in memory so the complete
// preparation flow can be exercised without touching a real repository.
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
