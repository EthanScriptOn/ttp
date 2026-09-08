package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

type abExperimentRow struct {
	ID               string    `gorm:"size:64;primaryKey"`
	SpaceID          string    `gorm:"size:64;index;not null"`
	ProjectID        string    `gorm:"size:64;index;not null"`
	Name             string    `gorm:"size:120;not null"`
	TargetID         string    `gorm:"size:64;not null"`
	Environment      string    `gorm:"size:64;not null"`
	EnvironmentStage string    `gorm:"size:16;not null;default:custom"`
	ClusterID        string    `gorm:"size:64;not null"`
	Namespace        string    `gorm:"size:120;not null"`
	Replicas         int       `gorm:"not null;default:1"`
	Strategy         string    `gorm:"size:32;not null;default:rolling"`
	Assignment       string    `gorm:"size:32;not null;default:percentage"`
	RoutingRule      *string   `gorm:"type:json"`
	AReleaseID       string    `gorm:"size:64;not null;default:''"`
	BReleaseID       string    `gorm:"size:64;not null"`
	AVersion         string    `gorm:"type:json;not null"`
	BVersion         string    `gorm:"type:json;not null"`
	AStats           string    `gorm:"type:json;not null"`
	BStats           string    `gorm:"type:json;not null"`
	APods            string    `gorm:"type:json;not null"`
	BPods            string    `gorm:"type:json;not null"`
	Events           string    `gorm:"type:json;not null"`
	ATraffic         int       `gorm:"not null;default:99"`
	BTraffic         int       `gorm:"not null;default:1"`
	Status           string    `gorm:"size:32;index;not null;default:running"`
	CreatedBy        *uint64   `gorm:"index"`
	StartedAt        time.Time `gorm:"not null"`
	FinishedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (abExperimentRow) TableName() string { return "ab_experiments" }

func (s *MySQL) ListABExperiments(ctx context.Context, spaceID, projectID string) ([]domain.ABExperiment, error) {
	var rows []abExperimentRow
	if err := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]domain.ABExperiment, 0, len(rows))
	for _, row := range rows {
		item, err := toABExperiment(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *MySQL) GetABExperiment(ctx context.Context, spaceID, projectID, experimentID string) (domain.ABExperiment, error) {
	var row abExperimentRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ? AND project_id = ?", experimentID, spaceID, projectID).First(&row).Error; err != nil {
		return domain.ABExperiment{}, mapDBError(err)
	}
	return toABExperiment(row)
}

func (s *MySQL) CreateABExperiment(ctx context.Context, spaceID, projectID string, input CreateABExperimentInput) (domain.ABExperiment, error) {
	normalized, err := normalizeABExperimentInput(input)
	if err != nil {
		return domain.ABExperiment{}, err
	}
	input = normalized
	if err := validateABExperimentInput(input); err != nil {
		return domain.ABExperiment{}, err
	}
	var project projectRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ?", projectID, spaceID).First(&project).Error; err != nil {
		return domain.ABExperiment{}, mapDBError(err)
	}
	var target deploymentTargetRow
	if err := s.db.WithContext(ctx).Where("id = ? AND project_id = ? AND space_id = ?", input.TargetID, projectID, spaceID).First(&target).Error; err != nil {
		return domain.ABExperiment{}, mapDBError(err)
	}
	var active int64
	if err := s.db.WithContext(ctx).Model(&abExperimentRow{}).Where("space_id = ? AND project_id = ? AND target_id = ? AND status = ?", spaceID, projectID, input.TargetID, domain.ABExperimentRunning).Count(&active).Error; err != nil {
		return domain.ABExperiment{}, err
	}
	if active > 0 {
		return domain.ABExperiment{}, fmt.Errorf("%w: 该环境已有运行中的 A/B 实验", ErrConflict)
	}
	now := time.Now().UTC()
	item := domain.ABExperiment{ID: "ab-" + uuid.NewString()[:8], ProjectID: projectID, Name: strings.TrimSpace(input.Name), TargetID: input.TargetID, Environment: input.Environment, EnvironmentStage: input.EnvironmentStage, ClusterID: input.ClusterID, Namespace: input.Namespace, Replicas: input.Replicas, Strategy: input.Strategy, Assignment: input.Assignment, RoutingRule: input.RoutingRule, AVersion: input.AVersion, BVersion: input.BVersion, ATraffic: input.ATraffic, BTraffic: input.BTraffic, Status: domain.ABExperimentRunning, CreatedBy: input.CreatedBy, StartedAt: now, CreatedAt: now, UpdatedAt: now, Events: []domain.ABExperimentEvent{{ID: uuid.NewString(), Type: "created", Message: "A/B 实验已创建，B 版本从 1% 流量开始", CreatedAt: now}}}
	row, err := fromABExperiment(item, spaceID)
	if err != nil {
		return domain.ABExperiment{}, err
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return domain.ABExperiment{}, err
	}
	return item, nil
}

