package release

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

// PreparationStatus describes whether a source branch can be published
// against the selected base branch without a merge step.
type PreparationStatus string

const (
	PreparationReady       PreparationStatus = "ready"
	PreparationNeedsMerge  PreparationStatus = "needs_merge"
	PreparationConflict    PreparationStatus = "conflict"
	PreparationUnsupported PreparationStatus = "unsupported"
)

// ConflictFile is the transport shape for a file-level merge conflict. The
// current read-only Provider cannot calculate file conflicts, so Prepare
// leaves the list empty and reports that limitation in Capabilities and
// Message rather than inventing conflict data.
type ConflictFile struct {
	Path            string `json:"path"`
	Status          string `json:"status,omitempty"`
	Reason          string `json:"reason,omitempty"`
	BaseContent     string `json:"base_content,omitempty"`
	SourceContent   string `json:"source_content,omitempty"`
	ResolvedContent string `json:"resolved_content,omitempty"`
}

// PreparationCapabilities tells callers which follow-up operations this
// analysis can perform. Prepare is deliberately read-only: it never creates a
// branch, merges commits, or changes the repository.
type PreparationCapabilities struct {
	ReadOnly                 bool `json:"read_only"`
	CanWriteGit              bool `json:"can_write_git"`
	CanCreateTemporaryBranch bool `json:"can_create_temporary_branch"`
	CanAutoMerge             bool `json:"can_auto_merge"`
	CanDetectConflicts       bool `json:"can_detect_conflicts"`
}

// Preparation is the result of comparing a release source branch with a
// base branch. TemporaryBranch is only a suggested name; it is never created
// by this package.
type Preparation struct {
	ProjectID    string `json:"project_id"`
	RepositoryID string `json:"repository_id"`
	SourceBranch string `json:"source_branch"`
	BaseBranch   string `json:"base_branch"`
	SelectedSHA  string `json:"selected_sha"`

	SourceHeadSHA string `json:"source_head_sha,omitempty"`
	BaseHeadSHA   string `json:"base_head_sha,omitempty"`
	SourceAhead   int    `json:"source_ahead"`
	BaseAhead     int    `json:"base_ahead"`
	BaseIncluded  bool   `json:"base_included"`

	Status          PreparationStatus `json:"status"`
	CanPublish      bool              `json:"can_publish"`
	RequiresMerge   bool              `json:"requires_merge"`
	TemporaryBranch string            `json:"temporary_branch,omitempty"`
	ReleaseBranch   string            `json:"release_branch,omitempty"`
	ReleaseSHA      string            `json:"release_sha,omitempty"`
	ConflictFiles   []ConflictFile    `json:"conflict_files"`
	// SupportsConflictResolution is true only when the configured provider can
	// create an isolated merge workspace and accept explicit resolutions.
	SupportsConflictResolution bool                    `json:"supports_conflict_resolution"`
	Capabilities               PreparationCapabilities `json:"capabilities"`
	Message                    string                  `json:"message"`
}

// A non-positive limit asks the Provider for its available/default history.
// The concrete demo provider returns all commits for this value, while remote
// providers may apply their own documented safety cap. We remain conservative
// when the returned histories do not prove that the base is included.
const preparationHistoryLimit = 0

// PrepareMerge asks an optional write-capable provider to create an isolated
// temporary merge workspace after the read-only release-version check. The
// protected base branch is never changed by this operation.
func (s *Service) PrepareMerge(ctx context.Context, input CreateInput, baseBranch, selectedSHA, temporaryBranch string) (Preparation, error) {
	result, err := s.Prepare(ctx, input, baseBranch, selectedSHA)
	if err != nil {
		return Preparation{}, err
	}
	// The normal Prepare path no longer blocks a release because the source
	// branch is behind base. Keep this explicit operation available for callers
	// that intentionally open the legacy online merge editor.
	if result.BaseIncluded {
		return result, nil
	}
	operator, ok := s.provider.(git.MergeOperator)
	if !ok {
		return result, nil
	}
	if strings.TrimSpace(temporaryBranch) == "" {
		temporaryBranch = result.TemporaryBranch
	}
	mergeResult, err := operator.PrepareMerge(ctx, git.MergeRequest{
		RepositoryID:    result.RepositoryID,
		SourceBranch:    result.SourceBranch,
		BaseBranch:      result.BaseBranch,
		SelectedSHA:     result.SelectedSHA,
		TemporaryBranch: strings.TrimSpace(temporaryBranch),
	})
	if err != nil {
		return Preparation{}, err
	}
	return mergePreparationResult(result, mergeResult), nil
}

