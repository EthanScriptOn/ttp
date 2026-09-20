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
	ID                   string `gorm:"size:64;primaryKey"`
	SpaceID              string `gorm:"size:64;index;not null"`
	Name                 string `gorm:"size:120;not null"`
	Description          string `gorm:"size:255"`
	RepositoryID         string `gorm:"size:255;not null"`
	RepositoryURL        string `gorm:"size:500;not null"`
	DefaultBranch        string `gorm:"size:120;not null"`
	ClusterID            string `gorm:"size:64;not null"`
	Namespace            string `gorm:"size:120;not null"`
	DeployStrategy       string `gorm:"size:32;not null"`
	Replicas             int    `gorm:"not null;default:1"`
	ContainerPort        int    `gorm:"not null;default:8080"`
	ImageRepository      string `gorm:"size:500"`
	RegistryConnectionID string `gorm:"column:registry_connection_id;size:64;index:idx_projects_registry_connection"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
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

type projectRoleRow struct {
	ID          string `gorm:"size:64;primaryKey"`
	SpaceID     string `gorm:"size:64;index;not null"`
	Key         string `gorm:"size:80;index;not null"`
	Name        string `gorm:"size:120;not null"`
	Description string `gorm:"size:255;not null;default:''"`
	CreatedBy   uint64 `gorm:"index;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (projectRoleRow) TableName() string { return "project_roles" }

type projectRolePermissionRow struct {
	RoleID        string `gorm:"size:64;primaryKey"`
	PermissionKey string `gorm:"size:80;primaryKey"`
}

func (projectRolePermissionRow) TableName() string { return "project_role_permissions" }