func (s *MySQL) UpdateABExperimentTraffic(ctx context.Context, spaceID, projectID, experimentID string, input UpdateABTrafficInput) (domain.ABExperiment, error) {
	return s.updateABExperiment(ctx, spaceID, projectID, experimentID, func(item *domain.ABExperiment, now time.Time) error {
		if item.Status != domain.ABExperimentRunning {
			return fmt.Errorf("%w: 实验当前不可调整流量", ErrConflict)
		}
		if err := validateABTraffic(input.ATraffic, input.BTraffic); err != nil {
			return err
		}
		item.ATraffic, item.BTraffic = input.ATraffic, input.BTraffic
		item.Events = append(item.Events, domain.ABExperimentEvent{ID: uuid.NewString(), Type: "traffic", Message: fmt.Sprintf("流量调整为 A %d%% / B %d%%", input.ATraffic, input.BTraffic), CreatedAt: now})
		return nil
	})
}

func (s *MySQL) StopABExperiment(ctx context.Context, spaceID, projectID, experimentID string) (domain.ABExperiment, error) {
	return s.updateABExperiment(ctx, spaceID, projectID, experimentID, func(item *domain.ABExperiment, now time.Time) error {
		if item.Status != domain.ABExperimentRunning {
			return fmt.Errorf("%w: 实验当前不可停止", ErrConflict)
		}
		item.Status, item.ATraffic, item.BTraffic, item.FinishedAt = domain.ABExperimentStopped, 100, 0, timePtr(now)
		item.Events = append(item.Events, domain.ABExperimentEvent{ID: uuid.NewString(), Type: "stopped", Message: "实验已停止，流量全部切回 A 版本", CreatedAt: now})
		return nil
	})
}

func (s *MySQL) FinishABExperiment(ctx context.Context, spaceID, projectID, experimentID, result string) (domain.ABExperiment, error) {
	return s.updateABExperiment(ctx, spaceID, projectID, experimentID, func(item *domain.ABExperiment, now time.Time) error {
		if item.Status != domain.ABExperimentRunning {
			return fmt.Errorf("%w: 实验当前不可结束", ErrConflict)
		}
		result = strings.TrimSpace(strings.ToLower(result))
		if result != "keep_a" && result != "promote_b" {
			return fmt.Errorf("%w: 结束结果必须是 keep_a 或 promote_b", ErrInvalidInput)
		}
		item.Status, item.FinishedAt = domain.ABExperimentFinished, timePtr(now)
		if result == "promote_b" {
			item.ATraffic, item.BTraffic = 0, 100
			item.Events = append(item.Events, domain.ABExperimentEvent{ID: uuid.NewString(), Type: "finished", Message: "实验结束，B 版本通过并接管全部流量", CreatedAt: now})
		} else {
			item.ATraffic, item.BTraffic = 100, 0
			item.Events = append(item.Events, domain.ABExperimentEvent{ID: uuid.NewString(), Type: "finished", Message: "实验结束，保留 A 版本并切回全部流量", CreatedAt: now})
		}
		return nil
	})
}

func (s *MySQL) updateABExperiment(ctx context.Context, spaceID, projectID, experimentID string, update func(*domain.ABExperiment, time.Time) error) (domain.ABExperiment, error) {
	item, err := s.GetABExperiment(ctx, spaceID, projectID, experimentID)
	if err != nil {
		return domain.ABExperiment{}, err
	}
	now := time.Now().UTC()
	if err := update(&item, now); err != nil {
		return domain.ABExperiment{}, err
	}
	item.UpdatedAt = now
	row, err := fromABExperiment(item, spaceID)
	if err != nil {
		return domain.ABExperiment{}, err
	}
	if err := s.db.WithContext(ctx).Model(&abExperimentRow{}).Where("id = ? AND space_id = ? AND project_id = ?", experimentID, spaceID, projectID).Updates(map[string]any{
		"a_traffic": row.ATraffic, "b_traffic": row.BTraffic, "status": row.Status, "finished_at": row.FinishedAt, "events": row.Events, "updated_at": row.UpdatedAt,
	}).Error; err != nil {
		return domain.ABExperiment{}, err
	}
	return item, nil
}

