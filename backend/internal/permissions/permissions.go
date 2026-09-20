package permissions

import "strings"

// Permission is a capability exposed in the space settings page. The key is
// also the value used by the HTTP authorization middleware.
type Permission struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Role describes the effective capabilities of a space role. Permissions are
// returned as concrete keys even for owner/admin, which keeps the UI simple.
type Role struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

const (
	SpaceRead       = "space:read"
	SpaceUpdate     = "space:update"
	MemberRead      = "member:read"
	MemberManage    = "member:manage"
	ClusterRead     = "cluster:read"
	ClusterManage   = "cluster:manage"
	ProjectRead     = "project:read"
	ProjectCreate   = "project:create"
	ProjectUpdate   = "project:update"
	RegistryRead    = "registry:read"
	RegistryManage  = "registry:manage"
	ReleaseRead     = "release:read"
	ReleaseCreate   = "release:create"
	ReleaseUpdate   = "release:update"
	ReleasePublish  = "release:publish"
	RuntimeRead     = "runtime:read"
	RuntimeConfig   = "runtime:config"
	RuntimeTerminal = "runtime:terminal"
	AuditRead       = "audit:read"

	// Project permissions are evaluated against a project membership. Space
	// owners and admins implicitly receive all of them for projects in their
	// space; other members must be explicitly assigned a project role.
	ProjectView             = "project:view"
	ProjectSettings         = "project:settings"
	ProjectGitRead          = "project:git:read"
	ProjectGitManage        = "project:git:manage"
	ProjectDeploymentRead   = "project:deployment:read"
	ProjectDeploymentManage = "project:deployment:manage"
	ProjectReleaseRead      = "project:release:read"
	ProjectReleaseCreate    = "project:release:create"
	ProjectReleaseUpdate    = "project:release:update"
	ProjectReleasePublish   = "project:release:publish"
	ProjectRuntimeRead      = "project:runtime:read"
	ProjectRuntimeConfig    = "project:runtime:config"
	ProjectRuntimeTerminal  = "project:runtime:terminal"
	ProjectMembersRead      = "project:members:read"
	ProjectMembersManage    = "project:members:manage"
	ProjectRolesManage      = "project:roles:manage"
)

var permissionCatalog = []Permission{
	{Key: SpaceRead, Name: "查看空间", Description: "查看当前空间和基础信息"},
	{Key: SpaceUpdate, Name: "修改空间设置", Description: "修改空间名称和说明"},
	{Key: MemberRead, Name: "查看成员", Description: "查看空间成员和角色"},
	{Key: MemberManage, Name: "管理成员", Description: "添加、移除成员或调整角色"},
	{Key: ClusterRead, Name: "查看集群", Description: "查看集群连接和监控信息"},
	{Key: ClusterManage, Name: "管理集群", Description: "添加、修改和测试 Kubernetes 集群"},
	{Key: ProjectRead, Name: "查看项目", Description: "查看项目、仓库和部署配置"},
	{Key: ProjectCreate, Name: "创建项目", Description: "把代码仓库登记为项目"},
	{Key: ProjectUpdate, Name: "修改项目", Description: "修改仓库、命名空间和部署配置"},
	{Key: RegistryRead, Name: "查看镜像仓库连接", Description: "查看当前空间可用的镜像仓库连接"},
	{Key: RegistryManage, Name: "管理镜像仓库连接", Description: "新增、修改、测试和删除镜像仓库连接"},
	{Key: ReleaseRead, Name: "查看发布", Description: "查看发布记录和发布进度"},
	{Key: ReleaseCreate, Name: "创建发布", Description: "选择分支和 commit 创建发布草稿"},
	{Key: ReleaseUpdate, Name: "修改发布草稿", Description: "从草稿中移除 commit"},
	{Key: ReleasePublish, Name: "执行发布", Description: "开始、取消或重复发布"},
	{Key: RuntimeRead, Name: "查看运行态", Description: "查看 Pod、日志和运行指标"},
	{Key: RuntimeConfig, Name: "修改运行配置", Description: "修改 Pod 对应的运行时配置"},
	{Key: RuntimeTerminal, Name: "进入 Pod 终端", Description: "在 Pod 容器内执行命令"},
	{Key: AuditRead, Name: "查看操作记录", Description: "查看空间内的审计记录"},
	{Key: ProjectRolesManage, Name: "管理项目角色", Description: "创建、修改和删除空间中的自定义项目角色"},
}

