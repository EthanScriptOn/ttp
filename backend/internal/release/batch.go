package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

var (
	ErrBatchNotFound     = errors.New("release batch not found")
	ErrBatchClosed       = errors.New("release batch is closed")
	ErrBatchConflict     = errors.New("release batch merge conflict")
	ErrMainMergeNotReady = errors.New("release is not ready to merge into main")
)

type BatchStatus string

const (
	BatchOpen   BatchStatus = "open"
	BatchClosed BatchStatus = "closed"
)

// MainMergeStatus belongs to an individual release item, not to the batch.
// A batch can contain several developers' work while each item independently
// waits for production and then enters main.
type MainMergeStatus string

const (
	MainMergePending    MainMergeStatus = "pending"
	MainMergeMerged     MainMergeStatus = "merged"
	MainMergeConflict   MainMergeStatus = "conflict"
	MainMergeCancelled  MainMergeStatus = "cancelled"
	MainMergeTerminated MainMergeStatus = "terminated"
)

type BatchItem struct {
	ReleaseID             string          `json:"release_id"`
	Branch                string          `json:"branch"`
	SourceBranch          string          `json:"source_branch,omitempty"`
	OwnerID               uint64          `json:"owner_id,omitempty"`
	OwnerName             string          `json:"owner_name,omitempty"`
	Commits               []git.Commit    `json:"commits"`
	ReleaseStatus         Status          `json:"release_status"`
	CurrentEnvironment    string          `json:"current_environment,omitempty"`
	CompletedEnvironments int             `json:"completed_environments"`
	EnvironmentCount      int             `json:"environment_count"`
	MainMergeStatus       MainMergeStatus `json:"main_merge_status"`
	MainMergeSHA          string          `json:"main_merge_sha,omitempty"`
	MainMergedAt          *time.Time      `json:"main_merged_at,omitempty"`
	BatchSnapshotSHA      string          `json:"batch_snapshot_sha,omitempty"`
	BatchSnapshotRevision int             `json:"batch_snapshot_revision,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
}

type Batch struct {
	ID             string      `json:"id"`
	SpaceID        string      `json:"space_id,omitempty"`
	ProjectID      string      `json:"project_id"`
	RepositoryID   string      `json:"repository_id"`
	BaseBranch     string      `json:"base_branch"`
	BaseSHA        string      `json:"base_sha"`
	Branch         string      `json:"branch"`
	HeadSHA        string      `json:"head_sha"`
	CurrentMainSHA string      `json:"current_main_sha,omitempty"`
	Revision       int         `json:"revision"`
	Status         BatchStatus `json:"status"`
	Items          []BatchItem `json:"items"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	ClosedAt       *time.Time  `json:"closed_at,omitempty"`
}

