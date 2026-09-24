package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
)

type releaseRecordRow struct {
	ID                  string     `gorm:"size:64;primaryKey"`
	SpaceID             string     `gorm:"size:64;index;not null"`
	ProjectID           string     `gorm:"size:64;index;not null"`
	ReleaseName         string     `gorm:"column:release_name;size:120;not null;default:''"`
	SourceRepositoryID  string     `gorm:"column:source_repository_id;size:255;not null"`
	SourceBranch        string     `gorm:"column:source_branch;size:120;not null"`
	FlowID              string     `gorm:"column:flow_id;size:64;not null;default:''"`
	FlowVersion         int        `gorm:"column:flow_version;not null;default:0"`
	BaseBranch          string     `gorm:"column:base_branch;size:120;not null;default:''"`
	FlowParticipants    string     `gorm:"column:flow_participants;type:json"`
	ParentReleaseID     string     `gorm:"column:parent_release_id;size:64;not null;default:''"`
	ReplacesReleaseID   string     `gorm:"column:replaces_release_id;size:64;not null;default:''"`
	ReplacedByReleaseID string     `gorm:"column:replaced_by_release_id;size:64;not null;default:''"`
	ReplacedAt          *time.Time `gorm:"column:replaced_at"`
	ReplacementState    string     `gorm:"column:replacement_state;size:16;not null;default:''"`
	RemovedBranch       string     `gorm:"column:removed_branch;size:120;not null;default:''"`
	ReleaseFingerprint  string     `gorm:"size:64;not null"`
	Strategy            string     `gorm:"size:32;not null;default:rolling"`
	// These values are normalized by the release service for every strategy.
	// Do not add GORM defaults here: zero is meaningful for blue/green plans.
	StablePercent    int        `gorm:"not null"`
	CandidatePercent int        `gorm:"not null"`
	BluePercent      int        `gorm:"not null"`
	GreenPercent     int        `gorm:"not null"`
	Status           string     `gorm:"size:32;not null;default:draft"`
	Progress         int        `gorm:"not null;default:0"`
	Stage            string     `gorm:"size:80;not null;default:''"`
	Message          string     `gorm:"type:text"`
	Error            string     `gorm:"type:text"`
	ImageRef         string     `gorm:"column:image_ref;size:700;not null;default:''"`
	ImageDigest      string     `gorm:"column:image_digest;size:80;not null;default:''"`
	ImageCommitSHA   string     `gorm:"column:image_commit_sha;size:128;not null;default:''"`
	ImageBuiltAt     *time.Time `gorm:"column:image_built_at"`
	CreatedBy        *uint64    `gorm:"index"`
	StartedAt        *time.Time
	FinishedAt       *time.Time
	PublishedAt      *time.Time
	IsRemoved        bool       `gorm:"column:is_removed;not null;default:false;index"`
	RemovedAt        *time.Time `gorm:"column:removed_at"`
	RemovedBy        *uint64    `gorm:"column:removed_by"`
	RemoveReason     string     `gorm:"column:remove_reason;size:255;not null;default:''"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (releaseRecordRow) TableName() string { return "project_releases" }

type releaseCommitRecordRow struct {
	SpaceID        string `gorm:"size:64;index;not null"`
	ReleaseID      string `gorm:"size:64;primaryKey"`
	CommitSHA      string `gorm:"column:commit_sha;size:128;primaryKey"`
	CommitPosition int    `gorm:"column:commit_position;not null"`
	IsRemoved      bool   `gorm:"not null;default:false;index"`
	RemovedAt      *time.Time
	RemovedBy      *uint64
	RemoveReason   string     `gorm:"size:255;not null;default:''"`
	ShortSHA       string     `gorm:"size:32;not null;default:''"`
	CommitMessage  string     `gorm:"column:commit_message;type:text"`
	Author         string     `gorm:"size:255;not null;default:''"`
	AuthoredAt     *time.Time `gorm:"column:authored_at"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (releaseCommitRecordRow) TableName() string { return "release_commits" }

type releaseTargetRecordRow struct {
	SpaceID          string `gorm:"size:64;index;not null"`
	ReleaseID        string `gorm:"size:64;primaryKey"`
	TargetID         string `gorm:"column:target_id;size:64;primaryKey"`
	Name             string `gorm:"size:120;not null"`
	Environment      string `gorm:"size:64;not null"`
	EnvironmentStage string `gorm:"column:environment_stage;size:16;not null;default:custom"`
	SortOrder        int    `gorm:"not null;default:1"`
	ClusterID        string `gorm:"size:64;not null"`
	Namespace        string `gorm:"size:120;not null"`
	Replicas         int    `gorm:"not null;default:1"`
	ContainerPort    int    `gorm:"not null;default:8080"`
	DeployStrategy   string `gorm:"size:32;not null;default:rolling"`
	Status           string `gorm:"size:32;not null;default:pending"`
	Progress         int    `gorm:"not null;default:0"`
	Stage            string `gorm:"size:80;not null;default:''"`
	Message          string `gorm:"type:text"`
	Error            string `gorm:"type:text"`
	StartedAt        *time.Time
	FinishedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (releaseTargetRecordRow) TableName() string { return "release_runtime_targets" }

type releaseExecutionLogRecordRow struct {
	ID        string    `gorm:"size:128;primaryKey"`
	SpaceID   string    `gorm:"size:64;index;not null"`
	ReleaseID string    `gorm:"size:64;index;not null"`
	TargetID  string    `gorm:"size:64;index;not null"`
	Timestamp time.Time `gorm:"column:logged_at;index;not null"`
	Source    string    `gorm:"size:32;not null"`
	Stream    string    `gorm:"size:32;not null"`
	Level     string    `gorm:"size:16;not null"`
	Line      string    `gorm:"type:text;not null"`
}

func (releaseExecutionLogRecordRow) TableName() string { return "release_execution_logs" }

func (s *MySQL) LoadReleases(ctx context.Context) ([]release.Release, error) {
	var rows []releaseRecordRow
	if err := s.db.WithContext(ctx).Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]release.Release, 0, len(rows))
	for _, row := range rows {
		item, err := s.loadRelease(ctx, row)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *MySQL) loadRelease(ctx context.Context, row releaseRecordRow) (release.Release, error) {
	item := release.Release{
		ID: row.ID, SpaceID: row.SpaceID, ProjectID: row.ProjectID, RepositoryID: row.SourceRepositoryID,
		Branch: row.SourceBranch, Name: row.ReleaseName, FlowID: row.FlowID, FlowVersion: row.FlowVersion, BaseBranch: row.BaseBranch,
		ParentReleaseID: row.ParentReleaseID, ReplacesReleaseID: row.ReplacesReleaseID, ReplacedByReleaseID: row.ReplacedByReleaseID,
		ReplacedAt: cloneTimePtr(row.ReplacedAt), ReplacementState: row.ReplacementState, RemovedBranch: row.RemovedBranch,
		Plan: release.RolloutPlan{Strategy: release.Strategy(row.Strategy), Traffic: release.TrafficSplit{
			StablePercent: row.StablePercent, CandidatePercent: row.CandidatePercent,
			BluePercent: row.BluePercent, GreenPercent: row.GreenPercent,
		}},
		Status: release.Status(row.Status), Progress: row.Progress, Stage: row.Stage,
		Message: row.Message, Error: row.Error, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		StartedAt: cloneTimePtr(row.StartedAt), FinishedAt: cloneTimePtr(row.FinishedAt),
		Removed: row.IsRemoved, RemovedAt: cloneTimePtr(row.RemovedAt), RemoveReason: row.RemoveReason,
	}
	if strings.TrimSpace(row.FlowParticipants) != "" {
		if err := json.Unmarshal([]byte(row.FlowParticipants), &item.FlowParticipants); err != nil {
			return release.Release{}, fmt.Errorf("decode release flow participants: %w", err)
		}
		for _, participant := range item.FlowParticipants {
			if !participant.Active || strings.TrimSpace(participant.Branch) == "" || strings.TrimSpace(participant.Head.SHA) == "" {
				continue
			}
			item.Participants = append(item.Participants, release.FlowParticipantSnapshot{Branch: participant.Branch, Head: participant.Head})
		}
	}
	if row.CreatedBy != nil {
		item.CreatedBy = *row.CreatedBy
		var creator userRow
		if err := s.db.WithContext(ctx).First(&creator, item.CreatedBy).Error; err == nil {
			item.CreatedByName = strings.TrimSpace(creator.DisplayName)
			if item.CreatedByName == "" {
				item.CreatedByName = strings.TrimSpace(creator.Username)
			}
		}
	}
	if row.RemovedBy != nil {
		item.RemovedBy = *row.RemovedBy
	}
	if strings.TrimSpace(row.ImageRef) != "" && strings.TrimSpace(row.ImageDigest) != "" && strings.TrimSpace(row.ImageCommitSHA) != "" {
		builtAt := row.UpdatedAt.UTC()
		if row.ImageBuiltAt != nil {
			builtAt = row.ImageBuiltAt.UTC()
		}
		item.Artifact = &release.Artifact{Image: row.ImageRef, Digest: row.ImageDigest, CommitSHA: row.ImageCommitSHA, BuiltAt: builtAt}
	}

	var commitRows []releaseCommitRecordRow
	if err := s.db.WithContext(ctx).Where("release_id = ? AND is_removed = ?", row.ID, false).Order("commit_position ASC").Find(&commitRows).Error; err != nil {
		return release.Release{}, err
	}
	item.Commits = make([]git.Commit, 0, len(commitRows))
	for _, commitRow := range commitRows {
		shortSHA := commitRow.ShortSHA
		if shortSHA == "" {
			shortSHA = commitRow.CommitSHA
			if len(shortSHA) > 10 {
				shortSHA = shortSHA[:10]
			}
		}
		commit := git.Commit{SHA: commitRow.CommitSHA, ShortSHA: shortSHA, Message: commitRow.CommitMessage, Author: commitRow.Author}
		if commitRow.AuthoredAt != nil {
			commit.AuthoredAt = commitRow.AuthoredAt.UTC()
		}
		item.Commits = append(item.Commits, commit)
	}

	var targetRows []releaseTargetRecordRow
	if err := s.db.WithContext(ctx).Where("release_id = ?", row.ID).Order("sort_order ASC, target_id ASC").Find(&targetRows).Error; err != nil {
		return release.Release{}, err
	}
	item.Targets = make([]release.ReleaseTarget, 0, len(targetRows))
	for _, targetRow := range targetRows {
		var logRows []releaseExecutionLogRecordRow
		if err := s.db.WithContext(ctx).Where("release_id = ? AND target_id = ?", row.ID, targetRow.TargetID).Order("logged_at ASC, id ASC").Find(&logRows).Error; err != nil {
			return release.Release{}, err
		}
		logs := make([]release.ExecutionLog, 0, len(logRows))
		for _, logRow := range logRows {
			logs = append(logs, release.ExecutionLog{ID: logRow.ID, Timestamp: logRow.Timestamp.UTC(), Source: logRow.Source, Stream: logRow.Stream, Level: logRow.Level, Line: logRow.Line})
		}
		item.Targets = append(item.Targets, release.ReleaseTarget{
			TargetInput: release.TargetInput{ID: targetRow.TargetID, Name: targetRow.Name, Environment: targetRow.Environment, EnvironmentStage: targetRow.EnvironmentStage, SortOrder: targetRow.SortOrder, ClusterID: targetRow.ClusterID, Namespace: targetRow.Namespace, Replicas: targetRow.Replicas, ContainerPort: targetRow.ContainerPort, DeployStrategy: targetRow.DeployStrategy},
			Status:      release.TargetStatus(targetRow.Status), Progress: targetRow.Progress, Stage: targetRow.Stage, Message: targetRow.Message, Error: targetRow.Error,
			StartedAt: cloneTimePtr(targetRow.StartedAt), FinishedAt: cloneTimePtr(targetRow.FinishedAt), Logs: logs,
		})
	}
	return item, nil
}

func (s *MySQL) SaveRelease(ctx context.Context, item release.Release) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.SpaceID) == "" || strings.TrimSpace(item.ProjectID) == "" {
		return fmt.Errorf("release persistence requires id, space_id and project_id")
	}
	if len(item.Commits) == 0 {
		return fmt.Errorf("release persistence requires at least one commit")
	}
	fingerprint := release.Fingerprint(item)
	if item.ForceNew {
		fingerprint = release.FingerprintWithID(item)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := releaseRecordRow{}
		err := tx.Where("id = ?", item.ID).First(&row).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == gorm.ErrRecordNotFound {
			row = releaseRecordRow{ID: item.ID, SpaceID: item.SpaceID, ProjectID: item.ProjectID, CreatedAt: item.CreatedAt}
			if row.CreatedAt.IsZero() {
				row.CreatedAt = time.Now().UTC()
			}
		} else if row.SpaceID != item.SpaceID || row.ProjectID != item.ProjectID {
			return fmt.Errorf("release %s belongs to another project or space", item.ID)
		}
		// Existing rows keep the fingerprint assigned at creation. This is
		// important after a restart, when the internal ForceNew hint is not
		// serialized, and it also preserves fingerprints created by older
		// versions of the schema.
		if err != gorm.ErrRecordNotFound && !item.ForceNew && strings.TrimSpace(row.ReleaseFingerprint) != "" {
			fingerprint = row.ReleaseFingerprint
		}
		if item.CreatedBy != 0 {
			createdBy := item.CreatedBy
			row.CreatedBy = &createdBy
		}
		row.ReleaseName = item.Name
		row.SourceRepositoryID = item.RepositoryID
		row.SourceBranch = item.Branch
		row.FlowID = item.FlowID
		row.FlowVersion = item.FlowVersion
		row.BaseBranch = item.BaseBranch
		row.ParentReleaseID = item.ParentReleaseID
		row.ReplacesReleaseID = item.ReplacesReleaseID
		row.ReplacedByReleaseID = item.ReplacedByReleaseID
		row.ReplacedAt = cloneTimePtr(item.ReplacedAt)
		row.ReplacementState = item.ReplacementState
		row.RemovedBranch = item.RemovedBranch
		// JSON columns cannot accept an empty string. Legacy releases may not
		// have a flow snapshot yet, so persist an empty JSON array instead of
		// writing invalid JSON into the nullable column.
		row.FlowParticipants = "[]"
		if len(item.FlowParticipants) > 0 {
			encoded, marshalErr := json.Marshal(item.FlowParticipants)
			if marshalErr != nil {
				return marshalErr
			}
			row.FlowParticipants = string(encoded)
		}
		row.ReleaseFingerprint = fingerprint
		row.Strategy = string(item.Plan.Strategy)
		row.StablePercent = item.Plan.Traffic.StablePercent
		row.CandidatePercent = item.Plan.Traffic.CandidatePercent
		row.BluePercent = item.Plan.Traffic.BluePercent
		row.GreenPercent = item.Plan.Traffic.GreenPercent
		row.Status = string(item.Status)
		row.Progress = item.Progress
		row.Stage = item.Stage
		row.Message = item.Message
		row.Error = item.Error
		row.ImageRef = ""
		row.ImageDigest = ""
		row.ImageCommitSHA = ""
		row.ImageBuiltAt = nil
		if item.Artifact != nil {
			row.ImageRef = item.Artifact.Image
			row.ImageDigest = item.Artifact.Digest
			row.ImageCommitSHA = item.Artifact.CommitSHA
			builtAt := item.Artifact.BuiltAt.UTC()
			row.ImageBuiltAt = &builtAt
		}
		row.StartedAt = cloneTimePtr(item.StartedAt)
		row.FinishedAt = cloneTimePtr(item.FinishedAt)
		row.PublishedAt = nil
		row.IsRemoved = item.Removed
		row.RemovedAt = cloneTimePtr(item.RemovedAt)
		row.RemovedBy = nil
		if item.RemovedBy != 0 {
			removedBy := item.RemovedBy
			row.RemovedBy = &removedBy
		}
		row.RemoveReason = item.RemoveReason
		if item.Status == release.StatusSucceeded && item.FinishedAt != nil {
			row.PublishedAt = cloneTimePtr(item.FinishedAt)
		}
		row.UpdatedAt = item.UpdatedAt
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = time.Now().UTC()
		}
		if err == gorm.ErrRecordNotFound {
			// GORM omits zero-valued fields when a default tag is present. The
			// blue/green plan intentionally stores stable/candidate as zero, so
			// include every column explicitly instead of letting MySQL apply the
			// rolling defaults.
			if err := tx.Select("*").Create(&row).Error; err != nil {
				return err
			}
		} else {
			updates := map[string]any{
				"release_name": row.ReleaseName, "source_repository_id": row.SourceRepositoryID, "source_branch": row.SourceBranch, "flow_id": row.FlowID, "flow_version": row.FlowVersion, "base_branch": row.BaseBranch, "flow_participants": row.FlowParticipants, "parent_release_id": row.ParentReleaseID, "replaces_release_id": row.ReplacesReleaseID, "replaced_by_release_id": row.ReplacedByReleaseID, "replaced_at": row.ReplacedAt, "replacement_state": row.ReplacementState, "removed_branch": row.RemovedBranch, "release_fingerprint": row.ReleaseFingerprint,
				"strategy": row.Strategy, "stable_percent": row.StablePercent, "candidate_percent": row.CandidatePercent, "blue_percent": row.BluePercent, "green_percent": row.GreenPercent,
				"status": row.Status, "progress": row.Progress, "stage": row.Stage, "message": row.Message, "error": row.Error, "image_ref": row.ImageRef, "image_digest": row.ImageDigest, "image_commit_sha": row.ImageCommitSHA, "image_built_at": row.ImageBuiltAt, "started_at": row.StartedAt, "finished_at": row.FinishedAt, "published_at": row.PublishedAt, "is_removed": row.IsRemoved, "removed_at": row.RemovedAt, "removed_by": row.RemovedBy, "remove_reason": row.RemoveReason, "updated_at": row.UpdatedAt,
			}
			if item.CreatedBy != 0 {
				updates["created_by"] = row.CreatedBy
			}
			if err := tx.Model(&releaseRecordRow{}).Where("id = ?", item.ID).Updates(updates).Error; err != nil {
				return err
			}
		}

		if err := s.saveReleaseCommits(tx, item); err != nil {
			return err
		}
		if err := s.saveReleaseTargets(tx, item); err != nil {
			return err
		}
		return s.saveReleaseLogs(tx, item)
	})
}

