package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

type MySQL struct{ db *gorm.DB }

const (
	defaultAdminUsername    = "admin"
	defaultAdminDisplayName = "管理员"
	defaultAdminPassword    = "ttp"
)

type userRow struct {
	ID           uint64 `gorm:"primaryKey"`
	Username     string `gorm:"size:100;uniqueIndex;not null"`
	DisplayName  string `gorm:"size:120;not null"`
	PasswordHash string `gorm:"size:255;not null"`
	IsSuperAdmin bool   `gorm:"not null;default:false"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (userRow) TableName() string { return "users" }

type spaceRow struct {
	ID          string `gorm:"size:64;primaryKey"`
	Name        string `gorm:"size:120;not null"`
	Slug        string `gorm:"size:80;uniqueIndex;not null"`
	Description string `gorm:"size:255"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (spaceRow) TableName() string { return "spaces" }

type memberRow struct {
	UserID    uint64 `gorm:"primaryKey"`
	SpaceID   string `gorm:"size:64;primaryKey"`
	Role      string `gorm:"size:32;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (memberRow) TableName() string { return "space_members" }

type clusterRow struct {
	ID             string `gorm:"size:64;primaryKey"`
	SpaceID        string `gorm:"size:64;not null"`
	Name           string `gorm:"size:120;not null"`
	Provider       string `gorm:"size:32;not null;default:kubernetes"`
	APIEndpoint    string `gorm:"size:500;not null;default:''"`
	KubeContext    string `gorm:"size:255;not null;default:''"`
	ConnectionMode string `gorm:"size:32;not null;default:kubeconfig"`
	KubeconfigPath string `gorm:"size:500;not null;default:''"`
	Status         string `gorm:"size:32;not null;default:active"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (clusterRow) TableName() string { return "clusters" }

type projectRow struct {
	ID              string `gorm:"size:64;primaryKey"`
	SpaceID         string `gorm:"size:64;index;not null"`
	Name            string `gorm:"size:120;not null"`
	Description     string `gorm:"size:255"`
	RepositoryID    string `gorm:"size:255;not null"`
	RepositoryURL   string `gorm:"size:500;not null"`
	DefaultBranch   string `gorm:"size:120;not null"`
	ClusterID       string `gorm:"size:64;not null"`
	Namespace       string `gorm:"size:120;not null"`
	DeployStrategy  string `gorm:"size:32;not null"`
	Replicas        int    `gorm:"not null;default:1"`
	ContainerPort   int    `gorm:"not null;default:8080"`
	ImageRepository string `gorm:"size:500"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (projectRow) TableName() string { return "projects" }

type projectGitCredentialRow struct {
	ProjectID       string `gorm:"size:64;primaryKey"`
	SpaceID         string `gorm:"size:64;index;not null"`
	Provider        string `gorm:"size:32;not null"`
	Username        string `gorm:"size:120;not null"`
	TokenCiphertext string `gorm:"type:longtext;not null"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (projectGitCredentialRow) TableName() string { return "project_git_credentials" }

type deploymentTargetRow struct {
	ID             string `gorm:"size:64;primaryKey"`
	SpaceID        string `gorm:"size:64;index;not null"`
	ProjectID      string `gorm:"size:64;index;not null"`
	Name           string `gorm:"size:120;not null"`
	Environment    string `gorm:"size:64;not null"`
	Stage          string `gorm:"size:16;not null;default:custom"`
	SortOrder      int    `gorm:"not null;default:1;index"`
	ClusterID      string `gorm:"size:64;not null"`
	Namespace      string `gorm:"size:120;not null"`
	Replicas       int    `gorm:"not null;default:1"`
	ContainerPort  int    `gorm:"not null;default:8080"`
	DeployStrategy string `gorm:"size:32;not null;default:rolling"`
	IsDefault      bool   `gorm:"not null;default:false;index"`
	Enabled        bool   `gorm:"not null;default:true"`
	Status         string `gorm:"size:32;not null;default:active"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (deploymentTargetRow) TableName() string { return "project_deployment_targets" }

type deploymentConfigRow struct {
	ProjectID string `gorm:"size:64;primaryKey"`
	SpaceID   string `gorm:"size:64;index;not null"`
	Namespace string `gorm:"size:120;not null"`
	Format    string `gorm:"size:16;not null;default:yaml"`
	Manifest  string `gorm:"type:longtext;not null"`
	Version   int    `gorm:"not null;default:1"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (deploymentConfigRow) TableName() string { return "project_deployment_configs" }

type auditLogRow struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	SpaceID    *string   `gorm:"size:64;index"`
	UserID     *uint64   `gorm:"index"`
	Action     string    `gorm:"size:80;not null"`
	TargetType string    `gorm:"size:80;not null"`
	TargetID   string    `gorm:"size:128;not null"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

func (auditLogRow) TableName() string { return "audit_logs" }

func NewMySQL(ctx context.Context, dsn string) (*MySQL, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql dsn is required")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	store := &MySQL{db: db}
	if err := db.AutoMigrate(&userRow{}, &spaceRow{}, &memberRow{}, &clusterRow{}, &projectRow{}, &projectGitCredentialRow{}, &deploymentTargetRow{}, &deploymentConfigRow{}, &auditLogRow{}, &abExperimentRow{}, &releaseRecordRow{}, &releaseCommitRecordRow{}, &releaseTargetRecordRow{}, &releaseExecutionLogRecordRow{}); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := store.dropRemovedSchema(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := store.migrateDeploymentTargetMetadata(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := store.seed(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := store.seedLegacyDeploymentTargets(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return store, nil
}

// dropRemovedSchema handles destructive migrations that AutoMigrate cannot.
func (s *MySQL) dropRemovedSchema(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Migrator().DropTable("project_build_configs"); err != nil {
		return fmt.Errorf("drop removed project build configuration table: %w", err)
	}
	return nil
}

func (s *MySQL) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

func (s *MySQL) Authenticate(ctx context.Context, username, password string) (domain.User, error) {
	var row userRow
	if err := s.db.WithContext(ctx).Where("username = ?", strings.TrimSpace(username)).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(password)) != nil {
		return domain.User{}, ErrInvalidCredentials
	}
	return toUser(row), nil
}

func (s *MySQL) User(ctx context.Context, id uint64) (domain.User, error) {
	var row userRow
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return domain.User{}, mapDBError(err)
	}
	return toUser(row), nil
}

func (s *MySQL) ListSpaces(ctx context.Context, userID uint64) ([]domain.Space, error) {
	user, err := s.User(ctx, userID)
	if err != nil {
		return nil, err
	}
	var rows []spaceRow
	query := s.db.WithContext(ctx)
	if !user.IsSuperAdmin {
		query = query.Joins("JOIN space_members ON space_members.space_id = spaces.id").Where("space_members.user_id = ?", userID)
	}
	if err := query.Order("spaces.name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.Space, 0, len(rows))
	for _, row := range rows {
		item := toSpace(row)
		item.Role, _ = s.Role(ctx, userID, item.ID)
		result = append(result, item)
	}
	return result, nil
}

func (s *MySQL) Space(ctx context.Context, userID uint64, spaceID string) (domain.Space, error) {
	var row spaceRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", spaceID).Error; err != nil {
		return domain.Space{}, mapDBError(err)
	}
	if _, err := s.Role(ctx, userID, spaceID); err != nil {
		return domain.Space{}, err
	}
	item := toSpace(row)
	item.Role, _ = s.Role(ctx, userID, spaceID)
	return item, nil
}

func (s *MySQL) Role(ctx context.Context, userID uint64, spaceID string) (string, error) {
	user, err := s.User(ctx, userID)
	if err != nil {
		return "", err
	}
	if user.IsSuperAdmin {
		return "admin", nil
	}
	var row memberRow
	if err := s.db.WithContext(ctx).Where("user_id = ? AND space_id = ?", userID, spaceID).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", ErrForbidden
		}
		return "", err
	}
	return row.Role, nil
}

func (s *MySQL) CreateSpace(ctx context.Context, userID uint64, input CreateSpaceInput) (domain.Space, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Space{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	slug := strings.Trim(strings.ToLower(strings.TrimSpace(input.Slug)), "-")
	if slug == "" {
		slug = "space-" + uuid.NewString()[:8]
	}
	space := spaceRow{ID: "space-" + uuid.NewString(), Name: name, Slug: slug, Description: strings.TrimSpace(input.Description)}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&space).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return ErrConflict
			}
			return err
		}
		if err := tx.Create(&memberRow{UserID: userID, SpaceID: space.ID, Role: "owner"}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.Space{}, err
	}
	item := toSpace(space)
	item.Role = "owner"
	return item, nil
}

func (s *MySQL) ListClusters(ctx context.Context, spaceID string) ([]Cluster, error) {
	var rows []clusterRow
	if err := s.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]Cluster, 0, len(rows))
	for _, row := range rows {
		result = append(result, toCluster(row))
	}
	return result, nil
}

func (s *MySQL) GetCluster(ctx context.Context, spaceID, clusterID string) (Cluster, error) {
	var row clusterRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ?", clusterID, spaceID).First(&row).Error; err != nil {
		return Cluster{}, mapDBError(err)
	}
	return toCluster(row), nil
}

func (s *MySQL) CreateCluster(ctx context.Context, spaceID string, input CreateClusterInput) (Cluster, error) {
	cluster, err := newCluster(spaceID, input)
	if err != nil {
		return Cluster{}, err
	}
	now := time.Now().UTC()
	row := clusterRow{
		ID: cluster.ID, SpaceID: spaceID, Name: cluster.Name, Provider: cluster.Provider,
		APIEndpoint: cluster.APIEndpoint, KubeContext: cluster.KubeContext,
		ConnectionMode: cluster.ConnectionMode, KubeconfigPath: cluster.KubeconfigPath,
		Status: cluster.Status, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return Cluster{}, ErrConflict
		}
		return Cluster{}, err
	}
	return toCluster(row), nil
}

func (s *MySQL) UpdateCluster(ctx context.Context, spaceID, clusterID string, input UpdateClusterInput) (Cluster, error) {
	current, err := s.GetCluster(ctx, spaceID, clusterID)
	if err != nil {
		return Cluster{}, err
	}
	updated, err := updateCluster(current, input)
	if err != nil {
		return Cluster{}, err
	}
	updates := map[string]any{
		"name":            updated.Name,
		"api_endpoint":    updated.APIEndpoint,
		"kube_context":    updated.KubeContext,
		"connection_mode": updated.ConnectionMode,
		"kubeconfig_path": updated.KubeconfigPath,
		"status":          updated.Status,
	}
	if err := s.db.WithContext(ctx).Model(&clusterRow{}).Where("id = ? AND space_id = ?", clusterID, spaceID).Updates(updates).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return Cluster{}, ErrConflict
		}
		return Cluster{}, err
	}
	return s.GetCluster(ctx, spaceID, clusterID)
}

func (s *MySQL) ListProjects(ctx context.Context, spaceID string) ([]domain.Project, error) {
	var rows []projectRow
	if err := s.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.Project, 0, len(rows))
	for _, row := range rows {
		result = append(result, toProject(row))
	}
	return result, nil
}

func (s *MySQL) GetProject(ctx context.Context, spaceID, projectID string) (domain.Project, error) {
	var row projectRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ?", projectID, spaceID).First(&row).Error; err != nil {
		return domain.Project{}, mapDBError(err)
	}
	return toProject(row), nil
}

func (s *MySQL) CreateProject(ctx context.Context, spaceID string, input CreateProjectInput) (domain.Project, error) {
	var err error
	input, err = normalizeCreateProjectInput(input)
	if err != nil {
		return domain.Project{}, err
	}
	name := input.Name
	repo := input.RepositoryURL
	branch := input.DefaultBranch
	cluster := normalizeClusterID(spaceID, input.ClusterID)
	if cluster == "" {
		return domain.Project{}, fmt.Errorf("%w: cluster_id is required", ErrInvalidInput)
	}
	namespace := input.Namespace
	strategy := input.DeployStrategy
	replicas := input.Replicas
	port := input.ContainerPort
	repoID := strings.TrimSpace(input.RepositoryID)
	if repoID == "" {
		repoID = "repo-" + uuid.NewString()
	}
	if err := s.clusterExists(ctx, spaceID, cluster); err != nil {
		return domain.Project{}, err
	}
	projectID := strings.TrimSpace(input.ID)
	if projectID == "" {
		projectID = uuid.NewString()
	}
	row := projectRow{ID: projectID, SpaceID: spaceID, Name: name, Description: strings.TrimSpace(input.Description), RepositoryID: repoID, RepositoryURL: repo, DefaultBranch: branch, ClusterID: cluster, Namespace: namespace, DeployStrategy: strategy, Replicas: replicas, ContainerPort: port, ImageRepository: strings.TrimSpace(input.ImageRepository)}
	target := domain.DeploymentTarget{
		ID: "target-" + uuid.NewString(), ProjectID: row.ID, SpaceID: spaceID,
		Name: defaultTargetName, Environment: defaultTargetEnvironment, Stage: DeploymentStageDev, SortOrder: 1,
		ClusterID: cluster, Namespace: namespace, Replicas: replicas, ContainerPort: port,
		DeployStrategy: strategy, Enabled: true, Status: "active", Health: "unknown",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return ErrConflict
			}
			return err
		}
		return tx.Create(&deploymentTargetRow{
			ID: target.ID, SpaceID: target.SpaceID, ProjectID: target.ProjectID, Name: target.Name,
			Environment: target.Environment, Stage: target.Stage, SortOrder: target.SortOrder,
			ClusterID: target.ClusterID, Namespace: target.Namespace, Replicas: target.Replicas,
			ContainerPort: target.ContainerPort, DeployStrategy: target.DeployStrategy,
			Enabled: target.Enabled, Status: target.Status, CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt,
		}).Error
	}); err != nil {
		return domain.Project{}, err
	}
	return toProject(row), nil
}

func (s *MySQL) UpdateProject(ctx context.Context, spaceID, projectID string, input UpdateProjectInput) (domain.Project, error) {
	if err := validateProjectUpdateInput(input); err != nil {
		return domain.Project{}, err
	}
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return domain.Project{}, err
	}
	updates := map[string]any{}
	if input.RepositoryURL != nil {
		repositoryURL := strings.TrimSpace(*input.RepositoryURL)
		if repositoryURL == "" {
			return domain.Project{}, fmt.Errorf("%w: repository_url is required", ErrInvalidInput)
		}
		updates["repository_url"] = repositoryURL
	}
	if input.RepositoryID != nil && strings.TrimSpace(*input.RepositoryID) != "" {
		updates["repository_id"] = strings.TrimSpace(*input.RepositoryID)
	}
	if input.Description != nil {
		updates["description"] = strings.TrimSpace(*input.Description)
	}
	if input.DefaultBranch != nil {
		updates["default_branch"] = strings.TrimSpace(*input.DefaultBranch)
	}
	if input.ClusterID != nil {
		clusterID := normalizeClusterID(spaceID, *input.ClusterID)
		if err := s.clusterExists(ctx, spaceID, clusterID); err != nil {
			return domain.Project{}, err
		}
		updates["cluster_id"] = clusterID
	}
	if input.Namespace != nil {
		updates["namespace"] = strings.TrimSpace(*input.Namespace)
	}
	if input.Replicas != nil {
		updates["replicas"] = *input.Replicas
	}
	if input.ContainerPort != nil {
		updates["container_port"] = *input.ContainerPort
	}
	if input.ImageRepository != nil {
		updates["image_repository"] = strings.TrimSpace(*input.ImageRepository)
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&projectRow{}).Where("id = ? AND space_id = ?", projectID, spaceID).Updates(updates).Error; err != nil {
			return domain.Project{}, err
		}
	}
	return s.GetProject(ctx, spaceID, projectID)
}

func (s *MySQL) GetProjectGitCredential(ctx context.Context, spaceID, projectID string) (ProjectGitCredential, error) {
	var row projectGitCredentialRow
	if err := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID).First(&row).Error; err != nil {
		return ProjectGitCredential{}, mapDBError(err)
	}
	return toProjectGitCredential(row), nil
}

func (s *MySQL) SaveProjectGitCredential(ctx context.Context, spaceID, projectID string, input SaveProjectGitCredentialInput) (ProjectGitCredential, error) {
	if strings.TrimSpace(input.Provider) == "" || strings.TrimSpace(input.Username) == "" || strings.TrimSpace(input.TokenCiphertext) == "" {
		return ProjectGitCredential{}, fmt.Errorf("%w: git credential fields are required", ErrInvalidInput)
	}
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return ProjectGitCredential{}, err
	}
	now := time.Now().UTC()
	row := projectGitCredentialRow{ProjectID: projectID, SpaceID: spaceID, Provider: strings.ToLower(strings.TrimSpace(input.Provider)), Username: strings.TrimSpace(input.Username), TokenCiphertext: input.TokenCiphertext, CreatedAt: now, UpdatedAt: now}
	var existing projectGitCredentialRow
	err := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID).First(&existing).Error
	switch {
	case err == nil:
		if err := s.db.WithContext(ctx).Model(&projectGitCredentialRow{}).Where("space_id = ? AND project_id = ?", spaceID, projectID).Updates(map[string]any{"provider": row.Provider, "username": row.Username, "token_ciphertext": row.TokenCiphertext, "updated_at": now}).Error; err != nil {
			return ProjectGitCredential{}, err
		}
	case err == gorm.ErrRecordNotFound:
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return ProjectGitCredential{}, err
		}
	default:
		return ProjectGitCredential{}, err
	}
	return s.GetProjectGitCredential(ctx, spaceID, projectID)
}

func (s *MySQL) DeleteProjectGitCredential(ctx context.Context, spaceID, projectID string) error {
	result := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID).Delete(&projectGitCredentialRow{})
	return result.Error
}

func (s *MySQL) ListDeploymentTargets(ctx context.Context, spaceID, projectID string) ([]domain.DeploymentTarget, error) {
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return nil, err
	}
	var rows []deploymentTargetRow
	if err := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID).Order("sort_order ASC, created_at ASC, name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.DeploymentTarget, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDeploymentTarget(row))
	}
	return result, nil
}

func (s *MySQL) GetDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string) (domain.DeploymentTarget, error) {
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return domain.DeploymentTarget{}, err
	}
	var row deploymentTargetRow
	query := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID)
	if strings.TrimSpace(targetID) == "" {
		query = query.Order("sort_order ASC, created_at ASC, name ASC")
	} else {
		query = query.Where("id = ?", strings.TrimSpace(targetID))
	}
	if err := query.First(&row).Error; err != nil {
		return domain.DeploymentTarget{}, mapDBError(err)
	}
	return toDeploymentTarget(row), nil
}

func (s *MySQL) CreateDeploymentTarget(ctx context.Context, spaceID, projectID string, input CreateDeploymentTargetInput) (domain.DeploymentTarget, error) {
	project, err := s.GetProject(ctx, spaceID, projectID)
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	clusterID := normalizeClusterID(spaceID, input.ClusterID)
	if err := s.clusterExists(ctx, spaceID, clusterID); err != nil {
		return domain.DeploymentTarget{}, err
	}
	input.ClusterID = clusterID
	var count int64
	if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ?", spaceID, projectID).Count(&count).Error; err != nil {
		return domain.DeploymentTarget{}, err
	}
	if input.SortOrder <= 0 {
		var maxSortOrder int
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ?", spaceID, projectID).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSortOrder).Error; err != nil {
			return domain.DeploymentTarget{}, err
		}
		input.SortOrder = maxSortOrder + 1
	}
	if count == 0 {
		input.Stage = DeploymentStageDev
		input.SortOrder = 1
	}
	if input.Stage == "" && count > 0 {
		input.Stage = DeploymentStageCustom
	}
	target, err := newDeploymentTarget(spaceID, projectID, input, project)
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	var duplicate int64
	if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ? AND (LOWER(name) = LOWER(?) OR LOWER(environment) = LOWER(?))", spaceID, projectID, target.Name, target.Environment).Count(&duplicate).Error; err != nil {
		return domain.DeploymentTarget{}, err
	}
	if duplicate > 0 {
		return domain.DeploymentTarget{}, ErrConflict
	}
	if target.Stage == DeploymentStageDev {
		var devCount int64
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ? AND stage = ?", spaceID, projectID, DeploymentStageDev).Count(&devCount).Error; err != nil {
			return domain.DeploymentTarget{}, err
		}
		if devCount > 0 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: 一个项目只能有一个 DEV 环境", ErrConflict)
		}
	}
	row := fromDeploymentTarget(target)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return ErrConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	return s.GetDeploymentTarget(ctx, spaceID, projectID, target.ID)
}

func (s *MySQL) UpdateDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string, input UpdateDeploymentTargetInput) (domain.DeploymentTarget, error) {
	current, err := s.GetDeploymentTarget(ctx, spaceID, projectID, targetID)
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	if input.ClusterID != nil {
		clusterID := normalizeClusterID(spaceID, *input.ClusterID)
		if err := s.clusterExists(ctx, spaceID, clusterID); err != nil {
			return domain.DeploymentTarget{}, err
		}
		input.ClusterID = &clusterID
	}
	updated, err := updateDeploymentTarget(current, input)
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	var duplicate int64
	if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ? AND id <> ? AND (LOWER(name) = LOWER(?) OR LOWER(environment) = LOWER(?))", spaceID, projectID, targetID, updated.Name, updated.Environment).Count(&duplicate).Error; err != nil {
		return domain.DeploymentTarget{}, err
	}
	if duplicate > 0 {
		return domain.DeploymentTarget{}, ErrConflict
	}
	var otherDevCount int64
	if updated.Stage == DeploymentStageDev {
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ? AND id <> ? AND stage = ?", spaceID, projectID, targetID, DeploymentStageDev).Count(&otherDevCount).Error; err != nil {
			return domain.DeploymentTarget{}, err
		}
		if otherDevCount > 0 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: 一个项目只能有一个 DEV 环境", ErrConflict)
		}
	}
	if current.Stage == DeploymentStageDev && updated.Stage != DeploymentStageDev {
		var remainingDev int64
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ? AND id <> ? AND stage = ?", spaceID, projectID, targetID, DeploymentStageDev).Count(&remainingDev).Error; err != nil {
			return domain.DeploymentTarget{}, err
		}
		if remainingDev == 0 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: 项目必须保留 DEV 环境", ErrConflict)
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&deploymentTargetRow{}).Where("id = ? AND space_id = ? AND project_id = ?", targetID, spaceID, projectID).Updates(map[string]any{
			"name": updated.Name, "environment": updated.Environment, "stage": updated.Stage, "sort_order": updated.SortOrder, "cluster_id": updated.ClusterID, "namespace": updated.Namespace,
			"replicas": updated.Replicas, "container_port": updated.ContainerPort, "deploy_strategy": updated.DeployStrategy,
			"enabled": updated.Enabled, "updated_at": updated.UpdatedAt,
		}).Error; err != nil {
			return mapDBError(err)
		}
		return nil
	})
	if err != nil {
		return domain.DeploymentTarget{}, err
	}
	return s.GetDeploymentTarget(ctx, spaceID, projectID, targetID)
}

func (s *MySQL) DeleteDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string) error {
	target, err := s.GetDeploymentTarget(ctx, spaceID, projectID, targetID)
	if err != nil {
		return err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ?", spaceID, projectID).Count(&count).Error; err != nil {
		return err
	}
	if count <= 1 {
		return fmt.Errorf("%w: 项目至少需要保留一个部署目标", ErrConflict)
	}
	if target.Stage == DeploymentStageDev {
		var remainingDev int64
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("space_id = ? AND project_id = ? AND id <> ? AND stage = ?", spaceID, projectID, targetID, DeploymentStageDev).Count(&remainingDev).Error; err != nil {
			return err
		}
		if remainingDev == 0 {
			return fmt.Errorf("%w: 项目必须保留 DEV 环境", ErrConflict)
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Where("id = ? AND space_id = ? AND project_id = ?", targetID, spaceID, projectID).Delete(&deploymentTargetRow{}).Error
	})
}

func (s *MySQL) GetDeploymentConfig(ctx context.Context, spaceID, projectID string) (domain.DeploymentConfig, error) {
	var row deploymentConfigRow
	if err := s.db.WithContext(ctx).Where("project_id = ? AND space_id = ?", projectID, spaceID).First(&row).Error; err != nil {
		return domain.DeploymentConfig{}, mapDBError(err)
	}
	return toDeploymentConfig(row), nil
}

func (s *MySQL) SaveDeploymentConfig(ctx context.Context, spaceID, projectID string, input SaveDeploymentConfigInput) (domain.DeploymentConfig, error) {
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
	project, err := s.GetProject(ctx, spaceID, projectID)
	if err != nil {
		return domain.DeploymentConfig{}, err
	}
	var row deploymentConfigRow
	queryErr := s.db.WithContext(ctx).Where("project_id = ? AND space_id = ?", projectID, spaceID).First(&row).Error
	switch {
	case queryErr == nil:
		row.Namespace = project.Namespace
		row.Format = format
		row.Manifest = manifest
		row.Version++
		if err := s.db.WithContext(ctx).Model(&deploymentConfigRow{}).Where("project_id = ? AND space_id = ?", projectID, spaceID).Updates(map[string]any{
			"namespace": row.Namespace,
			"format":    row.Format,
			"manifest":  row.Manifest,
			"version":   row.Version,
		}).Error; err != nil {
			return domain.DeploymentConfig{}, err
		}
	case queryErr == gorm.ErrRecordNotFound:
		row = deploymentConfigRow{ProjectID: projectID, SpaceID: spaceID, Namespace: project.Namespace, Format: format, Manifest: manifest, Version: 1}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return domain.DeploymentConfig{}, ErrConflict
			}
			return domain.DeploymentConfig{}, err
		}
	default:
		return domain.DeploymentConfig{}, queryErr
	}
	return s.GetDeploymentConfig(ctx, spaceID, projectID)
}

func (s *MySQL) AppendAuditLog(ctx context.Context, entry domain.AuditLog) error {
	if strings.TrimSpace(entry.SpaceID) == "" || strings.TrimSpace(entry.Action) == "" {
		return fmt.Errorf("%w: audit space and action are required", ErrInvalidInput)
	}
	row := auditLogRow{
		SpaceID:    stringPtr(entry.SpaceID),
		Action:     strings.TrimSpace(entry.Action),
		TargetType: "resource",
		TargetID:   strings.TrimSpace(entry.Target),
		CreatedAt:  entry.CreatedAt,
	}
	if entry.UserID != 0 {
		row.UserID = uint64Ptr(entry.UserID)
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Create(&row).Error
}

func (s *MySQL) ListAuditLogs(ctx context.Context, spaceID string, limit int) ([]domain.AuditLog, error) {
	if strings.TrimSpace(spaceID) == "" {
		return nil, fmt.Errorf("%w: space is required", ErrInvalidInput)
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []auditLogRow
	if err := s.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.AuditLog, 0, len(rows))
	for _, row := range rows {
		userID := uint64(0)
		if row.UserID != nil {
			userID = *row.UserID
		}
		result = append(result, domain.AuditLog{ID: row.ID, SpaceID: valueOrEmpty(row.SpaceID), UserID: userID, Action: row.Action, Target: row.TargetID, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func (s *MySQL) seed(ctx context.Context) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&userRow{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash default admin password: %w", err)
	}
	return s.db.WithContext(ctx).Create(&userRow{
		Username:     defaultAdminUsername,
		DisplayName:  defaultAdminDisplayName,
		PasswordHash: string(hash),
		IsSuperAdmin: true,
	}).Error
}

func (s *MySQL) seedLegacyDeploymentTargets(ctx context.Context) error {
	var projects []projectRow
	if err := s.db.WithContext(ctx).Find(&projects).Error; err != nil {
		return err
	}
	for _, projectRow := range projects {
		var count int64
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("project_id = ? AND space_id = ?", projectRow.ID, projectRow.SpaceID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		project := toProject(projectRow)
		target := domain.DeploymentTarget{
			ID: "target-" + uuid.NewString(), ProjectID: project.ID, SpaceID: project.SpaceID,
			Name: defaultTargetName, Environment: defaultTargetEnvironment, Stage: DeploymentStageDev, SortOrder: 1, ClusterID: project.ClusterID,
			Namespace: project.Namespace, Replicas: project.Replicas, ContainerPort: project.ContainerPort,
			DeployStrategy: project.DeployStrategy, Enabled: true, Status: "active", Health: "unknown",
			CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
		}
		if target.CreatedAt.IsZero() {
			target.CreatedAt = time.Now().UTC()
		}
		if target.UpdatedAt.IsZero() {
			target.UpdatedAt = target.CreatedAt
		}
		if err := s.db.WithContext(ctx).Create(fromDeploymentTarget(target)).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateDeploymentTargetMetadata upgrades rows created before stage and
// sort_order existed. The legacy default flag is used only during this one
// migration; normal runtime behavior never reads it.
func (s *MySQL) migrateDeploymentTargetMetadata(ctx context.Context) error {
	var rows []deploymentTargetRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return err
	}
	byProject := make(map[string][]deploymentTargetRow)
	for _, row := range rows {
		key := row.SpaceID + "\x00" + row.ProjectID
		byProject[key] = append(byProject[key], row)
	}
	for _, projectRows := range byProject {
		sort.SliceStable(projectRows, func(i, j int) bool {
			if projectRows[i].IsDefault != projectRows[j].IsDefault {
				return projectRows[i].IsDefault
			}
			if projectRows[i].CreatedAt.Equal(projectRows[j].CreatedAt) {
				return projectRows[i].ID < projectRows[j].ID
			}
			return projectRows[i].CreatedAt.Before(projectRows[j].CreatedAt)
		})
		for index, row := range projectRows {
			stage := normalizeDeploymentStage(row.Stage)
			if row.IsDefault || index == 0 {
				stage = DeploymentStageDev
			} else if stage == "" || (stage == DeploymentStageCustom && row.SortOrder <= 1) {
				stage = legacyDeploymentStage(row.Environment)
			}
			if stage == DeploymentStageDev && index != 0 {
				stage = DeploymentStageCustom
			}
			if stage == "" {
				stage = DeploymentStageCustom
			}
			if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("id = ?", row.ID).Updates(map[string]any{
				"stage": stage, "sort_order": index + 1, "is_default": false,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MySQL) clusterExists(ctx context.Context, spaceID, clusterID string) error {
	var row clusterRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ?", clusterID, spaceID).First(&row).Error; err != nil {
		return mapDBError(err)
	}
	return nil
}

func mapDBError(err error) error {
	if err == gorm.ErrRecordNotFound {
		return ErrNotFound
	}
	return err
}

func stringPtr(value string) *string { return &value }
func uint64Ptr(value uint64) *uint64 { return &value }
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func toUser(row userRow) domain.User {
	return domain.User{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName, PasswordHash: row.PasswordHash, IsSuperAdmin: row.IsSuperAdmin}
}
func toSpace(row spaceRow) domain.Space {
	return domain.Space{ID: row.ID, Name: row.Name, Slug: row.Slug, Description: row.Description, CreatedAt: row.CreatedAt}
}
func toCluster(row clusterRow) Cluster {
	mode := normalizeClusterConnectionMode(row.ConnectionMode)
	if mode == "" {
		mode = ClusterConnectionKubeconfig
	}
	return Cluster{
		ID: row.ID, SpaceID: row.SpaceID, Name: row.Name, Provider: row.Provider,
		APIEndpoint: row.APIEndpoint, KubeContext: row.KubeContext, ConnectionMode: mode,
		KubeconfigPath: row.KubeconfigPath, KubeconfigConfigured: strings.TrimSpace(row.KubeconfigPath) != "",
		Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
func toProject(row projectRow) domain.Project {
	return domain.Project{ID: row.ID, SpaceID: row.SpaceID, Name: row.Name, Description: row.Description, RepositoryID: row.RepositoryID, RepositoryURL: row.RepositoryURL, DefaultBranch: row.DefaultBranch, ClusterID: row.ClusterID, Namespace: row.Namespace, DeployStrategy: row.DeployStrategy, Replicas: row.Replicas, ContainerPort: row.ContainerPort, ImageRepository: row.ImageRepository, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func toProjectGitCredential(row projectGitCredentialRow) ProjectGitCredential {
	return ProjectGitCredential{ProjectID: row.ProjectID, SpaceID: row.SpaceID, Provider: row.Provider, Username: row.Username, TokenCiphertext: row.TokenCiphertext, Configured: strings.TrimSpace(row.TokenCiphertext) != "", UpdatedAt: row.UpdatedAt}
}

func fromDeploymentTarget(target domain.DeploymentTarget) deploymentTargetRow {
	return deploymentTargetRow{
		ID: target.ID, SpaceID: target.SpaceID, ProjectID: target.ProjectID, Name: target.Name,
		Environment: target.Environment, Stage: target.Stage, SortOrder: target.SortOrder, ClusterID: target.ClusterID, Namespace: target.Namespace,
		Replicas: target.Replicas, ContainerPort: target.ContainerPort, DeployStrategy: target.DeployStrategy,
		Enabled: target.Enabled, Status: target.Status,
		CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt,
	}
}

func toDeploymentTarget(row deploymentTargetRow) domain.DeploymentTarget {
	status := strings.TrimSpace(row.Status)
	if status == "" {
		status = "active"
	}
	return domain.DeploymentTarget{
		ID: row.ID, SpaceID: row.SpaceID, ProjectID: row.ProjectID, Name: row.Name,
		Environment: row.Environment, Stage: normalizeDeploymentStage(row.Stage), SortOrder: row.SortOrder, ClusterID: row.ClusterID, Namespace: row.Namespace,
		Replicas: row.Replicas, ContainerPort: row.ContainerPort, DeployStrategy: row.DeployStrategy,
		Enabled: row.Enabled, Status: status,
		Health: "unknown", CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toDeploymentConfig(row deploymentConfigRow) domain.DeploymentConfig {
	format := strings.ToLower(strings.TrimSpace(row.Format))
	if format == "yml" {
		format = "yaml"
	}
	if format == "" {
		format = "yaml"
	}
	return domain.DeploymentConfig{ProjectID: row.ProjectID, Namespace: row.Namespace, Format: format, Manifest: row.Manifest, Version: row.Version, UpdatedAt: row.UpdatedAt}
}
