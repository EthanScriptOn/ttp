const API = '/api'

export class ApiError extends Error {
  constructor(message, { status = 0, code = 'api_error', cause } = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    if (cause) this.cause = cause
  }
}

class NetworkError extends Error {
  constructor(cause) {
    super('后端服务暂时无法连接，请确认服务已启动')
    this.name = 'NetworkError'
    this.cause = cause
  }
}

export function storageValue(key) {
  if (typeof localStorage === 'undefined') return ''
  try {
    return localStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

export function isRecord(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

export function firstValue(source, ...keys) {
  for (const key of keys) {
    const value = source?.[key]
    if (value !== undefined && value !== null) return value
  }
  return undefined
}

export function asString(value, fallback = '') {
  if (typeof value === 'string') return value
  if (typeof value === 'number') return String(value)
  return fallback
}

export function asNumber(value, fallback = 0) {
  const number = Number(value)
  return Number.isFinite(number) ? number : fallback
}

export function nullableNumber(value) {
  if (value === null || value === undefined || value === '') return null
  if (typeof value === 'string' && ['unavailable', 'unknown', 'n/a', 'na', '-'].includes(value.trim().toLowerCase())) return null
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}

export function asBoolean(value, fallback = false) {
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') return value.toLowerCase() === 'true'
  if (typeof value === 'number') return value !== 0
  return fallback
}

export function responseItems(payload) {
  if (Array.isArray(payload)) return payload
  if (!isRecord(payload)) return []
  if (Array.isArray(payload.items)) return payload.items
  if (Array.isArray(payload.list)) return payload.list
  if (Array.isArray(payload.data)) return payload.data
  if (isRecord(payload.data) && Array.isArray(payload.data.items)) return payload.data.items
  if (isRecord(payload.data) && Array.isArray(payload.data.list)) return payload.data.list
  return []
}

function responseErrorCode(payload) {
  if (!isRecord(payload)) return ''
  if (typeof payload.error === 'string') return ''
  return asString(payload.error?.code || payload.code || payload.data?.error?.code)
}

function responseErrorMessage(payload) {
  if (!isRecord(payload)) return ''
  const error = payload.error
  const candidates = [
    typeof error === 'string' ? error : '',
    error?.message,
    payload.message,
    payload.error_message,
    Array.isArray(payload.errors) ? payload.errors[0]?.message || payload.errors[0] : '',
  ]
  return candidates.find((value) => typeof value === 'string' && value.trim())?.trim() || ''
}

const codeMessages = {
  unauthorized: '登录已失效，请重新登录',
  forbidden: '没有权限执行这个操作',
  project_forbidden: '当前账号没有该项目的操作权限',
  git_write_access_denied: '项目仓库机器人没有目标仓库的写权限',
  git_access_check_timeout: '检查项目仓库机器人超时，请稍后重试',
  git_access_check_failed: '暂时无法检查项目仓库机器人，请稍后重试',
  image_build_unsupported: '未配置可用的镜像构建器，发布已被阻止',
  image_builder_unauthorized: 'TTP 无法认证 Builder，请检查 Builder 地址和访问令牌',
  image_registry_preflight_failed: '镜像仓库预检失败，请检查仓库地址、凭证和推送权限',
  registry_connection_test_failed: '镜像仓库连接测试失败，请检查地址和凭证',
  kubernetes_release_access_denied: 'TTP 使用的 Kubernetes 身份没有目标环境的发布权限，请检查 RoleBinding',
  provider_timeout: 'Git 服务响应超时，请稍后重试',
  provider_error: 'Git 服务暂时不可用，请稍后重试',
  git_credential_invalid: '项目仓库机器人授权已失效，请在项目设置中重新保存',
  repository_conflict: '这个仓库地址已经绑定到其他项目，请检查项目配置',
  space_required: '请先选择空间',
  invalid_credentials: '用户名或密码错误',
  git_credential_required: '请先配置并验证仓库机器人',
  image_registry_connection_required: '请选择镜像仓库连接',
  not_found: '找不到请求的资源',
  conflict: '当前状态不允许执行这个操作',
  invalid_request: '提交的内容不正确',
}

export async function request(path, options = {}) {
  const token = storageValue('cicd_token')
  let response
  try {
    response = await fetch(`${API}${path}`, {
      ...options,
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(options.headers || {}),
      },
    })
  } catch (error) {
    throw new NetworkError(error)
  }

  let payload = null
  try {
    const text = typeof response.text === 'function' ? await response.text() : JSON.stringify(await response.json())
    if (text.trim()) {
      try {
        payload = JSON.parse(text)
      } catch {
        payload = text
      }
    }
  } catch (error) {
    throw new ApiError('服务器响应无法读取', { status: response.status, code: 'invalid_response', cause: error })
  }

  if (!response.ok) {
    const code = responseErrorCode(payload) || 'http_error'
    const message = responseErrorMessage(payload) || codeMessages[code] || `请求失败（${response.status}）`
    throw new ApiError(message, { status: response.status, code })
  }
  return payload
}

export function segment(value) {
  return encodeURIComponent(asString(value))
}

export function clone(value) {
  if (Array.isArray(value)) return value.map((item) => clone(item))
  if (isRecord(value)) return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, clone(item)]))
  return value
}

export function normalizeUser(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    id: firstValue(source, 'id', 'user_id'),
    username: asString(firstValue(source, 'username', 'user_name')),
    display_name: asString(firstValue(source, 'display_name', 'displayName', 'name')),
    is_super_admin: asBoolean(firstValue(source, 'is_super_admin', 'isSuperAdmin', 'super_admin')),
  }
}

export function normalizeSpace(value) {
  const source = isRecord(value?.space) ? value.space : isRecord(value) ? value : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'space_id')),
    name: asString(firstValue(source, 'name', 'space_name'), '未命名空间'),
    slug: asString(firstValue(source, 'slug', 'space_slug')),
    description: asString(firstValue(source, 'description')),
    role: asString(firstValue(source, 'role', 'member_role'), 'developer'),
    created_at: firstValue(source, 'created_at', 'createdAt'),
  }
}

export function normalizeSpaceMember(value) {
  const source = isRecord(value?.member) ? value.member : isRecord(value) ? value : {}
  return {
    ...source,
    user_id: asNumber(firstValue(source, 'user_id', 'userId', 'id'), 0),
    username: asString(firstValue(source, 'username', 'user_name', 'userName')),
    display_name: asString(firstValue(source, 'display_name', 'displayName', 'name')),
    role: asString(firstValue(source, 'role', 'member_role', 'memberRole'), 'viewer').toLowerCase(),
    is_current_user: asBoolean(firstValue(source, 'is_current_user', 'isCurrentUser')),
    is_super_admin: asBoolean(firstValue(source, 'is_super_admin', 'isSuperAdmin')),
    joined_at: firstValue(source, 'joined_at', 'joinedAt', 'created_at', 'createdAt'),
  }
}

