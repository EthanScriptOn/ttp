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
	ErrReleaseRemoved        = errors.New("release has been removed")
	ErrInvalidStatus         = errors.New("invalid release status")
	ErrInvalidStatusFlow     = errors.New("invalid release status transition")
	ErrReleaseImmutable      = errors.New("release is immutable in its current status")
	ErrLastCommit            = errors.New("a release must contain at least one commit")
	ErrInvalidProgress       = errors.New("invalid release progress")
	ErrTargetRetry           = errors.New("target retry is not allowed")
	ErrFlowNotFound          = errors.New("release flow not found")
	ErrFlowBaseBranch        = errors.New("the flow base branch cannot be removed")
	ErrFlowConflict          = errors.New("release flow changed while the replacement was being prepared")
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

// FlowParticipant is the immutable branch snapshot captured by a release
// flow. A flow is the logical set of branches that currently make up the
// deployable line; individual release records keep their own copy so history
// never changes when the flow changes later.
type FlowParticipant struct {
	Branch       string     `json:"branch"`
	Head         git.Commit `json:"head"`
	Active       bool       `json:"active"`
	AddedAt      time.Time  `json:"added_at"`
	RemovedAt    *time.Time `json:"removed_at,omitempty"`
	RemovedBy    uint64     `json:"removed_by,omitempty"`
	RemoveReason string     `json:"remove_reason,omitempty"`
}

// ReleaseFlow is the mutable aggregate that owns the current participant set.
// Releases reference a flow and capture FlowParticipants at creation time;
// they are never edited to reflect a later branch removal.
type ReleaseFlow struct {
	ID               string            `json:"id"`
	SpaceID          string            `json:"space_id,omitempty"`
	ProjectID        string            `json:"project_id"`
	RepositoryID     string            `json:"repository_id"`
	BaseBranch       string            `json:"base_branch"`
	Version          int               `json:"version"`
	CurrentReleaseID string            `json:"current_release_id,omitempty"`
	Participants     []FlowParticipant `json:"participants"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// FlowParticipantSnapshot is stored on a release and is intentionally
// separate from the mutable flow participant record.
type FlowParticipantSnapshot struct {
	Branch string     `json:"branch"`
	Head   git.Commit `json:"head"`
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
	ID            string `json:"id"`
	SpaceID       string `json:"space_id,omitempty"`
	ProjectID     string `json:"project_id"`
	RepositoryID  string `json:"repository_id"`
	CreatedBy     uint64 `json:"created_by,omitempty"`
	CreatedByName string `json:"created_by_name,omitempty"`
	// CreatedByUsername is kept for server-side filtering. The API continues
	// to expose the display name as the visible publisher label.
	CreatedByUsername   string                    `json:"-"`
	Branch              string                    `json:"branch"`
	Name                string                    `json:"name,omitempty"`
	Commits             []git.Commit              `json:"commits"`
	FlowID              string                    `json:"flow_id,omitempty"`
	FlowVersion         int                       `json:"flow_version,omitempty"`
	BaseBranch          string                    `json:"base_branch,omitempty"`
	Participants        []FlowParticipantSnapshot `json:"participants,omitempty"`
	FlowParticipants    []FlowParticipant         `json:"flow_participants,omitempty"`
	ParentReleaseID     string                    `json:"parent_release_id,omitempty"`
	ReplacesReleaseID   string                    `json:"replaces_release_id,omitempty"`
	ReplacedByReleaseID string                    `json:"replaced_by_release_id,omitempty"`
	ReplacedAt          *time.Time                `json:"replaced_at,omitempty"`
	ReplacementState    string                    `json:"replacement_state,omitempty"`
	RemovedBranch       string                    `json:"removed_branch,omitempty"`
	Targets             []ReleaseTarget           `json:"targets,omitempty"`
	Artifact            *Artifact                 `json:"artifact,omitempty"`
	Plan                RolloutPlan               `json:"plan"`
	Status              Status                    `json:"status"`
	Progress            int                       `json:"progress,omitempty"`
	Stage               string                    `json:"stage,omitempty"`
	Message             string                    `json:"message,omitempty"`
	Error               string                    `json:"error,omitempty"`
	StartedAt           *time.Time                `json:"started_at,omitempty"`
	FinishedAt          *time.Time                `json:"finished_at,omitempty"`
	Removed             bool                      `json:"removed,omitempty"`
	RemovedAt           *time.Time                `json:"removed_at,omitempty"`
	RemovedBy           uint64                    `json:"removed_by,omitempty"`
	RemoveReason        string                    `json:"remove_reason,omitempty"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
	// ForceNew is an internal persistence hint for an explicit replacement
	// release. It is intentionally omitted from API responses.
	ForceNew bool `json:"-"`
}

