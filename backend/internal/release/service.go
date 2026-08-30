package release

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

var (
	ErrInvalidRelease    = errors.New("invalid release")
	ErrReleaseNotFound   = errors.New("release not found")
	ErrInvalidStatus     = errors.New("invalid release status")
	ErrInvalidStatusFlow = errors.New("invalid release status transition")
	ErrReleaseImmutable  = errors.New("release is immutable in its current status")
	ErrLastCommit        = errors.New("a release must contain at least one commit")
	ErrInvalidProgress   = errors.New("invalid release progress")
	ErrTargetRetry       = errors.New("target retry is not allowed")
	ErrTargetNotReady    = errors.New("target is waiting for a prerequisite environment")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Strategy string

const (
	StrategyRolling   Strategy = "rolling"
	StrategyCanary    Strategy = "canary"
	StrategyBlueGreen Strategy = "blue_green"
)

type TrafficSplit struct {
	StablePercent    int `json:"stable_percent"`
	CandidatePercent int `json:"candidate_percent"`
	BluePercent      int `json:"blue_percent"`
	GreenPercent     int `json:"green_percent"`
}

type RolloutPlan struct {
	Strategy Strategy     `json:"strategy"`
	Traffic  TrafficSplit `json:"traffic"`
}

type TargetStatus string

const (
	TargetPending   TargetStatus = "pending"
	TargetWaiting   TargetStatus = "waiting"
	TargetRunning   TargetStatus = "running"
	TargetSucceeded TargetStatus = "succeeded"
	TargetFailed    TargetStatus = "failed"
	TargetCancelled TargetStatus = "cancelled"
)

// TargetInput is a snapshot of the environment selected for a release. The
// snapshot makes a historical release auditable even if a target is renamed or
// reconfigured later.
type TargetInput struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Environment      string `json:"environment"`
	EnvironmentStage string `json:"environment_stage,omitempty"`
	SortOrder        int    `json:"sort_order,omitempty"`
	ClusterID        string `json:"cluster_id"`
	Namespace        string `json:"namespace"`
	Replicas         int    `json:"replicas"`
	ContainerPort    int    `json:"container_port"`
	DeployStrategy   string `json:"deploy_strategy"`
}

type ReleaseTarget struct {
	TargetInput
	Status     TargetStatus `json:"status"`
	Progress   int          `json:"progress"`
	Stage      string       `json:"stage,omitempty"`
	Message    string       `json:"message,omitempty"`
	Error      string       `json:"error,omitempty"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
}

type Release struct {
	ID                    string          `json:"id"`
	SpaceID               string          `json:"space_id,omitempty"`
	ProjectID             string          `json:"project_id"`
	RepositoryID          string          `json:"repository_id"`
	Branch                string          `json:"branch"`
	SourceBranch          string          `json:"source_branch,omitempty"`
	BaseBranch            string          `json:"base_branch,omitempty"`
	Name                  string          `json:"name,omitempty"`
	OwnerID               uint64          `json:"owner_id,omitempty"`
	OwnerName             string          `json:"owner_name,omitempty"`
	Commits               []git.Commit    `json:"commits"`
	Targets               []ReleaseTarget `json:"targets,omitempty"`
	Plan                  RolloutPlan     `json:"plan"`
	Status                Status          `json:"status"`
	Progress              int             `json:"progress,omitempty"`
	Stage                 string          `json:"stage,omitempty"`
	Message               string          `json:"message,omitempty"`
	Error                 string          `json:"error,omitempty"`
	StartedAt             *time.Time      `json:"started_at,omitempty"`
	FinishedAt            *time.Time      `json:"finished_at,omitempty"`
	BatchID               string          `json:"batch_id,omitempty"`
	BatchBranch           string          `json:"batch_branch,omitempty"`
	BatchBaseSHA          string          `json:"batch_base_sha,omitempty"`
	BatchHeadSHA          string          `json:"batch_head_sha,omitempty"`
	BatchSnapshotSHA      string          `json:"batch_snapshot_sha,omitempty"`
	BatchSnapshotRevision int             `json:"batch_snapshot_revision,omitempty"`
	BatchRevision         int             `json:"batch_revision,omitempty"`
	MainMergeStatus       MainMergeStatus `json:"main_merge_status"`
	MainMergeSHA          string          `json:"main_merge_sha,omitempty"`
	MainMergedAt          *time.Time      `json:"main_merged_at,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type CreateInput struct {
	SpaceID      string        `json:"space_id"`
	ProjectID    string        `json:"project_id"`
	RepositoryID string        `json:"repository_id"`
	Branch       string        `json:"branch"`
	SourceBranch string        `json:"source_branch"`
	BaseBranch   string        `json:"base_branch"`
	Name         string        `json:"name"`
	OwnerID      uint64        `json:"owner_id"`
	OwnerName    string        `json:"owner_name"`
	CommitSHAs   []string      `json:"commit_shas"`
	Strategy     Strategy      `json:"strategy"`
	Traffic      TrafficSplit  `json:"traffic"`
	Targets      []TargetInput `json:"targets,omitempty"`
}

