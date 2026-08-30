package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

type membership struct {
	UserID   uint64
	SpaceID  string
	Role     string
	JoinedAt time.Time
}

type Memory struct {
	mu                sync.RWMutex
	users             map[uint64]domain.User
	spaces            map[string]domain.Space
	memberships       map[string]membership
	clusters          map[string]Cluster
	projects          map[string]domain.Project
	deploymentTargets map[string]domain.DeploymentTarget
	deploymentConfigs map[string]domain.DeploymentConfig
	auditLogs         []domain.AuditLog
	nextAuditID       uint64
}

func NewMemory() *Memory {
	return NewMemoryWithAdminPassword(os.Getenv("CICD_DEMO_ADMIN_PASSWORD"))
}

func NewMemoryWithAdminPassword(adminPassword string) *Memory {
	if strings.TrimSpace(adminPassword) == "" {
		adminPassword = uuid.NewString()
	}
	password, _ := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	now := time.Now().UTC()
	space := domain.Space{ID: "space-lab", Name: "实验室空间", Slug: "lab", Description: "本地演示空间", CreatedAt: now}
	cluster := Cluster{ID: demoClusterIDForSpace(space.ID), SpaceID: space.ID, Name: "演示集群", Provider: "kubernetes", ConnectionMode: ClusterConnectionKubeconfig, Status: "active", CreatedAt: now, UpdatedAt: now}
	uatCluster := Cluster{ID: "demo-cluster-uat", SpaceID: space.ID, Name: "测试集群", Provider: "kubernetes", ConnectionMode: ClusterConnectionKubeconfig, Status: "active", CreatedAt: now, UpdatedAt: now}
	project := domain.Project{
		ID: "reverse-lab", SpaceID: space.ID, Name: "Reverse Lab API", Description: "用于验证从 commit 到 Pod 的完整流程",
		RepositoryID: "demo-repo", RepositoryURL: "https://example.invalid/android-reverse-lab", DefaultBranch: "main",
		ClusterID: "demo-cluster", Namespace: "lab", DeployStrategy: "rolling", Replicas: 2, ContainerPort: 8080,
		CreatedAt: now, UpdatedAt: now,
	}
	target := domain.DeploymentTarget{
		ID: "target-reverse-lab-dev", ProjectID: project.ID, SpaceID: space.ID,
		Name: "开发环境", Environment: "dev", ClusterID: project.ClusterID, Namespace: project.Namespace,
		Replicas: project.Replicas, ContainerPort: project.ContainerPort, DeployStrategy: project.DeployStrategy,
		Stage: DeploymentStageDev, SortOrder: 1, Enabled: true, Status: "active", Health: "unknown", CreatedAt: now, UpdatedAt: now,
	}
	uatTarget := domain.DeploymentTarget{
		ID: "target-reverse-lab-uat", ProjectID: project.ID, SpaceID: space.ID,
		Name: "测试环境", Environment: "uat", ClusterID: uatCluster.ID, Namespace: "uat",
		Replicas: 2, ContainerPort: project.ContainerPort, DeployStrategy: project.DeployStrategy,
		Stage: DeploymentStageUAT, SortOrder: 2, Enabled: true, Status: "active", Health: "unknown", CreatedAt: now, UpdatedAt: now,
	}
	preTarget := domain.DeploymentTarget{
		ID: "target-reverse-lab-pre", ProjectID: project.ID, SpaceID: space.ID,
		Name: "预发布环境", Environment: "pre", ClusterID: uatCluster.ID, Namespace: "pre",
		Replicas: 2, ContainerPort: project.ContainerPort, DeployStrategy: project.DeployStrategy,
		Stage: DeploymentStagePre, SortOrder: 3, Enabled: true, Status: "active", Health: "unknown", CreatedAt: now, UpdatedAt: now,
	}
	prodTarget := domain.DeploymentTarget{
		ID: "target-reverse-lab-prod", ProjectID: project.ID, SpaceID: space.ID,
		Name: "生产环境", Environment: "prod", ClusterID: cluster.ID, Namespace: "prod",
		Replicas: 2, ContainerPort: project.ContainerPort, DeployStrategy: project.DeployStrategy,
		Stage: DeploymentStageProd, SortOrder: 4, Enabled: true, Status: "active", Health: "unknown", CreatedAt: now, UpdatedAt: now,
	}
	return &Memory{
		users:       map[uint64]domain.User{1: {ID: 1, Username: "admin", DisplayName: "平台管理员", PasswordHash: string(password), IsSuperAdmin: true}},
		spaces:      map[string]domain.Space{space.ID: space},
		memberships: map[string]membership{"1\x00" + space.ID: {UserID: 1, SpaceID: space.ID, Role: "owner", JoinedAt: now}},
		clusters:    map[string]Cluster{cluster.ID: cluster, uatCluster.ID: uatCluster},
		projects:    map[string]domain.Project{project.ID: project},
		deploymentTargets: map[string]domain.DeploymentTarget{
			target.ID: target, uatTarget.ID: uatTarget, preTarget.ID: preTarget, prodTarget.ID: prodTarget,
		},
		deploymentConfigs: make(map[string]domain.DeploymentConfig),
	}
}

