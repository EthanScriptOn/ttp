import { asBoolean, asNumber, asString, firstValue, isRecord } from './api-helpers'

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
    scope: asString(firstValue(source, 'scope'), 'global'),
    target_id: asString(firstValue(source, 'target_id', 'targetId')),
    global_resource_id: asString(firstValue(source, 'global_resource_id', 'globalResourceId')),
    override_id: asString(firstValue(source, 'override_id', 'overrideId')),
    base_global_version: asNumber(firstValue(source, 'base_global_version', 'baseGlobalVersion'), 0),
    global_version: asNumber(firstValue(source, 'global_version', 'globalVersion'), 0),
    base_content: asString(firstValue(source, 'base_content', 'baseContent')),
    global_content: asString(firstValue(source, 'global_content', 'globalContent')),
    global_changed: asBoolean(firstValue(source, 'global_changed', 'globalChanged')),
    updated_at: firstValue(source, 'updated_at', 'updatedAt'),
  }
}

export function normalizeDeploymentResources(payload) {
  const raw = firstValue(payload, 'resources', 'files', 'items')
  return Array.isArray(raw) ? raw.map(normalizeDeploymentResource).filter((item) => item.id || item.path) : []
}
