import {
  ApiError,
  asString,
  normalizeBranch,
  normalizeCluster,
  normalizeCommit,
  normalizeDeploymentConfig,
  normalizeDeploymentTarget,
  normalizeGitCredential,
  normalizeGitAccess,
  normalizeAuditLog,
  normalizeABExperiment,
  normalizeMetrics,
  normalizePreparation,
  normalizePod,
  normalizePodDetail,
  normalizeProject,
  normalizeRelease,
  normalizeSpace,
  normalizeSpaceMember,
  normalizeSpaceMembers,
  normalizeSpacePermissions,
  normalizeSpaceSettings,
  normalizeSpaces,
  normalizeTag,
  normalizeUser,
  releaseResponse,
  request,
  requireRelease,
  requireToken,
  responseItems,
  segment,
} from './api-helpers'

export { ApiError, normalizePreparation, normalizeRelease }

export async function login(username, password) {
  const result = await request('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) })
  const token = requireToken(result)
  const spaces = normalizeSpaces(result.spaces || result.data?.spaces)
  return {
    ...result,
    token,
    user: normalizeUser(result.user || result.data?.user),
    spaces,
    current_space_id: asString(result.current_space_id || result.current_space?.id || result.data?.current_space_id || spaces[0]?.id),
  }
}

export async function getCurrentUser() {
  const result = await request('/auth/me')
  const spaces = normalizeSpaces(result.spaces || result.data?.spaces)
  const current = normalizeSpace(result.current_space || result.data?.current_space)
  return {
    ...result,
    user: normalizeUser(result.user || result.data?.user),
    spaces,
    current_space: current.id ? current : null,
    current_space_id: asString(result.current_space_id || current.id),
  }
}

export async function selectSpace(spaceId) {
  const nextID = asString(spaceId).trim()
  if (!nextID) throw new ApiError('请选择空间', { code: 'invalid_request', status: 400 })
  const result = await request('/auth/select-space', { method: 'POST', body: JSON.stringify({ space_id: nextID }) })
  const current = normalizeSpace(result.current_space || result.space || result.data?.current_space)
  return { ...result, token: requireToken(result), current_space: current, current_space_id: current.id || nextID }
}

export async function createSpace(payload) {
  return normalizeSpace(await request('/spaces', { method: 'POST', body: JSON.stringify(payload) }))
}

export async function getSpaces() {
  const result = await request('/spaces')
  return normalizeSpaces(result)
}

export async function getSpaceSettings() {
  const result = await request('/space/settings')
  return normalizeSpaceSettings(result)
}

export async function updateSpaceSettings(payload = {}) {
  const body = {
    name: asString(payload.name),
    description: asString(payload.description),
  }
  return normalizeSpaceSettings(await request('/space/settings', { method: 'PATCH', body: JSON.stringify(body) }))
}

export async function getSpaceMembers() {
  const result = await request('/space/members')
  return normalizeSpaceMembers(result)
}

export async function createSpaceMember(payload = {}) {
  const body = {
    username: asString(payload.username),
    display_name: asString(payload.display_name || payload.displayName),
    password: asString(payload.password),
    role: asString(payload.role),
  }
  return normalizeSpaceMember(await request('/space/members', { method: 'POST', body: JSON.stringify(body) }))
}

export async function updateSpaceMember(userId, payload = {}) {
  const body = { role: asString(payload.role) }
  return normalizeSpaceMember(await request(`/space/members/${segment(userId)}`, { method: 'PATCH', body: JSON.stringify(body) }))
}

export async function removeSpaceMember(userId) {
  return request(`/space/members/${segment(userId)}`, { method: 'DELETE' })
}

export async function getSpacePermissions() {
  const result = await request('/space/permissions')
  return normalizeSpacePermissions(result)
}

export async function getProjects() {
  const result = await request('/projects')
  return responseItems(result).map(normalizeProject).filter((item) => item.id)
}

export async function createProject(payload) {
  return normalizeProject(await request('/projects', { method: 'POST', body: JSON.stringify(payload) }))
}

export async function updateProject(projectId, payload) {
  return normalizeProject(await request(`/projects/${segment(projectId)}`, { method: 'PATCH', body: JSON.stringify(payload) }))
}

export async function getDeploymentTargets(projectId) {
  const result = await request(`/projects/${segment(projectId)}/deployment-targets`)
  return responseItems(result).map(normalizeDeploymentTarget).filter((item) => item.id)
}

export async function createDeploymentTarget(projectId, payload = {}) {
  const body = {
    name: asString(payload.name),
    environment: asString(payload.environment),
    stage: asString(payload.stage),
    sort_order: Number(payload.sort_order),
    cluster_id: asString(payload.cluster_id),
    namespace: asString(payload.namespace),
    replicas: Number(payload.replicas),
    container_port: Number(payload.container_port),
    deploy_strategy: asString(payload.deploy_strategy),
    enabled: payload.enabled === undefined ? true : Boolean(payload.enabled),
  }
  return normalizeDeploymentTarget(await request(`/projects/${segment(projectId)}/deployment-targets`, { method: 'POST', body: JSON.stringify(body) }))
}

export async function updateDeploymentTarget(projectId, targetId, payload = {}) {
  const body = { ...payload }
  return normalizeDeploymentTarget(await request(`/projects/${segment(projectId)}/deployment-targets/${segment(targetId)}`, { method: 'PATCH', body: JSON.stringify(body) }))
}

export async function deleteDeploymentTarget(projectId, targetId) {
  return request(`/projects/${segment(projectId)}/deployment-targets/${segment(targetId)}`, { method: 'DELETE' })
}

export async function getDeploymentConfig(projectId, format = '') {
  const suffix = format ? `?format=${encodeURIComponent(asString(format))}` : ''
  const result = await request(`/projects/${segment(projectId)}/deployment-config${suffix}`)
  return normalizeDeploymentConfig(result)
}

export async function saveDeploymentConfig(projectId, payload = {}) {
  const body = {
    manifest: asString(payload.manifest),
    format: asString(payload.format),
  }
  return normalizeDeploymentConfig(await request(`/projects/${segment(projectId)}/deployment-config`, { method: 'PUT', body: JSON.stringify(body) }))
}

export async function validateDeploymentConfig(projectId, payload = {}) {
  const body = { manifest: asString(payload.manifest), format: asString(payload.format) }
  return normalizeDeploymentConfig(await request(`/projects/${segment(projectId)}/deployment-config/validate`, { method: 'POST', body: JSON.stringify(body) }))
}

export async function convertDeploymentConfig(projectId, payload = {}) {
  const body = { manifest: asString(payload.manifest), format: asString(payload.format) }
  return normalizeDeploymentConfig(await request(`/projects/${segment(projectId)}/deployment-config/convert`, { method: 'POST', body: JSON.stringify(body) }))
}

export async function getCommits(projectId, branch = 'main') {
  const branchName = asString(branch, 'main')
  const result = await request(`/projects/${segment(projectId)}/git/commits?branch=${encodeURIComponent(branchName)}&limit=200`)
  return responseItems(result).map(normalizeCommit).filter((item) => item.sha)
}

export async function getTags(projectId) {
  const result = await request(`/projects/${segment(projectId)}/git/tags?limit=500`)
  return responseItems(result).map(normalizeTag).filter((item) => item.name && item.sha)
}

export async function getBranches(projectId) {
  const result = await request(`/projects/${segment(projectId)}/git/branches`)
  return responseItems(result).map(normalizeBranch).filter((item) => item.name)
}

export async function getGitAccess(projectId) {
  const result = await request(`/projects/${segment(projectId)}/git/access`)
  return normalizeGitAccess(result)
}

export async function getProjectGitCredential(projectId) {
  return normalizeGitCredential(await request(`/projects/${segment(projectId)}/git/credential`))
}

export async function saveProjectGitCredential(projectId, payload = {}) {
  const body = {
    provider: asString(payload.provider, 'auto'),
    username: asString(payload.username),
    token: asString(payload.token),
  }
  const result = await request(`/projects/${segment(projectId)}/git/credential`, { method: 'PUT', body: JSON.stringify(body) })
  return {
    credential: normalizeGitCredential(result),
    access: normalizeGitAccess(result),
  }
}

export async function deleteProjectGitCredential(projectId) {
  return normalizeGitCredential(await request(`/projects/${segment(projectId)}/git/credential`, { method: 'DELETE' }))
}

export async function prepareRelease(projectId, payload = {}) {
  const body = {
    action: asString(payload.action),
    source_branch: asString(payload.source_branch || payload.sourceBranch),
    base_branch: asString(payload.base_branch || payload.baseBranch),
    selected_sha: asString(payload.selected_sha || payload.selectedSha),
    temporary_branch: asString(payload.temporary_branch || payload.temporaryBranch),
    resolutions: Array.isArray(payload.resolutions)
      ? payload.resolutions.map((item) => ({ path: asString(item?.path), content: asString(item?.content) })).filter((item) => item.path)
      : [],
  }
  return normalizePreparation(await request(`/projects/${segment(projectId)}/release-preparation`, { method: 'POST', body: JSON.stringify(body) }))
}

export async function getReleases(projectId) {
  const result = await request(`/projects/${segment(projectId)}/releases`)
  return responseItems(result).map(normalizeRelease).filter((item) => item.id)
}

export async function getABExperiments(projectId) {
  const result = await request(`/projects/${segment(projectId)}/ab-experiments`)
  return responseItems(result).map(normalizeABExperiment).filter((item) => item.id)
}

export async function getABExperiment(projectId, experimentId) {
  return normalizeABExperiment(await request(`/projects/${segment(projectId)}/ab-experiments/${segment(experimentId)}`))
}

export async function createABExperiment(projectId, payload = {}) {
  return normalizeABExperiment(await request(`/projects/${segment(projectId)}/ab-experiments`, { method: 'POST', body: JSON.stringify(payload) }))
}

export async function updateABExperimentTraffic(projectId, experimentId, payload = {}) {
  return normalizeABExperiment(await request(`/projects/${segment(projectId)}/ab-experiments/${segment(experimentId)}/traffic`, { method: 'PATCH', body: JSON.stringify(payload) }))
}

export async function stopABExperiment(projectId, experimentId) {
  return normalizeABExperiment(await request(`/projects/${segment(projectId)}/ab-experiments/${segment(experimentId)}/stop`, { method: 'POST' }))
}

export async function finishABExperiment(projectId, experimentId, result) {
  return normalizeABExperiment(await request(`/projects/${segment(projectId)}/ab-experiments/${segment(experimentId)}/finish`, { method: 'POST', body: JSON.stringify({ result }) }))
}

export async function getReleaseTargetLogs(projectId, releaseId, targetId) {
  const result = await request(`/projects/${segment(projectId)}/releases/${segment(releaseId)}/targets/${segment(targetId)}/logs`)
  return responseItems(result.logs || result)
}

export async function createRelease(projectId, payload) {
  const body = {
    ...(payload || {}),
    commit_shas: Array.isArray(payload?.commit_shas) ? payload.commit_shas.filter(Boolean) : [],
    publish: Boolean(payload?.publish),
  }
  return releaseResponse(await request(`/projects/${segment(projectId)}/releases`, { method: 'POST', body: JSON.stringify(body) }))
}

export async function publishRelease(projectId, releaseId) {
  return releaseResponse(await request(`/projects/${segment(projectId)}/releases/${segment(releaseId)}/publish`, { method: 'POST' }))
}

export async function retryReleaseTarget(projectId, releaseId, targetId) {
  return releaseResponse(await request(`/projects/${segment(projectId)}/releases/${segment(releaseId)}/targets/${segment(targetId)}/retry`, { method: 'POST' }))
}

export async function cancelRelease(projectId, releaseId, message = '') {
  const trimmedMessage = asString(message).trim()
  return releaseResponse(await request(`/projects/${segment(projectId)}/releases/${segment(releaseId)}/cancel`, {
    method: 'POST',
    body: trimmedMessage ? JSON.stringify({ message: trimmedMessage }) : undefined,
  }))
}

export async function removeCommit(projectId, releaseId, sha) {
  return requireRelease(await request(`/projects/${segment(projectId)}/releases/${segment(releaseId)}/commits/${segment(sha)}`, { method: 'DELETE' }))
}

export async function getPods(projectId, targetId = '') {
  const query = targetId ? `?target_id=${encodeURIComponent(asString(targetId))}` : ''
  const result = await request(`/projects/${segment(projectId)}/pods${query}`)
  return responseItems(result).map((item) => normalizePod({ ...item, project_id: item.project_id || projectId })).filter((item) => item.name)
}

export async function getPod(projectId, podName, targetId = '') {
  const query = targetId ? `?target_id=${encodeURIComponent(asString(targetId))}` : ''
  return normalizePodDetail(await request(`/projects/${segment(projectId)}/pods/${segment(podName)}${query}`))
}

export async function getPodLogs(projectId, podName, container, targetId = '') {
  const params = new URLSearchParams()
  if (container) params.set('container', asString(container))
  if (targetId) params.set('target_id', asString(targetId))
  const query = params.toString() ? `?${params.toString()}` : ''
  const result = await request(`/projects/${segment(projectId)}/pods/${segment(podName)}/logs${query}`)
  if (typeof result === 'string') return result
  return asString(result?.logs || result?.data?.logs)
}

export async function updatePodConfig(projectId, podName, payload, targetId = '') {
  const query = targetId ? `?target_id=${encodeURIComponent(asString(targetId))}` : ''
  return normalizePodDetail(await request(`/projects/${segment(projectId)}/pods/${segment(podName)}/config${query}`, { method: 'PATCH', body: JSON.stringify(payload) }))
}

export async function execPodCommand(projectId, podName, payload, targetId = '') {
  const query = targetId ? `?target_id=${encodeURIComponent(asString(targetId))}` : ''
  return request(`/projects/${segment(projectId)}/pods/${segment(podName)}/exec${query}`, { method: 'POST', body: JSON.stringify(payload) })
}

export async function getMetrics(projectId, targetId = '') {
  const query = targetId ? `?target_id=${encodeURIComponent(asString(targetId))}` : ''
  const result = await request(`/projects/${segment(projectId)}/metrics${query}`)
  return normalizeMetrics(result)
}

export async function getClusters() {
  const result = await request('/clusters')
  return responseItems(result).map(normalizeCluster).filter((item) => item.id)
}

export async function getCluster(clusterId) {
  const result = await request(`/clusters/${segment(clusterId)}`)
  return normalizeCluster(result)
}

export async function createCluster(payload) {
  const result = await request('/clusters', { method: 'POST', body: JSON.stringify(payload) })
  return normalizeClusterResult(result)
}

export async function updateCluster(clusterId, payload) {
  const result = await request(`/clusters/${segment(clusterId)}`, { method: 'PATCH', body: JSON.stringify(payload) })
  return normalizeClusterResult(result)
}

export async function testCluster(clusterId) {
  const result = await request(`/clusters/${segment(clusterId)}/test`, { method: 'POST' })
  return normalizeClusterResult(result)
}

function normalizeClusterResult(value) {
  const source = value && typeof value === 'object' ? value : {}
  return {
    ...source,
    cluster: normalizeCluster(source.cluster || source.data?.cluster || source),
    connected: Boolean(source.connected),
    connection: source.connection || source.data?.connection || null,
    message: asString(source.message),
  }
}

export async function getClusterMetrics(clusterId) {
  const result = await request(`/clusters/${segment(clusterId)}/metrics`)
  return normalizeMetrics(result)
}

export async function getAuditLogs(limit = 100) {
  const bounded = Math.min(Math.max(Number(limit) || 100, 1), 200)
  const result = await request(`/audit-logs?limit=${bounded}`)
  return responseItems(result).map(normalizeAuditLog).filter((item) => item.action)
}