export function normalizeSpaceMembers(payload) {
  return responseItems(payload).map(normalizeSpaceMember).filter((item) => item.user_id && item.username)
}

export function normalizeSpaceSettings(value) {
  const source = isRecord(value) ? value : {}
  const space = normalizeSpace(firstValue(source, 'space') || source)
  return {
    ...source,
    space,
    role: asString(firstValue(source, 'role', 'member_role', 'memberRole'), space.role || 'viewer').toLowerCase(),
    role_name: asString(firstValue(source, 'role_name', 'roleName')),
  }
}

export function normalizePermission(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    key: asString(firstValue(source, 'key', 'permission')),
    name: asString(firstValue(source, 'name', 'label')),
    description: asString(firstValue(source, 'description', 'detail')),
  }
}

export function normalizeRoleDefinition(value) {
  const source = isRecord(value) ? value : {}
  const rawPermissions = firstValue(source, 'permissions', 'permission_keys', 'permissionKeys')
  return {
    ...source,
    key: asString(firstValue(source, 'key', 'role')),
    name: asString(firstValue(source, 'name', 'label')),
    description: asString(firstValue(source, 'description', 'detail')),
    permissions: Array.isArray(rawPermissions) ? rawPermissions.map((item) => asString(item)).filter(Boolean) : [],
  }
}

export function normalizeSpacePermissions(value) {
  const source = isRecord(value) ? value : {}
  const rawRoles = firstValue(source, 'roles', 'role_definitions', 'roleDefinitions')
  const rawPermissions = firstValue(source, 'permissions', 'permission_definitions', 'permissionDefinitions')
  return {
    ...source,
    role: asString(firstValue(source, 'role', 'member_role', 'memberRole'), 'viewer').toLowerCase(),
    role_name: asString(firstValue(source, 'role_name', 'roleName')),
    roles: Array.isArray(rawRoles) ? rawRoles.map(normalizeRoleDefinition).filter((item) => item.key) : [],
    permissions: Array.isArray(rawPermissions) ? rawPermissions.map(normalizePermission).filter((item) => item.key) : [],
  }
}