type Service struct {
	mu sync.RWMutex
	// Main merges are serialized so two release items cannot both pass the
	// service-side head check before either provider call finishes. The Git
	// provider still has to enforce CurrentMainSHA atomically against the real
	// remote branch.
	mergeMu     sync.Mutex
	provider    git.Provider
	persistence Persistence
	now         func() time.Time
	nextID      uint64
	nextBatchID uint64
	releases    map[string]Release
	batches     map[string]Batch
	duplicates  map[string]string
}

func NewService(provider git.Provider) *Service {
	return newService(provider, nil)
}

// NewPersistentService restores durable release state before serving requests.
// NewService remains available for unit tests and explicit in-memory usage.
func NewPersistentService(provider git.Provider, persistence Persistence) (*Service, error) {
	service := newService(provider, persistence)
	if persistence == nil {
		return service, nil
	}
	if err := service.restore(context.Background()); err != nil {
		return nil, err
	}
	return service, nil
}

func newService(provider git.Provider, persistence Persistence) *Service {
	if provider == nil {
		provider = git.NewDemoProvider()
	}
	return &Service{
		provider:    provider,
		persistence: persistence,
		now:         func() time.Time { return time.Now().UTC() },
		releases:    make(map[string]Release),
		batches:     make(map[string]Batch),
		duplicates:  make(map[string]string),
	}
}

// Persistence stores complete release and batch snapshots. Keeping the
// snapshot here makes state transitions atomic from the service's point of
// view while the database adapter remains outside the release domain package.
type Persistence interface {
	Load(ctx context.Context) ([]Release, []Batch, error)
	Replace(ctx context.Context, releases []Release, batches []Batch) error
}

func (s *Service) restore(ctx context.Context) error {
	releases, batches, err := s.persistence.Load(ctx)
	if err != nil {
		return err
	}
	for _, item := range releases {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		s.releases[item.ID] = cloneRelease(item)
		if value := numericSuffix(item.ID, "rel-"); value > s.nextID {
			s.nextID = value
		}
		s.duplicates[releaseDuplicateKey(item)] = item.ID
	}
	for _, batch := range batches {
		if strings.TrimSpace(batch.ID) == "" {
			continue
		}
		s.batches[batch.ID] = cloneBatch(batch)
		if value := numericSuffix(batch.ID, "batch-"); value > s.nextBatchID {
			s.nextBatchID = value
		}
	}
	return nil
}

func numericSuffix(value, prefix string) uint64 {
	value = strings.TrimPrefix(strings.TrimSpace(value), prefix)
	var result uint64
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0
		}
		result = result*10 + uint64(char-'0')
	}
	return result
}

func (s *Service) persistLocked() error {
	if s.persistence == nil {
		return nil
	}
	releases := make([]Release, 0, len(s.releases))
	for _, item := range s.releases {
		releases = append(releases, cloneRelease(item))
	}
	batches := make([]Batch, 0, len(s.batches))
	for _, batch := range s.batches {
		batches = append(batches, cloneBatch(batch))
	}
	return s.persistence.Replace(context.Background(), releases, batches)
}

// SetClock is useful to deterministic callers and tests. It should be called
// during setup, before the service is shared between goroutines.
func (s *Service) SetClock(clock func() time.Time) {
	if clock != nil {
		s.now = clock
	}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Release, bool, error) {
	if err := validateIdentity(input); err != nil {
		return Release{}, false, err
	}
	plan, err := normalizePlan(input.Strategy, input.Traffic)
	if err != nil {
		return Release{}, false, err
	}
	commits, err := s.resolveCommits(ctx, input)
	if err != nil {
		return Release{}, false, err
	}
	key := duplicateKey(input, commits, plan)

	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID, ok := s.duplicates[key]; ok {
		return cloneRelease(s.releases[existingID]), true, nil
	}
	now := s.now().UTC()
	s.nextID++
	id := fmt.Sprintf("rel-%06d", s.nextID)
	sourceBranch := strings.TrimSpace(input.SourceBranch)
	if sourceBranch == "" {
		sourceBranch = strings.TrimSpace(input.Branch)
	}
	baseBranch := strings.TrimSpace(input.BaseBranch)
	result := Release{ID: id, SpaceID: strings.TrimSpace(input.SpaceID), ProjectID: input.ProjectID, RepositoryID: input.RepositoryID, Branch: input.Branch, SourceBranch: sourceBranch, BaseBranch: baseBranch, Name: strings.TrimSpace(input.Name), OwnerID: input.OwnerID, OwnerName: strings.TrimSpace(input.OwnerName), Commits: append([]git.Commit(nil), commits...), Targets: newReleaseTargets(input.Targets), Plan: plan, Status: StatusDraft, MainMergeStatus: MainMergePending, CreatedAt: now, UpdatedAt: now}
	s.releases[id] = result
	s.duplicates[key] = id
	if err := s.persistLocked(); err != nil {
		return Release{}, false, err
	}
	return cloneRelease(result), false, nil
}