func (s *MySQL) saveReleaseCommits(tx *gorm.DB, item release.Release) error {
	const positionOffset = 1000000
	// Move every historical row out of the live position range first. The
	// position key is unique per release, so updating only active rows would
	// collide with a removed row while a draft is being edited.
	if err := tx.Model(&releaseCommitRecordRow{}).Where("release_id = ?", item.ID).UpdateColumn("commit_position", gorm.Expr("commit_position + ?", positionOffset)).Error; err != nil {
		return err
	}
	active := make(map[string]struct{}, len(item.Commits))
	now := time.Now().UTC()
	for index, commit := range item.Commits {
		sha := strings.TrimSpace(commit.SHA)
		if sha == "" {
			continue
		}
		active[strings.ToLower(sha)] = struct{}{}
		shortSHA := strings.TrimSpace(commit.ShortSHA)
		if shortSHA == "" {
			shortSHA = sha
			if len(shortSHA) > 10 {
				shortSHA = shortSHA[:10]
			}
		}
		commitRow := releaseCommitRecordRow{}
		err := tx.Where("release_id = ? AND commit_sha = ?", item.ID, sha).First(&commitRow).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		var authoredAt *time.Time
		if !commit.AuthoredAt.IsZero() {
			value := commit.AuthoredAt.UTC()
			authoredAt = &value
		}
		if err == gorm.ErrRecordNotFound {
			commitRow = releaseCommitRecordRow{SpaceID: item.SpaceID, ReleaseID: item.ID, CommitSHA: sha, CreatedAt: now}
		}
		commitRow.SpaceID = item.SpaceID
		commitRow.CommitPosition = index + 1
		commitRow.IsRemoved = false
		commitRow.RemovedAt = nil
		commitRow.RemovedBy = nil
		commitRow.RemoveReason = ""
		commitRow.ShortSHA = shortSHA
		commitRow.CommitMessage = commit.Message
		commitRow.Author = commit.Author
		commitRow.AuthoredAt = authoredAt
		commitRow.UpdatedAt = now
		if err == gorm.ErrRecordNotFound {
			if err := tx.Create(&commitRow).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&releaseCommitRecordRow{}).Where("release_id = ? AND commit_sha = ?", item.ID, sha).Updates(map[string]any{
			"space_id": item.SpaceID, "commit_position": commitRow.CommitPosition, "is_removed": false, "removed_at": nil, "removed_by": nil, "remove_reason": "", "short_sha": shortSHA, "commit_message": commit.Message, "author": commit.Author, "authored_at": authoredAt, "updated_at": now,
		}).Error; err != nil {
			return err
		}
	}
	var oldRows []releaseCommitRecordRow
	if err := tx.Where("release_id = ? AND is_removed = ?", item.ID, false).Find(&oldRows).Error; err != nil {
		return err
	}
	for _, oldRow := range oldRows {
		if _, ok := active[strings.ToLower(oldRow.CommitSHA)]; ok {
			continue
		}
		if err := tx.Model(&releaseCommitRecordRow{}).Where("release_id = ? AND commit_sha = ?", item.ID, oldRow.CommitSHA).Updates(map[string]any{"is_removed": true, "removed_at": now, "remove_reason": "removed from draft", "updated_at": now}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQL) saveReleaseTargets(tx *gorm.DB, item release.Release) error {
	now := time.Now().UTC()
	for _, target := range item.Targets {
		row := releaseTargetRecordRow{}
		err := tx.Where("release_id = ? AND target_id = ?", item.ID, target.ID).First(&row).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == gorm.ErrRecordNotFound {
			row = releaseTargetRecordRow{ReleaseID: item.ID, TargetID: target.ID, CreatedAt: now}
		}
		row.SpaceID = item.SpaceID
		row.Name = target.Name
		row.Environment = target.Environment
		row.EnvironmentStage = target.EnvironmentStage
		row.SortOrder = target.SortOrder
		row.ClusterID = target.ClusterID
		row.Namespace = target.Namespace
		row.Replicas = target.Replicas
		row.ContainerPort = target.ContainerPort
		row.DeployStrategy = target.DeployStrategy
		row.Status = string(target.Status)
		row.Progress = target.Progress
		row.Stage = target.Stage
		row.Message = target.Message
		row.Error = target.Error
		row.StartedAt = cloneTimePtr(target.StartedAt)
		row.FinishedAt = cloneTimePtr(target.FinishedAt)
		row.UpdatedAt = now
		if err == gorm.ErrRecordNotFound {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&releaseTargetRecordRow{}).Where("release_id = ? AND target_id = ?", item.ID, target.ID).Updates(map[string]any{
			"space_id": item.SpaceID, "name": row.Name, "environment": row.Environment, "environment_stage": row.EnvironmentStage, "sort_order": row.SortOrder, "cluster_id": row.ClusterID, "namespace": row.Namespace, "replicas": row.Replicas, "container_port": row.ContainerPort, "deploy_strategy": row.DeployStrategy, "status": row.Status, "progress": row.Progress, "stage": row.Stage, "message": row.Message, "error": row.Error, "started_at": row.StartedAt, "finished_at": row.FinishedAt, "updated_at": row.UpdatedAt,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQL) saveReleaseLogs(tx *gorm.DB, item release.Release) error {
	for _, target := range item.Targets {
		for _, logEntry := range target.Logs {
			if strings.TrimSpace(logEntry.ID) == "" || strings.TrimSpace(logEntry.Line) == "" {
				continue
			}
			row := releaseExecutionLogRecordRow{ID: logEntry.ID, SpaceID: item.SpaceID, ReleaseID: item.ID, TargetID: target.ID, Timestamp: logEntry.Timestamp.UTC(), Source: logEntry.Source, Stream: logEntry.Stream, Level: logEntry.Level, Line: logEntry.Line}
			if row.Timestamp.IsZero() {
				row.Timestamp = time.Now().UTC()
			}
			var existing releaseExecutionLogRecordRow
			err := tx.Where("id = ?", row.ID).First(&existing).Error
			if err == gorm.ErrRecordNotFound {
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := tx.Model(&releaseExecutionLogRecordRow{}).Where("id = ?", row.ID).Updates(map[string]any{"space_id": row.SpaceID, "release_id": row.ReleaseID, "target_id": row.TargetID, "logged_at": row.Timestamp, "source": row.Source, "stream": row.Stream, "level": row.Level, "line": row.Line}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := value.UTC()
	return &copyValue
}

var _ release.Repository = (*MySQL)(nil)