export function normalizeProjectRole(value) {
  const source = isRecord(value?.role) ? value.role : isRecord(value) ? value : {}
  const rawPermissions = firstValue(source, 'permissions', 'permission_keys', 'permissionKeys')
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'role_id', 'roleId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    key: asString(firstValue(source, 'key', 'role_key', 'roleKey')),
    name: asString(firstValue(source, 'name', 'label')),
    description: asString(firstValue(source, 'description', 'detail')),
    is_system: asBoolean(firstValue(source, 'is_system', 'isSystem')),
    permissions: Array.isArray(rawPermissions) ? rawPermissions.map((item) => asString(item)).filter(Boolean) : [],
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeProjectMember(value) {
  const source = isRecord(value?.member) ? value.member : isRecord(value) ? value : {}
  const rawPermissions = firstValue(source, 'permissions', 'permission_keys', 'permissionKeys')
  return {
    ...source,
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    user_id: asNumber(firstValue(source, 'user_id', 'userId', 'id'), 0),
    username: asString(firstValue(source, 'username', 'user_name', 'userName')),
    display_name: asString(firstValue(source, 'display_name', 'displayName', 'name')),
    role_id: asString(firstValue(source, 'role_id', 'roleId')),
    role_key: asString(firstValue(source, 'role_key', 'roleKey')),
    role_name: asString(firstValue(source, 'role_name', 'roleName')),
    permissions: Array.isArray(rawPermissions) ? rawPermissions.map((item) => asString(item)).filter(Boolean) : [],
    is_current_user: asBoolean(firstValue(source, 'is_current_user', 'isCurrentUser')),
    is_super_admin: asBoolean(firstValue(source, 'is_super_admin', 'isSuperAdmin')),
    joined_at: firstValue(source, 'joined_at', 'joinedAt', 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeProjectAccess(value) {
  const source = isRecord(value?.access) ? value.access : isRecord(value) ? value : {}
  const rawPermissions = firstValue(source, 'permissions', 'permission_keys', 'permissionKeys')
  return {
    ...source,
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    role_id: asString(firstValue(source, 'role_id', 'roleId')),
    role_key: asString(firstValue(source, 'role_key', 'roleKey')),
    role_name: asString(firstValue(source, 'role_name', 'roleName')),
    permissions: Array.isArray(rawPermissions) ? rawPermissions.map((item) => asString(item)).filter(Boolean) : [],
    is_space_admin: asBoolean(firstValue(source, 'is_space_admin', 'isSpaceAdmin')),
    is_super_admin: asBoolean(firstValue(source, 'is_super_admin', 'isSuperAdmin')),
  }
}

export function normalizeSpaces(payload) {
  return responseItems(payload).map(normalizeSpace).filter((item) => item.id)
}

export function normalizeCluster(value) {
  const source = isRecord(value?.cluster) ? value.cluster : isRecord(value) ? value : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'cluster_id', 'clusterId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    name: asString(firstValue(source, 'name', 'cluster_name', 'clusterName'), '未命名集群'),
    provider: asString(firstValue(source, 'provider', 'type'), 'kubernetes'),
    type: asString(firstValue(source, 'type', 'provider'), 'Kubernetes'),
    api_endpoint: asString(firstValue(source, 'api_endpoint', 'apiEndpoint', 'server')),
    kube_context: asString(firstValue(source, 'kube_context', 'kubeContext', 'context')),
    connection_mode: asString(firstValue(source, 'connection_mode', 'connectionMode'), 'kubeconfig'),
    kubeconfig_configured: asBoolean(firstValue(source, 'kubeconfig_configured', 'kubeconfigConfigured')),
    status: asString(firstValue(source, 'status', 'state'), 'unknown'),
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeGitServiceAccount(value) {
  const source = isRecord(value?.account)
    ? value.account
    : isRecord(value?.service_account)
      ? value.service_account
      : isRecord(value)
        ? value
        : {}
  return {
    ...source,
    username: asString(firstValue(source, 'username', 'user_name', 'userName')),
    display_name: asString(firstValue(source, 'display_name', 'displayName', 'name')),
    email: asString(firstValue(source, 'email')),
    provider: asString(firstValue(source, 'provider'), 'unknown'),
    auth_method: asString(firstValue(source, 'auth_method', 'authMethod'), 'unknown'),
    configured: asBoolean(firstValue(source, 'configured', 'is_configured', 'isConfigured')),
  }
}

export function normalizeGitCredential(value) {
  const source = isRecord(value?.credential)
    ? value.credential
    : isRecord(value?.data?.credential)
      ? value.data.credential
      : isRecord(value)
        ? value
        : {}
  return {
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    provider: asString(firstValue(source, 'provider'), 'auto'),
    username: asString(firstValue(source, 'username', 'user_name', 'userName')),
    configured: asBoolean(firstValue(source, 'configured', 'is_configured', 'isConfigured')),
    invalid: asBoolean(firstValue(source, 'invalid', 'is_invalid', 'isInvalid')),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeGitAccess(value) {
  const source = isRecord(value?.access)
    ? value.access
    : isRecord(value?.data?.access)
      ? value.data.access
      : isRecord(value)
        ? value
        : {}
  const rawAccount = firstValue(source, 'account') || value?.account
  const account = rawAccount ? normalizeGitServiceAccount(rawAccount) : null
  return {
    ...source,
    repository_id: asString(firstValue(source, 'repository_id', 'repositoryId')),
    repository_url: asString(firstValue(source, 'repository_url', 'repositoryUrl')),
    provider: asString(firstValue(source, 'provider'), account?.provider || 'unknown'),
    supported: asBoolean(firstValue(source, 'supported', 'is_supported', 'isSupported')),
    account,
    authenticated: asBoolean(firstValue(source, 'authenticated', 'is_authenticated', 'isAuthenticated')),
    authenticated_username: asString(firstValue(source, 'authenticated_username', 'authenticatedUsername', 'actual_username', 'actualUsername')),
    repository_found: asBoolean(firstValue(source, 'repository_found', 'repositoryFound')),
    account_matches: asBoolean(firstValue(source, 'account_matches', 'accountMatches')),
    permission: asString(firstValue(source, 'permission'), '未检查'),
    can_read: asBoolean(firstValue(source, 'can_read', 'canRead')),
    can_write: asBoolean(firstValue(source, 'can_write', 'canWrite')),
    can_create_temporary_branch: asBoolean(firstValue(source, 'can_create_temporary_branch', 'canCreateTemporaryBranch')),
    can_merge: asBoolean(firstValue(source, 'can_merge', 'canMerge')),
    usable: asBoolean(firstValue(source, 'usable', 'ready')),
    required_permission: asString(firstValue(source, 'required_permission', 'requiredPermission')),
    required_merge_permission: asString(firstValue(source, 'required_merge_permission', 'requiredMergePermission')),
    message: asString(firstValue(source, 'message', 'summary', 'detail')),
    checked_at: firstValue(source, 'checked_at', 'checkedAt'),
  }
}

export function normalizeProject(value) {
  const source = isRecord(value?.project) ? value.project : isRecord(value) ? value : {}
  const repository = isRecord(source.repository) ? source.repository : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'project_id')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    name: asString(firstValue(source, 'name', 'project_name'), '未命名项目'),
    description: asString(firstValue(source, 'description')),
    repository_id: asString(firstValue(source, 'repository_id', 'repositoryId') || repository.id),
    repository_url: asString(firstValue(source, 'repository_url', 'repositoryUrl', 'repo_url') || repository.url),
    default_branch: asString(firstValue(source, 'default_branch', 'defaultBranch') || repository.default_branch, 'main'),
    cluster_id: asString(firstValue(source, 'cluster_id', 'clusterId', 'cluster') || firstValue(source.target, 'cluster_id', 'clusterId')),
    namespace: asString(firstValue(source, 'namespace', 'kubernetes_namespace', 'kubernetesNamespace') || firstValue(source.target, 'namespace', 'kubernetes_namespace', 'kubernetesNamespace')),
    deploy_strategy: asString(firstValue(source, 'deploy_strategy', 'deployStrategy', 'strategy'), 'rolling'),
    replicas: asNumber(firstValue(source, 'replicas', 'replica_count', 'replicaCount'), 1),
    container_port: asNumber(firstValue(source, 'container_port', 'containerPort', 'port'), 8080),
    image_repository: asString(firstValue(source, 'image_repository', 'imageRepository')),
    registry_connection_id: asString(firstValue(source, 'registry_connection_id', 'registryConnectionId')),
    health: asString(firstValue(source, 'health', 'status'), 'unknown'),
    pod_count: asNumber(firstValue(source, 'pod_count', 'podCount'), 0),
    pods: Array.isArray(firstValue(source, 'pods')) ? firstValue(source, 'pods').map((pod) => normalizePod(pod)).filter((pod) => pod.name) : [],
    healthy_pod_count: asNumber(firstValue(source, 'healthy_pod_count', 'healthyPodCount'), 0),
    deployment_target_count: asNumber(firstValue(source, 'deployment_target_count', 'deploymentTargetCount'), 0),
    default_target_id: asString(firstValue(source, 'default_target_id', 'defaultTargetId')),
    last_release: asString(firstValue(source, 'last_release', 'lastRelease')),
    last_commit: asString(firstValue(source, 'last_commit', 'lastCommit')),
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeImageRegistryConnection(value) {
  const source = isRecord(value?.connection)
    ? value.connection
    : isRecord(value?.data?.connection)
      ? value.data.connection
      : isRecord(value)
        ? value
        : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'connection_id', 'connectionId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    name: asString(firstValue(source, 'name', 'connection_name')),
    registry: asString(firstValue(source, 'registry', 'registry_host')),
    auth_type: asString(firstValue(source, 'auth_type', 'authType'), 'basic').toLowerCase(),
    username: asString(firstValue(source, 'username', 'user_name', 'userName')),
    pull_secret_name: asString(firstValue(source, 'pull_secret_name', 'pullSecretName')),
    configured: asBoolean(firstValue(source, 'configured', 'is_configured', 'isConfigured')),
    status: asString(firstValue(source, 'status', 'state'), 'unverified'),
    last_checked_at: firstValue(source, 'last_checked_at', 'lastCheckedAt'),
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeDeploymentTarget(value) {
  const source = isRecord(value?.target)
    ? value.target
    : isRecord(value?.data?.target)
      ? value.data.target
      : isRecord(value)
        ? value
        : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'target_id', 'targetId')),
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    name: asString(firstValue(source, 'name', 'target_name', 'targetName'), '未命名环境'),
    environment: asString(firstValue(source, 'environment', 'env'), 'dev'),
    stage: asString(firstValue(source, 'stage', 'deployment_stage', 'deploymentStage'), 'custom'),
    sort_order: asNumber(firstValue(source, 'sort_order', 'sortOrder', 'release_order'), 1),
    cluster_id: asString(firstValue(source, 'cluster_id', 'clusterId')),
    namespace: asString(firstValue(source, 'namespace', 'kubernetes_namespace', 'kubernetesNamespace'), 'default'),
    replicas: asNumber(firstValue(source, 'replicas', 'replica_count', 'replicaCount'), 1),
    container_port: asNumber(firstValue(source, 'container_port', 'containerPort', 'port'), 8080),
    deploy_strategy: asString(firstValue(source, 'deploy_strategy', 'deployStrategy', 'strategy'), 'rolling'),
    enabled: asBoolean(firstValue(source, 'enabled', 'is_enabled', 'isEnabled'), true),
    resource_quota: normalizeNamespaceQuota(firstValue(source, 'resource_quota', 'resourceQuota')),
    status: asString(firstValue(source, 'status', 'state'), 'active'),
    health: asString(firstValue(source, 'health', 'health_status', 'healthStatus'), 'unknown'),
    pod_count: asNumber(firstValue(source, 'pod_count', 'podCount'), 0),
    healthy_pod_count: asNumber(firstValue(source, 'healthy_pod_count', 'healthyPodCount'), 0),
    last_release: asString(firstValue(source, 'last_release', 'lastRelease')),
    last_commit: asString(firstValue(source, 'last_commit', 'lastCommit')),
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export const DEFAULT_NAMESPACE_QUOTA = Object.freeze({
  cpu_request: '2',
  cpu_limit: '4',
  memory_request: '2Gi',
  memory_limit: '4Gi',
  ephemeral_storage_request: '10Gi',
  ephemeral_storage_limit: '20Gi',
  storage: '50Gi',
  pods: 20,
  persistent_volume_claims: 10,
  default_cpu_request: '100m',
  default_cpu_limit: '500m',
  default_memory_request: '128Mi',
  default_memory_limit: '512Mi',
  default_ephemeral_storage_request: '256Mi',
  default_ephemeral_storage_limit: '1Gi',
})

export function normalizeNamespaceQuota(value) {
  const source = isRecord(value) ? value : {}
  return {
    cpu_request: asString(firstValue(source, 'cpu_request', 'cpuRequest'), DEFAULT_NAMESPACE_QUOTA.cpu_request),
    cpu_limit: asString(firstValue(source, 'cpu_limit', 'cpuLimit'), DEFAULT_NAMESPACE_QUOTA.cpu_limit),
    memory_request: asString(firstValue(source, 'memory_request', 'memoryRequest'), DEFAULT_NAMESPACE_QUOTA.memory_request),
    memory_limit: asString(firstValue(source, 'memory_limit', 'memoryLimit'), DEFAULT_NAMESPACE_QUOTA.memory_limit),
    ephemeral_storage_request: asString(firstValue(source, 'ephemeral_storage_request', 'ephemeralStorageRequest'), DEFAULT_NAMESPACE_QUOTA.ephemeral_storage_request),
    ephemeral_storage_limit: asString(firstValue(source, 'ephemeral_storage_limit', 'ephemeralStorageLimit'), DEFAULT_NAMESPACE_QUOTA.ephemeral_storage_limit),
    storage: asString(firstValue(source, 'storage', 'persistent_storage', 'persistentStorage'), DEFAULT_NAMESPACE_QUOTA.storage),
    pods: asNumber(firstValue(source, 'pods', 'pod_limit', 'podLimit'), DEFAULT_NAMESPACE_QUOTA.pods),
    persistent_volume_claims: asNumber(firstValue(source, 'persistent_volume_claims', 'persistentVolumeClaims', 'pvc_limit', 'pvcLimit'), DEFAULT_NAMESPACE_QUOTA.persistent_volume_claims),
    default_cpu_request: asString(firstValue(source, 'default_cpu_request', 'defaultCpuRequest'), DEFAULT_NAMESPACE_QUOTA.default_cpu_request),
    default_cpu_limit: asString(firstValue(source, 'default_cpu_limit', 'defaultCpuLimit'), DEFAULT_NAMESPACE_QUOTA.default_cpu_limit),
    default_memory_request: asString(firstValue(source, 'default_memory_request', 'defaultMemoryRequest'), DEFAULT_NAMESPACE_QUOTA.default_memory_request),
    default_memory_limit: asString(firstValue(source, 'default_memory_limit', 'defaultMemoryLimit'), DEFAULT_NAMESPACE_QUOTA.default_memory_limit),
    default_ephemeral_storage_request: asString(firstValue(source, 'default_ephemeral_storage_request', 'defaultEphemeralStorageRequest'), DEFAULT_NAMESPACE_QUOTA.default_ephemeral_storage_request),
    default_ephemeral_storage_limit: asString(firstValue(source, 'default_ephemeral_storage_limit', 'defaultEphemeralStorageLimit'), DEFAULT_NAMESPACE_QUOTA.default_ephemeral_storage_limit),
  }
}

export function normalizeDeploymentResource(value) {
  const source = isRecord(value?.resource)
    ? value.resource
    : isRecord(value?.data?.resource)
      ? value.data.resource
      : isRecord(value)
        ? value
        : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'resource_id')),
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    name: asString(firstValue(source, 'name')),
    path: asString(firstValue(source, 'path', 'file_path', 'filePath')),
    format: asString(firstValue(source, 'format'), 'yaml').toLowerCase() === 'json' ? 'json' : 'yaml',
    content: asString(firstValue(source, 'content', 'manifest')),
    api_version: asString(firstValue(source, 'api_version', 'apiVersion')),
    kind: asString(firstValue(source, 'kind')),
    resource_name: asString(firstValue(source, 'resource_name', 'resourceName', 'name')),
    namespace: asString(firstValue(source, 'namespace')),
    sort_order: asNumber(firstValue(source, 'sort_order', 'sortOrder'), 0),
    version: asNumber(firstValue(source, 'version'), 0),
    release_supported: asBoolean(firstValue(source, 'release_supported', 'releaseSupported')),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeDeploymentResources(payload) {
  const raw = firstValue(payload, 'resources', 'files', 'items')
  return Array.isArray(raw) ? raw.map(normalizeDeploymentResource).filter((item) => item.id || item.path) : []
}

export function normalizeCommit(value) {
  const source = isRecord(value?.commit) ? value.commit : isRecord(value) ? value : {}
  const sha = asString(firstValue(source, 'sha', 'id', 'commit_sha'))
  const rawTags = firstValue(source, 'tags', 'tag_names', 'tagNames')
  const tags = Array.isArray(rawTags)
    ? rawTags.map((item) => asString(isRecord(item) ? firstValue(item, 'name', 'tag') : item)).filter(Boolean)
    : []
  return {
    ...source,
    sha,
    short_sha: asString(firstValue(source, 'short_sha', 'shortSha', 'short')) || sha.slice(0, 7),
    message: asString(firstValue(source, 'message', 'title'), '无提交说明'),
    author: asString(firstValue(source, 'author', 'author_name', 'authorName'), '-'),
    authored_at: firstValue(source, 'authored_at', 'authoredAt', 'created_at', 'createdAt'),
    tags,
  }
}

export function normalizeTag(value) {
  const source = isRecord(value?.tag) ? value.tag : isRecord(value) ? value : {}
  return {
    ...source,
    name: asString(firstValue(source, 'name', 'tag', 'tag_name', 'tagName')),
    sha: asString(firstValue(source, 'sha', 'commit_sha', 'commitSha', 'target_sha', 'targetSha')),
  }
}

export function normalizeBranch(value) {
  const source = isRecord(value?.branch) ? value.branch : isRecord(value) ? value : {}
  const headValue = firstValue(source, 'head', 'latest_commit', 'latestCommit')
  return {
    ...source,
    name: asString(firstValue(source, 'name', 'branch_name', 'branchName')),
    head: headValue ? normalizeCommit(headValue) : null,
    is_head: asBoolean(firstValue(source, 'is_head', 'isHead', 'default'), false),
  }
}

export function normalizePreparation(value) {
  const source = isRecord(value?.preparation)
    ? value.preparation
    : isRecord(value?.data?.preparation)
      ? value.data.preparation
      : isRecord(value)
        ? value
        : {}
  const rawConflicts = firstValue(source, 'conflicts', 'conflict_files', 'conflictFiles')
  const conflicts = (Array.isArray(rawConflicts) ? rawConflicts : []).map((item) => {
    const conflict = isRecord(item) ? item : {}
    return {
      ...conflict,
      path: asString(firstValue(conflict, 'path', 'file', 'file_path', 'filePath')),
      base_content: asString(firstValue(conflict, 'base_content', 'baseContent')),
      source_content: asString(firstValue(conflict, 'source_content', 'sourceContent', 'ours')),
      resolved_content: asString(firstValue(conflict, 'resolved_content', 'resolvedContent', 'resolution')),
      resolved: asBoolean(firstValue(conflict, 'resolved', 'is_resolved', 'isResolved')),
    }
  }).filter((item) => item.path)
  const status = asString(firstValue(source, 'status', 'state'), 'unknown').toLowerCase()
  return {
    ...source,
    status,
    can_publish: asBoolean(firstValue(source, 'can_publish', 'canPublish'), status === 'ready'),
    source_branch: asString(firstValue(source, 'source_branch', 'sourceBranch')),
    base_branch: asString(firstValue(source, 'base_branch', 'baseBranch')),
    selected_sha: asString(firstValue(source, 'selected_sha', 'selectedSha')),
    temp_branch: asString(firstValue(source, 'temp_branch', 'tempBranch', 'temporary_branch', 'temporaryBranch')),
    release_branch: asString(firstValue(source, 'release_branch', 'releaseBranch', 'effective_branch', 'effectiveBranch')),
    release_sha: asString(firstValue(source, 'release_sha', 'releaseSha', 'effective_sha', 'effectiveSha')),
    source_ahead: asNumber(firstValue(source, 'source_ahead', 'sourceAhead', 'ahead'), 0),
    base_ahead: asNumber(firstValue(source, 'base_ahead', 'baseAhead', 'behind'), 0),
    base_behind: asNumber(firstValue(source, 'base_behind', 'baseBehind', 'base_ahead', 'baseAhead', 'behind'), 0),
    summary: asString(firstValue(source, 'summary', 'message')),
    conflicts,
  }
}

export function normalizePod(value) {
  const source = isRecord(value?.pod) ? value.pod : isRecord(value) ? value : {}
  const phase = asString(firstValue(source, 'phase', 'status'), 'Unknown')
  const restartCount = asNumber(firstValue(source, 'restarts', 'restart_count', 'restartCount'), 0)
  return {
    ...source,
    cluster_id: asString(firstValue(source, 'cluster_id', 'clusterId')),
    target_id: asString(firstValue(source, 'target_id', 'targetId')),
    namespace: asString(firstValue(source, 'namespace')),
    name: asString(firstValue(source, 'name', 'pod_name', 'podName')),
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    node_name: asString(firstValue(source, 'node_name', 'nodeName', 'node')),
    pod_ip: asString(firstValue(source, 'pod_ip', 'podIP', 'ip')),
    phase,
    ready: asBoolean(firstValue(source, 'ready'), phase.toLowerCase() === 'running'),
    restarts: restartCount,
    restart_count: restartCount,
    container: asString(firstValue(source, 'container', 'container_name', 'containerName')),
    labels: isRecord(firstValue(source, 'labels')) ? firstValue(source, 'labels') : {},
    started_at: firstValue(source, 'started_at', 'startedAt'),
  }
}

function normalizeContainers(value) {
  const result = {}
  if (Array.isArray(value)) {
    for (const item of value) {
      const container = normalizeContainer(item)
      if (container.name) result[container.name] = container
    }
    return result
  }
  if (!isRecord(value)) return result
  for (const [key, item] of Object.entries(value)) {
    const container = normalizeContainer({ ...(isRecord(item) ? item : {}), name: item?.name || key })
    if (container.name) result[container.name] = container
  }
  return result
}

function normalizeContainer(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    name: asString(firstValue(source, 'name', 'container_name', 'containerName')),
    image: asString(firstValue(source, 'image', 'image_name', 'imageName')),
    ready: asBoolean(firstValue(source, 'ready')),
    restart_count: asNumber(firstValue(source, 'restart_count', 'restartCount', 'restarts'), 0),
  }
}

export function normalizePodDetail(value) {
  const source = isRecord(value?.pod) ? { ...value, ...value.pod } : isRecord(value) ? value : {}
  const pod = normalizePod(source)
  return {
    ...source,
    ...pod,
    containers: normalizeContainers(firstValue(source, 'containers', 'container_statuses', 'containerStatuses')),
    config: isRecord(firstValue(source, 'config', 'configs')) ? firstValue(source, 'config', 'configs') : {},
    environment: isRecord(firstValue(source, 'environment', 'env', 'env_vars', 'envVars')) ? firstValue(source, 'environment', 'env', 'env_vars', 'envVars') : {},
  }
}

export function normalizeMetrics(value) {
  const source = isRecord(value?.metrics)
    ? value.metrics
    : isRecord(value?.data?.metrics)
      ? value.data.metrics
      : isRecord(value?.data) && !Array.isArray(value.data)
        ? value.data
        : isRecord(value)
          ? value
          : {}
  const rawMetricsAvailable = Object.prototype.hasOwnProperty.call(source, 'metrics_available')
    ? source.metrics_available
    : source.metricsAvailable
  const metricsAvailable = rawMetricsAvailable === undefined || rawMetricsAvailable === null
    ? rawMetricsAvailable
    : asBoolean(rawMetricsAvailable)
  const metricNumber = (...keys) => {
    const number = nullableNumber(firstValue(source, ...keys))
    return metricsAvailable === false ? null : number
  }
  const cpuUsedPercent = metricNumber('cpu_used_percent', 'cpuUsedPercent', 'cpu_percent', 'cpuPercent')
  const memoryUsedPercent = metricNumber('memory_used_percent', 'memoryUsedPercent', 'memory_percent', 'memoryPercent')
  const seriesSource = firstValue(source, 'series', 'history', 'time_series', 'timeSeries', 'points')
  const nodesSource = firstValue(source, 'nodes', 'node_metrics', 'nodeMetrics')
  return {
    ...source,
    cluster_id: asString(firstValue(source, 'cluster_id', 'clusterId')),
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    target_id: asString(firstValue(source, 'target_id', 'targetId')),
    cpu_used_percent: cpuUsedPercent,
    memory_used_percent: memoryUsedPercent,
    swap_used_percent: metricNumber('swap_used_percent', 'swapUsedPercent', 'swap_percent', 'swapPercent'),
    disk_used_percent: metricNumber('disk_used_percent', 'diskUsedPercent', 'disk_percent', 'diskPercent'),
    disk_read_mbps: metricNumber('disk_read_mbps', 'diskReadMbps', 'disk_read_mb', 'diskReadMb'),
    disk_write_mbps: metricNumber('disk_write_mbps', 'diskWriteMbps', 'disk_write_mb', 'diskWriteMb'),
    network_receive_mbps: metricNumber('network_receive_mbps', 'networkReceiveMbps', 'network_rx_mbps', 'networkRxMbps', 'bandwidth_in_mbps'),
    network_transmit_mbps: metricNumber('network_transmit_mbps', 'networkTransmitMbps', 'network_tx_mbps', 'networkTxMbps', 'bandwidth_out_mbps'),
    load_1m: metricNumber('load_1m', 'load1', 'load_1'),
    load_5m: metricNumber('load_5m', 'load5', 'load_5'),
    load_15m: metricNumber('load_15m', 'load15', 'load_15'),
    request_rate_rps: metricNumber('request_rate_rps', 'requestRateRPS', 'request_rate', 'rps'),
    error_rate_percent: metricNumber('error_rate_percent', 'errorRatePercent', 'error_rate', 'errorPercent'),
    latency_p50_ms: metricNumber('latency_p50_ms', 'latencyP50Ms', 'p50_ms', 'p50'),
    latency_p95_ms: metricNumber('latency_p95_ms', 'latencyP95Ms', 'p95_ms', 'p95'),
    latency_p99_ms: metricNumber('latency_p99_ms', 'latencyP99Ms', 'p99_ms', 'p99'),
    metrics_available: metricsAvailable,
    pod_count: asNumber(firstValue(source, 'pod_count', 'podCount'), 0),
    healthy_pod_count: asNumber(firstValue(source, 'healthy_pod_count', 'healthyPodCount'), 0),
    pod_restart_count: asNumber(firstValue(source, 'pod_restart_count', 'podRestartCount', 'restarts'), 0),
    pending_pod_count: asNumber(firstValue(source, 'pending_pod_count', 'pendingPodCount'), 0),
    failed_pod_count: asNumber(firstValue(source, 'failed_pod_count', 'failedPodCount'), 0),
    crash_loop_count: asNumber(firstValue(source, 'crash_loop_count', 'crashLoopCount'), 0),
    oom_killed_count: asNumber(firstValue(source, 'oom_killed_count', 'oomKilledCount'), 0),
    node_count: asNumber(firstValue(source, 'node_count', 'nodeCount'), 0),
    ready_node_count: asNumber(firstValue(source, 'ready_node_count', 'readyNodeCount'), 0),
    deployment_desired: asNumber(firstValue(source, 'deployment_desired', 'deploymentDesired'), 0),
    deployment_available: asNumber(firstValue(source, 'deployment_available', 'deploymentAvailable'), 0),
    metrics_source: asString(firstValue(source, 'metrics_source', 'metricsSource')),
    monitoring: normalizeMonitoring(firstValue(source, 'monitoring')),
    observed_at: firstValue(source, 'observed_at', 'observedAt'),
    series: Array.isArray(seriesSource) ? seriesSource.map(normalizeMetricPoint).filter((item) => item.timestamp) : [],
    nodes: Array.isArray(nodesSource) ? nodesSource.map(normalizeNodeMetric).filter((item) => item.name) : [],
  }
}

export function normalizeMonitoring(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    available: asBoolean(firstValue(source, 'available')),
    component: asString(firstValue(source, 'component')),
    display_name: asString(firstValue(source, 'display_name', 'displayName')),
    installable: asBoolean(firstValue(source, 'installable')),
    installed: asBoolean(firstValue(source, 'installed')),
    install_version: asString(firstValue(source, 'install_version', 'installVersion')),
    message: asString(firstValue(source, 'message')),
    history_available: asBoolean(firstValue(source, 'history_available', 'historyAvailable')),
    retention_days: asNumber(firstValue(source, 'retention_days', 'retentionDays'), 0),
    dependencies: Array.isArray(firstValue(source, 'dependencies')) ? firstValue(source, 'dependencies').map((item) => ({
      ...item,
      component: asString(item?.component),
      display_name: asString(firstValue(item, 'display_name', 'displayName')),
      available: asBoolean(item?.available),
      installed: asBoolean(item?.installed),
      installable: asBoolean(item?.installable),
      install_version: asString(firstValue(item, 'install_version', 'installVersion')),
      message: asString(item?.message),
    })) : [],
  }
}

function normalizeMetricPoint(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    timestamp: firstValue(source, 'timestamp', 'time', 'observed_at', 'observedAt'),
    cpu_used_percent: nullableNumber(firstValue(source, 'cpu_used_percent', 'cpuUsedPercent', 'cpu_percent', 'cpuPercent')),
    memory_used_percent: nullableNumber(firstValue(source, 'memory_used_percent', 'memoryUsedPercent', 'memory_percent', 'memoryPercent')),
    swap_used_percent: nullableNumber(firstValue(source, 'swap_used_percent', 'swapUsedPercent')),
    disk_used_percent: nullableNumber(firstValue(source, 'disk_used_percent', 'diskUsedPercent')),
    disk_read_mbps: nullableNumber(firstValue(source, 'disk_read_mbps', 'diskReadMbps')),
    disk_write_mbps: nullableNumber(firstValue(source, 'disk_write_mbps', 'diskWriteMbps')),
    network_receive_mbps: nullableNumber(firstValue(source, 'network_receive_mbps', 'networkReceiveMbps')),
    network_transmit_mbps: nullableNumber(firstValue(source, 'network_transmit_mbps', 'networkTransmitMbps')),
    load_1m: nullableNumber(firstValue(source, 'load_1m', 'load1')),
    load_5m: nullableNumber(firstValue(source, 'load_5m', 'load5')),
    load_15m: nullableNumber(firstValue(source, 'load_15m', 'load15')),
    request_rate_rps: nullableNumber(firstValue(source, 'request_rate_rps', 'requestRateRPS')),
    error_rate_percent: nullableNumber(firstValue(source, 'error_rate_percent', 'errorRatePercent')),
    latency_p50_ms: nullableNumber(firstValue(source, 'latency_p50_ms', 'latencyP50Ms')),
    latency_p95_ms: nullableNumber(firstValue(source, 'latency_p95_ms', 'latencyP95Ms')),
    latency_p99_ms: nullableNumber(firstValue(source, 'latency_p99_ms', 'latencyP99Ms')),
    pod_count: asNumber(firstValue(source, 'pod_count', 'podCount'), 0),
    healthy_pod_count: asNumber(firstValue(source, 'healthy_pod_count', 'healthyPodCount'), 0),
    pod_restart_count: asNumber(firstValue(source, 'pod_restart_count', 'podRestartCount'), 0),
  }
}

function normalizeNodeMetric(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    name: asString(firstValue(source, 'name', 'node_name', 'nodeName')),
    ready: asBoolean(firstValue(source, 'ready', 'is_ready', 'isReady')),
    cpu_used_percent: nullableNumber(firstValue(source, 'cpu_used_percent', 'cpuUsedPercent')),
    memory_used_percent: nullableNumber(firstValue(source, 'memory_used_percent', 'memoryUsedPercent')),
    swap_used_percent: nullableNumber(firstValue(source, 'swap_used_percent', 'swapUsedPercent')),
    disk_used_percent: nullableNumber(firstValue(source, 'disk_used_percent', 'diskUsedPercent')),
    disk_read_mbps: nullableNumber(firstValue(source, 'disk_read_mbps', 'diskReadMbps')),
    disk_write_mbps: nullableNumber(firstValue(source, 'disk_write_mbps', 'diskWriteMbps')),
    load_1m: nullableNumber(firstValue(source, 'load_1m', 'load1')),
    load_5m: nullableNumber(firstValue(source, 'load_5m', 'load5')),
    load_15m: nullableNumber(firstValue(source, 'load_15m', 'load15')),
    network_receive_mbps: nullableNumber(firstValue(source, 'network_receive_mbps', 'networkReceiveMbps')),
    network_transmit_mbps: nullableNumber(firstValue(source, 'network_transmit_mbps', 'networkTransmitMbps')),
    pod_count: asNumber(firstValue(source, 'pod_count', 'podCount'), 0),
  }
}

export function normalizeAuditLog(value) {
  const source = isRecord(value?.log) ? value.log : isRecord(value) ? value : {}
  return {
    ...source,
    id: asNumber(firstValue(source, 'id', 'log_id'), 0),
    space_id: asString(firstValue(source, 'space_id', 'spaceId')),
    user_id: asNumber(firstValue(source, 'user_id', 'userId'), 0),
    user_name: asString(firstValue(source, 'user_name', 'userName', 'username', 'operator')),
    action: asString(firstValue(source, 'action', 'operation'), '系统操作'),
    target: asString(firstValue(source, 'target', 'target_name', 'targetName', 'target_id')),
    created_at: firstValue(source, 'created_at', 'createdAt', 'time'),
  }
}

function normalizeStatus(value) {
  const status = asString(value).toLowerCase()
  return ({
    success: 'succeeded',
    successful: 'succeeded',
    complete: 'succeeded',
    completed: 'succeeded',
    in_progress: 'running',
    processing: 'running',
    canceled: 'cancelled',
  })[status] || status || 'unknown'
}

function unwrapRelease(value) {
  if (!isRecord(value)) return {}
  if (isRecord(value.release)) return value.release
  if (isRecord(value.data?.release)) return value.data.release
  return value
}

export function normalizeRelease(value) {
  const source = unwrapRelease(value)
  const planSource = isRecord(source.plan) ? source.plan : {}
  const trafficSource = isRecord(planSource.traffic) ? planSource.traffic : isRecord(source.traffic) ? source.traffic : {}
  const rawCommits = firstValue(source, 'commits', 'commit_list', 'commitList', 'versions')
  const commits = (Array.isArray(rawCommits) ? rawCommits : []).map(normalizeCommit).filter((item) => item.sha)
  const rawTargets = firstValue(source, 'targets', 'deployment_targets', 'deploymentTargets')
  const targets = (Array.isArray(rawTargets) ? rawTargets : []).map((item) => {
    const target = isRecord(item) ? item : {}
    return {
      ...target,
      id: asString(firstValue(target, 'id', 'target_id', 'targetId')),
      name: asString(firstValue(target, 'name', 'target_name', 'targetName'), '未命名环境'),
      environment: asString(firstValue(target, 'environment', 'env'), 'dev'),
      environment_stage: asString(firstValue(target, 'environment_stage', 'environmentStage', 'stage'), 'custom'),
      sort_order: asNumber(firstValue(target, 'sort_order', 'sortOrder', 'release_order'), 1),
      cluster_id: asString(firstValue(target, 'cluster_id', 'clusterId')),
      namespace: asString(firstValue(target, 'namespace'), 'default'),
      replicas: asNumber(firstValue(target, 'replicas'), 1),
      container_port: asNumber(firstValue(target, 'container_port', 'containerPort'), 8080),
      deploy_strategy: asString(firstValue(target, 'deploy_strategy', 'deployStrategy'), 'rolling'),
      status: normalizeStatus(firstValue(target, 'status', 'state')) === 'unknown' ? 'pending' : normalizeStatus(firstValue(target, 'status', 'state')),
      progress: nullableNumber(firstValue(target, 'progress', 'progress_percent', 'progressPercent')) ?? 0,
      stage: asString(firstValue(target, 'stage', 'current_stage', 'currentStage')),
      message: asString(firstValue(target, 'message', 'status_message', 'statusMessage')),
      error: asString(firstValue(target, 'error', 'error_message', 'errorMessage')),
      started_at: firstValue(target, 'started_at', 'startedAt'),
      finished_at: firstValue(target, 'finished_at', 'finishedAt'),
      logs: firstValue(target, 'logs', 'execution_logs', 'executionLogs', 'raw_logs', 'rawLogs', 'output'),
    }
  }).filter((item) => item.id)
  const strategy = asString(firstValue(source, 'strategy', 'deploy_strategy', 'deployStrategy') || planSource.strategy, 'rolling')
  const defaultStable = strategy === 'canary' ? 90 : strategy === 'blue_green' ? 0 : 100
  const defaultCandidate = 100 - defaultStable
  const defaultBlue = strategy === 'blue_green' ? 0 : 0
  const defaultGreen = strategy === 'blue_green' ? 100 : 0
  const stablePercent = asNumber(firstValue(trafficSource, 'stable_percent', 'stablePercent') ?? firstValue(source, 'stable_percent', 'stablePercent'), defaultStable)
  const candidatePercent = asNumber(firstValue(trafficSource, 'candidate_percent', 'candidatePercent') ?? firstValue(source, 'candidate_percent', 'candidatePercent'), defaultCandidate)
  const bluePercent = asNumber(firstValue(trafficSource, 'blue_percent', 'bluePercent') ?? firstValue(source, 'blue_percent', 'bluePercent'), defaultBlue)
  const greenPercent = asNumber(firstValue(trafficSource, 'green_percent', 'greenPercent') ?? firstValue(source, 'green_percent', 'greenPercent'), defaultGreen)
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'release_id', 'releaseId')),
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    repository_id: asString(firstValue(source, 'repository_id', 'repositoryId')),
    created_by: asNumber(firstValue(source, 'created_by', 'createdBy'), 0),
    created_by_name: asString(firstValue(source, 'created_by_name', 'createdByName')),
    branch: asString(firstValue(source, 'branch', 'branch_name', 'branchName')),
    commits,
    targets,
    plan: {
      ...planSource,
      strategy,
      traffic: { ...trafficSource, stable_percent: stablePercent, candidate_percent: candidatePercent, blue_percent: bluePercent, green_percent: greenPercent },
    },
    strategy,
    status: normalizeStatus(firstValue(source, 'status', 'state')),
    progress: nullableNumber(firstValue(source, 'progress', 'progress_percent', 'progressPercent')),
    stage: asString(firstValue(source, 'stage', 'current_stage', 'currentStage')),
    error: asString(firstValue(source, 'error', 'error_message', 'errorMessage')),
    started_at: firstValue(source, 'started_at', 'startedAt'),
    finished_at: firstValue(source, 'finished_at', 'finishedAt'),
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
    short: asString(firstValue(source, 'short')) || commits[0]?.short_sha || '',
    message: asString(firstValue(source, 'message', 'status_message', 'statusMessage')),
  }
}