type projectMemberRow struct {
	ProjectID string `gorm:"size:64;primaryKey"`
	UserID    uint64 `gorm:"primaryKey"`
	SpaceID   string `gorm:"size:64;index;not null"`
	RoleID    string `gorm:"size:64;index;not null;default:''"`
	RoleKey   string `gorm:"size:80;index;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (projectMemberRow) TableName() string { return "project_members" }

type imageRegistryConnectionRow struct {
	ID                   string     `gorm:"size:64;primaryKey"`
	SpaceID              string     `gorm:"size:64;index;not null"`
	Name                 string     `gorm:"size:120;not null"`
	Registry             string     `gorm:"size:255;not null"`
	AuthType             string     `gorm:"size:32;not null"`
	Username             string     `gorm:"size:120;not null;default:''"`
	PullSecretName       string     `gorm:"size:120;not null"`
	CredentialCiphertext string     `gorm:"type:longtext;not null"`
	Status               string     `gorm:"size:32;not null;default:unverified"`
	LastCheckedAt        *time.Time `gorm:"index"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (imageRegistryConnectionRow) TableName() string { return "image_registry_connections" }

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

type namespaceQuotaRow struct {
	SpaceID                        string `gorm:"size:64;primaryKey"`
	ClusterID                      string `gorm:"size:64;primaryKey"`
	Environment                    string `gorm:"size:64;primaryKey"`
	Namespace                      string `gorm:"size:120;not null"`
	CPURequest                     string `gorm:"column:cpu_request;size:32;not null"`
	CPULimit                       string `gorm:"column:cpu_limit;size:32;not null"`
	MemoryRequest                  string `gorm:"column:memory_request;size:32;not null"`
	MemoryLimit                    string `gorm:"column:memory_limit;size:32;not null"`
	EphemeralStorageRequest        string `gorm:"column:ephemeral_storage_request;size:32;not null"`
	EphemeralStorageLimit          string `gorm:"column:ephemeral_storage_limit;size:32;not null"`
	Storage                        string `gorm:"size:32;not null"`
	Pods                           int    `gorm:"not null"`
	PersistentVolumeClaims         int    `gorm:"column:persistent_volume_claims;not null"`
	DefaultCPURequest              string `gorm:"column:default_cpu_request;size:32;not null"`
	DefaultCPULimit                string `gorm:"column:default_cpu_limit;size:32;not null"`
	DefaultMemoryRequest           string `gorm:"column:default_memory_request;size:32;not null"`
	DefaultMemoryLimit             string `gorm:"column:default_memory_limit;size:32;not null"`
	DefaultEphemeralStorageRequest string `gorm:"column:default_ephemeral_storage_request;size:32;not null"`
	DefaultEphemeralStorageLimit   string `gorm:"column:default_ephemeral_storage_limit;size:32;not null"`
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
}

func (namespaceQuotaRow) TableName() string { return "deployment_namespace_quotas" }

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

type deploymentResourceFileRow struct {
	ID               string `gorm:"size:64;primaryKey"`
	SpaceID          string `gorm:"size:64;index;not null"`
	ProjectID        string `gorm:"size:64;index;not null"`
	Name             string `gorm:"size:255;not null"`
	Path             string `gorm:"size:500;not null"`
	Format           string `gorm:"size:16;not null;default:yaml"`
	Content          string `gorm:"type:longtext;not null"`
	APIVersion       string `gorm:"size:120;not null"`
	Kind             string `gorm:"size:120;not null"`
	ResourceName     string `gorm:"size:253;not null"`
	Namespace        string `gorm:"size:120;not null;default:''"`
	SortOrder        int    `gorm:"not null;default:1"`
	Version          int    `gorm:"not null;default:1"`
	ReleaseSupported bool   `gorm:"not null;default:false"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (deploymentResourceFileRow) TableName() string { return "project_deployment_resource_files" }

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
	if err := db.AutoMigrate(&userRow{}, &spaceRow{}, &memberRow{}, &clusterRow{}, &imageRegistryConnectionRow{}, &projectRow{}, &projectGitCredentialRow{}, &projectRoleRow{}, &projectRolePermissionRow{}, &projectMemberRow{}, &deploymentTargetRow{}, &namespaceQuotaRow{}, &deploymentConfigRow{}, &deploymentResourceFileRow{}, &auditLogRow{}, &abExperimentRow{}, &releaseRecordRow{}, &releaseCommitRecordRow{}, &releaseTargetRecordRow{}, &releaseExecutionLogRecordRow{}); err != nil {
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
	if err := store.migrateDeploymentTargetNamespaces(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := store.migrateProjectAccess(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := store.seed(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return store, nil
}

// migrateProjectAccess backfills explicit project memberships for projects
// created before project-level access control existed. Space owners/admins
// retain management access and regular members keep the closest project role.
func (s *MySQL) migrateProjectAccess(ctx context.Context) error {
	query := `
		INSERT INTO project_members (project_id, user_id, space_id, role_id, role_key, created_at, updated_at)
		SELECT p.id, sm.user_id, p.space_id,
			CASE
				WHEN sm.role IN ('owner', 'admin') THEN 'system:project_maintainer'
				WHEN sm.role = 'developer' THEN 'system:project_developer'
				ELSE 'system:project_viewer'
			END,
			CASE
				WHEN sm.role IN ('owner', 'admin') THEN 'project_maintainer'
				WHEN sm.role = 'developer' THEN 'project_developer'
				ELSE 'project_viewer'
			END,
			CURRENT_TIMESTAMP(6), CURRENT_TIMESTAMP(6)
		FROM projects p
		JOIN space_members sm ON sm.space_id = p.space_id
		ON DUPLICATE KEY UPDATE project_id = VALUES(project_id)`
	return s.db.WithContext(ctx).Exec(query).Error
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
	registryConnectionID := strings.TrimSpace(input.RegistryConnectionID)
	if repoID == "" {
		repoID = "repo-" + uuid.NewString()
	}
	if err := s.clusterExists(ctx, spaceID, cluster); err != nil {
		return domain.Project{}, err
	}
	if registryConnectionID != "" {
		connection, err := s.GetImageRegistryConnection(ctx, spaceID, registryConnectionID)
		if err != nil {
			return domain.Project{}, err
		}
		if err := validateImageRepositoryRegistry(input.ImageRepository, connection.Registry); err != nil {
			return domain.Project{}, err
		}
	}
	projectID := strings.TrimSpace(input.ID)
	if projectID == "" {
		projectID = uuid.NewString()
	}
	row := projectRow{ID: projectID, SpaceID: spaceID, Name: name, Description: strings.TrimSpace(input.Description), RepositoryID: repoID, RepositoryURL: repo, DefaultBranch: branch, ClusterID: cluster, Namespace: namespace, DeployStrategy: strategy, Replicas: replicas, ContainerPort: port, ImageRepository: strings.TrimSpace(input.ImageRepository), RegistryConnectionID: registryConnectionID}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return ErrConflict
			}
			return err
		}
		return nil
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
	if input.RegistryConnectionID != nil {
		registryConnectionID := strings.TrimSpace(*input.RegistryConnectionID)
		if registryConnectionID != "" {
			connection, err := s.GetImageRegistryConnection(ctx, spaceID, registryConnectionID)
			if err != nil {
				return domain.Project{}, err
			}
			if input.ImageRepository != nil {
				if err := validateImageRepositoryRegistry(strings.TrimSpace(*input.ImageRepository), connection.Registry); err != nil {
					return domain.Project{}, err
				}
			} else {
				// A managed registry owns the repository naming policy. Clear any
				// legacy hand-entered path so the builder derives one from the
				// connection and project ID.
				updates["image_repository"] = ""
			}
		}
		updates["registry_connection_id"] = registryConnectionID
	}
	if input.ImageRepository != nil && input.RegistryConnectionID == nil {
		if current, currentErr := s.GetProject(ctx, spaceID, projectID); currentErr == nil && current.RegistryConnectionID != "" {
			connection, connectionErr := s.GetImageRegistryConnection(ctx, spaceID, current.RegistryConnectionID)
			if connectionErr != nil {
				return domain.Project{}, connectionErr
			}
			if err := validateImageRepositoryRegistry(strings.TrimSpace(*input.ImageRepository), connection.Registry); err != nil {
				return domain.Project{}, err
			}
		}
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&projectRow{}).Where("id = ? AND space_id = ?", projectID, spaceID).Updates(updates).Error; err != nil {
			return domain.Project{}, err
		}
	}
	return s.GetProject(ctx, spaceID, projectID)
}

func (s *MySQL) ListImageRegistryConnections(ctx context.Context, spaceID string) ([]domain.ImageRegistryConnection, error) {
	var rows []imageRegistryConnectionRow
	if err := s.db.WithContext(ctx).Where("space_id = ?", spaceID).Order("name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]domain.ImageRegistryConnection, 0, len(rows))
	for _, row := range rows {
		result = append(result, toImageRegistryConnection(row))
	}
	return result, nil
}

func (s *MySQL) GetImageRegistryConnection(ctx context.Context, spaceID, connectionID string) (domain.ImageRegistryConnection, error) {
	var row imageRegistryConnectionRow
	if err := s.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, strings.TrimSpace(connectionID)).First(&row).Error; err != nil {
		return domain.ImageRegistryConnection{}, mapDBError(err)
	}
	return toImageRegistryConnection(row), nil
}

func (s *MySQL) CreateImageRegistryConnection(ctx context.Context, spaceID string, input CreateImageRegistryConnectionInput) (domain.ImageRegistryConnection, error) {
	item, err := normalizeImageRegistryConnection(spaceID, input)
	if err != nil {
		return domain.ImageRegistryConnection{}, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&imageRegistryConnectionRow{}).Where("space_id = ? AND (LOWER(name) = LOWER(?) OR registry = ?)", spaceID, item.Name, item.Registry).Count(&count).Error; err != nil {
		return domain.ImageRegistryConnection{}, err
	}
	if count > 0 {
		return domain.ImageRegistryConnection{}, ErrConflict
	}
	row := imageRegistryConnectionRow{ID: item.ID, SpaceID: item.SpaceID, Name: item.Name, Registry: item.Registry, AuthType: item.AuthType, Username: item.Username, PullSecretName: item.PullSecretName, CredentialCiphertext: item.CredentialCiphertext, Status: item.Status, LastCheckedAt: item.LastCheckedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return domain.ImageRegistryConnection{}, ErrConflict
		}
		return domain.ImageRegistryConnection{}, err
	}
	return toImageRegistryConnection(row), nil
}

func (s *MySQL) UpdateImageRegistryConnection(ctx context.Context, spaceID, connectionID string, input UpdateImageRegistryConnectionInput) (domain.ImageRegistryConnection, error) {
	current, err := s.GetImageRegistryConnection(ctx, spaceID, connectionID)
	if err != nil {
		return domain.ImageRegistryConnection{}, err
	}
	updated, err := updateImageRegistryConnection(current, input)
	if err != nil {
		return domain.ImageRegistryConnection{}, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&imageRegistryConnectionRow{}).Where("space_id = ? AND id <> ? AND (LOWER(name) = LOWER(?) OR registry = ?)", spaceID, current.ID, updated.Name, updated.Registry).Count(&count).Error; err != nil {
		return domain.ImageRegistryConnection{}, err
	}
	if count > 0 {
		return domain.ImageRegistryConnection{}, ErrConflict
	}
	updates := map[string]any{"name": updated.Name, "registry": updated.Registry, "auth_type": updated.AuthType, "username": updated.Username, "pull_secret_name": updated.PullSecretName, "status": updated.Status, "updated_at": updated.UpdatedAt}
	if input.CredentialCiphertext != nil {
		updates["credential_ciphertext"] = updated.CredentialCiphertext
	}
	if input.LastCheckedAt != nil {
		updates["last_checked_at"] = updated.LastCheckedAt
	}
	if err := s.db.WithContext(ctx).Model(&imageRegistryConnectionRow{}).Where("space_id = ? AND id = ?", spaceID, current.ID).Updates(updates).Error; err != nil {
		return domain.ImageRegistryConnection{}, err
	}
	return s.GetImageRegistryConnection(ctx, spaceID, current.ID)
}

func (s *MySQL) DeleteImageRegistryConnection(ctx context.Context, spaceID, connectionID string) error {
	if _, err := s.GetImageRegistryConnection(ctx, spaceID, connectionID); err != nil {
		return err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&projectRow{}).Where("space_id = ? AND registry_connection_id = ?", spaceID, strings.TrimSpace(connectionID)).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrConflict
	}
	if err := s.db.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, strings.TrimSpace(connectionID)).Delete(&imageRegistryConnectionRow{}).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") || strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return ErrConflict
		}
		return err
	}
	return nil
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

func (s *MySQL) GetNamespaceQuota(ctx context.Context, spaceID, clusterID, environment string) (domain.NamespaceQuota, error) {
	clusterID = normalizeClusterID(spaceID, clusterID)
	var row namespaceQuotaRow
	if err := s.db.WithContext(ctx).Where("space_id = ? AND cluster_id = ? AND environment = ?", spaceID, clusterID, strings.ToLower(strings.TrimSpace(environment))).First(&row).Error; err != nil {
		return domain.NamespaceQuota{}, mapDBError(err)
	}
	return namespaceQuotaFromRow(row), nil
}

func (s *MySQL) UpsertNamespaceQuota(ctx context.Context, spaceID, clusterID, environment, namespace string, quota domain.NamespaceQuota) (domain.NamespaceQuota, error) {
	normalized, err := NormalizeNamespaceQuota(&quota)
	if err != nil {
		return domain.NamespaceQuota{}, err
	}
	clusterID = normalizeClusterID(spaceID, clusterID)
	environment = strings.ToLower(strings.TrimSpace(environment))
	namespace = strings.TrimSpace(namespace)
	if environment == "" || namespace == "" {
		return domain.NamespaceQuota{}, fmt.Errorf("%w: namespace quota identity is required", ErrInvalidInput)
	}
	if err := s.clusterExists(ctx, spaceID, clusterID); err != nil {
		return domain.NamespaceQuota{}, err
	}
	var row namespaceQuotaRow
	queryErr := s.db.WithContext(ctx).Where("space_id = ? AND cluster_id = ? AND environment = ?", spaceID, clusterID, environment).First(&row).Error
	switch {
	case queryErr == nil:
		applyNamespaceQuotaToRow(&row, namespace, normalized)
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return domain.NamespaceQuota{}, err
		}
	case queryErr == gorm.ErrRecordNotFound:
		row = namespaceQuotaRow{
			SpaceID: spaceID, ClusterID: clusterID, Environment: environment,
			Namespace: namespace,
		}
		applyNamespaceQuotaToRow(&row, namespace, normalized)
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return domain.NamespaceQuota{}, ErrConflict
			}
			return domain.NamespaceQuota{}, err
		}
	default:
		return domain.NamespaceQuota{}, queryErr
	}
	return normalized, nil
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

func (s *MySQL) ListDeploymentResourceFiles(ctx context.Context, spaceID, projectID string) ([]domain.DeploymentResourceFile, error) {
	var rows []deploymentResourceFileRow
	if err := s.db.WithContext(ctx).Where("space_id = ? AND project_id = ?", spaceID, projectID).Order("sort_order ASC, path ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
			return nil, err
		}
	}
	result := make([]domain.DeploymentResourceFile, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDeploymentResourceFile(row))
	}
	return result, nil
}

func (s *MySQL) CreateDeploymentResourceFile(ctx context.Context, spaceID, projectID string, input SaveDeploymentResourceFileInput) (domain.DeploymentResourceFile, error) {
	if err := validateResourceFileInput(input); err != nil {
		return domain.DeploymentResourceFile{}, err
	}
	if _, err := s.GetProject(ctx, spaceID, projectID); err != nil {
		return domain.DeploymentResourceFile{}, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&deploymentResourceFileRow{}).Where("space_id = ? AND project_id = ?", spaceID, projectID).Count(&count).Error; err != nil {
		return domain.DeploymentResourceFile{}, err
	}
	if count >= 100 {
		return domain.DeploymentResourceFile{}, fmt.Errorf("%w: no more than 100 resource files are allowed", ErrInvalidInput)
	}
	var duplicateCount int64
	if err := s.db.WithContext(ctx).Model(&deploymentResourceFileRow{}).Where("space_id = ? AND project_id = ? AND (path = ? OR name = ?)", spaceID, projectID, strings.TrimSpace(input.Path), strings.TrimSpace(input.Name)).Count(&duplicateCount).Error; err != nil {
		return domain.DeploymentResourceFile{}, err
	}
	if duplicateCount > 0 {
		return domain.DeploymentResourceFile{}, ErrConflict
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "resource-" + uuid.NewString()[:8]
	}
	row := deploymentResourceFileRow{ID: id, SpaceID: spaceID, ProjectID: projectID, Name: strings.TrimSpace(input.Name), Path: strings.TrimSpace(input.Path), Format: strings.ToLower(strings.TrimSpace(input.Format)), Content: input.Content, APIVersion: strings.TrimSpace(input.APIVersion), Kind: strings.TrimSpace(input.Kind), ResourceName: strings.TrimSpace(input.ResourceName), Namespace: strings.TrimSpace(input.Namespace), SortOrder: input.SortOrder, Version: 1, ReleaseSupported: input.ReleaseSupported}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return domain.DeploymentResourceFile{}, ErrConflict
		}
		return domain.DeploymentResourceFile{}, err
	}
	return toDeploymentResourceFile(row), nil
}

func (s *MySQL) UpdateDeploymentResourceFile(ctx context.Context, spaceID, projectID, resourceID string, input SaveDeploymentResourceFileInput) (domain.DeploymentResourceFile, error) {
	if err := validateResourceFileInput(input); err != nil {
		return domain.DeploymentResourceFile{}, err
	}
	var row deploymentResourceFileRow
	if err := s.db.WithContext(ctx).Where("id = ? AND space_id = ? AND project_id = ?", resourceID, spaceID, projectID).First(&row).Error; err != nil {
		return domain.DeploymentResourceFile{}, mapDBError(err)
	}
	row.Name = strings.TrimSpace(input.Name)
	row.Path = strings.TrimSpace(input.Path)
	var duplicateCount int64
	if err := s.db.WithContext(ctx).Model(&deploymentResourceFileRow{}).Where("space_id = ? AND project_id = ? AND id <> ? AND (path = ? OR name = ?)", spaceID, projectID, resourceID, row.Path, row.Name).Count(&duplicateCount).Error; err != nil {
		return domain.DeploymentResourceFile{}, err
	}
	if duplicateCount > 0 {
		return domain.DeploymentResourceFile{}, ErrConflict
	}
	row.Format = strings.ToLower(strings.TrimSpace(input.Format))
	row.Content = input.Content
	row.APIVersion = strings.TrimSpace(input.APIVersion)
	row.Kind = strings.TrimSpace(input.Kind)
	row.ResourceName = strings.TrimSpace(input.ResourceName)
	row.Namespace = strings.TrimSpace(input.Namespace)
	row.SortOrder = input.SortOrder
	row.ReleaseSupported = input.ReleaseSupported
	row.Version++
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return domain.DeploymentResourceFile{}, ErrConflict
		}
		return domain.DeploymentResourceFile{}, err
	}
	return toDeploymentResourceFile(row), nil
}

func (s *MySQL) DeleteDeploymentResourceFile(ctx context.Context, spaceID, projectID, resourceID string) error {
	result := s.db.WithContext(ctx).Where("id = ? AND space_id = ? AND project_id = ?", resourceID, spaceID, projectID).Delete(&deploymentResourceFileRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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

// migrateDeploymentTargetNamespaces moves legacy hand-written namespaces to
// the space/environment namespace scheme. Kubernetes namespace creation is
// deliberately deferred to the runtime provider because the store does not
// own live cluster connections.
func (s *MySQL) migrateDeploymentTargetNamespaces(ctx context.Context) error {
	var spaces []spaceRow
	if err := s.db.WithContext(ctx).Find(&spaces).Error; err != nil {
		return err
	}
	spaceSources := make(map[string]string, len(spaces))
	for _, space := range spaces {
		source := strings.TrimSpace(space.Slug)
		if source == "" {
			source = strings.TrimSpace(space.Name)
		}
		if source == "" {
			source = strings.TrimSpace(space.ID)
		}
		spaceSources[space.ID] = source
	}

	var rows []deploymentTargetRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		source := spaceSources[row.SpaceID]
		if source == "" {
			continue
		}
		namespace, err := BuildDeploymentNamespace(source, row.Environment)
		if err != nil {
			return fmt.Errorf("migrate deployment target %s namespace: %w", row.ID, err)
		}
		if row.Namespace == namespace {
			continue
		}
		if err := s.db.WithContext(ctx).Model(&deploymentTargetRow{}).Where("id = ?", row.ID).Update("namespace", namespace).Error; err != nil {
			return err
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
	return domain.Project{ID: row.ID, SpaceID: row.SpaceID, Name: row.Name, Description: row.Description, RepositoryID: row.RepositoryID, RepositoryURL: row.RepositoryURL, DefaultBranch: row.DefaultBranch, ClusterID: row.ClusterID, Namespace: row.Namespace, DeployStrategy: row.DeployStrategy, Replicas: row.Replicas, ContainerPort: row.ContainerPort, ImageRepository: row.ImageRepository, RegistryConnectionID: row.RegistryConnectionID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func toProjectGitCredential(row projectGitCredentialRow) ProjectGitCredential {
	return ProjectGitCredential{ProjectID: row.ProjectID, SpaceID: row.SpaceID, Provider: row.Provider, Username: row.Username, TokenCiphertext: row.TokenCiphertext, Configured: strings.TrimSpace(row.TokenCiphertext) != "", UpdatedAt: row.UpdatedAt}
}

func toImageRegistryConnection(row imageRegistryConnectionRow) domain.ImageRegistryConnection {
	return domain.ImageRegistryConnection{ID: row.ID, SpaceID: row.SpaceID, Name: row.Name, Registry: row.Registry, AuthType: row.AuthType, Username: row.Username, PullSecretName: row.PullSecretName, Configured: strings.TrimSpace(row.CredentialCiphertext) != "", Status: row.Status, LastCheckedAt: row.LastCheckedAt, CredentialCiphertext: row.CredentialCiphertext, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
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

func applyNamespaceQuotaToRow(row *namespaceQuotaRow, namespace string, quota domain.NamespaceQuota) {
	row.Namespace = strings.TrimSpace(namespace)
	row.CPURequest = quota.CPURequest
	row.CPULimit = quota.CPULimit
	row.MemoryRequest = quota.MemoryRequest
	row.MemoryLimit = quota.MemoryLimit
	row.EphemeralStorageRequest = quota.EphemeralStorageRequest
	row.EphemeralStorageLimit = quota.EphemeralStorageLimit
	row.Storage = quota.Storage
	row.Pods = quota.Pods
	row.PersistentVolumeClaims = quota.PersistentVolumeClaims
	row.DefaultCPURequest = quota.DefaultCPURequest
	row.DefaultCPULimit = quota.DefaultCPULimit
	row.DefaultMemoryRequest = quota.DefaultMemoryRequest
	row.DefaultMemoryLimit = quota.DefaultMemoryLimit
	row.DefaultEphemeralStorageRequest = quota.DefaultEphemeralStorageRequest
	row.DefaultEphemeralStorageLimit = quota.DefaultEphemeralStorageLimit
}

func namespaceQuotaFromRow(row namespaceQuotaRow) domain.NamespaceQuota {
	return domain.NamespaceQuota{
		CPURequest:                     row.CPURequest,
		CPULimit:                       row.CPULimit,
		MemoryRequest:                  row.MemoryRequest,
		MemoryLimit:                    row.MemoryLimit,
		EphemeralStorageRequest:        row.EphemeralStorageRequest,
		EphemeralStorageLimit:          row.EphemeralStorageLimit,
		Storage:                        row.Storage,
		Pods:                           row.Pods,
		PersistentVolumeClaims:         row.PersistentVolumeClaims,
		DefaultCPURequest:              row.DefaultCPURequest,
		DefaultCPULimit:                row.DefaultCPULimit,
		DefaultMemoryRequest:           row.DefaultMemoryRequest,
		DefaultMemoryLimit:             row.DefaultMemoryLimit,
		DefaultEphemeralStorageRequest: row.DefaultEphemeralStorageRequest,
		DefaultEphemeralStorageLimit:   row.DefaultEphemeralStorageLimit,
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

func toDeploymentResourceFile(row deploymentResourceFileRow) domain.DeploymentResourceFile {
	format := strings.ToLower(strings.TrimSpace(row.Format))
	if format == "yml" {
		format = "yaml"
	}
	if format == "" {
		format = "yaml"
	}
	return domain.DeploymentResourceFile{
		ID: row.ID, ProjectID: row.ProjectID, Name: row.Name, Path: row.Path, Format: format,
		Content: row.Content, APIVersion: row.APIVersion, Kind: row.Kind, ResourceName: row.ResourceName,
		Namespace: row.Namespace, SortOrder: row.SortOrder, Version: row.Version,
		ReleaseSupported: row.ReleaseSupported, UpdatedAt: row.UpdatedAt,
	}
}
