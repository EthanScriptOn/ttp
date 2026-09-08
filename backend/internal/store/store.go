package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotFound           = errors.New("resource not found")
	ErrConflict           = errors.New("resource already exists")
	ErrForbidden          = errors.New("forbidden")
	ErrInvalidInput       = errors.New("invalid input")
)

type CreateProjectInput struct {
	ID              string `json:"-"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	RepositoryID    string `json:"repository_id"`
	RepositoryURL   string `json:"repository_url"`
	DefaultBranch   string `json:"default_branch"`
	ClusterID       string `json:"cluster_id"`
	Namespace       string `json:"namespace"`
	DeployStrategy  string `json:"deploy_strategy"`
	Replicas        int    `json:"replicas"`
	ContainerPort   int    `json:"container_port"`
	ImageRepository string `json:"image_repository"`
}

// ProjectGitCredential is the safe project-level Git identity. The encrypted
// token is used internally by the API and is never serialized.
type ProjectGitCredential struct {
	ProjectID       string    `json:"project_id"`
	SpaceID         string    `json:"space_id"`
	Provider        string    `json:"provider"`
	Username        string    `json:"username"`
	TokenCiphertext string    `json:"-"`
	Configured      bool      `json:"configured"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SaveProjectGitCredentialInput struct {
	Provider        string
	Username        string
	TokenCiphertext string
}

// Cluster is a deployment target registered in a space. The runtime provider
// owns live Kubernetes state; the store owns this durable registry.
type Cluster struct {
	ID                   string    `json:"id"`
	SpaceID              string    `json:"space_id"`
	Name                 string    `json:"name"`
	Provider             string    `json:"provider"`
	APIEndpoint          string    `json:"api_endpoint,omitempty"`
	KubeContext          string    `json:"kube_context,omitempty"`
	ConnectionMode       string    `json:"connection_mode"`
	KubeconfigPath       string    `json:"-"`
	KubeconfigConfigured bool      `json:"kubeconfig_configured"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

const (
	ClusterConnectionKubeconfig = "kubeconfig"
	ClusterConnectionInCluster  = "in_cluster"
)

type CreateClusterInput struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	APIEndpoint    string `json:"api_endpoint"`
	KubeContext    string `json:"kube_context"`
	ConnectionMode string `json:"connection_mode"`
	KubeconfigPath string `json:"kubeconfig_path"`
}

type UpdateClusterInput struct {
	Name           *string `json:"name"`
	APIEndpoint    *string `json:"api_endpoint"`
	KubeContext    *string `json:"kube_context"`
	ConnectionMode *string `json:"connection_mode"`
	KubeconfigPath *string `json:"kubeconfig_path"`
	Status         *string `json:"status"`
}

func newCluster(spaceID string, input CreateClusterInput) (Cluster, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Cluster{}, fmt.Errorf("%w: cluster name is required", ErrInvalidInput)
	}
	if len(name) > 120 {
		return Cluster{}, fmt.Errorf("%w: cluster name is too long", ErrInvalidInput)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = "cluster-" + uuid.NewString()[:8]
	}
	if !validClusterIdentifier(id) {
		return Cluster{}, fmt.Errorf("%w: cluster id is invalid", ErrInvalidInput)
	}
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = "kubernetes"
	}
	if !strings.EqualFold(provider, "kubernetes") {
		return Cluster{}, fmt.Errorf("%w: only kubernetes clusters are supported", ErrInvalidInput)
	}
	mode := normalizeClusterConnectionMode(input.ConnectionMode)
	if mode == "" {
		return Cluster{}, fmt.Errorf("%w: connection mode is invalid", ErrInvalidInput)
	}
	path := strings.TrimSpace(input.KubeconfigPath)
	if strings.ContainsAny(path, "\x00\r\n") {
		return Cluster{}, fmt.Errorf("%w: kubeconfig path is invalid", ErrInvalidInput)
	}
	endpoint := strings.TrimSpace(input.APIEndpoint)
	if len(endpoint) > 500 || strings.ContainsAny(endpoint, "\x00\r\n") {
		return Cluster{}, fmt.Errorf("%w: api endpoint is invalid", ErrInvalidInput)
	}
	cluster := Cluster{
		ID:                   id,
		SpaceID:              spaceID,
		Name:                 name,
		Provider:             "kubernetes",
		APIEndpoint:          endpoint,
		KubeContext:          strings.TrimSpace(input.KubeContext),
		ConnectionMode:       mode,
		KubeconfigPath:       path,
		KubeconfigConfigured: path != "",
		Status:               "active",
	}
	return cluster, nil
}

func updateCluster(current Cluster, input UpdateClusterInput) (Cluster, error) {
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len(name) > 120 {
			return Cluster{}, fmt.Errorf("%w: cluster name is invalid", ErrInvalidInput)
		}
		current.Name = name
	}
	if input.APIEndpoint != nil {
		endpoint := strings.TrimSpace(*input.APIEndpoint)
		if len(endpoint) > 500 || strings.ContainsAny(endpoint, "\x00\r\n") {
			return Cluster{}, fmt.Errorf("%w: api endpoint is invalid", ErrInvalidInput)
		}
		current.APIEndpoint = endpoint
	}
	if input.KubeContext != nil {
		current.KubeContext = strings.TrimSpace(*input.KubeContext)
	}
	if input.ConnectionMode != nil {
		mode := normalizeClusterConnectionMode(*input.ConnectionMode)
		if mode == "" {
			return Cluster{}, fmt.Errorf("%w: connection mode is invalid", ErrInvalidInput)
		}
		current.ConnectionMode = mode
	}
	if input.KubeconfigPath != nil {
		path := strings.TrimSpace(*input.KubeconfigPath)
		if strings.ContainsAny(path, "\x00\r\n") {
			return Cluster{}, fmt.Errorf("%w: kubeconfig path is invalid", ErrInvalidInput)
		}
		current.KubeconfigPath = path
		current.KubeconfigConfigured = path != ""
	}
	if input.Status != nil {
		status := strings.TrimSpace(strings.ToLower(*input.Status))
		if status != "active" && status != "draining" && status != "offline" {
			return Cluster{}, fmt.Errorf("%w: cluster status is invalid", ErrInvalidInput)
		}
		current.Status = status
	}
	return current, nil
}

func normalizeClusterConnectionMode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ClusterConnectionKubeconfig
	}
	if value == ClusterConnectionKubeconfig || value == ClusterConnectionInCluster {
		return value
	}
	return ""
}

func validClusterIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func normalizeClusterID(_ string, requested string) string {
	return strings.TrimSpace(requested)
}

type CreateSpaceInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type UpdateSpaceInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type CreateSpaceMemberInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

type UpdateSpaceMemberInput struct {
	Role string `json:"role"`
}

type UpdateProjectInput struct {
	RepositoryURL   *string `json:"repository_url"`
	RepositoryID    *string `json:"-"`
	Description     *string `json:"description"`
	DefaultBranch   *string `json:"default_branch"`
	ClusterID       *string `json:"cluster_id"`
	Namespace       *string `json:"namespace"`
	Replicas        *int    `json:"replicas"`
	ContainerPort   *int    `json:"container_port"`
	ImageRepository *string `json:"image_repository"`
}

type CreateDeploymentTargetInput struct {
	Name           string `json:"name"`
	Environment    string `json:"environment"`
	Stage          string `json:"stage"`
	SortOrder      int    `json:"sort_order"`
	ClusterID      string `json:"cluster_id"`
	Namespace      string `json:"namespace"`
	Replicas       int    `json:"replicas"`
	ContainerPort  int    `json:"container_port"`
	DeployStrategy string `json:"deploy_strategy"`
	Enabled        *bool  `json:"enabled"`
}

type UpdateDeploymentTargetInput struct {
	Name           *string `json:"name"`
	Environment    *string `json:"environment"`
	Stage          *string `json:"stage"`
	SortOrder      *int    `json:"sort_order"`
	ClusterID      *string `json:"cluster_id"`
	Namespace      *string `json:"namespace"`
	Replicas       *int    `json:"replicas"`
	ContainerPort  *int    `json:"container_port"`
	DeployStrategy *string `json:"deploy_strategy"`
	Enabled        *bool   `json:"enabled"`
}

type SaveDeploymentConfigInput struct {
	Manifest string `json:"manifest"`
	Format   string `json:"format"`
}

type CreateABExperimentInput struct {
	Name             string
	TargetID         string
	Environment      string
	EnvironmentStage string
	ClusterID        string
	Namespace        string
	Replicas         int
	Strategy         string
	Assignment       string
	RoutingRule      domain.ABRoutingRule
	AVersion         domain.ABExperimentVersion
	BVersion         domain.ABExperimentVersion
	ATraffic         int
	BTraffic         int
	CreatedBy        uint64
}

type UpdateABTrafficInput struct {
	ATraffic int
	BTraffic int
}

// Store is deliberately narrower than the database schema. It keeps request
// handlers independent from GORM and makes isolated tests deterministic.
type Store interface {
	Close() error
	Authenticate(ctx context.Context, username, password string) (domain.User, error)
	User(ctx context.Context, id uint64) (domain.User, error)
	ListSpaces(ctx context.Context, userID uint64) ([]domain.Space, error)
	Space(ctx context.Context, userID uint64, spaceID string) (domain.Space, error)
	Role(ctx context.Context, userID uint64, spaceID string) (string, error)
	CreateSpace(ctx context.Context, userID uint64, input CreateSpaceInput) (domain.Space, error)
	UpdateSpace(ctx context.Context, spaceID string, input UpdateSpaceInput) (domain.Space, error)
	ListSpaceMembers(ctx context.Context, spaceID string) ([]domain.SpaceMember, error)
	CreateSpaceMember(ctx context.Context, spaceID string, input CreateSpaceMemberInput) (domain.SpaceMember, error)
	UpdateSpaceMember(ctx context.Context, spaceID string, userID uint64, input UpdateSpaceMemberInput) (domain.SpaceMember, error)
	RemoveSpaceMember(ctx context.Context, spaceID string, userID uint64) error
	ListClusters(ctx context.Context, spaceID string) ([]Cluster, error)
	GetCluster(ctx context.Context, spaceID, clusterID string) (Cluster, error)
	CreateCluster(ctx context.Context, spaceID string, input CreateClusterInput) (Cluster, error)
	UpdateCluster(ctx context.Context, spaceID, clusterID string, input UpdateClusterInput) (Cluster, error)
	ListProjects(ctx context.Context, spaceID string) ([]domain.Project, error)
	GetProject(ctx context.Context, spaceID, projectID string) (domain.Project, error)
	CreateProject(ctx context.Context, spaceID string, input CreateProjectInput) (domain.Project, error)
	UpdateProject(ctx context.Context, spaceID, projectID string, input UpdateProjectInput) (domain.Project, error)
	GetProjectGitCredential(ctx context.Context, spaceID, projectID string) (ProjectGitCredential, error)
	SaveProjectGitCredential(ctx context.Context, spaceID, projectID string, input SaveProjectGitCredentialInput) (ProjectGitCredential, error)
	DeleteProjectGitCredential(ctx context.Context, spaceID, projectID string) error
	ListDeploymentTargets(ctx context.Context, spaceID, projectID string) ([]domain.DeploymentTarget, error)
	GetDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string) (domain.DeploymentTarget, error)
	CreateDeploymentTarget(ctx context.Context, spaceID, projectID string, input CreateDeploymentTargetInput) (domain.DeploymentTarget, error)
	UpdateDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string, input UpdateDeploymentTargetInput) (domain.DeploymentTarget, error)
	DeleteDeploymentTarget(ctx context.Context, spaceID, projectID, targetID string) error
	GetDeploymentConfig(ctx context.Context, spaceID, projectID string) (domain.DeploymentConfig, error)
	SaveDeploymentConfig(ctx context.Context, spaceID, projectID string, input SaveDeploymentConfigInput) (domain.DeploymentConfig, error)
	AppendAuditLog(ctx context.Context, entry domain.AuditLog) error
	ListAuditLogs(ctx context.Context, spaceID string, limit int) ([]domain.AuditLog, error)
	ListABExperiments(ctx context.Context, spaceID, projectID string) ([]domain.ABExperiment, error)
	GetABExperiment(ctx context.Context, spaceID, projectID, experimentID string) (domain.ABExperiment, error)
	CreateABExperiment(ctx context.Context, spaceID, projectID string, input CreateABExperimentInput) (domain.ABExperiment, error)
	UpdateABExperimentTraffic(ctx context.Context, spaceID, projectID, experimentID string, input UpdateABTrafficInput) (domain.ABExperiment, error)
	StopABExperiment(ctx context.Context, spaceID, projectID, experimentID string) (domain.ABExperiment, error)
	FinishABExperiment(ctx context.Context, spaceID, projectID, experimentID, result string) (domain.ABExperiment, error)
}