function normalizeABVersion(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    role: asString(firstValue(source, 'role'), 'A'),
    release_id: asString(firstValue(source, 'release_id', 'releaseId')),
    branch: asString(firstValue(source, 'branch', 'branch_name', 'branchName')),
    commit_sha: asString(firstValue(source, 'commit_sha', 'commitSha', 'sha')),
    short_sha: asString(firstValue(source, 'short_sha', 'shortSha')),
    message: asString(firstValue(source, 'message')),
  }
}

function normalizeABStats(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    metrics_available: asBoolean(firstValue(source, 'metrics_available', 'metricsAvailable')),
    metrics_message: asString(firstValue(source, 'metrics_message', 'metricsMessage')),
    request_rate_rps: nullableNumber(firstValue(source, 'request_rate_rps', 'requestRateRPS')),
    error_rate_percent: nullableNumber(firstValue(source, 'error_rate_percent', 'errorRatePercent')),
    latency_p95_ms: nullableNumber(firstValue(source, 'latency_p95_ms', 'latencyP95Ms')),
  }
}

function normalizeABRoutingRule(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    source: asString(firstValue(source, 'source'), 'json_body'),
    path: asString(firstValue(source, 'path'), '$.user_id'),
    missing_behavior: asString(firstValue(source, 'missing_behavior', 'missingBehavior'), 'stable'),
    algorithm: asString(firstValue(source, 'algorithm'), 'consistent_hash'),
  }
}

