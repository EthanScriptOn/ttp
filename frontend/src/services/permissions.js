export const PERMISSIONS = Object.freeze({
  SPACE_READ: 'space:read',
  SPACE_UPDATE: 'space:update',
  MEMBER_READ: 'member:read',
  MEMBER_MANAGE: 'member:manage',
  CLUSTER_READ: 'cluster:read',
  CLUSTER_MANAGE: 'cluster:manage',
  PROJECT_READ: 'project:read',
  PROJECT_CREATE: 'project:create',
  PROJECT_UPDATE: 'project:update',
  RELEASE_READ: 'release:read',
  RELEASE_CREATE: 'release:create',
  RELEASE_UPDATE: 'release:update',
  RELEASE_PUBLISH: 'release:publish',
  RUNTIME_READ: 'runtime:read',
  RUNTIME_CONFIG: 'runtime:config',
  RUNTIME_TERMINAL: 'runtime:terminal',
  AUDIT_READ: 'audit:read',
})

export const ROLE_LABELS = Object.freeze({
  owner: '所有者',
  admin: '管理员',
  developer: '开发者',
  viewer: '只读成员',
})

export const ROLE_DESCRIPTIONS = Object.freeze({
  owner: '空间的最终负责人，可以管理全部设置和成员。',
  admin: '负责空间日常管理，但不能移除或降级所有者。',
  developer: '可以创建项目、准备发布和修改运行配置。',
  viewer: '只能查看项目、集群、发布和运行状态。',
})

const ALL_PERMISSIONS = Object.values(PERMISSIONS)

const ROLE_PERMISSIONS = Object.freeze({
  owner: ALL_PERMISSIONS,
  admin: ALL_PERMISSIONS,
  developer: [
    PERMISSIONS.SPACE_READ,
    PERMISSIONS.MEMBER_READ,
    PERMISSIONS.CLUSTER_READ,
    PERMISSIONS.PROJECT_READ,
    PERMISSIONS.PROJECT_CREATE,
    PERMISSIONS.PROJECT_UPDATE,
    PERMISSIONS.RELEASE_READ,
    PERMISSIONS.RELEASE_CREATE,
    PERMISSIONS.RELEASE_UPDATE,
    PERMISSIONS.RELEASE_PUBLISH,
    PERMISSIONS.RUNTIME_READ,
    PERMISSIONS.RUNTIME_CONFIG,
    PERMISSIONS.RUNTIME_TERMINAL,
    PERMISSIONS.AUDIT_READ,
  ],
  viewer: [
    PERMISSIONS.SPACE_READ,
    PERMISSIONS.MEMBER_READ,
    PERMISSIONS.CLUSTER_READ,
    PERMISSIONS.PROJECT_READ,
    PERMISSIONS.RELEASE_READ,
    PERMISSIONS.RUNTIME_READ,
    PERMISSIONS.AUDIT_READ,
  ],
})

export function normalizeRole(value, fallback = 'viewer') {
  const role = String(value || '').trim().toLowerCase()
  return ROLE_LABELS[role] ? role : fallback
}

export function roleLabel(value) {
  return ROLE_LABELS[normalizeRole(value)] || '未知角色'
}

export function roleDescription(value) {
  return ROLE_DESCRIPTIONS[normalizeRole(value)] || '当前角色没有可用说明。'
}

export function permissionsForRole(value) {
  return [...(ROLE_PERMISSIONS[normalizeRole(value)] || [])]
}

export function hasPermission(role, permission, isSuperAdmin = false) {
  if (isSuperAdmin) return true
  const normalized = normalizeRole(role)
  return ROLE_PERMISSIONS[normalized]?.includes(permission) || false
}

export function canManageMembers(role, isSuperAdmin = false) {
  return hasPermission(role, PERMISSIONS.MEMBER_MANAGE, isSuperAdmin)
}

export const ROLE_OPTIONS = Object.freeze([
  { value: 'admin', label: ROLE_LABELS.admin },
  { value: 'developer', label: ROLE_LABELS.developer },
  { value: 'viewer', label: ROLE_LABELS.viewer },
])

export const ROLE_ORDER = Object.freeze(['owner', 'admin', 'developer', 'viewer'])

export function localRoleDefinitions() {
  return ROLE_ORDER.map((key) => ({
    key,
    name: ROLE_LABELS[key],
    description: ROLE_DESCRIPTIONS[key],
    permissions: permissionsForRole(key),
  }))
}