func (m *Memory) Close() error { return nil }

func (m *Memory) Authenticate(ctx context.Context, username, password string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, user := range m.users {
		if strings.EqualFold(user.Username, strings.TrimSpace(username)) && bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
			return user, nil
		}
	}
	return domain.User{}, ErrInvalidCredentials
}

func (m *Memory) User(ctx context.Context, id uint64) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.users[id]
	if !ok {
		return domain.User{}, ErrNotFound
	}
	return user, nil
}

func (m *Memory) ListSpaces(ctx context.Context, userID uint64) ([]domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	result := make([]domain.Space, 0)
	if user.IsSuperAdmin {
		for _, space := range m.spaces {
			result = append(result, space)
		}
	} else {
		for _, member := range m.memberships {
			if member.UserID == userID {
				if space, exists := m.spaces[member.SpaceID]; exists {
					space.Role = member.Role
					result = append(result, space)
				}
			}
		}
	}
	for i := range result {
		if result[i].Role == "" {
			result[i].Role = "admin"
		}
	}
	return result, nil
}

func (m *Memory) Space(ctx context.Context, userID uint64, spaceID string) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	space, ok := m.spaces[spaceID]
	if !ok {
		return domain.Space{}, ErrNotFound
	}
	user, ok := m.users[userID]
	if !ok {
		return domain.Space{}, ErrNotFound
	}
	if !user.IsSuperAdmin {
		member, allowed := m.memberships[memberKey(userID, spaceID)]
		if !allowed {
			return domain.Space{}, ErrForbidden
		}
		space.Role = member.Role
	} else if space.Role == "" {
		space.Role = "admin"
	}
	return space, nil
}

func (m *Memory) Role(ctx context.Context, userID uint64, spaceID string) (string, error) {
	space, err := m.Space(ctx, userID, spaceID)
	if err != nil {
		return "", err
	}
	if space.Role == "" {
		return "admin", nil
	}
	return space.Role, nil
}

func (m *Memory) CreateSpace(ctx context.Context, userID uint64, input CreateSpaceInput) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Space{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	slug := normalizeSlug(input.Slug)
	if slug == "" {
		slug = normalizeSlug(name)
	}
	if slug == "" {
		slug = fmt.Sprintf("space-%d", time.Now().Unix())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return domain.Space{}, ErrNotFound
	}
	for _, existing := range m.spaces {
		if strings.EqualFold(existing.Slug, slug) {
			return domain.Space{}, ErrConflict
		}
	}
	now := time.Now().UTC()
	space := domain.Space{ID: "space-" + uuid.NewString(), Name: name, Slug: slug, Description: strings.TrimSpace(input.Description), Role: "owner", CreatedAt: now}
	cluster := Cluster{ID: demoClusterIDForSpace(space.ID), SpaceID: space.ID, Name: "演示集群", Provider: "kubernetes", ConnectionMode: ClusterConnectionKubeconfig, Status: "active", CreatedAt: now, UpdatedAt: now}
	m.spaces[space.ID] = space
	m.memberships[memberKey(userID, space.ID)] = membership{UserID: userID, SpaceID: space.ID, Role: "owner", JoinedAt: now}
	m.clusters[cluster.ID] = cluster
	return space, nil
}