function normalizeABPod(value) {
  const source = isRecord(value) ? value : {}
  return {
    ...source,
    name: asString(firstValue(source, 'name', 'pod_name', 'podName')),
    variant: asString(firstValue(source, 'variant'), 'a'),
    version: asString(firstValue(source, 'version')),
    node_name: asString(firstValue(source, 'node_name', 'nodeName')),
    pod_ip: asString(firstValue(source, 'pod_ip', 'podIP')),
    phase: asString(firstValue(source, 'phase')),
    ready: asBoolean(firstValue(source, 'ready')),
    restart_count: asNumber(firstValue(source, 'restart_count', 'restartCount'), 0),
  }
}

export function normalizeABExperiment(value) {
  const source = isRecord(value?.experiment) ? value.experiment : isRecord(value?.data?.experiment) ? value.data.experiment : isRecord(value) ? value : {}
  return {
    ...source,
    id: asString(firstValue(source, 'id', 'experiment_id', 'experimentId')),
    project_id: asString(firstValue(source, 'project_id', 'projectId')),
    name: asString(firstValue(source, 'name'), '未命名实验'),
    target_id: asString(firstValue(source, 'target_id', 'targetId')),
    environment: asString(firstValue(source, 'environment', 'env')),
    environment_stage: asString(firstValue(source, 'environment_stage', 'environmentStage')),
    cluster_id: asString(firstValue(source, 'cluster_id', 'clusterId')),
    namespace: asString(firstValue(source, 'namespace')),
    replicas: asNumber(firstValue(source, 'replicas'), 1),
    strategy: asString(firstValue(source, 'strategy'), 'rolling'),
    assignment: asString(firstValue(source, 'assignment'), 'percentage'),
    routing_rule: normalizeABRoutingRule(firstValue(source, 'routing_rule', 'routingRule')),
    a_version: normalizeABVersion(firstValue(source, 'a_version', 'aVersion')),
    b_version: normalizeABVersion(firstValue(source, 'b_version', 'bVersion')),
    a_traffic: asNumber(firstValue(source, 'a_traffic', 'aTraffic'), 99),
    b_traffic: asNumber(firstValue(source, 'b_traffic', 'bTraffic'), 1),
    a_stats: normalizeABStats(firstValue(source, 'a_stats', 'aStats')),
    b_stats: normalizeABStats(firstValue(source, 'b_stats', 'bStats')),
    a_pods: (Array.isArray(firstValue(source, 'a_pods', 'aPods')) ? firstValue(source, 'a_pods', 'aPods') : []).map(normalizeABPod),
    b_pods: (Array.isArray(firstValue(source, 'b_pods', 'bPods')) ? firstValue(source, 'b_pods', 'bPods') : []).map(normalizeABPod),
    status: asString(firstValue(source, 'status'), 'running'),
    started_at: firstValue(source, 'started_at', 'startedAt'),
    finished_at: firstValue(source, 'finished_at', 'finishedAt'),
    created_at: firstValue(source, 'created_at', 'createdAt'),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
    events: Array.isArray(source.events) ? source.events : [],
  }
}

export function requireToken(value) {
  const token = asString(value?.token || value?.data?.token)
  if (!token) throw new ApiError('登录响应缺少会话信息', { code: 'invalid_response' })
  return token
}

export function requireRelease(value) {
  const release = normalizeRelease(value)
  if (!release.id) throw new ApiError('服务器返回的发布记录无效', { code: 'invalid_response' })
  return release
}

export function releaseResponse(value) {
  const release = requireRelease(value)
  if (isRecord(value) && value.duplicate !== undefined) release.duplicate = asBoolean(value.duplicate)
  return release
}