type CreateInput struct {
	SpaceID           string                    `json:"space_id,omitempty"`
	ProjectID         string                    `json:"project_id"`
	RepositoryID      string                    `json:"repository_id"`
	CreatedBy         uint64                    `json:"-"`
	CreatedByName     string                    `json:"-"`
	Branch            string                    `json:"branch"`
	Name              string                    `json:"name"`
	BaseBranch        string                    `json:"-"`
	FlowID            string                    `json:"-"`
	FlowVersion       int                       `json:"-"`
	FlowParticipants  []FlowParticipant         `json:"-"`
	Participants      []FlowParticipantSnapshot `json:"-"`
	ParentReleaseID   string                    `json:"-"`
	ReplacesReleaseID string                    `json:"-"`
	ReplacementState  string                    `json:"-"`
	RemovedBranch     string                    `json:"-"`
	CommitSHAs        []string                  `json:"commit_shas"`
	Strategy          Strategy                  `json:"strategy"`
	Traffic           TrafficSplit              `json:"traffic"`
	Targets           []TargetInput             `json:"targets,omitempty"`
	// ForceNew bypasses idempotent duplicate detection for an explicit
	// operator-requested rerun of the same branch revision.
	ForceNew bool `json:"-"`
}

type Service struct {
	mu            sync.RWMutex
	provider      git.Provider
	repository    Repository
	now           func() time.Time
	nextID        uint64
	nextFlowID    uint64
	releases      map[string]Release
	duplicates    map[string]string
	flows         map[string]ReleaseFlow
	flowByProject map[string]string
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
		provider:      provider,
		now:           func() time.Time { return time.Now().UTC() },
		releases:      make(map[string]Release),
		duplicates:    make(map[string]string),
		flows:         make(map[string]ReleaseFlow),
		flowByProject: make(map[string]string),
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
		service.restoreFlowFromRelease(item)
		if !item.Removed {
			service.duplicates[releaseDuplicateKey(item)] = item.ID
		}
		if parsed, parseErr := strconv.ParseUint(strings.TrimPrefix(item.ID, "rel-"), 10, 64); parseErr == nil && parsed > service.nextID {
			service.nextID = parsed
		}
	}
	// A process may stop after the replacement deployment succeeds but before
	// the completion callback commits the flow relation. Reconcile that narrow
	// window during startup so a successful immutable release cannot remain
	// permanently pending.
	for _, item := range items {
		if item.ReplacesReleaseID == "" || item.ReplacementState != "pending" || item.Status != StatusSucceeded {
			continue
		}
		_, _, _ = service.CommitBranchRemoval(item.ReplacesReleaseID, item.ID, item.CreatedBy)
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

// Flow returns the current mutable release-flow aggregate for a project.
// Historical release snapshots are not returned here; callers that need those
// should inspect the release record itself.
func (s *Service) Flow(projectID string) (ReleaseFlow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projectID = strings.TrimSpace(projectID)
	flowID := s.flowByProject[projectID]
	if flowID == "" {
		var latest *Release
		for _, candidate := range s.releases {
			if candidate.Removed || candidate.ProjectID != projectID {
				continue
			}
			if latest == nil || candidate.CreatedAt.After(latest.CreatedAt) {
				copy := candidate
				latest = &copy
			}
		}
		if latest == nil {
			return ReleaseFlow{}, ErrFlowNotFound
		}
		flow := legacyFlowForRelease(*latest, s.nowValue())
		if flow.ID == "" {
			flow.ID = "legacy-" + projectID
		}
		return cloneReleaseFlow(flow), nil
	}
	flow, ok := s.flows[flowID]
	if !ok {
		return ReleaseFlow{}, ErrFlowNotFound
	}
	return cloneReleaseFlow(flow), nil
}

// BranchRemoval describes a replacement that has not been committed yet. The
// current flow remains untouched until the replacement release succeeds.
type BranchRemoval struct {
	Original      Release
	Flow          ReleaseFlow
	RemovedBranch string
	BaseCommit    git.Commit
	Participants  []FlowParticipantSnapshot
}

// PrepareBranchRemoval stages removal of the release's branch from the flow.
// It does not mutate the flow or the old release, which means a failed
// replacement leaves the running environment and current branch set intact.
func (s *Service) PrepareBranchRemoval(id string) (BranchRemoval, error) {
	s.mu.RLock()
	item, ok := s.releases[strings.TrimSpace(id)]
	if !ok {
		s.mu.RUnlock()
		return BranchRemoval{}, ErrReleaseNotFound
	}
	item = cloneRelease(item)
	flowID := strings.TrimSpace(item.FlowID)
	if flowID == "" {
		flowID = s.flowByProject[item.ProjectID]
	}
	flow, flowOK := s.flows[flowID]
	if !flowOK {
		flow = legacyFlowForRelease(item, s.nowValue())
		if flow.ID == "" {
			flow.ID = flowID
		}
	}
	flow = cloneReleaseFlow(flow)
	s.mu.RUnlock()

	if item.Removed {
		return BranchRemoval{}, ErrReleaseRemoved
	}
	branch := strings.TrimSpace(item.Branch)
	if branch == "" {
		return BranchRemoval{}, fmt.Errorf("%w: release branch is required", ErrInvalidRelease)
	}
	if strings.EqualFold(branch, strings.TrimSpace(flow.BaseBranch)) {
		return BranchRemoval{}, ErrFlowBaseBranch
	}
	removed := false
	for index := range flow.Participants {
		participant := &flow.Participants[index]
		if strings.EqualFold(participant.Branch, branch) && participant.Active {
			participant.Active = false
			removed = true
			continue
		}
	}
	if !removed {
		return BranchRemoval{}, fmt.Errorf("%w: branch %s is not in the current flow", ErrFlowConflict, branch)
	}
	if flow.Version <= 0 {
		flow.Version = 1
	}
	flow.Version++
	flow.CurrentReleaseID = ""
	now := s.nowValue()
	flow.UpdatedAt = now
	for index := range flow.Participants {
		participant := &flow.Participants[index]
		if !participant.Active && participant.RemovedAt == nil && strings.EqualFold(participant.Branch, branch) {
			participant.RemovedAt = timestamp(now)
			participant.RemoveReason = "从当前发布流程移出"
		}
	}
	participants := make([]FlowParticipantSnapshot, 0, len(flow.Participants))
	var baseCommit git.Commit
	for _, participant := range flow.Participants {
		if !participant.Active {
			continue
		}
		snapshot := FlowParticipantSnapshot{Branch: participant.Branch, Head: participant.Head}
		participants = append(participants, snapshot)
		if strings.EqualFold(participant.Branch, flow.BaseBranch) {
			baseCommit = participant.Head
		}
	}
	if strings.TrimSpace(baseCommit.SHA) == "" && len(participants) > 0 {
		baseCommit = participants[0].Head
	}
	return BranchRemoval{Original: item, Flow: flow, RemovedBranch: branch, BaseCommit: baseCommit, Participants: participants}, nil
}

// CommitBranchRemoval atomically makes a successful replacement the current
// flow and annotates the old immutable release with the replacement relation.
func (s *Service) CommitBranchRemoval(oldID, newID string, removedBy uint64) (Release, Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.releases[strings.TrimSpace(oldID)]
	if !ok {
		return Release{}, Release{}, ErrReleaseNotFound
	}
	next, ok := s.releases[strings.TrimSpace(newID)]
	if !ok {
		return Release{}, Release{}, ErrReleaseNotFound
	}
	if next.Status != StatusSucceeded {
		return Release{}, Release{}, fmt.Errorf("%w: replacement release must succeed before committing the flow", ErrInvalidStatusFlow)
	}
	if old.FlowID == "" {
		old.FlowID = next.FlowID
	}
	if old.FlowID == "" || next.FlowID == "" || old.FlowID != next.FlowID {
		return Release{}, Release{}, ErrFlowConflict
	}
	flow, ok := s.flows[old.FlowID]
	if !ok {
		flow = legacyFlowForRelease(old, s.now().UTC())
		flow.ID = next.FlowID
	}
	if next.FlowVersion <= flow.Version {
		return Release{}, Release{}, ErrFlowConflict
	}
	now := s.now().UTC()
	old.ReplacedByReleaseID = next.ID
	old.ReplacedAt = timestamp(now)
	old.UpdatedAt = now
	old.ReplacementState = "replaced"
	next.ReplacementState = "committed"
	next.UpdatedAt = now
	flow = cloneReleaseFlowFromRelease(next)
	flow.CurrentReleaseID = next.ID
	flow.UpdatedAt = now
	if err := s.persist(context.Background(), old); err != nil {
		return Release{}, Release{}, err
	}
	if err := s.persist(context.Background(), next); err != nil {
		return Release{}, Release{}, err
	}
	s.releases[old.ID] = old
	s.releases[next.ID] = next
	for key, releaseID := range s.duplicates {
		if releaseID == old.ID {
			delete(s.duplicates, key)
		}
	}
	s.flows[flow.ID] = flow
	s.flowByProject[flow.ProjectID] = flow.ID
	return cloneRelease(old), cloneRelease(next), nil
}

// RejectBranchRemoval leaves the current flow untouched and records that the
// replacement release failed to change it.
func (s *Service) RejectBranchRemoval(newID string) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.releases[strings.TrimSpace(newID)]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	item.ReplacementState = "rejected"
	item.UpdatedAt = s.now().UTC()
	if err := s.persist(context.Background(), item); err != nil {
		return Release{}, err
	}
	s.releases[item.ID] = item
	return cloneRelease(item), nil
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
	if !input.ForceNew {
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
	}
	commits, err := s.resolveCommits(ctx, input)
	if err != nil {
		return Release{}, false, err
	}
	flow, participants, flowErr := s.prepareFlowSnapshot(ctx, input, commits)
	if flowErr != nil {
		return Release{}, false, flowErr
	}
	key := duplicateKey(input, commits, plan)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !input.ForceNew {
		if existingID, ok := s.duplicates[key]; ok {
			return cloneRelease(s.releases[existingID]), true, nil
		}
	}
	now := s.now().UTC()
	s.nextID++
	id := fmt.Sprintf("rel-%06d", s.nextID)
	if strings.TrimSpace(flow.ID) == "" {
		s.nextFlowID++
		flow.ID = fmt.Sprintf("flow-%06d", s.nextFlowID)
	}
	flow.SpaceID = strings.TrimSpace(input.SpaceID)
	flow.ProjectID = strings.TrimSpace(input.ProjectID)
	flow.RepositoryID = strings.TrimSpace(input.RepositoryID)
	flow.UpdatedAt = now
	if flow.CreatedAt.IsZero() {
		flow.CreatedAt = now
	}
	result := Release{
		ID: id, SpaceID: strings.TrimSpace(input.SpaceID), ProjectID: input.ProjectID, RepositoryID: input.RepositoryID,
		CreatedBy: input.CreatedBy, CreatedByName: strings.TrimSpace(input.CreatedByName), Branch: input.Branch,
		BaseBranch: flow.BaseBranch, Name: strings.TrimSpace(input.Name), Commits: append([]git.Commit(nil), commits...),
		FlowID: flow.ID, FlowVersion: flow.Version, Participants: append([]FlowParticipantSnapshot(nil), participants...),
		FlowParticipants: cloneFlowParticipants(flow.Participants), ParentReleaseID: input.ParentReleaseID,
		ReplacesReleaseID: input.ReplacesReleaseID, ReplacementState: strings.TrimSpace(input.ReplacementState), RemovedBranch: strings.TrimSpace(input.RemovedBranch),
		Targets: newReleaseTargets(input.Targets), Plan: plan, Status: StatusDraft, CreatedAt: now, UpdatedAt: now, ForceNew: input.ForceNew,
	}
	if result.ReplacementState == "" && result.ReplacesReleaseID != "" {
		result.ReplacementState = "pending"
	}
	if err := s.persist(ctx, result); err != nil {
		return Release{}, false, err
	}
	s.releases[id] = result
	// A replacement release carries a candidate flow snapshot, but it must not
	// become the current flow until its deployment succeeds. This keeps a
	// failed removal from silently changing the active branch set.
	if input.ReplacementState != "pending" {
		flow.CurrentReleaseID = id
		s.flows[flow.ID] = cloneReleaseFlow(flow)
		s.flowByProject[flow.ProjectID] = flow.ID
	}
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

func (s *Service) prepareFlowSnapshot(ctx context.Context, input CreateInput, selected []git.Commit) (ReleaseFlow, []FlowParticipantSnapshot, error) {
	baseBranch := strings.TrimSpace(input.BaseBranch)
	if baseBranch == "" {
		baseBranch = strings.TrimSpace(input.Branch)
	}
	s.mu.RLock()
	flowID := strings.TrimSpace(input.FlowID)
	if flowID == "" {
		flowID = s.flowByProject[strings.TrimSpace(input.ProjectID)]
	}
	current, exists := s.flows[flowID]
	if exists {
		current = cloneReleaseFlow(current)
	}
	s.mu.RUnlock()

	if len(input.FlowParticipants) > 0 {
		if !exists && flowID != "" {
			return ReleaseFlow{}, nil, ErrFlowConflict
		}
		if current.ID != "" && input.FlowVersion > 0 && current.Version >= input.FlowVersion && input.ReplacementState == "pending" {
			return ReleaseFlow{}, nil, ErrFlowConflict
		}
		if current.ID == "" {
			current.ID = flowID
		}
		current.BaseBranch = baseBranch
		current.Version = input.FlowVersion
		if current.Version <= 0 {
			current.Version = 1
		}
		current.Participants = cloneFlowParticipants(input.FlowParticipants)
		return current, activeParticipantSnapshots(current.Participants), nil
	}

	if !exists {
		current = ReleaseFlow{ID: flowID, BaseBranch: baseBranch, Version: 0, Participants: []FlowParticipant{}}
	} else if current.BaseBranch == "" {
		current.BaseBranch = baseBranch
	}
	if current.BaseBranch == "" {
		current.BaseBranch = baseBranch
	}
	byBranch := make(map[string]FlowParticipant, len(current.Participants)+2)
	order := make([]string, 0, len(current.Participants)+2)
	for _, participant := range current.Participants {
		branch := strings.TrimSpace(participant.Branch)
		if branch == "" || !participant.Active {
			if branch != "" {
				byBranch[strings.ToLower(branch)] = participant
				order = append(order, branch)
			}
			continue
		}
		byBranch[strings.ToLower(branch)] = participant
		order = append(order, branch)
	}
	addBranch := func(branch string) {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			return
		}
		key := strings.ToLower(branch)
		if _, ok := byBranch[key]; !ok {
			order = append(order, branch)
			byBranch[key] = FlowParticipant{Branch: branch}
		} else {
			participant := byBranch[key]
			participant.Branch = branch
			participant.Active = true
			participant.RemovedAt = nil
			participant.RemovedBy = 0
			participant.RemoveReason = ""
			byBranch[key] = participant
		}
	}
	addBranch(current.BaseBranch)
	addBranch(input.Branch)

	selectedSHA := ""
	if len(selected) > 0 {
		selectedSHA = strings.ToLower(strings.TrimSpace(selected[0].SHA))
	}
	for _, branch := range order {
		key := strings.ToLower(branch)
		participant := byBranch[key]
		participant.Branch = branch
		participant.Active = true
		var head git.Commit
		if key == strings.ToLower(strings.TrimSpace(input.Branch)) && selectedSHA != "" {
			head = selected[0]
		} else if s.provider != nil {
			commits, err := s.provider.ListCommits(ctx, input.RepositoryID, branch, 1)
			if err != nil {
				return ReleaseFlow{}, nil, err
			}
			if len(commits) > 0 {
				head = commits[0]
			}
		}
		if strings.TrimSpace(head.SHA) == "" {
			return ReleaseFlow{}, nil, fmt.Errorf("%w: branch %s has no commits", ErrInvalidRelease, branch)
		}
		participant.Head = head
		if participant.AddedAt.IsZero() {
			participant.AddedAt = s.nowValue()
		}
		byBranch[key] = participant
	}
	participants := make([]FlowParticipant, 0, len(order))
	for _, branch := range order {
		participants = append(participants, byBranch[strings.ToLower(branch)])
	}
	current.Participants = participants
	current.Version++
	if current.Version <= 0 {
		current.Version = 1
	}
	current.BaseBranch = baseBranch
	return current, activeParticipantSnapshots(participants), nil
}