func (m *Memory) ListClusters(ctx context.Context, spaceID string) ([]Cluster, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return nil, ErrNotFound
	}
	result := make([]Cluster, 0)
	for _, cluster := range m.clusters {
		if cluster.SpaceID == spaceID {
			result = append(result, cluster)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (m *Memory) GetCluster(ctx context.Context, spaceID, clusterID string) (Cluster, error) {
	if err := ctx.Err(); err != nil {
		return Cluster{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	cluster, ok := m.clusters[clusterID]
	if !ok || cluster.SpaceID != spaceID {
		return Cluster{}, ErrNotFound
	}
	return cluster, nil
}

func (m *Memory) CreateCluster(ctx context.Context, spaceID string, input CreateClusterInput) (Cluster, error) {
	if err := ctx.Err(); err != nil {
		return Cluster{}, err
	}
	cluster, err := newCluster(spaceID, input)
	if err != nil {
		return Cluster{}, err
	}
	now := time.Now().UTC()
	cluster.CreatedAt = now
	cluster.UpdatedAt = now
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return Cluster{}, ErrNotFound
	}
	if _, ok := m.clusters[cluster.ID]; ok {
		return Cluster{}, ErrConflict
	}
	for _, existing := range m.clusters {
		if existing.SpaceID == spaceID && strings.EqualFold(existing.Name, cluster.Name) {
			return Cluster{}, ErrConflict
		}
	}
	m.clusters[cluster.ID] = cluster
	return cluster, nil
}

func (m *Memory) UpdateCluster(ctx context.Context, spaceID, clusterID string, input UpdateClusterInput) (Cluster, error) {
	if err := ctx.Err(); err != nil {
		return Cluster{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cluster, ok := m.clusters[clusterID]
	if !ok || cluster.SpaceID != spaceID {
		return Cluster{}, ErrNotFound
	}
	updated, err := updateCluster(cluster, input)
	if err != nil {
		return Cluster{}, err
	}
	for id, existing := range m.clusters {
		if id != clusterID && existing.SpaceID == spaceID && strings.EqualFold(existing.Name, updated.Name) {
			return Cluster{}, ErrConflict
		}
	}
	updated.UpdatedAt = time.Now().UTC()
	m.clusters[clusterID] = updated
	return updated, nil
}

func (m *Memory) ListProjects(ctx context.Context, spaceID string) ([]domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return nil, ErrNotFound
	}
	result := make([]domain.Project, 0)
	for _, project := range m.projects {
		if project.SpaceID == spaceID {
			result = append(result, project)
		}
	}
	return result, nil
}

func (m *Memory) GetProject(ctx context.Context, spaceID, projectID string) (domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return domain.Project{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.Project{}, ErrNotFound
	}
	return project, nil
}

func (m *Memory) CreateProject(ctx context.Context, spaceID string, input CreateProjectInput) (domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return domain.Project{}, err
	}
	name := strings.TrimSpace(input.Name)
	repo := strings.TrimSpace(input.RepositoryURL)
	if name == "" || repo == "" {
		return domain.Project{}, fmt.Errorf("%w: name and repository_url are required", ErrInvalidInput)
	}
	branch := strings.TrimSpace(input.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	cluster := normalizeClusterID(spaceID, input.ClusterID)
	namespace := strings.TrimSpace(input.Namespace)
	if namespace == "" {
		namespace = "lab"
	}
	strategy := strings.TrimSpace(input.DeployStrategy)
	if strategy == "" {
		strategy = "rolling"
	}
	replicas := input.Replicas
	if replicas <= 0 {
		replicas = 1
	}
	port := input.ContainerPort
	if port <= 0 {
		port = 8080
	}
	repositoryID := strings.TrimSpace(input.RepositoryID)
	if repositoryID == "" {
		repositoryID = "demo-repo"
	}
	now := time.Now().UTC()
	project := domain.Project{ID: uuid.NewString(), SpaceID: spaceID, Name: name, Description: strings.TrimSpace(input.Description), RepositoryID: repositoryID, RepositoryURL: repo, DefaultBranch: branch, ClusterID: cluster, Namespace: namespace, DeployStrategy: strategy, Replicas: replicas, ContainerPort: port, CreatedAt: now, UpdatedAt: now}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return domain.Project{}, ErrNotFound
	}
	if selected, ok := m.clusters[cluster]; !ok || selected.SpaceID != spaceID {
		return domain.Project{}, ErrNotFound
	}
	for _, existing := range m.projects {
		if existing.SpaceID == spaceID && strings.EqualFold(existing.Name, name) {
			return domain.Project{}, ErrConflict
		}
	}
	m.projects[project.ID] = project
	target, err := newDeploymentTarget(spaceID, project.ID, CreateDeploymentTargetInput{
		Name: "开发环境", Environment: "dev", ClusterID: project.ClusterID, Namespace: project.Namespace,
		Stage: DeploymentStageDev, SortOrder: 1, Replicas: project.Replicas, ContainerPort: project.ContainerPort, DeployStrategy: project.DeployStrategy,
	}, project)
	if err != nil {
		delete(m.projects, project.ID)
		return domain.Project{}, err
	}
	m.deploymentTargets[target.ID] = target
	return project, nil
}

func (m *Memory) UpdateProject(ctx context.Context, spaceID, projectID string, input UpdateProjectInput) (domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return domain.Project{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.Project{}, ErrNotFound
	}
	if input.RepositoryURL != nil {
		repositoryURL := strings.TrimSpace(*input.RepositoryURL)
		if repositoryURL == "" {
			return domain.Project{}, fmt.Errorf("%w: repository_url is required", ErrInvalidInput)
		}
		project.RepositoryURL = repositoryURL
	}
	if input.RepositoryID != nil && strings.TrimSpace(*input.RepositoryID) != "" {
		project.RepositoryID = strings.TrimSpace(*input.RepositoryID)
	}
	if input.Description != nil {
		project.Description = strings.TrimSpace(*input.Description)
	}
	if input.DefaultBranch != nil && strings.TrimSpace(*input.DefaultBranch) != "" {
		project.DefaultBranch = strings.TrimSpace(*input.DefaultBranch)
	}
	if input.ClusterID != nil && strings.TrimSpace(*input.ClusterID) != "" {
		clusterID := normalizeClusterID(spaceID, *input.ClusterID)
		if selected, ok := m.clusters[clusterID]; !ok || selected.SpaceID != spaceID {
			return domain.Project{}, ErrNotFound
		}
		project.ClusterID = clusterID
	}
	if input.Namespace != nil && strings.TrimSpace(*input.Namespace) != "" {
		project.Namespace = strings.TrimSpace(*input.Namespace)
	}
	if input.Replicas != nil && *input.Replicas > 0 {
		project.Replicas = *input.Replicas
	}
	if input.ContainerPort != nil && *input.ContainerPort > 0 {
		project.ContainerPort = *input.ContainerPort
	}
	project.UpdatedAt = time.Now().UTC()
	m.projects[projectID] = project
	return project, nil
}

func (m *Memory) ListDeploymentTargets(ctx context.Context, spaceID, projectID string) ([]domain.DeploymentTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return nil, ErrNotFound
	}
	result := make([]domain.DeploymentTarget, 0)
	for _, target := range m.deploymentTargets {
		if target.ProjectID == projectID && target.SpaceID == spaceID {
			result = append(result, cloneDeploymentTarget(target))
		}
	}
	if len(result) == 0 {
		result = append(result, LegacyDeploymentTarget(project))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortOrder == result[j].SortOrder {
			if result[i].Environment == result[j].Environment {
				return result[i].Name < result[j].Name
			}
			return result[i].Environment < result[j].Environment
		}
		return result[i].SortOrder < result[j].SortOrder
	})
	return result, nil
}

func (m *Memory) GetDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string) (domain.DeploymentTarget, error) {
	if err := ctx.Err(); err != nil {
		return domain.DeploymentTarget{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.DeploymentTarget{}, ErrNotFound
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		var first *domain.DeploymentTarget
		for _, candidate := range m.deploymentTargets {
			if candidate.ProjectID != projectID || candidate.SpaceID != spaceID {
				continue
			}
			if first == nil || candidate.SortOrder < first.SortOrder || (candidate.SortOrder == first.SortOrder && candidate.Name < first.Name) {
				copy := candidate
				first = &copy
			}
		}
		if first != nil {
			return cloneDeploymentTarget(*first), nil
		}
		return LegacyDeploymentTarget(project), nil
	}
	target, ok := m.deploymentTargets[targetID]
	if !ok || target.ProjectID != projectID || target.SpaceID != spaceID {
		if targetID == "legacy-"+projectID && !m.hasProjectTargetsLocked(spaceID, projectID) {
			return LegacyDeploymentTarget(project), nil
		}
		return domain.DeploymentTarget{}, ErrNotFound
	}
	return cloneDeploymentTarget(target), nil
}

func (m *Memory) CreateDeploymentTarget(ctx context.Context, spaceID, projectID string, input CreateDeploymentTargetInput) (domain.DeploymentTarget, error) {
	if err := ctx.Err(); err != nil {
		return domain.DeploymentTarget{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.DeploymentTarget{}, ErrNotFound
	}
	clusterID := normalizeClusterID(spaceID, input.ClusterID)
	if cluster, exists := m.clusters[clusterID]; !exists || cluster.SpaceID != spaceID {
		return domain.DeploymentTarget{}, ErrNotFound
	}
	input.ClusterID = clusterID
	count := 0
	maxSortOrder := 0
	for _, existing := range m.deploymentTargets {
		if existing.ProjectID != projectID || existing.SpaceID != spaceID {
			continue
		}
		count++
		if existing.SortOrder > maxSortOrder {
			maxSortOrder = existing.SortOrder
		}
	}
	if input.SortOrder <= 0 {
		input.SortOrder = maxSortOrder + 1
	}
	if count == 0 {
		input.Stage = DeploymentStageDev
		input.SortOrder = 1
	}
	target, err := newDeploymentTarget(spaceID, projectID, input, project)
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	for _, existing := range m.deploymentTargets {
		if existing.ProjectID != projectID || existing.SpaceID != spaceID {
			continue
		}
		if strings.EqualFold(existing.Name, target.Name) || strings.EqualFold(existing.Environment, target.Environment) {
			return domain.DeploymentTarget{}, ErrConflict
		}
	}
	m.deploymentTargets[target.ID] = target
	return cloneDeploymentTarget(target), nil
}

func (m *Memory) UpdateDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string, input UpdateDeploymentTargetInput) (domain.DeploymentTarget, error) {
	if err := ctx.Err(); err != nil {
		return domain.DeploymentTarget{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.DeploymentTarget{}, ErrNotFound
	}
	target, ok := m.deploymentTargets[targetID]
	if !ok || target.ProjectID != projectID || target.SpaceID != spaceID {
		return domain.DeploymentTarget{}, ErrNotFound
	}
	if input.ClusterID != nil {
		input.ClusterID = stringPtr(normalizeClusterID(spaceID, *input.ClusterID))
		cluster, exists := m.clusters[*input.ClusterID]
		if !exists || cluster.SpaceID != spaceID {
			return domain.DeploymentTarget{}, ErrNotFound
		}
	}
	updated, err := updateDeploymentTarget(target, input)
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	for id, existing := range m.deploymentTargets {
		if id == targetID || existing.ProjectID != projectID || existing.SpaceID != spaceID {
			continue
		}
		if strings.EqualFold(existing.Name, updated.Name) || strings.EqualFold(existing.Environment, updated.Environment) {
			return domain.DeploymentTarget{}, ErrConflict
		}
	}
	if target.Stage == DeploymentStageDev && updated.Stage != DeploymentStageDev && !m.hasOtherStageLocked(projectID, targetID, DeploymentStageDev) {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: 项目必须保留 DEV 环境", ErrConflict)
	}
	if target.Stage != DeploymentStageDev && updated.Stage == DeploymentStageDev && m.hasOtherStageLocked(projectID, targetID, DeploymentStageDev) {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: 一个项目只能有一个 DEV 环境", ErrConflict)
	}
	m.deploymentTargets[targetID] = updated
	return cloneDeploymentTarget(updated), nil
}

func (m *Memory) DeleteDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return ErrNotFound
	}
	target, ok := m.deploymentTargets[targetID]
	if !ok || target.ProjectID != projectID || target.SpaceID != spaceID {
		return ErrNotFound
	}
	count := 0
	for _, existing := range m.deploymentTargets {
		if existing.ProjectID == projectID && existing.SpaceID == spaceID {
			count++
		}
	}
	if count <= 1 {
		return fmt.Errorf("%w: 项目至少需要保留一个部署目标", ErrConflict)
	}
	if target.Stage == DeploymentStageDev && !m.hasOtherStageLocked(projectID, targetID, DeploymentStageDev) {
		return fmt.Errorf("%w: 项目必须保留 DEV 环境", ErrConflict)
	}
	delete(m.deploymentTargets, targetID)
	return nil
}

