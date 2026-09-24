import { asString, request, responseItems, segment } from './api-helpers'
import {
  normalizeDeploymentResource,
  normalizeDeploymentResources,
  normalizeDeploymentTarget,
  normalizeNamespaceQuota,
} from './deployment-api-helpers'

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
    resource_quota: normalizeNamespaceQuota(payload.resource_quota),
    deploy_strategy: asString(payload.deploy_strategy),
    enabled: payload.enabled === undefined ? true : Boolean(payload.enabled),
  }
  return normalizeDeploymentTarget(await request(`/projects/${segment(projectId)}/deployment-targets`, { method: 'POST', body: JSON.stringify(body) }))
}

export async function updateDeploymentTarget(projectId, targetId, payload = {}) {
  const body = { ...payload }
  if (payload.resource_quota !== undefined) body.resource_quota = normalizeNamespaceQuota(payload.resource_quota)
  return normalizeDeploymentTarget(await request(`/projects/${segment(projectId)}/deployment-targets/${segment(targetId)}`, { method: 'PATCH', body: JSON.stringify(body) }))
}

export async function deleteDeploymentTarget(projectId, targetId) {
  return request(`/projects/${segment(projectId)}/deployment-targets/${segment(targetId)}`, { method: 'DELETE' })
}

export async function getDeploymentResources(projectId, targetId = '') {
  const query = targetId ? `?target_id=${segment(targetId)}` : ''
  const result = await request(`/projects/${segment(projectId)}/deployment-resources${query}`)
  return normalizeDeploymentResources(result)
}

function resourceBody(payload = {}) {
  return {
    name: asString(payload.name),
    path: asString(payload.path),
    format: asString(payload.format),
    content: asString(payload.content),
    sort_order: Number(payload.sort_order || 0),
  }
}

export async function createDeploymentResource(projectId, payload = {}) {
  return normalizeDeploymentResource(await request(`/projects/${segment(projectId)}/deployment-resources`, {
    method: 'POST',
    body: JSON.stringify(resourceBody(payload)),
  }))
}

export async function updateDeploymentResource(projectId, resourceId, payload = {}) {
  return normalizeDeploymentResource(await request(`/projects/${segment(projectId)}/deployment-resources/${segment(resourceId)}`, {
    method: 'PUT',
    body: JSON.stringify(resourceBody(payload)),
  }))
}

export async function deleteDeploymentResource(projectId, resourceId) {
  return request(`/projects/${segment(projectId)}/deployment-resources/${segment(resourceId)}`, { method: 'DELETE' })
}

function overrideBody(payload = {}) {
  return {
    ...resourceBody(payload),
    target_id: asString(payload.target_id),
    global_resource_id: asString(payload.global_resource_id),
    base_global_version: Number(payload.base_global_version || 0),
  }
}

export async function createDeploymentResourceOverride(projectId, payload = {}) {
  return normalizeDeploymentResource(await request(`/projects/${segment(projectId)}/deployment-resource-overrides`, {
    method: 'POST',
    body: JSON.stringify(overrideBody(payload)),
  }))
}

export async function updateDeploymentResourceOverride(projectId, overrideId, payload = {}) {
  return normalizeDeploymentResource(await request(`/projects/${segment(projectId)}/deployment-resource-overrides/${segment(overrideId)}`, {
    method: 'PUT',
    body: JSON.stringify(overrideBody(payload)),
  }))
}

export async function deleteDeploymentResourceOverride(projectId, overrideId, targetId) {
  return request(`/projects/${segment(projectId)}/deployment-resource-overrides/${segment(overrideId)}?target_id=${segment(targetId)}`, { method: 'DELETE' })
}

export async function validateDeploymentResource(projectId, payload = {}) {
  return normalizeDeploymentResource(await request(`/projects/${segment(projectId)}/deployment-resources/validate`, {
    method: 'POST',
    body: JSON.stringify({ ...resourceBody(payload), target_id: asString(payload.target_id) }),
  }))
}
