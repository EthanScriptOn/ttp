package release

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
)

var (
	ErrProviderNotConfigured = errors.New("git provider is not configured")
	ErrInvalidRelease        = errors.New("invalid release")
	ErrReleaseNotFound       = errors.New("release not found")
	ErrInvalidStatus         = errors.New("invalid release status")
	ErrInvalidStatusFlow     = errors.New("invalid release status transition")
	ErrReleaseImmutable      = errors.New("release is immutable in its current status")
	ErrLastCommit            = errors.New("a release must contain at least one commit")
	ErrInvalidProgress       = errors.New("invalid release progress")
	ErrTargetRetry           = errors.New("target retry is not allowed")
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
	TargetRunning   TargetStatus = "running"
	TargetSucceeded TargetStatus = "succeeded"
	TargetFailed    TargetStatus = "failed"
	TargetCancelled TargetStatus = "cancelled"
)

// ExecutionLog is one raw line emitted by a release executor. The line is
// intentionally kept separate from the human-facing release status message.
type ExecutionLog struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Stream    string    `json:"stream"`
	Level     string    `json:"level"`
	Line      string    `json:"line"`
}

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
	Status     TargetStatus   `json:"status"`
	Progress   int            `json:"progress"`
	Stage      string         `json:"stage,omitempty"`
	Message    string         `json:"message,omitempty"`
	Error      string         `json:"error,omitempty"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	Logs       []ExecutionLog `json:"logs,omitempty"`
}

// Artifact is the immutable image produced for one execution of a release.
// A re-publish clears it and creates a fresh build of the same saved source
// revision; every environment within one execution reuses this digest.
type Artifact struct {
	Image     string    `json:"image"`
	Digest    string    `json:"digest"`
	CommitSHA string    `json:"commit_sha"`
	BuiltAt   time.Time `json:"built_at"`
}

type Release struct {
	ID           string          `json:"id"`
	SpaceID      string          `json:"space_id,omitempty"`
	ProjectID    string          `json:"project_id"`
	RepositoryID string          `json:"repository_id"`
	Branch       string          `json:"branch"`
	Name         string          `json:"name,omitempty"`
	Commits      []git.Commit    `json:"commits"`
	Targets      []ReleaseTarget `json:"targets,omitempty"`
	Artifact     *Artifact       `json:"artifact,omitempty"`
	Plan         RolloutPlan     `json:"plan"`
	Status       Status          `json:"status"`
	Progress     int             `json:"progress,omitempty"`
	Stage        string          `json:"stage,omitempty"`
	Message      string          `json:"message,omitempty"`
	Error        string          `json:"error,omitempty"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type CreateInput struct {
	SpaceID      string        `json:"space_id,omitempty"`
	ProjectID    string        `json:"project_id"`
	RepositoryID string        `json:"repository_id"`
	Branch       string        `json:"branch"`
	Name         string        `json:"name"`
	CommitSHAs   []string      `json:"commit_shas"`
	Strategy     Strategy      `json:"strategy"`
	Traffic      TrafficSplit  `json:"traffic"`
	Targets      []TargetInput `json:"targets,omitempty"`
}

type Service struct {
	mu         sync.RWMutex
	provider   git.Provider
	repository Repository
	now        func() time.Time
	nextID     uint64
	releases   map[string]Release
	duplicates map[string]string
}

// Repository persists the complete release snapshot. The service keeps a
// cache for the synchronous release API, while production startup loads this
// snapshot from durable storage so release state and executor logs survive a
// restart. Tests can omit the repository and use the volatile implementation.
type Repository interface {
	LoadReleases(ctx context.Context) ([]Release, error)
	SaveRelease(ctx context.Context, item Release) error
}

func NewService(provider git.Provider) *Service {
	return &Service{
		provider:   provider,
		now:        func() time.Time { return time.Now().UTC() },
		releases:   make(map[string]Release),
		duplicates: make(map[string]string),
	}
}