func (m *Memory) hasOtherStageLocked(projectID, excludedID, stage string) bool {
	for id, target := range m.deploymentTargets {
		if id != excludedID && target.ProjectID == projectID && target.Stage == stage {
			return true
		}
	}
	return false
}

func (m *Memory) hasProjectTargetsLocked(spaceID, projectID string) bool {
	for _, target := range m.deploymentTargets {
		if target.SpaceID == spaceID && target.ProjectID == projectID {
			return true
		}
	}
	return false
}

func cloneDeploymentTarget(value domain.DeploymentTarget) domain.DeploymentTarget { return value }

func (m *Memory) GetDeploymentConfig(ctx context.Context, spaceID, projectID string) (domain.DeploymentConfig, error) {
	if err := ctx.Err(); err != nil {
		return domain.DeploymentConfig{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.DeploymentConfig{}, ErrNotFound
	}
	config, ok := m.deploymentConfigs[projectID]
	if !ok {
		return domain.DeploymentConfig{}, ErrNotFound
	}
	return cloneDeploymentConfig(config), nil
}

func (m *Memory) SaveDeploymentConfig(ctx context.Context, spaceID, projectID string, input SaveDeploymentConfigInput) (domain.DeploymentConfig, error) {
	if err := ctx.Err(); err != nil {
		return domain.DeploymentConfig{}, err
	}
	manifest := strings.TrimSpace(input.Manifest)
	if manifest == "" {
		return domain.DeploymentConfig{}, fmt.Errorf("%w: deployment manifest is required", ErrInvalidInput)
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "yml" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" {
		return domain.DeploymentConfig{}, fmt.Errorf("%w: deployment format must be yaml or json", ErrInvalidInput)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project, ok := m.projects[projectID]
	if !ok || project.SpaceID != spaceID {
		return domain.DeploymentConfig{}, ErrNotFound
	}
	previous := m.deploymentConfigs[projectID]
	version := previous.Version + 1
	now := time.Now().UTC()
	config := domain.DeploymentConfig{ProjectID: projectID, Namespace: project.Namespace, Format: format, Manifest: manifest, Version: version, UpdatedAt: now}
	m.deploymentConfigs[projectID] = config
	return cloneDeploymentConfig(config), nil
}

func cloneDeploymentConfig(value domain.DeploymentConfig) domain.DeploymentConfig {
	value.Resources = append([]domain.DeploymentResource(nil), value.Resources...)
	return value
}

func (m *Memory) AppendAuditLog(ctx context.Context, entry domain.AuditLog) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spaces[entry.SpaceID]; !ok {
		return ErrNotFound
	}
	m.nextAuditID++
	entry.ID = m.nextAuditID
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	m.auditLogs = append(m.auditLogs, entry)
	return nil
}

func (m *Memory) ListAuditLogs(ctx context.Context, spaceID string, limit int) ([]domain.AuditLog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.spaces[spaceID]; !ok {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	result := make([]domain.AuditLog, 0, minInt(limit, len(m.auditLogs)))
	for index := len(m.auditLogs) - 1; index >= 0 && len(result) < limit; index-- {
		if m.auditLogs[index].SpaceID == spaceID {
			result = append(result, m.auditLogs[index])
		}
	}
	return result, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func memberKey(userID uint64, spaceID string) string { return fmt.Sprintf("%d\x00%s", userID, spaceID) }

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		case unicode.IsLetter(char) && char < 128:
			builder.WriteRune(char)
			lastDash = false
		case !lastDash && builder.Len() > 0:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