func fromABExperiment(item domain.ABExperiment, spaceID string) (abExperimentRow, error) {
	encode := func(value any) (string, error) { data, err := json.Marshal(value); return string(data), err }
	aVersion, err := encode(item.AVersion)
	if err != nil {
		return abExperimentRow{}, err
	}
	bVersion, err := encode(item.BVersion)
	if err != nil {
		return abExperimentRow{}, err
	}
	aStats, err := encode(item.AStats)
	if err != nil {
		return abExperimentRow{}, err
	}
	bStats, err := encode(item.BStats)
	if err != nil {
		return abExperimentRow{}, err
	}
	aPods, err := encode(item.APods)
	if err != nil {
		return abExperimentRow{}, err
	}
	bPods, err := encode(item.BPods)
	if err != nil {
		return abExperimentRow{}, err
	}
	events, err := encode(item.Events)
	if err != nil {
		return abExperimentRow{}, err
	}
	routingRule, err := encode(item.RoutingRule)
	if err != nil {
		return abExperimentRow{}, err
	}
	row := abExperimentRow{ID: item.ID, SpaceID: spaceID, ProjectID: item.ProjectID, Name: item.Name, TargetID: item.TargetID, Environment: item.Environment, EnvironmentStage: item.EnvironmentStage, ClusterID: item.ClusterID, Namespace: item.Namespace, Replicas: item.Replicas, Strategy: item.Strategy, Assignment: item.Assignment, RoutingRule: &routingRule, AReleaseID: item.AVersion.ReleaseID, BReleaseID: item.BVersion.ReleaseID, AVersion: aVersion, BVersion: bVersion, AStats: aStats, BStats: bStats, APods: aPods, BPods: bPods, Events: events, ATraffic: item.ATraffic, BTraffic: item.BTraffic, Status: item.Status, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if item.CreatedBy != 0 {
		row.CreatedBy = &item.CreatedBy
	}
	return row, nil
}

func toABExperiment(row abExperimentRow) (domain.ABExperiment, error) {
	item := domain.ABExperiment{ID: row.ID, ProjectID: row.ProjectID, Name: row.Name, TargetID: row.TargetID, Environment: row.Environment, EnvironmentStage: row.EnvironmentStage, ClusterID: row.ClusterID, Namespace: row.Namespace, Replicas: row.Replicas, Strategy: row.Strategy, Assignment: row.Assignment, ATraffic: row.ATraffic, BTraffic: row.BTraffic, Status: row.Status, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	if row.CreatedBy != nil {
		item.CreatedBy = *row.CreatedBy
	}
	decode := func(raw string, target any) error {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		return json.Unmarshal([]byte(raw), target)
	}
	if err := decode(row.AVersion, &item.AVersion); err != nil {
		return domain.ABExperiment{}, err
	}
	if err := decode(row.BVersion, &item.BVersion); err != nil {
		return domain.ABExperiment{}, err
	}
	if err := decode(row.AStats, &item.AStats); err != nil {
		return domain.ABExperiment{}, err
	}
	if err := decode(row.BStats, &item.BStats); err != nil {
		return domain.ABExperiment{}, err
	}
	if err := decode(row.APods, &item.APods); err != nil {
		return domain.ABExperiment{}, err
	}
	if err := decode(row.BPods, &item.BPods); err != nil {
		return domain.ABExperiment{}, err
	}
	if err := decode(row.Events, &item.Events); err != nil {
		return domain.ABExperiment{}, err
	}
	if row.RoutingRule != nil {
		if err := decode(*row.RoutingRule, &item.RoutingRule); err != nil {
			return domain.ABExperiment{}, err
		}
	}
	if item.Assignment == "user_id" {
		rule, err := normalizeABRoutingRule(item.Assignment, item.RoutingRule)
		if err != nil {
			return domain.ABExperiment{}, err
		}
		item.RoutingRule = rule
	}
	return item, nil
}