// ListBatches returns batches newest first. The in-memory release service is
// used by the local console; the same shape is intended for the persistent
// repository implementation.
func (s *Service) ListBatches(projectID string) []Batch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Batch, 0, len(s.batches))
	for _, batch := range s.batches {
		if strings.TrimSpace(projectID) != "" && batch.ProjectID != strings.TrimSpace(projectID) {
			continue
		}
		result = append(result, cloneBatch(batch))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (s *Service) CurrentBatch(projectID string) (Batch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, batch := range s.batches {
		if batch.ProjectID == strings.TrimSpace(projectID) && batch.Status == BatchOpen {
			return cloneBatch(batch), nil
		}
	}
	return Batch{}, ErrBatchNotFound
}

// AttachToBatch creates the first open batch from the current base branch and
// adds later release items to that same batch. It never changes main and it
// never turns the batch branch into the source of a final merge.
func (s *Service) AttachToBatch(ctx context.Context, releaseID, requestedBaseBranch string) (Release, Batch, error) {
	if s == nil || s.provider == nil {
		return Release{}, Batch{}, fmt.Errorf("%w: git provider is not configured", ErrInvalidRelease)
	}
	s.mu.RLock()
	current, ok := s.releases[releaseID]
	s.mu.RUnlock()
	if !ok {
		return Release{}, Batch{}, ErrReleaseNotFound
	}

	baseBranch := strings.TrimSpace(requestedBaseBranch)
	if baseBranch == "" {
		baseBranch = strings.TrimSpace(current.BaseBranch)
	}
	if baseBranch == "" {
		baseBranch = "main"
	}
	baseSHA, err := branchHead(ctx, s.provider, current.RepositoryID, baseBranch)
	if err != nil {
		return Release{}, Batch{}, err
	}
	// A prepared conflict-resolution branch can be the effective branch for
	// this release. Validate against it first so the generated merge commit is
	// not incorrectly rejected as missing from the original source branch.
	effectiveBranch := firstNonEmpty(current.Branch, current.SourceBranch)
	if err := validateReleaseCommitsOnBranch(ctx, s.provider, current.RepositoryID, effectiveBranch, current.Commits); err != nil {
		return Release{}, Batch{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok = s.releases[releaseID]
	if !ok {
		return Release{}, Batch{}, ErrReleaseNotFound
	}
	if current.BatchID != "" {
		batch, exists := s.batches[current.BatchID]
		if !exists {
			return Release{}, Batch{}, ErrBatchNotFound
		}
		if batch.Status != BatchOpen {
			return Release{}, Batch{}, ErrBatchClosed
		}
		return cloneRelease(current), cloneBatch(batch), nil
	}

	var batchID string
	var batch Batch
	for id, candidate := range s.batches {
		if candidate.ProjectID == current.ProjectID && candidate.RepositoryID == current.RepositoryID && candidate.Status == BatchOpen {
			batchID = id
			batch = candidate
			break
		}
	}
	now := s.now().UTC()
	if batchID == "" {
		s.nextBatchID++
		batchID = fmt.Sprintf("batch-%06d", s.nextBatchID)
		batch = Batch{
			ID:             batchID,
			SpaceID:        current.SpaceID,
			ProjectID:      current.ProjectID,
			RepositoryID:   current.RepositoryID,
			BaseBranch:     baseBranch,
			BaseSHA:        baseSHA,
			Branch:         batchID,
			CurrentMainSHA: baseSHA,
			Revision:       0,
			Status:         BatchOpen,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	} else {
		batch = s.batches[batchID]
		if batch.Status != BatchOpen {
			return Release{}, Batch{}, ErrBatchClosed
		}
		// A project has one open integration line. Later release items use the
		// original batch base even if main has moved in the meantime.
		baseBranch = batch.BaseBranch
		baseSHA = batch.BaseSHA
	}

	// A batch is a real Git branch, not just a database label. Refuse to start
	// a release when the configured provider cannot create or update it.
	previousBatchHead := strings.TrimSpace(batch.HeadSHA)
	batchHead := ""
	releaseSnapshot := ""
	if operator, supported := s.provider.(git.BatchOperator); supported {
		result, opErr := operator.IntegrateReleaseToBatch(ctx, git.BatchBranchRequest{
			RepositoryID:   current.RepositoryID,
			BatchBranch:    batch.Branch,
			BaseBranch:     batch.BaseBranch,
			BaseSHA:        batch.BaseSHA,
			SourceBranch:   effectiveBranch,
			CurrentHeadSHA: batch.HeadSHA,
			SelectedSHAs:   commitSHAs(current.Commits),
		})
		if opErr != nil {
			return Release{}, Batch{}, opErr
		}
		if strings.EqualFold(strings.TrimSpace(result.Status), "conflict") {
			return Release{}, Batch{}, fmt.Errorf("%w: %s", ErrBatchConflict, firstNonEmpty(result.Message, "批次分支合并冲突"))
		}
		if strings.TrimSpace(result.BatchBranch) != "" {
			batch.Branch = strings.TrimSpace(result.BatchBranch)
		}
		batchHead = strings.TrimSpace(result.HeadSHA)
		releaseSnapshot = strings.TrimSpace(result.ReleaseSnapshotSHA)
	} else {
		return Release{}, Batch{}, git.ErrWriteUnsupported
	}

	current.SourceBranch = firstNonEmpty(current.SourceBranch, current.Branch)
	current.BaseBranch = baseBranch
	current.BatchID = batch.ID
	current.BatchBranch = batch.Branch
	current.BatchBaseSHA = baseSHA
	current.MainMergeStatus = MainMergePending
	current.UpdatedAt = now
	s.releases[releaseID] = current
	s.batches[batch.ID] = batch
	s.rebuildBatchLocked(batch.ID, now, true)
	if batchHead != "" {
		s.setBatchHeadLocked(batch.ID, batchHead, now)
	}
	current = s.releases[releaseID]
	if releaseSnapshot == "" {
		// Older BatchOperator implementations only return the shared branch
		// HEAD. It is a valid release snapshot when integration moved that
		// branch; when it did not move, retain the selected commit instead of
		// assigning the same shared HEAD to every release item.
		if batchHead != "" && !strings.EqualFold(batchHead, previousBatchHead) {
			releaseSnapshot = batchHead
		} else if len(current.Commits) > 0 {
			releaseSnapshot = strings.TrimSpace(current.Commits[0].SHA)
		}
	}
	current.BatchSnapshotSHA = firstNonEmpty(releaseSnapshot, batchHead, s.batches[batch.ID].HeadSHA)
	current.BatchSnapshotRevision = s.batches[batch.ID].Revision
	s.releases[releaseID] = current
	// The snapshot is assigned after the membership rebuild so the materialized
	// batch item returned by the API contains the same immutable version.
	s.rebuildBatchLocked(batch.ID, now, false)
	if err := s.persistLocked(); err != nil {
		return Release{}, Batch{}, err
	}
	return cloneRelease(s.releases[releaseID]), cloneBatch(s.batches[batch.ID]), nil
}

// MergeToMain completes the individual release item. Providers must implement
// MainMergeOperator to perform the real Git write; read-only providers receive
// a clear unsupported error instead of a false success.
func (s *Service) MergeToMain(ctx context.Context, releaseID string) (Release, Batch, error) {
	if s == nil || s.provider == nil {
		return Release{}, Batch{}, fmt.Errorf("%w: git provider is not configured", ErrInvalidRelease)
	}
	// Keep the compare-and-merge window single-file within this service. The
	// provider receives CurrentMainSHA as an additional compare-and-swap guard
	// for changes made outside the service process.
	s.mergeMu.Lock()
	defer s.mergeMu.Unlock()

	s.mu.RLock()
	item, ok := s.releases[releaseID]
	if !ok {
		s.mu.RUnlock()
		return Release{}, Batch{}, ErrReleaseNotFound
	}
	if item.BatchID == "" {
		s.mu.RUnlock()
		return Release{}, Batch{}, fmt.Errorf("%w: release is not in a batch", ErrMainMergeNotReady)
	}
	batch, ok := s.batches[item.BatchID]
	if !ok {
		s.mu.RUnlock()
		return Release{}, Batch{}, ErrBatchNotFound
	}
	if batch.Status != BatchOpen {
		s.mu.RUnlock()
		return Release{}, Batch{}, ErrBatchClosed
	}
	if item.MainMergeStatus == MainMergeMerged {
		s.mu.RUnlock()
		return cloneRelease(item), cloneBatch(batch), nil
	}
	if item.Status != StatusSucceeded || !productionSucceeded(item.Targets) {
		s.mu.RUnlock()
		return Release{}, Batch{}, fmt.Errorf("%w: 生产环境尚未完成", ErrMainMergeNotReady)
	}
	actualMainSHA, err := branchHead(ctx, s.provider, item.RepositoryID, firstNonEmpty(batch.BaseBranch, item.BaseBranch, "main"))
	if err != nil {
		s.mu.RUnlock()
		return Release{}, Batch{}, err
	}
	if batch.CurrentMainSHA != "" && actualMainSHA != batch.CurrentMainSHA {
		s.mu.RUnlock()
		return Release{}, Batch{}, fmt.Errorf("%w: main 已有批次之外的新提交，请重新确认后再合入", ErrBatchConflict)
	}
	request := git.MainMergeRequest{
		RepositoryID:   item.RepositoryID,
		SourceBranch:   firstNonEmpty(item.Branch, item.SourceBranch),
		BaseBranch:     firstNonEmpty(batch.BaseBranch, item.BaseBranch, "main"),
		BatchBranch:    batch.Branch,
		CurrentMainSHA: actualMainSHA,
		SelectedSHAs:   commitSHAs(item.Commits),
	}
	s.mu.RUnlock()

	operator, supported := s.provider.(git.MainMergeOperator)
	if !supported {
		return Release{}, Batch{}, git.ErrWriteUnsupported
	}
	result, err := operator.MergeReleaseToMain(ctx, request)
	if err != nil {
		return Release{}, Batch{}, err
	}
	if strings.EqualFold(strings.TrimSpace(result.Status), "conflict") || len(result.Conflicts) > 0 {
		s.markMergeConflict(releaseID)
		return Release{}, Batch{}, ErrBatchConflict
	}
	mergeSHA := strings.TrimSpace(result.HeadSHA)
	if mergeSHA == "" {
		return Release{}, Batch{}, fmt.Errorf("%w: Git provider did not return the new main SHA", ErrBatchConflict)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok = s.releases[releaseID]
	if !ok {
		return Release{}, Batch{}, ErrReleaseNotFound
	}
	batch, ok = s.batches[item.BatchID]
	if !ok {
		return Release{}, Batch{}, ErrBatchNotFound
	}
	if item.MainMergeStatus == MainMergeMerged {
		return cloneRelease(item), cloneBatch(batch), nil
	}
	if batch.CurrentMainSHA != "" && batch.CurrentMainSHA != request.CurrentMainSHA {
		return Release{}, Batch{}, fmt.Errorf("%w: main 在合入过程中发生变化，请重新确认", ErrBatchConflict)
	}
	now := s.now().UTC()
	item.MainMergeStatus = MainMergeMerged
	item.MainMergeSHA = mergeSHA
	item.MainMergedAt = timestamp(now)
	item.UpdatedAt = now
	s.releases[releaseID] = item
	batch.CurrentMainSHA = mergeSHA
	s.batches[batch.ID] = batch
	s.rebuildBatchLocked(batch.ID, now, false)
	if err := s.persistLocked(); err != nil {
		return Release{}, Batch{}, err
	}
	return cloneRelease(s.releases[releaseID]), cloneBatch(s.batches[batch.ID]), nil
}

func (s *Service) CloseBatch(projectID, batchID string) (Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.batches[strings.TrimSpace(batchID)]
	if !ok || batch.ProjectID != strings.TrimSpace(projectID) {
		return Batch{}, ErrBatchNotFound
	}
	if batch.Status == BatchClosed {
		return cloneBatch(batch), nil
	}
	for _, item := range s.batchItemsLocked(batch.ID) {
		if !batchItemTerminal(item) {
			return Batch{}, fmt.Errorf("%w: 还有发布项没有合入 main、取消或终止", ErrBatchConflict)
		}
	}
	now := s.now().UTC()
	batch.Status = BatchClosed
	batch.ClosedAt = timestamp(now)
	batch.UpdatedAt = now
	s.batches[batch.ID] = batch
	s.rebuildBatchLocked(batch.ID, now, false)
	if err := s.persistLocked(); err != nil {
		return Batch{}, err
	}
	return cloneBatch(s.batches[batch.ID]), nil
}

func (s *Service) markMergeConflict(releaseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.releases[releaseID]
	if !ok {
		return
	}
	item.MainMergeStatus = MainMergeConflict
	item.UpdatedAt = s.now().UTC()
	s.releases[releaseID] = item
	if item.BatchID != "" {
		s.rebuildBatchLocked(item.BatchID, item.UpdatedAt, false)
	}
	if err := s.persistLocked(); err != nil {
		return
	}
}

func (s *Service) rebuildBatchLocked(batchID string, now time.Time, increment bool) {
	batch, ok := s.batches[batchID]
	if !ok {
		return
	}
	if increment || batch.Revision == 0 {
		batch.Revision++
	}
	items := s.batchItemsLocked(batchID)
	batch.Items = items
	// A provider-generated branch head is authoritative. Recompute the
	// deterministic fallback only when the batch membership changes or no
	// provider head has been recorded yet; status changes must not make the
	// branch appear to move.
	if increment || strings.TrimSpace(batch.HeadSHA) == "" {
		batch.HeadSHA = batchDigest(batch.BaseSHA, items)
	}
	batch.UpdatedAt = now
	for _, item := range items {
		release, exists := s.releases[item.ReleaseID]
		if !exists {
			continue
		}
		release.BatchRevision = batch.Revision
		release.BatchHeadSHA = batch.HeadSHA
		release.BatchBranch = batch.Branch
		s.releases[item.ReleaseID] = release
	}
	s.batches[batchID] = batch
}

// syncBatchLocked refreshes the materialized item summaries after a release
// target changes. It intentionally does not increment the batch revision or
// move the shared branch head; only batch membership/integration does that.
func (s *Service) syncBatchLocked(release Release, now time.Time) {
	if strings.TrimSpace(release.BatchID) == "" {
		return
	}
	s.rebuildBatchLocked(release.BatchID, now, false)
}

func (s *Service) setBatchHeadLocked(batchID, headSHA string, now time.Time) {
	batch, ok := s.batches[batchID]
	if !ok || strings.TrimSpace(headSHA) == "" {
		return
	}
	batch.HeadSHA = strings.TrimSpace(headSHA)
	batch.UpdatedAt = now
	for _, item := range batch.Items {
		release, exists := s.releases[item.ReleaseID]
		if !exists {
			continue
		}
		release.BatchHeadSHA = batch.HeadSHA
		s.releases[item.ReleaseID] = release
	}
	s.batches[batchID] = batch
}

func (s *Service) batchItemsLocked(batchID string) []BatchItem {
	items := make([]BatchItem, 0)
	for _, item := range s.releases {
		if item.BatchID != batchID {
			continue
		}
		completed, total, currentEnvironment := releaseEnvironmentProgress(item.Targets)
		items = append(items, BatchItem{
			ReleaseID:             item.ID,
			Branch:                firstNonEmpty(item.SourceBranch, item.Branch),
			SourceBranch:          item.SourceBranch,
			OwnerID:               item.OwnerID,
			OwnerName:             item.OwnerName,
			Commits:               append([]git.Commit(nil), item.Commits...),
			ReleaseStatus:         item.Status,
			CurrentEnvironment:    currentEnvironment,
			CompletedEnvironments: completed,
			EnvironmentCount:      total,
			MainMergeStatus:       item.MainMergeStatus,
			MainMergeSHA:          item.MainMergeSHA,
			MainMergedAt:          cloneTime(item.MainMergedAt),
			BatchSnapshotSHA:      item.BatchSnapshotSHA,
			BatchSnapshotRevision: item.BatchSnapshotRevision,
			CreatedAt:             item.CreatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ReleaseID < items[j].ReleaseID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func branchHead(ctx context.Context, provider git.Provider, repositoryID, branch string) (string, error) {
	branches, err := provider.ListBranches(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	for _, item := range branches {
		if strings.TrimSpace(item.Name) == strings.TrimSpace(branch) {
			if sha := strings.TrimSpace(item.Head.SHA); sha != "" {
				return sha, nil
			}
		}
	}
	return "", fmt.Errorf("%w: base branch %q", git.ErrBranchNotFound, branch)
}

func validateReleaseCommitsOnBranch(ctx context.Context, provider git.Provider, repositoryID, branch string, commits []git.Commit) error {
	history, err := provider.ListCommits(ctx, repositoryID, branch, 0)
	if err != nil {
		return err
	}
	historySet := commitSHASet(history)
	for _, commit := range commits {
		sha := strings.ToLower(strings.TrimSpace(commit.SHA))
		if sha == "" {
			return fmt.Errorf("%w: selected commit SHA is empty", ErrInvalidRelease)
		}
		if _, ok := historySet[sha]; !ok {
			return fmt.Errorf("%w: selected commit %q does not belong to source branch %q", git.ErrCommitNotFound, commit.SHA, branch)
		}
	}
	return nil
}

func releaseTargetsSucceeded(targets []ReleaseTarget) bool {
	if len(targets) == 0 {
		return true
	}
	for _, target := range targets {
		if target.Status != TargetSucceeded {
			return false
		}
	}
	return true
}

func productionSucceeded(targets []ReleaseTarget) bool {
	foundProduction := false
	for _, target := range targets {
		environment := strings.ToLower(strings.TrimSpace(target.Environment))
		stage := strings.ToLower(strings.TrimSpace(target.EnvironmentStage))
		// Main may only receive an explicitly configured PROD stage. The
		// environment name is used only for old snapshots without stage metadata.
		if stage == "prod" || stage == "" && environment == "prod" {
			foundProduction = true
			if target.Status != TargetSucceeded {
				return false
			}
		}
	}
	return foundProduction
}

func releaseEnvironmentProgress(targets []ReleaseTarget) (int, int, string) {
	if len(targets) == 0 {
		return 0, 0, ""
	}
	completed := 0
	current := ""
	for _, target := range targets {
		if target.Status == TargetSucceeded {
			completed++
			continue
		}
		if current == "" {
			current = firstNonEmpty(target.Name, target.Environment)
		}
	}
	if current == "" && completed == len(targets) {
		current = firstNonEmpty(targets[len(targets)-1].Name, targets[len(targets)-1].Environment)
	}
	return completed, len(targets), current
}

func batchItemTerminal(item BatchItem) bool {
	if item.MainMergeStatus == MainMergeMerged || item.MainMergeStatus == MainMergeCancelled || item.MainMergeStatus == MainMergeTerminated {
		return true
	}
	return false
}

func commitSHAs(commits []git.Commit) []string {
	result := make([]string, 0, len(commits))
	for _, commit := range commits {
		if sha := strings.TrimSpace(commit.SHA); sha != "" {
			result = append(result, sha)
		}
	}
	return result
}

func batchDigest(baseSHA string, items []BatchItem) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(strings.TrimSpace(baseSHA)))
	for _, item := range items {
		_, _ = hash.Write([]byte("\x00" + item.ReleaseID + "\x00"))
		for _, commit := range item.Commits {
			_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(commit.SHA)) + "\x00"))
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneBatch(batch Batch) Batch {
	batch.Items = append([]BatchItem(nil), batch.Items...)
	for index := range batch.Items {
		batch.Items[index].Commits = append([]git.Commit(nil), batch.Items[index].Commits...)
		batch.Items[index].MainMergedAt = cloneTime(batch.Items[index].MainMergedAt)
	}
	batch.ClosedAt = cloneTime(batch.ClosedAt)
	return batch
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