func activeParticipantSnapshots(participants []FlowParticipant) []FlowParticipantSnapshot {
	result := make([]FlowParticipantSnapshot, 0, len(participants))
	for _, participant := range participants {
		if !participant.Active || strings.TrimSpace(participant.Branch) == "" || strings.TrimSpace(participant.Head.SHA) == "" {
			continue
		}
		result = append(result, FlowParticipantSnapshot{Branch: participant.Branch, Head: participant.Head})
	}
	return result
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
		if release.Removed {
			continue
		}
		if projectID == "" || release.ProjectID == projectID {
			result = append(result, cloneRelease(release))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

// Remove archives a release from the default list without deleting its
// immutable source snapshot, target history, execution logs, or deployed
// resources. Active executions must be cancelled first so a list operation
// cannot hide an in-flight deployment from operators.
func (s *Service) Remove(id string, removedBy uint64) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if item.Removed {
		return cloneRelease(item), nil
	}
	if item.Status == StatusQueued || item.Status == StatusRunning {
		return Release{}, fmt.Errorf("%w: running releases must be cancelled before removal", ErrInvalidStatusFlow)
	}
	now := s.now().UTC()
	item.Removed = true
	item.RemovedAt = timestamp(now)
	item.RemovedBy = removedBy
	item.RemoveReason = "从发布单列表移除"
	item.UpdatedAt = now
	if err := s.persist(context.Background(), item); err != nil {
		return Release{}, err
	}
	s.releases[id] = item
	// Draft creation captures a candidate flow immediately so the draft can be
	// reviewed in the current-flow view. Removing that draft must roll the
	// mutable flow back to the previous immutable snapshot; otherwise the
	// removed branch would remain visible without a corresponding release.
	if item.Status == StatusDraft && item.FlowID != "" {
		if current, exists := s.flows[item.FlowID]; exists && current.CurrentReleaseID == item.ID {
			var previous *Release
			for candidateID, candidate := range s.releases {
				if candidateID == item.ID || candidate.Removed || candidate.FlowID != item.FlowID || candidate.ReplacementState == "pending" {
					continue
				}
				if candidate.FlowVersion > item.FlowVersion || (candidate.FlowVersion == item.FlowVersion && !candidate.CreatedAt.Before(item.CreatedAt)) {
					continue
				}
				if previous == nil || candidate.FlowVersion > previous.FlowVersion || (candidate.FlowVersion == previous.FlowVersion && candidate.CreatedAt.After(previous.CreatedAt)) {
					copy := candidate
					previous = &copy
				}
			}
			if previous == nil {
				delete(s.flows, item.FlowID)
				if s.flowByProject[item.ProjectID] == item.FlowID {
					delete(s.flowByProject, item.ProjectID)
				}
			} else {
				flow := cloneReleaseFlowFromRelease(*previous)
				flow.UpdatedAt = now
				s.flows[item.FlowID] = flow
				s.flowByProject[item.ProjectID] = item.FlowID
			}
		}
	}
	for key, releaseID := range s.duplicates {
		if releaseID == id {
			delete(s.duplicates, key)
		}
	}
	return cloneRelease(item), nil
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
	if release.Removed {
		return Release{}, ErrReleaseRemoved
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
	if release.Removed {
		return Release{}, ErrReleaseRemoved
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
	if item.Removed {
		return Release{}, ErrReleaseRemoved
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
	if release.Removed {
		return Release{}, ErrReleaseRemoved
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

// UpdateTraffic changes the live rollout split without changing the captured
// source revision or release targets. Traffic is operational state, so it is
// allowed while a release is running or after it has completed.
func (s *Service) UpdateTraffic(id string, traffic TrafficSplit) (Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, ok := s.releases[id]
	if !ok {
		return Release{}, ErrReleaseNotFound
	}
	if release.Removed {
		return Release{}, ErrReleaseRemoved
	}
	if release.Status != StatusRunning && release.Status != StatusSucceeded {
		return Release{}, fmt.Errorf("%w: traffic can only be adjusted while a release is running or completed", ErrInvalidStatusFlow)
	}
	plan, err := normalizePlan(release.Plan.Strategy, traffic)
	if err != nil {
		return Release{}, err
	}
	previousKey := releaseDuplicateKey(release)
	release.Plan = plan
	release.UpdatedAt = s.now().UTC()
	if err := s.persist(context.Background(), release); err != nil {
		return Release{}, err
	}
	s.releases[id] = release
	delete(s.duplicates, previousKey)
	s.duplicates[releaseDuplicateKey(release)] = id
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
	if release.Removed {
		return Release{}, ErrReleaseRemoved
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
	target := release.Targets[index]
	logs := target.Logs
	if logs == nil {
		return []ExecutionLog{}, nil
	}
	// Logs are retained on the release snapshot for audit history. A repeat
	// publish resets the target execution timestamps but intentionally keeps
	// those historical lines, so the detail view must only expose the current
	// run. While a new run is queued/running before its first progress update,
	// StartedAt is still nil; return no lines instead of leaking the previous
	// run into the new detail view.
	if target.StartedAt == nil {
		if release.Status == StatusQueued || release.Status == StatusRunning {
			return []ExecutionLog{}, nil
		}
		return append([]ExecutionLog{}, logs...), nil
	}
	current := make([]ExecutionLog, 0, len(logs))
	for _, log := range logs {
		if !log.Timestamp.Before(*target.StartedAt) {
			current = append(current, log)
		}
	}
	return current, nil
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
	release.Participants = append([]FlowParticipantSnapshot(nil), release.Participants...)
	release.FlowParticipants = cloneFlowParticipants(release.FlowParticipants)
	release.Targets = append([]ReleaseTarget(nil), release.Targets...)
	if release.StartedAt != nil {
		startedAt := *release.StartedAt
		release.StartedAt = &startedAt
	}
	if release.FinishedAt != nil {
		finishedAt := *release.FinishedAt
		release.FinishedAt = &finishedAt
	}
	if release.RemovedAt != nil {
		removedAt := *release.RemovedAt
		release.RemovedAt = &removedAt
	}
	if release.ReplacedAt != nil {
		replacedAt := *release.ReplacedAt
		release.ReplacedAt = &replacedAt
	}
	for index := range release.Participants {
		release.Participants[index].Head = cloneCommit(release.Participants[index].Head)
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

func cloneCommit(commit git.Commit) git.Commit { return commit }

func cloneFlowParticipants(participants []FlowParticipant) []FlowParticipant {
	result := append([]FlowParticipant(nil), participants...)
	for index := range result {
		result[index].Head = cloneCommit(result[index].Head)
		if result[index].RemovedAt != nil {
			removedAt := *result[index].RemovedAt
			result[index].RemovedAt = &removedAt
		}
	}
	return result
}

func cloneReleaseFlow(flow ReleaseFlow) ReleaseFlow {
	flow.Participants = cloneFlowParticipants(flow.Participants)
	return flow
}

func cloneReleaseFlowFromRelease(item Release) ReleaseFlow {
	return ReleaseFlow{ID: item.FlowID, SpaceID: item.SpaceID, ProjectID: item.ProjectID, RepositoryID: item.RepositoryID, BaseBranch: item.BaseBranch, Version: item.FlowVersion, CurrentReleaseID: item.ID, Participants: cloneFlowParticipants(item.FlowParticipants), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func legacyFlowForRelease(item Release, now time.Time) ReleaseFlow {
	baseBranch := strings.TrimSpace(item.BaseBranch)
	if baseBranch == "" {
		baseBranch = strings.TrimSpace(item.Branch)
	}
	participants := make([]FlowParticipant, 0, len(item.Participants))
	for _, snapshot := range item.Participants {
		participants = append(participants, FlowParticipant{Branch: snapshot.Branch, Head: snapshot.Head, Active: true, AddedAt: item.CreatedAt})
	}
	if len(participants) == 0 && item.Branch != "" && len(item.Commits) > 0 {
		participants = append(participants, FlowParticipant{Branch: item.Branch, Head: item.Commits[0], Active: true, AddedAt: item.CreatedAt})
	}
	return ReleaseFlow{ID: item.FlowID, SpaceID: item.SpaceID, ProjectID: item.ProjectID, RepositoryID: item.RepositoryID, BaseBranch: baseBranch, Version: maxInt(item.FlowVersion, 1), CurrentReleaseID: item.ID, Participants: participants, CreatedAt: item.CreatedAt, UpdatedAt: now}
}

func (s *Service) restoreFlowFromRelease(item Release) {
	if item.Removed || strings.TrimSpace(item.FlowID) == "" || strings.TrimSpace(item.ProjectID) == "" {
		return
	}
	if parsed, err := strconv.ParseUint(strings.TrimPrefix(item.FlowID, "flow-"), 10, 64); err == nil && parsed > s.nextFlowID {
		s.nextFlowID = parsed
	}
	current, exists := s.flows[item.FlowID]
	if exists && current.Version > item.FlowVersion {
		return
	}
	if item.ReplacesReleaseID != "" && item.ReplacementState != "committed" {
		return
	}
	flow := cloneReleaseFlowFromRelease(item)
	if flow.Version <= 0 {
		flow.Version = 1
	}
	s.flows[item.FlowID] = flow
	s.flowByProject[item.ProjectID] = item.FlowID
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
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