func NewPersistentService(ctx context.Context, provider git.Provider, repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("release repository is required")
	}
	service := NewService(provider)
	service.repository = repository
	items, err := repository.LoadReleases(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		service.releases[item.ID] = cloneRelease(item)
		service.duplicates[releaseDuplicateKey(item)] = item.ID
		if parsed, parseErr := strconv.ParseUint(strings.TrimPrefix(item.ID, "rel-"), 10, 64); parseErr == nil && parsed > service.nextID {
			service.nextID = parsed
		}
	}
	return service, nil
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
	// An explicit SHA identifies an immutable release snapshot. Check the
	// in-memory index before contacting the provider so a repeated request is
	// still idempotent when the remote Git service is temporarily unavailable.
	// The first request still resolves every SHA below, so this shortcut cannot
	// make an unvalidated commit publishable.
	if shas := normalizeSHAs(input.CommitSHAs); len(shas) > 0 {
		key := duplicateKey(input, commitsForSHAs(shas), plan)
		s.mu.RLock()
		if existingID, ok := s.duplicates[key]; ok {
			existing := cloneRelease(s.releases[existingID])
			s.mu.RUnlock()
			return existing, true, nil
		}
		s.mu.RUnlock()
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
	result := Release{ID: id, SpaceID: strings.TrimSpace(input.SpaceID), ProjectID: input.ProjectID, RepositoryID: input.RepositoryID, Branch: input.Branch, Name: strings.TrimSpace(input.Name), Commits: append([]git.Commit(nil), commits...), Targets: newReleaseTargets(input.Targets), Plan: plan, Status: StatusDraft, CreatedAt: now, UpdatedAt: now}
	if err := s.persist(ctx, result); err != nil {
		return Release{}, false, err
	}
	s.releases[id] = result
	s.duplicates[key] = id
	return cloneRelease(result), false, nil
}

func commitsForSHAs(shas []string) []git.Commit {
	commits := make([]git.Commit, 0, len(shas))
	for _, sha := range shas {
		commits = append(commits, git.Commit{SHA: sha})
	}
	return commits
}

