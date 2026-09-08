package domain

import "time"

// User is the small identity record needed by the control plane. PasswordHash
// never leaves the store or appears in an API response.
type User struct {
	ID           uint64
	Username     string
	DisplayName  string
	PasswordHash string
	IsSuperAdmin bool
}

type Space struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// SpaceMember is the safe, display-oriented view of a space membership.
// Password hashes and other authentication data never leave the store.
type SpaceMember struct {
	UserID        uint64    `json:"user_id"`
	Username      string    `json:"username"`
	DisplayName   string    `json:"display_name"`
	Role          string    `json:"role"`
	IsCurrentUser bool      `json:"is_current_user"`
	IsSuperAdmin  bool      `json:"is_super_admin"`
	JoinedAt      time.Time `json:"joined_at"`
}

type Project struct {
	ID                    string    `json:"id"`
	SpaceID               string    `json:"space_id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	RepositoryID          string    `json:"repository_id"`
	RepositoryURL         string    `json:"repository_url"`
	DefaultBranch         string    `json:"default_branch"`
	ClusterID             string    `json:"cluster_id"`
	Namespace             string    `json:"namespace"`
	DeployStrategy        string    `json:"deploy_strategy"`
	Replicas              int       `json:"replicas"`
	ContainerPort         int       `json:"container_port"`
	ImageRepository       string    `json:"image_repository,omitempty"`
	Health                string    `json:"health,omitempty"`
	PodCount              int       `json:"pod_count"`
	HealthyPodCount       int       `json:"healthy_pod_count"`
	DeploymentTargetCount int       `json:"deployment_target_count"`
	DefaultTargetID       string    `json:"default_target_id,omitempty"`
	LastRelease           string    `json:"last_release,omitempty"`
	LastCommit            string    `json:"last_commit,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// DeploymentTarget is the environment a project can be released to. A target
// may point at a namespace in the same cluster as another target, or at a
// completely different registered cluster.
type DeploymentTarget struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	SpaceID         string    `json:"space_id"`
	Name            string    `json:"name"`
	Environment     string    `json:"environment"`
	Stage           string    `json:"stage"`
	SortOrder       int       `json:"sort_order"`
	ClusterID       string    `json:"cluster_id"`
	Namespace       string    `json:"namespace"`
	Replicas        int       `json:"replicas"`
	ContainerPort   int       `json:"container_port"`
	DeployStrategy  string    `json:"deploy_strategy"`
	Enabled         bool      `json:"enabled"`
	Status          string    `json:"status"`
	Health          string    `json:"health,omitempty"`
	PodCount        int       `json:"pod_count"`
	HealthyPodCount int       `json:"healthy_pod_count"`
	LastRelease     string    `json:"last_release,omitempty"`
	LastCommit      string    `json:"last_commit,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DeploymentResource is the small summary returned alongside a saved
// Kubernetes manifest. The manifest itself remains the source of truth; this
// summary is only used by the console to show what will be applied.
type DeploymentResource struct {
	APIVersion       string `json:"api_version"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	Namespace        string `json:"namespace,omitempty"`
	ReleaseSupported bool   `json:"release_supported"`
}

type DeploymentConfigCapabilities struct {
	SupportedKinds       []string             `json:"supported_kinds"`
	UnsupportedResources []DeploymentResource `json:"unsupported_resources,omitempty"`
}

// DeploymentConfig stores the native Kubernetes configuration for a project.
// Helm is deliberately not part of this model: a project can publish a plain
// multi-document YAML or JSON manifest directly.
type DeploymentConfig struct {
	ProjectID     string                       `json:"project_id"`
	Namespace     string                       `json:"namespace"`
	Format        string                       `json:"format"`
	Manifest      string                       `json:"manifest"`
	Version       int                          `json:"version"`
	ResourceCount int                          `json:"resource_count"`
	Resources     []DeploymentResource         `json:"resources"`
	Capabilities  DeploymentConfigCapabilities `json:"capabilities"`
	IsDefault     bool                         `json:"is_default"`
	UpdatedAt     time.Time                    `json:"updated_at"`
}

type AuditLog struct {
	ID        uint64    `json:"id"`
	SpaceID   string    `json:"space_id"`
	UserID    uint64    `json:"user_id"`
	UserName  string    `json:"user_name,omitempty"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
}