// ResolveMerge submits the user's file resolutions to the same isolated
// workspace. A ready result means the temporary branch can be used for the
// release; it does not imply that main or another protected branch was pushed.
func (s *Service) ResolveMerge(ctx context.Context, input CreateInput, baseBranch, selectedSHA, temporaryBranch string, resolutions []git.MergeResolution) (Preparation, error) {
	result, err := s.Prepare(ctx, input, baseBranch, selectedSHA)
	if err != nil {
		return Preparation{}, err
	}
	operator, ok := s.provider.(git.MergeOperator)
	if !ok {
		return Preparation{}, git.ErrWriteUnsupported
	}
	if strings.TrimSpace(temporaryBranch) == "" {
		temporaryBranch = result.TemporaryBranch
	}
	if strings.TrimSpace(temporaryBranch) == "" {
		return Preparation{}, fmt.Errorf("%w: temporary branch is required", ErrInvalidRelease)
	}
	mergeResult, err := operator.ResolveMerge(ctx, git.MergeRequest{
		RepositoryID:    result.RepositoryID,
		SourceBranch:    result.SourceBranch,
		BaseBranch:      result.BaseBranch,
		SelectedSHA:     result.SelectedSHA,
		TemporaryBranch: strings.TrimSpace(temporaryBranch),
	}, resolutions)
	if err != nil {
		return Preparation{}, err
	}
	return mergePreparationResult(result, mergeResult), nil
}