func (s *Service) resolveCommits(ctx context.Context, input CreateInput) ([]git.Commit, error) {
	if s == nil || s.provider == nil {
		return nil, ErrProviderNotConfigured
	}
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

func (s *Service) persist(ctx context.Context, item Release) error {
	if s == nil || s.repository == nil {
		return nil
	}
	return s.repository.SaveRelease(ctx, cloneRelease(item))
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
	if release.Status != StatusDraft {
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
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	for key, releaseID := range s.duplicates {
		if releaseID == id {
			delete(s.duplicates, key)
		}
	}
	s.duplicates[releaseDuplicateKey(release)] = id
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
	if !canTransition(release.Status, target) {
		return Release{}, fmt.Errorf("%w: %s -> %s", ErrInvalidStatusFlow, release.Status, target)
	}
	previous := release.Status
	release.Status = target
	now := s.now().UTC()
	applyTransitionMetadata(&release, previous, target, now)
	release.UpdatedAt = now
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
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
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	return cloneRelease(release), nil
}

// SetArtifact records the image used by the active execution. The artifact is
// immutable for the duration of a run so every selected environment receives
// the exact same digest.
func (s *Service) SetArtifact(id string, artifact Artifact) (Release, error) {
	artifact.Image = strings.TrimSpace(artifact.Image)
	artifact.Digest = strings.ToLower(strings.TrimSpace(artifact.Digest))
	artifact.CommitSHA = strings.TrimSpace(artifact.CommitSHA)
	if artifact.Image == "" || artifact.Digest == "" || !strings.Contains(artifact.Image, "@"+artifact.Digest) || !strings.HasPrefix(artifact.Digest, "sha256:") || len(artifact.Digest) != len("sha256:")+64 || artifact.CommitSHA == "" {
		return Release{}, fmt.Errorf("%w: invalid release artifact", ErrInvalidRelease)
	}
	if artifact.BuiltAt.IsZero() {
		artifact.BuiltAt = s.now().UTC()
	} else {
		artifact.BuiltAt = artifact.BuiltAt.UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if item.Status != StatusQueued && item.Status != StatusRunning {
		return Release{}, ErrReleaseImmutable
	}
	if item.Artifact != nil {
		if item.Artifact.Image != artifact.Image || item.Artifact.Digest != artifact.Digest || item.Artifact.CommitSHA != artifact.CommitSHA {
			return Release{}, fmt.Errorf("%w: release artifact is already fixed", ErrReleaseImmutable)
		}
		return cloneRelease(item), nil
	}
	item.Artifact = &artifact
	item.UpdatedAt = s.now().UTC()
	if err := s.persist(context.Background(), item); err != nil {
		return Release{}, err
	}
	s.releases[id] = item
	return cloneRelease(item), nil
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
		if err := s.persist(context.Background(), release); err != nil {
			return Release{}, err
		}
		s.releases[id] = release
	}
	return cloneRelease(release), nil
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
	if target.Status != TargetPending && target.Status != TargetRunning {
		return Release{}, ErrReleaseImmutable
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
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	return cloneRelease(release), nil
}

// AppendTargetLog stores executor output without changing the target's
// lifecycle status. This lets a failed or cancelled run retain its final
// stderr line as well as the successful output before it.
func (s *Service) AppendTargetLog(id, targetID, source, stream, level, line string) (Release, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return Release{}, fmt.Errorf("%w: execution log line is required", ErrInvalidRelease)
	}
	if strings.TrimSpace(source) == "" {
		source = "executor"
	}
	if strings.TrimSpace(stream) == "" {
		stream = "stdout"
	}
	if strings.TrimSpace(level) == "" {
		level = "INFO"
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
	now := s.now().UTC()
	target.Logs = append(target.Logs, ExecutionLog{
		ID:        fmt.Sprintf("%s-%d", targetID, now.UnixNano()),
		Timestamp: now,
		Source:    strings.TrimSpace(source),
		Stream:    strings.TrimSpace(stream),
		Level:     strings.ToUpper(strings.TrimSpace(level)),
		Line:      line,
	})
	release.UpdatedAt = now
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	return cloneRelease(release), nil
}

func (s *Service) TargetLogs(id, targetID string) ([]ExecutionLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	release, ok := s.releases[id]
	if !ok {
		return nil, ErrReleaseNotFound
	}
	index := findReleaseTarget(release.Targets, targetID)
	if index < 0 {
		return nil, fmt.Errorf("%w: target %s", ErrInvalidRelease, targetID)
	}
	logs := release.Targets[index].Logs
	if logs == nil {
		return []ExecutionLog{}, nil
	}
	return append([]ExecutionLog{}, logs...), nil
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
	if target.Status != TargetRunning && target.Status != TargetPending {
		return Release{}, ErrReleaseImmutable
	}
	now := s.now().UTC()
	target.Status = TargetSucceeded
	target.Progress = 100
	target.Stage = "succeeded"
	target.Message = "环境发布完成"
	target.FinishedAt = &now
	release.Progress = aggregateTargetProgress(release.Targets)
	release.UpdatedAt = now
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
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
	target.Status = TargetFailed
	target.Progress = 0
	target.Stage = "failed"
	target.Message = "环境发布失败"
	target.Error = errorMessage
	target.FinishedAt = &now
	release.Stage = "failed"
	release.Message = "环境发布失败：" + target.Name
	release.UpdatedAt = now
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
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
		if target.Status != TargetPending && target.Status != TargetRunning {
			continue
		}
		target.Status = TargetCancelled
		target.Stage = "cancelled"
		target.Message = message
		target.FinishedAt = &now
	}
	release.UpdatedAt = now
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
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
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
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
		case TargetPending, TargetRunning:
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
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	return cloneRelease(release), nil
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
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
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
	for index := range release.Targets {
		target := &release.Targets[index]
		if target.Status == TargetPending || target.Status == TargetRunning {
			target.Status = TargetCancelled
			target.Stage = "cancelled"
			target.Message = "环境发布已取消"
			target.FinishedAt = timestamp(now)
		}
	}
	release.UpdatedAt = now
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	return cloneRelease(release), nil
}

func applyTransitionMetadata(release *Release, previous, target Status, now time.Time) {
	switch target {
	case StatusQueued:
		// Re-queueing a terminal release is the existing "再次发布" behavior.
		// Its immutable version data stays intact while execution metadata starts
		// a fresh run.
		if previous == StatusFailed || previous == StatusSucceeded || previous == StatusCancelled {
			release.Artifact = nil
			release.Progress = 0
			release.Stage = ""
			release.Message = ""
			release.Error = ""
			release.StartedAt = nil
			release.FinishedAt = nil
			for index := range release.Targets {
				release.Targets[index].Status = TargetPending
				release.Targets[index].Progress = 0
				release.Targets[index].Stage = ""
				release.Targets[index].Message = ""
				release.Targets[index].Error = ""
				release.Targets[index].StartedAt = nil
				release.Targets[index].FinishedAt = nil
			}
		}
	case StatusRunning:
		if release.StartedAt == nil {
			release.StartedAt = timestamp(now)
		}
		release.FinishedAt = nil
		release.Error = ""
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
	if release.Artifact != nil {
		artifact := *release.Artifact
		release.Artifact = &artifact
	}
	for index := range release.Targets {
		release.Targets[index].Logs = append([]ExecutionLog(nil), release.Targets[index].Logs...)
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
		return result[i].SortOrder < result[j].SortOrder
	})
	return result
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
