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
	ReleaseRead     = "release:read"
	ReleaseCreate   = "release:create"
	ReleaseUpdate   = "release:update"
	ReleasePublish  = "release:publish"
	RuntimeRead     = "runtime:read"
	RuntimeConfig   = "runtime:config"
	RuntimeTerminal = "runtime:terminal"
	AuditRead       = "audit:read"
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
	{Key: ReleaseRead, Name: "查看发布", Description: "查看发布记录和发布进度"},
	{Key: ReleaseCreate, Name: "创建发布", Description: "选择分支和 commit 创建发布草稿"},
	{Key: ReleaseUpdate, Name: "修改发布草稿", Description: "从草稿中移除 commit"},
	{Key: ReleasePublish, Name: "执行发布", Description: "开始、取消或重复发布"},
	{Key: RuntimeRead, Name: "查看运行态", Description: "查看 Pod、日志和运行指标"},
	{Key: RuntimeConfig, Name: "修改运行配置", Description: "修改 Pod 对应的运行时配置"},
	{Key: RuntimeTerminal, Name: "进入 Pod 终端", Description: "在 Pod 容器内执行命令"},
	{Key: AuditRead, Name: "查看操作记录", Description: "查看空间内的审计记录"},
}

var allPermissionKeys = permissionKeys()

var roleCatalog = []Role{
	{Key: "owner", Name: "所有者", Description: "空间的最终负责人，可以管理全部设置和成员。", Permissions: allPermissionKeys},
	{Key: "admin", Name: "管理员", Description: "负责空间日常管理，但不能移除或降级所有者。", Permissions: allPermissionKeys},
	{Key: "developer", Name: "开发者", Description: "可以创建项目、准备发布和修改运行配置。", Permissions: []string{
		SpaceRead, MemberRead, ClusterRead, ProjectRead, ProjectCreate, ProjectUpdate,
		ReleaseRead, ReleaseCreate, ReleaseUpdate, ReleasePublish, RuntimeRead, RuntimeConfig, RuntimeTerminal, AuditRead,
	}},
	{Key: "viewer", Name: "只读成员", Description: "只能查看项目、集群、发布和运行状态。", Permissions: []string{
		SpaceRead, MemberRead, ClusterRead, ProjectRead, ReleaseRead, RuntimeRead, AuditRead,
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
