import { parseAllDocuments, stringify } from 'yaml'

export const DEFAULT_MANIFEST_SIZE = 512 * 1024

export function parseManifest(manifest) {
  const text = String(manifest || '')
  if (!text.trim()) throw new Error('配置不能为空')
  if (text.length > DEFAULT_MANIFEST_SIZE) throw new Error('配置不能超过 512 KB')
  const documents = parseAllDocuments(text)
  const errors = documents.flatMap((document) => document.errors || [])
  if (errors.length) throw new Error(errors[0].message || 'YAML/JSON 语法错误')
  const values = documents
    .filter((document) => document.contents !== null && document.contents !== undefined)
    .map((document) => document.toJS({ mapAsMap: false }))
  if (values.length !== 1 || Array.isArray(values[0])) throw new Error('每个资源文件只能包含一个 Kubernetes 资源对象')
  return values
}

export function formatManifest(manifest, format) {
  const resources = parseManifest(manifest)
  if (resources.length !== 1) throw new Error('每个资源文件只能包含一个 Kubernetes 资源对象')
  if (format === 'json') return `${JSON.stringify(resources[0], null, 2)}\n`
  return stringify(resources[0], { indent: 2 })
}

export function detectManifestFormat(manifest) {
  const trimmed = String(manifest || '').trim()
  return trimmed.startsWith('{') || trimmed.startsWith('[') ? 'json' : 'yaml'
}

export function summarizeManifest(manifest) {
  const resources = parseManifest(manifest)
  return resources.map((resource) => {
    if (!resource || typeof resource !== 'object' || Array.isArray(resource)) throw new Error('每个文档都必须是 Kubernetes 资源对象')
    const metadata = resource.metadata
    if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) throw new Error('资源缺少 metadata')
    if (!resource.apiVersion) throw new Error('资源缺少 apiVersion')
    if (!resource.kind) throw new Error('资源缺少 kind')
    if (!metadata.name) throw new Error('资源缺少 metadata.name')
    return {
      api_version: String(resource.apiVersion),
      kind: String(resource.kind),
      name: String(metadata.name),
      namespace: metadata.namespace ? String(metadata.namespace) : '',
    }
  })
}

export function localValidateManifest(manifest, expectedNamespace) {
  try {
    const resources = summarizeManifest(manifest)
    const namespace = String(expectedNamespace || '').trim()
    const seen = new Set()
    resources.forEach((resource) => {
      if (namespace && resource.namespace && resource.namespace !== namespace) {
        throw new Error(`资源 ${resource.kind}/${resource.name} 的命名空间不是 ${namespace}`)
      }
      const key = [resource.api_version, resource.kind, resource.namespace, resource.name].join('\u0000')
      if (seen.has(key)) throw new Error(`重复资源 ${resource.kind}/${resource.name}`)
      seen.add(key)
    })
    return { ok: true, resources, message: `已识别 ${resources.length} 个 Kubernetes 资源` }
  } catch (error) {
    return { ok: false, resources: [], message: error?.message || '配置格式不正确' }
  }
}

export function localValidateResourceFile(content, expectedNamespace) {
  try {
    const resources = summarizeManifest(content)
    if (resources.length !== 1) throw new Error('每个资源文件只能包含一个 Kubernetes 资源对象，不能使用 --- 或 JSON 数组')
    const resource = resources[0]
    const namespace = String(expectedNamespace || '').trim()
    if (namespace && resource.namespace && resource.namespace !== namespace) {
      throw new Error(`资源 ${resource.kind}/${resource.name} 的命名空间不是 ${namespace}`)
    }
    return { ok: true, resource, message: `已识别 ${resource.kind}/${resource.name}` }
  } catch (error) {
    return { ok: false, resource: null, message: error?.message || '资源文件格式不正确' }
  }
}