var allPermissionKeys = permissionKeys()

var roleCatalog = []Role{
	{Key: "owner", Name: "所有者", Description: "空间的最终负责人，可以管理全部设置和成员。", Permissions: allPermissionKeys},
	{Key: "admin", Name: "管理员", Description: "负责空间日常管理，但不能移除或降级所有者。", Permissions: allPermissionKeys},
	{Key: "developer", Name: "开发者", Description: "可以创建项目、准备发布和修改运行配置。", Permissions: []string{
		SpaceRead, MemberRead, ClusterRead, ProjectRead, ProjectCreate, ProjectUpdate, RegistryRead,
		ReleaseRead, ReleaseCreate, ReleaseUpdate, ReleasePublish, RuntimeRead, RuntimeConfig, RuntimeTerminal, AuditRead,
	}},
	{Key: "viewer", Name: "只读成员", Description: "只能查看项目、集群、发布和运行状态。", Permissions: []string{
		SpaceRead, MemberRead, ClusterRead, ProjectRead, ReleaseRead, RuntimeRead, AuditRead,
	}},
}

var projectPermissionCatalog = []Permission{
	{Key: ProjectView, Name: "查看项目", Description: "查看项目基本信息和项目内资源"},
	{Key: ProjectSettings, Name: "管理项目设置", Description: "修改项目基本信息和默认发布配置"},
	{Key: ProjectGitRead, Name: "读取代码仓库", Description: "查看分支、提交、标签和 Git 连接状态"},
	{Key: ProjectGitManage, Name: "管理代码仓库连接", Description: "配置、替换和删除项目 Git 机器人"},
	{Key: ProjectDeploymentRead, Name: "查看部署配置", Description: "查看发布环境和 Kubernetes 资源文件"},
	{Key: ProjectDeploymentManage, Name: "管理部署配置", Description: "管理发布环境和 Kubernetes 资源文件"},
	{Key: ProjectReleaseRead, Name: "查看发布", Description: "查看发布记录、详情和日志"},
	{Key: ProjectReleaseCreate, Name: "创建发布", Description: "创建发布草稿和发布准备单"},
	{Key: ProjectReleaseUpdate, Name: "修改发布草稿", Description: "修改或删除发布草稿中的内容"},
	{Key: ProjectReleasePublish, Name: "执行发布", Description: "执行、取消、重试发布和管理 A/B 实验"},
	{Key: ProjectRuntimeRead, Name: "查看运行态", Description: "查看 Pod、日志和监控指标"},
	{Key: ProjectRuntimeConfig, Name: "修改运行配置", Description: "修改 Pod 运行时配置"},
	{Key: ProjectRuntimeTerminal, Name: "进入 Pod 终端", Description: "在 Pod 容器内执行命令"},
	{Key: ProjectMembersRead, Name: "查看项目成员", Description: "查看项目成员和角色绑定"},
	{Key: ProjectMembersManage, Name: "管理项目成员", Description: "添加、移除成员或调整项目角色"},
}

var projectRoleCatalog = []Role{
	{Key: "project_viewer", Name: "项目只读", Description: "查看项目、代码、部署和运行状态。", Permissions: []string{
		ProjectView, ProjectGitRead, ProjectDeploymentRead, ProjectReleaseRead, ProjectRuntimeRead,
	}},
	{Key: "project_developer", Name: "项目开发者", Description: "修改项目配置和部署资源，可以创建发布草稿。", Permissions: []string{
		ProjectView, ProjectSettings, ProjectGitRead, ProjectDeploymentRead, ProjectDeploymentManage,
		ProjectReleaseRead, ProjectReleaseCreate, ProjectReleaseUpdate, ProjectRuntimeRead, ProjectRuntimeConfig,
	}},
	{Key: "project_release_manager", Name: "发布负责人", Description: "负责发布流程和发布运行态，但不管理项目成员。", Permissions: []string{
		ProjectView, ProjectGitRead, ProjectDeploymentRead, ProjectReleaseRead, ProjectReleaseCreate,
		ProjectReleaseUpdate, ProjectReleasePublish, ProjectRuntimeRead,
	}},
	{Key: "project_maintainer", Name: "项目维护者", Description: "拥有项目全部权限，包括项目成员授权。", Permissions: []string{
		ProjectView, ProjectSettings, ProjectGitRead, ProjectGitManage, ProjectDeploymentRead, ProjectDeploymentManage,
		ProjectReleaseRead, ProjectReleaseCreate, ProjectReleaseUpdate, ProjectReleasePublish,
		ProjectRuntimeRead, ProjectRuntimeConfig, ProjectRuntimeTerminal, ProjectMembersRead, ProjectMembersManage,
	}},
}

