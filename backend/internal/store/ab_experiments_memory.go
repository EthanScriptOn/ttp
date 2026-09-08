package store

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

func (m *Memory) ListABExperiments(ctx context.Context, spaceID, projectID string) ([]domain.ABExperiment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return nil, ErrNotFound
	}
	items := make([]domain.ABExperiment, 0)
	for _, item := range m.abExperiments {
		if item.ProjectID == projectID {
			items = append(items, cloneABExperiment(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (m *Memory) GetABExperiment(ctx context.Context, spaceID, projectID, experimentID string) (domain.ABExperiment, error) {
	if err := ctx.Err(); err != nil {
		return domain.ABExperiment{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.abExperiments[strings.TrimSpace(experimentID)]
	if !ok || item.ProjectID != projectID {
		return domain.ABExperiment{}, ErrNotFound
	}
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.ABExperiment{}, ErrNotFound
	}
	return cloneABExperiment(item), nil
}

func (m *Memory) CreateABExperiment(ctx context.Context, spaceID, projectID string, input CreateABExperimentInput) (domain.ABExperiment, error) {
	if err := ctx.Err(); err != nil {
		return domain.ABExperiment{}, err
	}
	normalized, err := normalizeABExperimentInput(input)
	if err != nil {
		return domain.ABExperiment{}, err
	}
	input = normalized
	if err := validateABExperimentInput(input); err != nil {
		return domain.ABExperiment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.ABExperiment{}, ErrNotFound
	}
	target, ok := m.deploymentTargets[input.TargetID]
	if !ok || target.ProjectID != projectID || target.SpaceID != spaceID {
		return domain.ABExperiment{}, ErrNotFound
	}
	for _, existing := range m.abExperiments {
		if existing.ProjectID == projectID && existing.TargetID == input.TargetID && existing.Status == domain.ABExperimentRunning {
			return domain.ABExperiment{}, fmt.Errorf("%w: 该环境已有运行中的 A/B 实验", ErrConflict)
		}
	}
	now := time.Now().UTC()
	id := "ab-" + uuid.NewString()[:8]
	item := domain.ABExperiment{
		ID: id, ProjectID: projectID, Name: strings.TrimSpace(input.Name), TargetID: strings.TrimSpace(input.TargetID),
		Environment: strings.TrimSpace(input.Environment), EnvironmentStage: strings.TrimSpace(input.EnvironmentStage),
		ClusterID: strings.TrimSpace(input.ClusterID), Namespace: strings.TrimSpace(input.Namespace), Replicas: input.Replicas,
		Strategy: strings.TrimSpace(input.Strategy), Assignment: strings.TrimSpace(input.Assignment), RoutingRule: input.RoutingRule, AVersion: input.AVersion, BVersion: input.BVersion,
		ATraffic: input.ATraffic, BTraffic: input.BTraffic, Status: domain.ABExperimentRunning, CreatedBy: input.CreatedBy,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
		Events: []domain.ABExperimentEvent{{ID: uuid.NewString(), Type: "created", Message: "A/B 实验已创建，B 版本从 1% 流量开始", CreatedAt: now}},
	}
	m.abExperiments[id] = item
	return cloneABExperiment(item), nil
}

func (m *Memory) UpdateABExperimentTraffic(ctx context.Context, spaceID, projectID, experimentID string, input UpdateABTrafficInput) (domain.ABExperiment, error) {
	return m.updateABExperiment(ctx, spaceID, projectID, experimentID, func(item *domain.ABExperiment, now time.Time) error {
		if err := validateABTraffic(input.ATraffic, input.BTraffic); err != nil {
			return err
		}
		item.ATraffic = input.ATraffic
		item.BTraffic = input.BTraffic
		item.Events = append(item.Events, domain.ABExperimentEvent{ID: uuid.NewString(), Type: "traffic", Message: fmt.Sprintf("流量调整为 A %d%% / B %d%%", input.ATraffic, input.BTraffic), CreatedAt: now})
		return nil
	})
}

func (m *Memory) StopABExperiment(ctx context.Context, spaceID, projectID, experimentID string) (domain.ABExperiment, error) {
	return m.updateABExperiment(ctx, spaceID, projectID, experimentID, func(item *domain.ABExperiment, now time.Time) error {
		if item.Status != domain.ABExperimentRunning {
			return fmt.Errorf("%w: 实验当前不可停止", ErrConflict)
		}
		item.Status = domain.ABExperimentStopped
		item.ATraffic, item.BTraffic = 100, 0
		item.FinishedAt = timePtr(now)
		item.Events = append(item.Events, domain.ABExperimentEvent{ID: uuid.NewString(), Type: "stopped", Message: "实验已停止，流量全部切回 A 版本", CreatedAt: now})
		return nil
	})
}

func (m *Memory) FinishABExperiment(ctx context.Context, spaceID, projectID, experimentID, result string) (domain.ABExperiment, error) {
	return m.updateABExperiment(ctx, spaceID, projectID, experimentID, func(item *domain.ABExperiment, now time.Time) error {
		if item.Status != domain.ABExperimentRunning {
			return fmt.Errorf("%w: 实验当前不可结束", ErrConflict)
		}
		result = strings.TrimSpace(strings.ToLower(result))
		if result != "keep_a" && result != "promote_b" {
			return fmt.Errorf("%w: 结束结果必须是 keep_a 或 promote_b", ErrInvalidInput)
		}
		item.Status = domain.ABExperimentFinished
		item.FinishedAt = timePtr(now)
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

func (m *Memory) updateABExperiment(ctx context.Context, spaceID, projectID, experimentID string, update func(*domain.ABExperiment, time.Time) error) (domain.ABExperiment, error) {
	if err := ctx.Err(); err != nil {
		return domain.ABExperiment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.abExperiments[strings.TrimSpace(experimentID)]
	if !ok || item.ProjectID != projectID {
		return domain.ABExperiment{}, ErrNotFound
	}
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.ABExperiment{}, ErrNotFound
	}
	now := time.Now().UTC()
	if err := update(&item, now); err != nil {
		return domain.ABExperiment{}, err
	}
	item.UpdatedAt = now
	m.abExperiments[item.ID] = item
	return cloneABExperiment(item), nil
}

var safeABJSONPath = regexp.MustCompile(`^\$\.[A-Za-z_][A-Za-z0-9_]*(?:(?:\.[A-Za-z_][A-Za-z0-9_]*)|(?:\[[0-9]+\]))*$`)

func normalizeABExperimentInput(input CreateABExperimentInput) (CreateABExperimentInput, error) {
	input.Assignment = strings.TrimSpace(strings.ToLower(input.Assignment))
	if input.Assignment == "" {
		input.Assignment = "percentage"
	}
	if input.Assignment == "json_field" {
		input.Assignment = "user_id"
	}
	rule, err := normalizeABRoutingRule(input.Assignment, input.RoutingRule)
	if err != nil {
		return CreateABExperimentInput{}, err
	}
	input.RoutingRule = rule
	return input, nil
}

func normalizeABRoutingRule(assignment string, input domain.ABRoutingRule) (domain.ABRoutingRule, error) {
	if assignment == "percentage" {
		return domain.ABRoutingRule{}, nil
	}
	if assignment != "user_id" {
		return domain.ABRoutingRule{}, fmt.Errorf("%w: 分组方式不受支持", ErrInvalidInput)
	}
	rule := domain.ABRoutingRule{
		Source:          strings.TrimSpace(strings.ToLower(input.Source)),
		Path:            strings.TrimSpace(input.Path),
		MissingBehavior: strings.TrimSpace(strings.ToLower(input.MissingBehavior)),
		Algorithm:       strings.TrimSpace(strings.ToLower(input.Algorithm)),
	}
	if rule.Source == "" {
		rule.Source = "json_body"
	}
	if rule.Path == "" {
		rule.Path = "$.user_id"
	}
	if rule.MissingBehavior == "" {
		rule.MissingBehavior = "stable"
	}
	if rule.Algorithm == "" {
		rule.Algorithm = "consistent_hash"
	}
	if rule.Source != "json_body" {
		return domain.ABRoutingRule{}, fmt.Errorf("%w: 只支持从 JSON 请求体读取分流字段", ErrInvalidInput)
	}
	if !safeABJSONPath.MatchString(rule.Path) {
		return domain.ABRoutingRule{}, fmt.Errorf("%w: JSON 字段路径必须形如 $.wx_id 或 $.user.openid", ErrInvalidInput)
	}
	if rule.MissingBehavior != "stable" {
		return domain.ABRoutingRule{}, fmt.Errorf("%w: 字段缺失时只能进入 A 版本", ErrInvalidInput)
	}
	if rule.Algorithm != "consistent_hash" {
		return domain.ABRoutingRule{}, fmt.Errorf("%w: 只支持稳定哈希分组算法", ErrInvalidInput)
	}
	return rule, nil
}

func validateABExperimentInput(input CreateABExperimentInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return fmt.Errorf("%w: 实验名称不能为空", ErrInvalidInput)
	}
	if len([]rune(name)) > 120 {
		return fmt.Errorf("%w: 实验名称过长", ErrInvalidInput)
	}
	if strings.TrimSpace(input.TargetID) == "" || strings.TrimSpace(input.Environment) == "" || strings.TrimSpace(input.ClusterID) == "" || strings.TrimSpace(input.Namespace) == "" {
		return fmt.Errorf("%w: 实验环境信息不完整", ErrInvalidInput)
	}
	if strings.TrimSpace(input.AVersion.CommitSHA) == "" {
		return fmt.Errorf("%w: A 版本必须绑定环境当前稳定版本", ErrInvalidInput)
	}
	if strings.TrimSpace(input.BVersion.ReleaseID) == "" || strings.TrimSpace(input.BVersion.CommitSHA) == "" {
		return fmt.Errorf("%w: B 版本必须绑定已发布的发布单和 commit", ErrInvalidInput)
	}
	if input.Replicas <= 0 || input.Replicas > 100 {
		return fmt.Errorf("%w: 副本数必须在 1 到 100 之间", ErrInvalidInput)
	}
	if input.Assignment != "user_id" && input.Assignment != "percentage" {
		return fmt.Errorf("%w: 分组方式不受支持", ErrInvalidInput)
	}
	if _, err := normalizeABRoutingRule(input.Assignment, input.RoutingRule); err != nil {
		return err
	}
	return validateABTraffic(input.ATraffic, input.BTraffic)
}

func validateABTraffic(a, b int) error {
	if a < 0 || b < 0 || a > 100 || b > 100 || a+b != 100 {
		return fmt.Errorf("%w: A/B 流量必须在 0 到 100 之间且总和为 100", ErrInvalidInput)
	}
	return nil
}

func cloneABExperiment(value domain.ABExperiment) domain.ABExperiment {
	value.Events = append([]domain.ABExperimentEvent(nil), value.Events...)
	value.APods = append([]domain.ABExperimentPod(nil), value.APods...)
	value.BPods = append([]domain.ABExperimentPod(nil), value.BPods...)
	return value
}

func timePtr(value time.Time) *time.Time { return &value }
