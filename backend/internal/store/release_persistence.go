package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yuebuy/cicd-platform/backend/internal/release"
)

// The existing release tables hold searchable release fields and commit rows.
// state_json stores the complete state-machine snapshot so environment status,
// batch membership and provider snapshots survive a backend restart without
// making the release domain depend on GORM.
type projectReleaseRow struct {
	ID                 string  `gorm:"size:64;primaryKey"`
	SpaceID            string  `gorm:"size:64;index;not null"`
	ProjectID          string  `gorm:"size:64;index;not null"`
	ReleaseName        string  `gorm:"size:120;not null;default:''"`
	SourceRepositoryID string  `gorm:"size:255;not null"`
	SourceBranch       string  `gorm:"size:120;not null"`
	ReleaseFingerprint string  `gorm:"type:char(64);not null"`
	Strategy           string  `gorm:"size:32;not null;default:rolling"`
	StablePercent      int     `gorm:"not null;default:100"`
	CandidatePercent   int     `gorm:"not null;default:0"`
	BluePercent        int     `gorm:"not null;default:0"`
	GreenPercent       int     `gorm:"not null;default:0"`
	Status             string  `gorm:"size:32;not null;default:draft"`
	CreatedBy          *uint64 `gorm:"index"`
	PublishedAt        *time.Time
	StateJSON          string `gorm:"column:state_json;type:longtext;not null"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (projectReleaseRow) TableName() string { return "project_releases" }

type releaseCommitRow struct {
	SpaceID        string `gorm:"size:64;primaryKey"`
	ReleaseID      string `gorm:"size:64;primaryKey"`
	CommitSHA      string `gorm:"size:128;primaryKey"`
	CommitPosition int    `gorm:"column:commit_position;not null"`
	IsRemoved      bool   `gorm:"not null;default:false"`
	RemovedAt      *time.Time
	RemovedBy      *uint64
	RemoveReason   string `gorm:"size:255;not null;default:''"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (releaseCommitRow) TableName() string { return "release_commits" }

type releaseBatchStateRow struct {
	ID        string `gorm:"size:64;primaryKey"`
	SpaceID   string `gorm:"size:64;index;not null"`
	ProjectID string `gorm:"size:64;index;not null"`
	State     string `gorm:"type:longtext;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (releaseBatchStateRow) TableName() string { return "release_batch_state" }

func (s *MySQL) Load(ctx context.Context) ([]release.Release, []release.Batch, error) {
	var releaseRows []projectReleaseRow
	if err := s.db.WithContext(ctx).Where("state_json <> ''").Order("created_at ASC, id ASC").Find(&releaseRows).Error; err != nil {
		return nil, nil, err
	}
	releases := make([]release.Release, 0, len(releaseRows))
	for _, row := range releaseRows {
		var item release.Release
		if err := json.Unmarshal([]byte(row.StateJSON), &item); err != nil {
			return nil, nil, fmt.Errorf("decode release %s: %w", row.ID, err)
		}
		item.ID = firstNonEmptyRelease(item.ID, row.ID)
		item.SpaceID = firstNonEmptyRelease(item.SpaceID, row.SpaceID)
		item.ProjectID = firstNonEmptyRelease(item.ProjectID, row.ProjectID)
		releases = append(releases, item)
	}

	var batchRows []releaseBatchStateRow
	if err := s.db.WithContext(ctx).Order("created_at ASC, id ASC").Find(&batchRows).Error; err != nil {
		return nil, nil, err
	}
	batches := make([]release.Batch, 0, len(batchRows))
	for _, row := range batchRows {
		var item release.Batch
		if err := json.Unmarshal([]byte(row.State), &item); err != nil {
			return nil, nil, fmt.Errorf("decode release batch %s: %w", row.ID, err)
		}
		item.ID = firstNonEmptyRelease(item.ID, row.ID)
		item.SpaceID = firstNonEmptyRelease(item.SpaceID, row.SpaceID)
		item.ProjectID = firstNonEmptyRelease(item.ProjectID, row.ProjectID)
		batches = append(batches, item)
	}
	return releases, batches, nil
}

func (s *MySQL) Replace(ctx context.Context, releases []release.Release, batches []release.Batch) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range releases {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.ProjectID) == "" || strings.TrimSpace(item.SpaceID) == "" {
				return fmt.Errorf("release %q is missing space or project", item.ID)
			}
			state, err := json.Marshal(item)
			if err != nil {
				return fmt.Errorf("encode release %s: %w", item.ID, err)
			}
			row := projectReleaseRow{
				ID: item.ID, SpaceID: item.SpaceID, ProjectID: item.ProjectID,
				ReleaseName: item.Name, SourceRepositoryID: item.RepositoryID,
				SourceBranch:       firstNonEmptyRelease(item.SourceBranch, item.Branch),
				ReleaseFingerprint: releaseFingerprint(item), Strategy: string(item.Plan.Strategy),
				StablePercent: item.Plan.Traffic.StablePercent, CandidatePercent: item.Plan.Traffic.CandidatePercent,
				BluePercent: item.Plan.Traffic.BluePercent, GreenPercent: item.Plan.Traffic.GreenPercent,
				Status: string(item.Status), CreatedBy: nullableUserID(item.OwnerID), PublishedAt: releasePublishedAt(item),
				StateJSON: string(state), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{"space_id", "project_id", "release_name", "source_repository_id", "source_branch", "release_fingerprint", "strategy", "stable_percent", "candidate_percent", "blue_percent", "green_percent", "status", "created_by", "published_at", "state_json", "updated_at"}),
			}).Create(&row).Error; err != nil {
				return err
			}
			if err := tx.Where("space_id = ? AND release_id = ?", item.SpaceID, item.ID).Delete(&releaseCommitRow{}).Error; err != nil {
				return err
			}
			for position, commit := range item.Commits {
				if strings.TrimSpace(commit.SHA) == "" {
					continue
				}
				if err := tx.Create(&releaseCommitRow{
					SpaceID: item.SpaceID, ReleaseID: item.ID, CommitSHA: commit.SHA,
					CommitPosition: position, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
				}).Error; err != nil {
					return err
				}
			}
		}
		for _, item := range batches {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.ProjectID) == "" || strings.TrimSpace(item.SpaceID) == "" {
				return fmt.Errorf("release batch %q is missing space or project", item.ID)
			}
			state, err := json.Marshal(item)
			if err != nil {
				return fmt.Errorf("encode release batch %s: %w", item.ID, err)
			}
			row := releaseBatchStateRow{ID: item.ID, SpaceID: item.SpaceID, ProjectID: item.ProjectID, State: string(state), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{"space_id", "project_id", "state", "updated_at"}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func releaseFingerprint(item release.Release) string {
	commits := make([]string, 0, len(item.Commits))
	for _, commit := range item.Commits {
		commits = append(commits, strings.ToLower(strings.TrimSpace(commit.SHA)))
	}
	payload := struct {
		ProjectID    string              `json:"project_id"`
		RepositoryID string              `json:"repository_id"`
		Branch       string              `json:"branch"`
		BaseBranch   string              `json:"base_branch"`
		Commits      []string            `json:"commits"`
		Plan         release.RolloutPlan `json:"plan"`
	}{item.ProjectID, item.RepositoryID, item.Branch, item.BaseBranch, commits, item.Plan}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func releasePublishedAt(item release.Release) *time.Time {
	if item.Status != release.StatusSucceeded || item.FinishedAt == nil {
		return nil
	}
	value := item.FinishedAt.UTC()
	return &value
}

func nullableUserID(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	return &value
}

func firstNonEmptyRelease(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var _ release.Persistence = (*MySQL)(nil)