// Prepare performs a read-only release-version check.
//
// The source branch is input.Branch. The method validates both branch names
// through ListBranches, verifies selectedSHA through GetCommit, and confirms
// that the selected commit belongs to the source branch. The source/base
// history counts are retained as informational API fields for compatibility;
// they never prevent publishing. Batch integration is the point at which
// conflicts with other release items are checked.
func (s *Service) Prepare(ctx context.Context, input CreateInput, baseBranch, selectedSHA string) (Preparation, error) {
	result := Preparation{
		ProjectID:                  strings.TrimSpace(input.ProjectID),
		RepositoryID:               strings.TrimSpace(input.RepositoryID),
		SourceBranch:               strings.TrimSpace(input.Branch),
		BaseBranch:                 strings.TrimSpace(baseBranch),
		SelectedSHA:                strings.TrimSpace(selectedSHA),
		Capabilities:               readOnlyPreparationCapabilities(),
		ConflictFiles:              []ConflictFile{},
		SupportsConflictResolution: false,
	}

	input.ProjectID = result.ProjectID
	input.RepositoryID = result.RepositoryID
	input.Branch = result.SourceBranch
	if err := validateIdentity(input); err != nil {
		return Preparation{}, err
	}
	if err := validatePreparationBranch(result.SourceBranch, "source_branch"); err != nil {
		return Preparation{}, err
	}
	if err := validatePreparationBranch(result.BaseBranch, "base_branch"); err != nil {
		return Preparation{}, err
	}
	if result.SelectedSHA == "" {
		return Preparation{}, fmt.Errorf("%w: selected_sha is required", ErrInvalidRelease)
	}
	if strings.ContainsAny(result.SelectedSHA, "\x00\r\n") {
		return Preparation{}, fmt.Errorf("%w: selected_sha contains invalid characters", ErrInvalidRelease)
	}
	if s == nil || s.provider == nil {
		return Preparation{}, fmt.Errorf("%w: git provider is not configured", ErrInvalidRelease)
	}
	if err := ctx.Err(); err != nil {
		return Preparation{}, err
	}

	branches, err := s.provider.ListBranches(ctx, result.RepositoryID)
	if err != nil {
		return Preparation{}, err
	}
	sourceBranch, sourceOK := findBranch(branches, result.SourceBranch)
	if !sourceOK {
		return Preparation{}, fmt.Errorf("%w: source branch %q", git.ErrBranchNotFound, result.SourceBranch)
	}
	baseBranchInfo, baseOK := findBranch(branches, result.BaseBranch)
	if !baseOK {
		return Preparation{}, fmt.Errorf("%w: base branch %q", git.ErrBranchNotFound, result.BaseBranch)
	}

	selectedCommit, err := s.provider.GetCommit(ctx, result.RepositoryID, result.SelectedSHA)
	if err != nil {
		return Preparation{}, err
	}
	selectedCommit.SHA = strings.TrimSpace(selectedCommit.SHA)
	if selectedCommit.SHA == "" {
		return Preparation{}, fmt.Errorf("%w: provider returned an empty commit sha", ErrInvalidRelease)
	}
	result.SelectedSHA = selectedCommit.SHA

	sourceCommits, err := s.provider.ListCommits(ctx, result.RepositoryID, result.SourceBranch, preparationHistoryLimit)
	if err != nil {
		return Preparation{}, err
	}
	if err := ctx.Err(); err != nil {
		return Preparation{}, err
	}
	var baseCommits []git.Commit
	if result.SourceBranch == result.BaseBranch {
		baseCommits = sourceCommits
	} else {
		baseCommits, err = s.provider.ListCommits(ctx, result.RepositoryID, result.BaseBranch, preparationHistoryLimit)
		if err != nil {
			return Preparation{}, err
		}
	}
	if len(sourceCommits) == 0 || len(baseCommits) == 0 {
		return Preparation{}, fmt.Errorf("%w: source and base branches must contain commits", ErrInvalidRelease)
	}
	if !commitHistoryContains(sourceCommits, result.SelectedSHA) {
		return Preparation{}, fmt.Errorf("%w: selected commit %q does not belong to source branch %q", git.ErrCommitNotFound, result.SelectedSHA, result.SourceBranch)
	}

	sourceSet := commitSHASet(sourceCommits)
	baseSet := commitSHASet(baseCommits)
	result.SourceAhead = commitDifferenceCount(sourceSet, baseSet)
	result.BaseAhead = commitDifferenceCount(baseSet, sourceSet)
	result.SourceHeadSHA = branchHeadSHA(sourceBranch, sourceCommits)
	result.BaseHeadSHA = branchHeadSHA(baseBranchInfo, baseCommits)

	result.BaseIncluded = result.SourceBranch == result.BaseBranch || commitSetContainsAll(sourceSet, baseSet)
	result.Status = PreparationReady
	result.CanPublish = true
	result.RequiresMerge = false
	result.ReleaseBranch = result.SourceBranch
	result.ReleaseSHA = result.SelectedSHA
	result.Message = "代码版本可以发布。发布时会自动加入当前开放批次；如果和批次中的其他代码发生代码冲突，平台会在发布时拦截。"

	return result, nil
}

func readOnlyPreparationCapabilities() PreparationCapabilities {
	return PreparationCapabilities{
		ReadOnly:                 true,
		CanWriteGit:              false,
		CanCreateTemporaryBranch: false,
		CanAutoMerge:             false,
		CanDetectConflicts:       false,
	}
}

func preparationCapabilities(provider git.Provider) PreparationCapabilities {
	if !supportsMergeOperator(provider) {
		return readOnlyPreparationCapabilities()
	}
	return PreparationCapabilities{
		ReadOnly:                 false,
		CanWriteGit:              true,
		CanCreateTemporaryBranch: true,
		CanAutoMerge:             true,
		CanDetectConflicts:       true,
	}
}

func supportsMergeOperator(provider git.Provider) bool {
	if provider == nil {
		return false
	}
	_, ok := provider.(git.MergeOperator)
	return ok
}

