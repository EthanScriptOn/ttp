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

// NamespaceQuota is the resource budget attached to one generated TTP
// namespace. The quota is shared by every project that targets the same
// space/cluster/environment tuple. Quantity values use Kubernetes syntax,
// such as "500m", "2", "512Mi", "4Gi" and "10Gi".
type NamespaceQuota struct {
	CPURequest                     string `json:"cpu_request"`
	CPULimit                       string `json:"cpu_limit"`
	MemoryRequest                  string `json:"memory_request"`
	MemoryLimit                    string `json:"memory_limit"`
	EphemeralStorageRequest        string `json:"ephemeral_storage_request"`
	EphemeralStorageLimit          string `json:"ephemeral_storage_limit"`
	Storage                        string `json:"storage"`
	Pods                           int    `json:"pods"`
	PersistentVolumeClaims         int    `json:"persistent_volume_claims"`
	DefaultCPURequest              string `json:"default_cpu_request"`
	DefaultCPULimit                string `json:"default_cpu_limit"`
	DefaultMemoryRequest           string `json:"default_memory_request"`
	DefaultMemoryLimit             string `json:"default_memory_limit"`
	DefaultEphemeralStorageRequest string `json:"default_ephemeral_storage_request"`
	DefaultEphemeralStorageLimit   string `json:"default_ephemeral_storage_limit"`
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
	AutoMergeEnabled      bool      `json:"auto_merge_enabled"`
	AutoMergeTargetID     string    `json:"auto_merge_target_id,omitempty"`
	ClusterID             string    `json:"cluster_id"`
	Namespace             string    `json:"namespace"`
	DeployStrategy        string    `json:"deploy_strategy"`
	Replicas              int       `json:"replicas"`
	ContainerPort         int       `json:"container_port"`
	ImageRepository       string    `json:"image_repository,omitempty"`
	RegistryConnectionID  string    `json:"registry_connection_id,omitempty"`
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

// ProjectRole is a role that can be assigned to a project member. System
// roles are defined by the application; custom roles persist their permission
// bindings in the database.
type ProjectRole struct {
	ID          string    `json:"id"`
	SpaceID     string    `json:"space_id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	Permissions []string  `json:"permissions"`
	CreatedBy   uint64    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectMember is the safe project membership view returned by the access
// management API. It intentionally contains no authentication material.
type ProjectMember struct {
	ProjectID     string    `json:"project_id"`
	SpaceID       string    `json:"space_id"`
	UserID        uint64    `json:"user_id"`
	Username      string    `json:"username"`
	DisplayName   string    `json:"display_name"`
	RoleID        string    `json:"role_id,omitempty"`
	RoleKey       string    `json:"role_key"`
	RoleName      string    `json:"role_name"`
	Permissions   []string  `json:"permissions"`
	IsCurrentUser bool      `json:"is_current_user,omitempty"`
	IsSuperAdmin  bool      `json:"is_super_admin,omitempty"`
	JoinedAt      time.Time `json:"joined_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ProjectAccess describes the current user's effective project role.
type ProjectAccess struct {
	ProjectID    string   `json:"project_id"`
	SpaceID      string   `json:"space_id"`
	RoleID       string   `json:"role_id,omitempty"`
	RoleKey      string   `json:"role_key"`
	RoleName     string   `json:"role_name"`
	Permissions  []string `json:"permissions"`
	IsSpaceAdmin bool     `json:"is_space_admin"`
	IsSuperAdmin bool     `json:"is_super_admin"`
}

// ImageRegistryConnection is the safe, space-scoped metadata for an OCI
// registry. CredentialCiphertext is kept in the store for internal use and is
// never serialized to an API client.
type ImageRegistryConnection struct {
	ID                   string     `json:"id"`
	SpaceID              string     `json:"space_id"`
	Name                 string     `json:"name"`
	Registry             string     `json:"registry"`
	AuthType             string     `json:"auth_type"`
	Username             string     `json:"username,omitempty"`
	PullSecretName       string     `json:"pull_secret_name"`
	Configured           bool       `json:"configured"`
	Status               string     `json:"status"`
	LastCheckedAt        *time.Time `json:"last_checked_at,omitempty"`
	CredentialCiphertext string     `json:"-"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// DeploymentTarget is the environment a project can be released to. A target
// may point at a namespace in the same cluster as another target, or at a
// completely different registered cluster.
type DeploymentTarget struct {
	ID              string         `json:"id"`
	ProjectID       string         `json:"project_id"`
	SpaceID         string         `json:"space_id"`
	Name            string         `json:"name"`
	Environment     string         `json:"environment"`
	Stage           string         `json:"stage"`
	SortOrder       int            `json:"sort_order"`
	ClusterID       string         `json:"cluster_id"`
	Namespace       string         `json:"namespace"`
	Replicas        int            `json:"replicas"`
	ContainerPort   int            `json:"container_port"`
	DeployStrategy  string         `json:"deploy_strategy"`
	Enabled         bool           `json:"enabled"`
	Status          string         `json:"status"`
	ResourceQuota   NamespaceQuota `json:"resource_quota"`
	Health          string         `json:"health,omitempty"`
	PodCount        int            `json:"pod_count"`
	HealthyPodCount int            `json:"healthy_pod_count"`
	LastRelease     string         `json:"last_release,omitempty"`
	LastCommit      string         `json:"last_commit,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// DeploymentResource is the small summary shown by the console for a
// Kubernetes resource file.
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

// DeploymentConfig is the resolved deployment summary. New deployments use
// Files, with one independently applicable Kubernetes object per file.
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
	Files         []DeploymentResourceFile     `json:"files,omitempty"`
}

// DeploymentResourceFile is one independently applicable Kubernetes resource
// file. Content contains exactly one Kubernetes object; different resources
// are never encoded into one delimiter-separated manifest.
type DeploymentResourceFile struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Name              string    `json:"name"`
	Path              string    `json:"path"`
	Format            string    `json:"format"`
	Content           string    `json:"content"`
	APIVersion        string    `json:"api_version"`
	Kind              string    `json:"kind"`
	ResourceName      string    `json:"resource_name"`
	Namespace         string    `json:"namespace,omitempty"`
	SortOrder         int       `json:"sort_order"`
	Version           int       `json:"version"`
	ReleaseSupported  bool      `json:"release_supported"`
	UpdatedAt         time.Time `json:"updated_at"`
	Scope             string    `json:"scope,omitempty"`
	TargetID          string    `json:"target_id,omitempty"`
	GlobalResourceID  string    `json:"global_resource_id,omitempty"`
	OverrideID        string    `json:"override_id,omitempty"`
	BaseGlobalVersion int       `json:"base_global_version,omitempty"`
	GlobalVersion     int       `json:"global_version,omitempty"`
	BaseContent       string    `json:"base_content,omitempty"`
	GlobalContent     string    `json:"global_content,omitempty"`
	GlobalChanged     bool      `json:"global_changed,omitempty"`
}

// DeploymentResourceOverride stores one environment's complete replacement
// for a global file, or an environment-only resource when GlobalResourceID is
// empty. BaseGlobalContent is the merge base captured when the override was
// created or last reconciled with the global file.
type DeploymentResourceOverride struct {
	ID                string
	ProjectID         string
	TargetID          string
	GlobalResourceID  string
	Name              string
	Path              string
	Format            string
	Content           string
	APIVersion        string
	Kind              string
	ResourceName      string
	Namespace         string
	SortOrder         int
	Version           int
	ReleaseSupported  bool
	BaseGlobalVersion int
	BaseGlobalContent string
	UpdatedAt         time.Time
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