func (s *Service) resolveCommits(ctx context.Context, input CreateInput) ([]git.Commit, error) {
	shas := normalizeSHAs(input.CommitSHAs)
	if len(shas) == 0 {
		commits, err := s.provider.ListCommits(ctx, input.RepositoryID, input.Branch, 1)
		if err != nil {
			return nil, err
		}
		if len(commits) == 0 {
			return nil, fmt.Errorf("%w: branch has no commits", ErrInvalidRelease)
		}
		return commits[:1], nil
	}
	commits := make([]git.Commit, 0, len(shas))
	for _, sha := range shas {
		commit, err := s.provider.GetCommit(ctx, input.RepositoryID, sha)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(commit.SHA) == "" {
			return nil, fmt.Errorf("%w: provider returned an empty commit sha", ErrInvalidRelease)
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func (s *Service) Get(id string) (Release, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	return cloneRelease(release), nil
}

func (s *Service) List(projectID string) []Release {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Release, 0, len(s.releases))
	for _, release := range s.releases {
		if projectID == "" || release.ProjectID == projectID {
			result = append(result, cloneRelease(release))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

func (s *Service) RemoveCommit(id, sha string) (Release, error) {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return Release{}, fmt.Errorf("%w: commit sha is required", ErrInvalidRelease)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if release.Status != StatusDraft || release.BatchID != "" {
		return Release{}, ErrReleaseImmutable
	}
	index := -1
	for i, commit := range release.Commits {
		if strings.EqualFold(commit.SHA, sha) {
			index = i
			break
		}
	}
	if index < 0 {
		return Release{}, git.ErrCommitNotFound
	}
	if len(release.Commits) == 1 {
		return Release{}, ErrLastCommit
	}
	release.Commits = append(release.Commits[:index], release.Commits[index+1:]...)
	release.UpdatedAt = s.now().UTC()
	s.releases[id] = release
	for key, releaseID := range s.duplicates {
		if releaseID == id {
			delete(s.duplicates, key)
		}
	}
	s.duplicates[releaseDuplicateKey(release)] = id
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

func (s *Service) Transition(id string, target Status) (Release, error) {
	if !validStatus(target) {
		return Release{}, ErrInvalidStatus
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if target == StatusSucceeded && len(release.Targets) > 0 && !releaseTargetsSucceeded(release.Targets) {
		return Release{}, fmt.Errorf("%w: all deployment targets must succeed before the release can complete", ErrInvalidStatusFlow)
	}
	if !canTransition(release.Status, target) {
		return Release{}, fmt.Errorf("%w: %s -> %s", ErrInvalidStatusFlow, release.Status, target)
	}
	previous := release.Status
	release.Status = target
	now := s.now().UTC()
	applyTransitionMetadata(&release, previous, target, now)
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

// UpdateProgress records execution feedback without changing the release
// state. Executors may report progress while a release is queued or running;
// draft and terminal records remain immutable.
func (s *Service) UpdateProgress(id string, progress int, stage, message string) (Release, error) {
	if progress < 0 || progress > 100 {
		return Release{}, fmt.Errorf("%w: progress must be between 0 and 100", ErrInvalidProgress)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if release.Status != StatusQueued && release.Status != StatusRunning {
		return Release{}, ErrReleaseImmutable
	}
	release.Progress = progress
	release.Stage = strings.TrimSpace(stage)
	release.Message = strings.TrimSpace(message)
	release.UpdatedAt = s.now().UTC()
	s.releases[id] = release
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

// SetTargets attaches the selected environments to a legacy release the first
// time it is published. New releases already carry targets from Create.
func (s *Service) SetTargets(id string, inputs []TargetInput) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if len(release.Targets) == 0 {
		release.Targets = newReleaseTargets(inputs)
		release.UpdatedAt = s.now().UTC()
		s.releases[id] = release
		if err := s.persistLocked(); err != nil {
			return Release{}, err
		}
	}
	return cloneRelease(release), nil
}

// StartTarget claims the next environment in a release's ordered rollout.
// Only one target can be active at a time; later targets remain waiting until
// the current target is completed. The boolean is false when the target was
// already running, which lets HTTP callers safely make duplicate requests
// without starting a second executor.
func (s *Service) StartTarget(id, targetID string) (Release, bool, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return Release{}, false, fmt.Errorf("%w: target id is required", ErrInvalidRelease)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, false, ErrReleaseNotFound
	}
	if err := s.ensureOpenBatchLocked(release); err != nil {
		return Release{}, false, err
	}
	if len(release.Targets) == 0 {
		return Release{}, false, fmt.Errorf("%w: release has no deployment targets", ErrInvalidRelease)
	}
	index := findReleaseTarget(release.Targets, targetID)
	if index < 0 {
		return Release{}, false, fmt.Errorf("%w: target %s", ErrInvalidRelease, targetID)
	}

	// A terminal release is a version record that may be published again. Work
	// on a cloned value so a rejected request cannot partially reset the stored
	// target state.
	release = cloneRelease(release)
	if release.Status == StatusFailed || release.Status == StatusSucceeded || release.Status == StatusCancelled {
		resetTargetStatuses(release.Targets)
		release.Status = StatusRunning
		release.Progress = aggregateTargetProgress(release.Targets)
		release.Stage = ""
		release.Message = ""
		release.Error = ""
		release.StartedAt = nil
		release.FinishedAt = nil
		release.MainMergeStatus = MainMergePending
		release.MainMergeSHA = ""
		release.MainMergedAt = nil
	}

	target := &release.Targets[index]
	if target.Status == TargetSucceeded {
		return Release{}, false, fmt.Errorf("%w: target %s has already succeeded", ErrReleaseImmutable, targetID)
	}
	if target.Status == TargetFailed || target.Status == TargetCancelled {
		return Release{}, false, fmt.Errorf("%w: target %s must be retried first", ErrTargetRetry, targetID)
	}
	if target.Status == TargetRunning {
		if release.Status == StatusDraft || release.Status == StatusQueued {
			now := s.now().UTC()
			previous := release.Status
			release.Status = StatusRunning
			applyTransitionMetadata(&release, previous, StatusRunning, now)
			release.UpdatedAt = now
			s.releases[id] = release
			if err := s.persistLocked(); err != nil {
				return Release{}, false, err
			}
		}
		return cloneRelease(release), false, nil
	}

	nextIndex := nextTargetIndex(release.Targets)
	if nextIndex < 0 {
		return Release{}, false, fmt.Errorf("%w: all deployment targets have completed", ErrTargetNotReady)
	}
	if nextIndex != index {
		return Release{}, false, targetNotReadyError(release.Targets[nextIndex], *target)
	}
	if target.Status == TargetWaiting {
		// The target is now the first unfinished stage. Promote it before
		// claiming it so the externally visible state is unambiguous.
		target.Status = TargetPending
	}
	if target.Status != TargetPending {
		return Release{}, false, fmt.Errorf("%w: target %s is %s", ErrTargetNotReady, targetID, target.Status)
	}
	if release.Status == StatusFailed {
		return Release{}, false, fmt.Errorf("%w: target %s must be retried first", ErrTargetRetry, targetID)
	}
	if release.Status != StatusDraft && release.Status != StatusQueued && release.Status != StatusRunning {
		return Release{}, false, fmt.Errorf("%w: cannot start target while release is %s", ErrInvalidStatusFlow, release.Status)
	}

	now := s.now().UTC()
	previous := release.Status
	release.Status = StatusRunning
	applyTransitionMetadata(&release, previous, StatusRunning, now)
	target.Status = TargetRunning
	target.Progress = 0
	target.Stage = "preparing"
	target.Message = "正在准备环境发布"
	target.Error = ""
	if target.StartedAt == nil {
		target.StartedAt = timestamp(now)
	}
	release.Progress = aggregateTargetProgress(release.Targets)
	release.Stage = "preparing"
	release.Message = "正在准备发布：" + targetDisplayName(*target)
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, false, err
	}
	return cloneRelease(release), true, nil
}

func (s *Service) UpdateTargetProgress(id, targetID string, progress int, stage, message string) (Release, error) {
	if progress < 0 || progress > 100 {
		return Release{}, fmt.Errorf("%w: target progress must be between 0 and 100", ErrInvalidProgress)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	index := findReleaseTarget(release.Targets, targetID)
	if index < 0 {
		return Release{}, fmt.Errorf("%w: target %s", ErrInvalidRelease, targetID)
	}
	target := &release.Targets[index]
	if target.Status == TargetWaiting {
		return Release{}, targetNotReadyErrorForTarget(*target)
	}
	if target.Status != TargetPending && target.Status != TargetRunning {
		return Release{}, ErrReleaseImmutable
	}
	if err := ensureTargetIsNext(release.Targets, index); err != nil {
		return Release{}, err
	}
	target.Status = TargetRunning
	target.Progress = progress
	target.Stage = strings.TrimSpace(stage)
	target.Message = strings.TrimSpace(message)
	if target.StartedAt == nil {
		now := s.now().UTC()
		target.StartedAt = &now
	}
	release.Progress = aggregateTargetProgress(release.Targets)
	release.Stage = target.Stage
	release.Message = target.Message
	release.UpdatedAt = s.now().UTC()
	s.releases[id] = release
	s.syncBatchLocked(release, release.UpdatedAt)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

func (s *Service) CompleteTarget(id, targetID string) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	index := findReleaseTarget(release.Targets, targetID)
	if index < 0 {
		return Release{}, fmt.Errorf("%w: target %s", ErrInvalidRelease, targetID)
	}
	target := &release.Targets[index]
	if target.Status == TargetWaiting {
		return Release{}, targetNotReadyErrorForTarget(*target)
	}
	if target.Status != TargetRunning && target.Status != TargetPending {
		return Release{}, ErrReleaseImmutable
	}
	if err := ensureTargetIsNext(release.Targets, index); err != nil {
		return Release{}, err
	}
	now := s.now().UTC()
	if target.StartedAt == nil {
		target.StartedAt = timestamp(now)
	}
	target.Status = TargetSucceeded
	target.Progress = 100
	target.Stage = "succeeded"
	target.Message = "环境发布完成"
	target.FinishedAt = &now
	release.Progress = aggregateTargetProgress(release.Targets)
	if nextIndex := promoteNextTarget(release.Targets); nextIndex >= 0 {
		release.Stage = "waiting"
		release.Message = "等待推进环境：" + targetDisplayName(release.Targets[nextIndex])
	} else {
		release.Stage = "checking"
		release.Message = "所有环境发布完成，等待汇总"
	}
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

func (s *Service) FailTarget(id, targetID, errorMessage string) (Release, error) {
	errorMessage = strings.TrimSpace(errorMessage)
	if errorMessage == "" {
		return Release{}, fmt.Errorf("%w: target error message is required", ErrInvalidRelease)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	index := findReleaseTarget(release.Targets, targetID)
	if index < 0 {
		return Release{}, fmt.Errorf("%w: target %s", ErrInvalidRelease, targetID)
	}
	now := s.now().UTC()
	target := &release.Targets[index]
	if target.Status == TargetWaiting {
		return Release{}, targetNotReadyErrorForTarget(*target)
	}
	if target.Status != TargetPending && target.Status != TargetRunning {
		return Release{}, ErrReleaseImmutable
	}
	if err := ensureTargetIsNext(release.Targets, index); err != nil {
		return Release{}, err
	}
	target.Status = TargetFailed
	target.Progress = 0
	target.Stage = "failed"
	target.Message = "环境发布失败"
	target.Error = errorMessage
	target.FinishedAt = &now
	release.Progress = aggregateTargetProgress(release.Targets)
	release.Stage = "failed"
	release.Message = "环境发布失败：" + target.Name
	release.Error = errorMessage
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

// CancelPendingTargets closes environments that have not started when a
// serial multi-target publish stops at an earlier failure. The release itself
// remains in the caller-selected terminal state (usually failed).
func (s *Service) CancelPendingTargets(id, message string) (Release, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "因前置环境失败，未继续发布"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	now := s.now().UTC()
	for index := range release.Targets {
		target := &release.Targets[index]
		if target.Status != TargetPending && target.Status != TargetWaiting && target.Status != TargetRunning {
			continue
		}
		target.Status = TargetCancelled
		target.Stage = "cancelled"
		target.Message = message
		target.FinishedAt = &now
	}
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

// RetryTarget starts a new run for one environment of a failed release. The
// other environment snapshots and their terminal results stay untouched, so a
// transient failure in one cluster does not cause successful clusters to be
// redeployed.
func (s *Service) RetryTarget(id, targetID string) (Release, error) {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return Release{}, fmt.Errorf("%w: target id is required", ErrTargetRetry)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if err := s.ensureOpenBatchLocked(release); err != nil {
		return Release{}, err
	}
	if release.Status != StatusFailed {
		return Release{}, fmt.Errorf("%w: release must be failed", ErrTargetRetry)
	}
	index := findReleaseTarget(release.Targets, targetID)
	if index < 0 {
		return Release{}, fmt.Errorf("%w: target %s", ErrTargetRetry, targetID)
	}
	target := &release.Targets[index]
	if target.Status != TargetFailed && target.Status != TargetCancelled {
		return Release{}, fmt.Errorf("%w: target %s is %s", ErrTargetRetry, targetID, target.Status)
	}
	if err := ensureTargetIsNext(release.Targets, index); err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrTargetRetry, err)
	}
	release = cloneRelease(release)
	target = &release.Targets[index]
	now := s.now().UTC()
	target.Status = TargetPending
	target.Progress = 0
	target.Stage = ""
	target.Message = ""
	target.Error = ""
	target.StartedAt = nil
	target.FinishedAt = nil
	release.Status = StatusRunning
	release.Progress = aggregateTargetProgress(release.Targets)
	release.Stage = "preparing"
	release.Message = fmt.Sprintf("正在重试环境 %s", target.Name)
	release.Error = ""
	release.StartedAt = timestamp(now)
	release.FinishedAt = nil
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

// FinalizeTargets reconciles a running multi-target release after an executor
// finishes one target. It succeeds only when every target succeeded; once all
// targets are terminal, any remaining cancelled/failed target leaves the
// release failed and available for a targeted retry.
func (s *Service) FinalizeTargets(id string) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if release.Status != StatusRunning {
		return cloneRelease(release), nil
	}
	if len(release.Targets) == 0 {
		return Release{}, fmt.Errorf("%w: release has no deployment targets", ErrInvalidRelease)
	}
	allSucceeded := true
	active := false
	failedNames := make([]string, 0)
	for _, target := range release.Targets {
		switch target.Status {
		case TargetSucceeded:
		case TargetPending, TargetWaiting, TargetRunning:
			active = true
			allSucceeded = false
		case TargetFailed, TargetCancelled:
			allSucceeded = false
			failedNames = append(failedNames, target.Name)
		default:
			allSucceeded = false
			failedNames = append(failedNames, target.Name)
		}
	}
	if active {
		if nextIndex := nextTargetIndex(release.Targets); nextIndex >= 0 && release.Targets[nextIndex].Status == TargetWaiting {
			release.Targets[nextIndex].Status = TargetPending
			release.Stage = "waiting"
			release.Message = "等待推进环境：" + targetDisplayName(release.Targets[nextIndex])
			release.UpdatedAt = s.now().UTC()
			s.releases[id] = release
			s.syncBatchLocked(release, release.UpdatedAt)
			if err := s.persistLocked(); err != nil {
				return Release{}, err
			}
		}
		return cloneRelease(release), nil
	}
	now := s.now().UTC()
	if allSucceeded {
		release.Status = StatusSucceeded
		applyTransitionMetadata(&release, StatusRunning, StatusSucceeded, now)
	} else {
		release.Status = StatusFailed
		release.Progress = aggregateTargetProgress(release.Targets)
		release.Stage = "failed"
		release.Message = "部分环境发布未完成"
		if len(failedNames) > 0 {
			release.Error = "以下环境仍需重试：" + strings.Join(failedNames, "、")
		}
		release.FinishedAt = timestamp(now)
	}
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

func (s *Service) ensureOpenBatchLocked(release Release) error {
	batchID := strings.TrimSpace(release.BatchID)
	if batchID == "" {
		return nil
	}
	batch, ok := s.batches[batchID]
	if !ok {
		return ErrBatchNotFound
	}
	if batch.Status != BatchOpen {
		return ErrBatchClosed
	}
	return nil
}

// Fail marks an active release as failed and keeps the executor's error for
// operators. It is intentionally separate from Transition so callers cannot
// accidentally overwrite the error with an ordinary status update.
func (s *Service) Fail(id, errorMessage string) (Release, error) {
	errorMessage = strings.TrimSpace(errorMessage)
	if errorMessage == "" {
		return Release{}, fmt.Errorf("%w: error message is required", ErrInvalidRelease)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if release.Status != StatusRunning {
		return Release{}, ErrReleaseImmutable
	}
	now := s.now().UTC()
	release.Status = StatusFailed
	release.Error = errorMessage
	release.Stage = "failed"
	release.Message = "发布失败"
	release.FinishedAt = timestamp(now)
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

// Cancel stops an active release. The method shares the same transition rules
// as Transition, but accepts an operator-facing message in one operation.
func (s *Service) Cancel(id, message string) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if release.Status != StatusDraft && release.Status != StatusQueued && release.Status != StatusRunning {
		return Release{}, ErrReleaseImmutable
	}
	now := s.now().UTC()
	release.Status = StatusCancelled
	release.Stage = "cancelled"
	release.Message = strings.TrimSpace(message)
	release.FinishedAt = timestamp(now)
	release.MainMergeStatus = MainMergeCancelled
	for index := range release.Targets {
		target := &release.Targets[index]
		if target.Status == TargetPending || target.Status == TargetWaiting || target.Status == TargetRunning {
			target.Status = TargetCancelled
			target.Stage = "cancelled"
			target.Message = "环境发布已取消"
			target.FinishedAt = timestamp(now)
		}
	}
	release.UpdatedAt = now
	s.releases[id] = release
	s.syncBatchLocked(release, now)
	if err := s.persistLocked(); err != nil {
		return Release{}, err
	}
	return cloneRelease(release), nil
}

func applyTransitionMetadata(release *Release, previous, target Status, now time.Time) {
	switch target {
	case StatusQueued:
		// Re-queueing a terminal release is the existing "再次发布" behavior.
		// Its immutable version data stays intact while execution metadata starts
		// a fresh run.
		if previous == StatusFailed || previous == StatusSucceeded || previous == StatusCancelled {
			release.Progress = 0
			release.Stage = ""
			release.Message = ""
			release.Error = ""
			release.StartedAt = nil
			release.FinishedAt = nil
			release.MainMergeStatus = MainMergePending
			release.MainMergeSHA = ""
			release.MainMergedAt = nil
			resetTargetStatuses(release.Targets)
		}
	case StatusRunning:
		if release.StartedAt == nil {
			release.StartedAt = timestamp(now)
		}
		release.FinishedAt = nil
		release.Error = ""
		if nextIndex := nextTargetIndex(release.Targets); nextIndex >= 0 && release.Targets[nextIndex].Status == TargetWaiting {
			release.Targets[nextIndex].Status = TargetPending
		}
	case StatusSucceeded:
		release.Progress = 100
		release.Stage = "succeeded"
		release.Message = "发布完成"
		release.FinishedAt = timestamp(now)
	case StatusFailed:
		release.Stage = "failed"
		release.Message = "发布失败"
		release.FinishedAt = timestamp(now)
	case StatusCancelled:
		release.Stage = "cancelled"
		release.Message = "发布已取消"
		release.FinishedAt = timestamp(now)
	}
}

func timestamp(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func validateIdentity(input CreateInput) error {
	if strings.TrimSpace(input.ProjectID) == "" {
		return fmt.Errorf("%w: project_id is required", ErrInvalidRelease)
	}
	if strings.TrimSpace(input.RepositoryID) == "" {
		return fmt.Errorf("%w: repository_id is required", ErrInvalidRelease)
	}
	if strings.TrimSpace(input.Branch) == "" {
		return fmt.Errorf("%w: branch is required", ErrInvalidRelease)
	}
	return nil
}

func normalizePlan(strategy Strategy, traffic TrafficSplit) (RolloutPlan, error) {
	if strategy == "" {
		strategy = StrategyRolling
	}
	if strategy != StrategyRolling && strategy != StrategyCanary && strategy != StrategyBlueGreen {
		return RolloutPlan{}, fmt.Errorf("%w: unsupported strategy %q", ErrInvalidRelease, strategy)
	}
	// Older clients only sent stable/candidate for every strategy. For a
	// blue/green request, interpret those fields as blue/green when the new
	// fields are absent so upgrades remain compatible.
	if strategy == StrategyBlueGreen && traffic.BluePercent == 0 && traffic.GreenPercent == 0 && (traffic.StablePercent != 0 || traffic.CandidatePercent != 0) {
		traffic.BluePercent = traffic.StablePercent
		traffic.GreenPercent = traffic.CandidatePercent
		traffic.StablePercent = 0
		traffic.CandidatePercent = 0
	}
	if traffic.StablePercent == 0 && traffic.CandidatePercent == 0 && traffic.BluePercent == 0 && traffic.GreenPercent == 0 {
		switch strategy {
		case StrategyRolling:
			traffic = TrafficSplit{StablePercent: 100}
		case StrategyCanary:
			traffic = TrafficSplit{StablePercent: 90, CandidatePercent: 10}
		case StrategyBlueGreen:
			traffic = TrafficSplit{GreenPercent: 100}
		}
	}
	if traffic.StablePercent < 0 || traffic.CandidatePercent < 0 || traffic.BluePercent < 0 || traffic.GreenPercent < 0 || traffic.StablePercent > 100 || traffic.CandidatePercent > 100 || traffic.BluePercent > 100 || traffic.GreenPercent > 100 {
		return RolloutPlan{}, fmt.Errorf("%w: traffic percentages must be non-negative and sum to 100", ErrInvalidRelease)
	}
	switch strategy {
	case StrategyRolling:
		if traffic.StablePercent != 100 || traffic.CandidatePercent != 0 || traffic.BluePercent != 0 || traffic.GreenPercent != 0 {
			return RolloutPlan{}, fmt.Errorf("%w: rolling traffic must be stable 100%%", ErrInvalidRelease)
		}
	case StrategyCanary:
		if traffic.StablePercent+traffic.CandidatePercent != 100 || traffic.BluePercent != 0 || traffic.GreenPercent != 0 {
			return RolloutPlan{}, fmt.Errorf("%w: canary traffic must use stable and candidate percentages", ErrInvalidRelease)
		}
	case StrategyBlueGreen:
		if traffic.BluePercent+traffic.GreenPercent != 100 || traffic.StablePercent != 0 || traffic.CandidatePercent != 0 {
			return RolloutPlan{}, fmt.Errorf("%w: blue/green traffic must use blue and green percentages", ErrInvalidRelease)
		}
	}
	return RolloutPlan{Strategy: strategy, Traffic: traffic}, nil
}

func validStatus(status Status) bool {
	switch status {
	case StatusDraft, StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

func canTransition(from, to Status) bool {
	switch from {
	case StatusDraft:
		return to == StatusQueued || to == StatusCancelled
	case StatusQueued:
		return to == StatusRunning || to == StatusCancelled
	case StatusRunning:
		return to == StatusSucceeded || to == StatusFailed || to == StatusCancelled
	case StatusFailed:
		return to == StatusQueued
	case StatusSucceeded, StatusCancelled:
		// A release is an immutable version record, but its deployment can be
		// started again. This is the "再次发布" action in the console.
		return to == StatusQueued
	default:
		return false
	}
}

func normalizeSHAs(shas []string) []string {
	result := make([]string, 0, len(shas))
	seen := make(map[string]struct{}, len(shas))
	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha != "" {
			key := strings.ToLower(sha)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, sha)
			}
		}
	}
	return result
}

func duplicateKey(input CreateInput, commits []git.Commit, plan RolloutPlan) string {
	shas := make([]string, len(commits))
	for i, commit := range commits {
		shas[i] = strings.ToLower(commit.SHA)
	}
	sort.Strings(shas)
	targetIDs := make([]string, 0, len(input.Targets))
	for _, target := range input.Targets {
		if id := strings.TrimSpace(target.ID); id != "" {
			targetIDs = append(targetIDs, id)
		}
	}
	sort.Strings(targetIDs)
	return strings.Join([]string{strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.RepositoryID), strings.TrimSpace(input.Branch), string(plan.Strategy), fmt.Sprintf("%d", plan.Traffic.StablePercent), fmt.Sprintf("%d", plan.Traffic.CandidatePercent), fmt.Sprintf("%d", plan.Traffic.BluePercent), fmt.Sprintf("%d", plan.Traffic.GreenPercent), strings.Join(shas, ","), strings.Join(targetIDs, ",")}, "\x00")
}

func releaseDuplicateKey(release Release) string {
	targets := make([]TargetInput, 0, len(release.Targets))
	for _, target := range release.Targets {
		targets = append(targets, target.TargetInput)
	}
	return duplicateKey(CreateInput{ProjectID: release.ProjectID, RepositoryID: release.RepositoryID, Branch: release.Branch, Targets: targets}, release.Commits, release.Plan)
}

func cloneRelease(release Release) Release {
	release.Commits = append([]git.Commit(nil), release.Commits...)
	release.Targets = append([]ReleaseTarget(nil), release.Targets...)
	if release.StartedAt != nil {
		startedAt := *release.StartedAt
		release.StartedAt = &startedAt
	}
	if release.FinishedAt != nil {
		finishedAt := *release.FinishedAt
		release.FinishedAt = &finishedAt
	}
	if release.MainMergedAt != nil {
		mergedAt := *release.MainMergedAt
		release.MainMergedAt = &mergedAt
	}
	for index := range release.Targets {
		if release.Targets[index].StartedAt != nil {
			startedAt := *release.Targets[index].StartedAt
			release.Targets[index].StartedAt = &startedAt
		}
		if release.Targets[index].FinishedAt != nil {
			finishedAt := *release.Targets[index].FinishedAt
			release.Targets[index].FinishedAt = &finishedAt
		}
	}
	return release
}

func newReleaseTargets(inputs []TargetInput) []ReleaseTarget {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]ReleaseTarget, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		input.ID = strings.TrimSpace(input.ID)
		if input.ID == "" {
			continue
		}
		if _, exists := seen[input.ID]; exists {
			continue
		}
		seen[input.ID] = struct{}{}
		result = append(result, ReleaseTarget{TargetInput: input, Status: TargetPending})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := targetStageRank(result[i]), targetStageRank(result[j])
		if left == right {
			return targetDisplayName(result[i]) < targetDisplayName(result[j])
		}
		return left < right
	})
	resetTargetStatuses(result)
	return result
}

const unknownTargetStage = 4

// targetStageRank defines the only order in which environments may advance.
// New releases use the persisted numeric order. Name inference remains only
// for release snapshots created before explicit environment stages existed.
func targetStageRank(target ReleaseTarget) int {
	if target.SortOrder > 0 {
		return target.SortOrder
	}
	if stage := strings.ToLower(strings.TrimSpace(target.EnvironmentStage)); stage != "" {
		switch stage {
		case "dev":
			return 1
		case "uat":
			return 2
		case "pre":
			return 3
		case "prod":
			return 4
		}
	}
	environment := strings.ToLower(strings.TrimSpace(target.Environment))
	switch environment {
	case "dev", "develop", "development":
		return 1
	case "uat", "qa", "test", "testing":
		return 2
	case "pre", "preprod", "pre-production", "stage", "staging":
		return 3
	case "prod", "pro", "production":
		return 4
	}
	if environment != "" {
		return unknownTargetStage + 1
	}

	value := strings.ToLower(strings.Join([]string{target.ID, target.Name}, " "))
	switch {
	case strings.Contains(value, "开发") || strings.Contains(value, "dev"):
		return 1
	case strings.Contains(value, "测试") || strings.Contains(value, "uat") || strings.Contains(value, "qa"):
		return 2
	case strings.Contains(value, "预发布") || strings.Contains(value, "staging") || strings.Contains(value, "stage") || strings.Contains(value, "pre"):
		return 3
	case strings.Contains(value, "生产") || strings.Contains(value, "prod"):
		return 4
	default:
		return unknownTargetStage + 1
	}
}

func targetDisplayName(target ReleaseTarget) string {
	return firstNonEmpty(target.Name, target.Environment, target.ID)
}

func orderedTargetIndexes(targets []ReleaseTarget) []int {
	indexes := make([]int, len(targets))
	for index := range targets {
		indexes[index] = index
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		return targetStageRank(targets[indexes[i]]) < targetStageRank(targets[indexes[j]])
	})
	return indexes
}

func nextTargetIndex(targets []ReleaseTarget) int {
	for _, index := range orderedTargetIndexes(targets) {
		if targets[index].Status == TargetSucceeded {
			continue
		}
		return index
	}
	return -1
}

func ensureTargetIsNext(targets []ReleaseTarget, index int) error {
	nextIndex := nextTargetIndex(targets)
	if nextIndex < 0 {
		return fmt.Errorf("%w: all deployment targets have completed", ErrTargetNotReady)
	}
	if nextIndex != index {
		return targetNotReadyError(targets[nextIndex], targets[index])
	}
	return nil
}

func targetNotReadyError(blocking, requested ReleaseTarget) error {
	return fmt.Errorf("%w: 请先完成环境 %s，不能推进环境 %s", ErrTargetNotReady, targetDisplayName(blocking), targetDisplayName(requested))
}

func targetNotReadyErrorForTarget(target ReleaseTarget) error {
	return fmt.Errorf("%w: 环境 %s 尚未轮到发布", ErrTargetNotReady, targetDisplayName(target))
}

func resetTargetStatuses(targets []ReleaseTarget) {
	ordered := orderedTargetIndexes(targets)
	readyAssigned := false
	for _, index := range ordered {
		target := &targets[index]
		target.Status = TargetWaiting
		if !readyAssigned {
			target.Status = TargetPending
			readyAssigned = true
		}
		target.Progress = 0
		target.Stage = ""
		target.Message = ""
		target.Error = ""
		target.StartedAt = nil
		target.FinishedAt = nil
	}
}

func promoteNextTarget(targets []ReleaseTarget) int {
	index := nextTargetIndex(targets)
	if index >= 0 && targets[index].Status == TargetWaiting {
		targets[index].Status = TargetPending
	}
	return index
}

func findReleaseTarget(targets []ReleaseTarget, targetID string) int {
	targetID = strings.TrimSpace(targetID)
	for index := range targets {
		if targets[index].ID == targetID {
			return index
		}
	}
	return -1
}

func aggregateTargetProgress(targets []ReleaseTarget) int {
	if len(targets) == 0 {
		return 0
	}
	total := 0
	for _, target := range targets {
		total += target.Progress
	}
	return total / len(targets)
}