func mergePreparationResult(base Preparation, result git.MergeResult) Preparation {
	if strings.TrimSpace(result.TemporaryBranch) != "" {
		base.TemporaryBranch = strings.TrimSpace(result.TemporaryBranch)
	}
	base.ConflictFiles = make([]ConflictFile, 0, len(result.Conflicts))
	for _, conflict := range result.Conflicts {
		base.ConflictFiles = append(base.ConflictFiles, ConflictFile{
			Path:            strings.TrimSpace(conflict.Path),
			Status:          strings.TrimSpace(conflict.Status),
			BaseContent:     conflict.BaseContent,
			SourceContent:   conflict.SourceContent,
			ResolvedContent: conflict.ResolvedContent,
		})
	}
	base.SupportsConflictResolution = true
	base.Capabilities = preparationCapabilitiesFromMergeResult(base.Capabilities)
	base.Message = strings.TrimSpace(result.Message)
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "ready", "merged", "success", "succeeded":
		base.Status = PreparationReady
		base.CanPublish = true
		base.RequiresMerge = false
		base.BaseIncluded = true
		base.ReleaseBranch = base.SourceBranch
		if base.TemporaryBranch != "" && strings.TrimSpace(result.HeadSHA) != "" {
			base.ReleaseBranch = base.TemporaryBranch
		}
		base.ReleaseSHA = strings.TrimSpace(result.HeadSHA)
		if base.ReleaseSHA == "" {
			base.ReleaseSHA = base.SelectedSHA
		}
		if base.Message == "" {
			base.Message = "临时发布分支已准备好，可以继续发布；基准分支保持不变。"
		}
	case "conflict", "conflicts", "needs_merge":
		base.Status = PreparationConflict
		base.CanPublish = false
		base.RequiresMerge = true
		if base.Message == "" {
			base.Message = "发现代码冲突，请处理后再继续发布。"
		}
	default:
		base.Status = PreparationUnsupported
		base.CanPublish = false
		if base.Message == "" {
			base.Message = "当前 Git 连接暂不支持完成合并。"
		}
	}
	return base
}

func preparationCapabilitiesFromMergeResult(current PreparationCapabilities) PreparationCapabilities {
	current.ReadOnly = false
	current.CanWriteGit = true
	current.CanCreateTemporaryBranch = true
	current.CanAutoMerge = true
	current.CanDetectConflicts = true
	return current
}

func validatePreparationBranch(branch, field string) error {
	if strings.TrimSpace(branch) == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidRelease, field)
	}
	if strings.ContainsAny(branch, "\x00\r\n") {
		return fmt.Errorf("%w: %s contains invalid characters", ErrInvalidRelease, field)
	}
	return nil
}

func findBranch(branches []git.Branch, name string) (git.Branch, bool) {
	for _, branch := range branches {
		if strings.TrimSpace(branch.Name) == name {
			return branch, true
		}
	}
	return git.Branch{}, false
}

func commitSHASet(commits []git.Commit) map[string]struct{} {
	result := make(map[string]struct{}, len(commits))
	for _, commit := range commits {
		sha := strings.ToLower(strings.TrimSpace(commit.SHA))
		if sha != "" {
			result[sha] = struct{}{}
		}
	}
	return result
}

func commitHistoryContains(commits []git.Commit, sha string) bool {
	target := strings.ToLower(strings.TrimSpace(sha))
	if target == "" {
		return false
	}
	for _, commit := range commits {
		if strings.ToLower(strings.TrimSpace(commit.SHA)) == target {
			return true
		}
	}
	return false
}

func commitDifferenceCount(left, right map[string]struct{}) int {
	count := 0
	for sha := range left {
		if _, ok := right[sha]; !ok {
			count++
		}
	}
	return count
}

func commitSetContainsAll(haystack, needles map[string]struct{}) bool {
	if len(needles) == 0 {
		return false
	}
	for sha := range needles {
		if _, ok := haystack[sha]; !ok {
			return false
		}
	}
	return true
}

func branchHeadSHA(branch git.Branch, commits []git.Commit) string {
	if sha := strings.TrimSpace(branch.Head.SHA); sha != "" {
		return sha
	}
	if len(commits) > 0 {
		return strings.TrimSpace(commits[0].SHA)
	}
	return ""
}

func preparationBranchName(now time.Time, selectedSHA, shortSHA string) string {
	short := strings.TrimSpace(shortSHA)
	if short == "" {
		short = strings.TrimSpace(selectedSHA)
	}
	if len(short) > 7 {
		short = short[:7]
	}
	short = strings.ToLower(short)
	return fmt.Sprintf("release-prep-%s-%s", now.UTC().Format("20060102-150405"), short)
}

func (s *Service) nowValue() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}