func permissionKeys() []string {
	keys := make([]string, 0, len(permissionCatalog))
	for _, item := range permissionCatalog {
		keys = append(keys, item.Key)
	}
	return keys
}

// NormalizeRole returns the canonical role key or an empty string.
func NormalizeRole(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "owner", "admin", "developer", "viewer":
		return value
	default:
		return ""
	}
}

func IsRole(value string) bool { return NormalizeRole(value) != "" }

// Has checks an effective permission. The wildcard is accepted for callers
// that keep a compact owner/admin permission map.
func Has(role, permission string) bool {
	role = NormalizeRole(role)
	permission = strings.TrimSpace(permission)
	if role == "owner" || role == "admin" {
		return permission != ""
	}
	for _, item := range roleCatalog {
		if item.Key != role {
			continue
		}
		for _, key := range item.Permissions {
			if key == permission || key == "*" {
				return true
			}
		}
	}
	return false
}

func PermissionsFor(role string) []string {
	role = NormalizeRole(role)
	if role == "owner" || role == "admin" {
		return append([]string(nil), allPermissionKeys...)
	}
	for _, item := range roleCatalog {
		if item.Key == role {
			return append([]string(nil), item.Permissions...)
		}
	}
	return nil
}

func PermissionDefinitions() []Permission {
	return append([]Permission(nil), permissionCatalog...)
}

func RoleDefinitions() []Role {
	result := make([]Role, 0, len(roleCatalog))
	for _, item := range roleCatalog {
		copyItem := item
		copyItem.Permissions = append([]string(nil), item.Permissions...)
		result = append(result, copyItem)
	}
	return result
}

func ProjectPermissionDefinitions() []Permission {
	return append([]Permission(nil), projectPermissionCatalog...)
}

func ProjectRoleDefinitions() []Role {
	result := make([]Role, 0, len(projectRoleCatalog))
	for _, item := range projectRoleCatalog {
		copyItem := item
		copyItem.Permissions = append([]string(nil), item.Permissions...)
		result = append(result, copyItem)
	}
	return result
}

func ProjectRoleDefinition(key string) (Role, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, item := range projectRoleCatalog {
		if item.Key == key {
			item.Permissions = append([]string(nil), item.Permissions...)
			return item, true
		}
	}
	return Role{}, false
}

func IsProjectPermission(value string) bool {
	value = strings.TrimSpace(value)
	for _, item := range projectPermissionCatalog {
		if item.Key == value {
			return true
		}
	}
	return false
}

func RoleName(role string) string {
	role = NormalizeRole(role)
	for _, item := range roleCatalog {
		if item.Key == role {
			return item.Name
		}
	}
	return "未知角色"
}

// CanManageTarget prevents an administrator from changing the owner. Owner
// transfer is deliberately a separate future workflow with an explicit
// confirmation step.
func CanManageTarget(actorRole, targetRole string) bool {
	actorRole = NormalizeRole(actorRole)
	targetRole = NormalizeRole(targetRole)
	if actorRole != "owner" && actorRole != "admin" {
		return false
	}
	return targetRole != "owner"
}

// CanAssignRole prevents the member API from creating a second owner through
// a normal add-member request.
func CanAssignRole(actorRole, targetRole string) bool {
	return CanManageTarget(actorRole, "developer") && NormalizeRole(targetRole) != "owner" && IsRole(targetRole)
}
