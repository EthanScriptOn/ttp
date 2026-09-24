const SUMMARY_STEPS = [
  { key: 'source', title: '读取代码' },
  { key: 'batch', title: '合入批次分支' },
  { key: 'build', title: '构建镜像' },
  { key: 'registry', title: '推送镜像' },
  { key: 'k8s', title: '更新 Kubernetes' },
  { key: 'pod', title: '等待 Pod 就绪' },
]

const SOURCE_OUTPUT = /(?:preparing source workspace|fetching source revision|checking out source revision|source revision ready|source (?:checkout|fetch) failed|git network attempt|fatal: unable to access|remote:|enumerating objects|counting objects|compressing objects|receiving objects|resolving deltas|fetch_head|from https?:|branch=.*commit=|^[0-9a-f]{40}$)/i
const REGISTRY_OUTPUT = /(?:pushing (?:layers|manifest)|push completed|image build and push completed|image=.*@sha256:|exporting (?:manifest|config|to image)|\bdigest\b)/i
const POD_OUTPUT = /(?:pod\/|rollout status|rollout complete|successfully rolled out|期望副本|就绪|ready=true|release target=.*completed)/i
const KUBERNETES_OUTPUT = /(?:\bapply\b|configured|resourceversion|generation|kubernetes|client-go|decoded \d+ .*resources)/i
const BUILD_OUTPUT = /(?:buildkit|load build definition|load metadata|load \.dockerignore|\bbuilding\b|\bsuccessfully built\b|^#\d+)/i

export function sourceForExecutionLog(source, line) {
  const normalized = `${source || ''}`.trim().toLowerCase()
  if (normalized !== 'build' && normalized !== 'builder') return normalized || 'executor'
  if (SOURCE_OUTPUT.test(`${line || ''}`)) return 'git'
  if (REGISTRY_OUTPUT.test(`${line || ''}`)) return 'registry'
  return 'build'
}

function stageOf(entry) {
  const source = `${entry.source || ''}`.trim().toLowerCase()
  const line = `${entry.line || ''}`.trim()
  if (source === 'git') return 'source'
  if (source === 'registry') return 'registry'
  if (source === 'k8s' || source === 'kubernetes') {
    if (/^manifest format=/i.test(line)) return ''
    return POD_OUTPUT.test(line) ? 'pod' : 'k8s'
  }
  if (source === 'build' || source === 'builder') {
    if (SOURCE_OUTPUT.test(line)) return 'source'
    if (REGISTRY_OUTPUT.test(line)) return 'registry'
    return 'build'
  }
  if (/(?:merge|batch|批次)/i.test(line)) return 'batch'
  if (SOURCE_OUTPUT.test(line) || /git (?:fetch|checkout|rev-parse|diff)/i.test(line)) return 'source'
  if (POD_OUTPUT.test(line)) return 'pod'
  if (KUBERNETES_OUTPUT.test(line)) return 'k8s'
  if (REGISTRY_OUTPUT.test(line)) return 'registry'
  if (BUILD_OUTPUT.test(line)) return 'build'
  return ''
}

export function executionStageFromLogs(normalized = []) {
  let current = ''
  normalized.forEach((entry) => {
    const stage = stageOf(entry)
    if (stage) current = stage
  })
  return current
}

function sourceDetail(entries) {
  const lines = entries.map((entry) => entry.line)
  const combined = lines.join('\n')
  const last = lines[lines.length - 1] || ''
  const branch = combined.match(/branch=([^\s]+)/i)?.[1] || combined.match(/refs\/heads\/([^\s]+)/i)?.[1] || combined.match(/checkout\s+([^\s]+)/i)?.[1]
  const sha = combined.match(/commit=([0-9a-f]{7,})/i)?.[1] || combined.match(/(?:HEAD|commit)\s*(?:-?>|=)?\s*([0-9a-f]{7,})/i)?.[1]
  if (/remote:|receiving objects|resolving deltas|fetching source revision/i.test(last)) return last
  return [branch && `分支 ${branch}`, sha && `版本 ${sha.slice(0, 10)}`].filter(Boolean).join(' · ') || last || '正在读取代码仓库'
}

function summaryDetail(step, entries) {
  const last = entries[entries.length - 1]?.line || ''
  if (step.key === 'source') return sourceDetail(entries)
  if (step.key === 'build') return last.replace(/^.*?Successfully built\s+/i, '镜像 ').replace(/^.*?build image\s+/i, '镜像 ') || '正在构建镜像'
  if (step.key === 'registry') return /image=.*@sha256:|digest/i.test(last) ? '镜像已推送，已取得镜像摘要' : last || '正在推送镜像'
  if (step.key === 'k8s') return last.replace(/^.*?apply\s+/i, '已应用 ').replace(/^.*?(deployment\.apps\/[^\s]+) configured.*$/i, '$1 已更新') || '正在应用 Kubernetes 配置'
  if (step.key === 'pod') {
    if (/release target=.*completed/i.test(last)) return '目标环境发布完成'
    return last.replace(/^.*?(deployment\/[^:]+:\s*)/i, '').replace(/^.*?pod\//i, 'Pod ') || '正在等待 Pod 通过就绪检查'
  }
  if (step.key === 'batch') return last.replace(/^.*?(merge|合入)\s*/i, '已合入 ') || '代码已进入当前批次'
  return last
}

export function summarizeExecutionLogs(normalized = []) {
  if (!normalized.length) return []
  const groups = new Map(SUMMARY_STEPS.map((step) => [step.key, []]))
  normalized.forEach((entry) => {
    const stage = stageOf(entry)
    if (stage) groups.get(stage)?.push(entry)
  })

  const result = SUMMARY_STEPS.flatMap((step) => {
    const entries = groups.get(step.key) || []
    if (!entries.length) return []
    const failed = entries[entries.length - 1]?.level === 'ERROR'
    return [{
      id: `summary-${step.key}-${entries[0].id}`,
      title: step.title,
      detail: summaryDetail(step, entries),
      timestamp: entries[0].timestamp,
      state: failed ? 'failed' : 'done',
    }]
  })

  const errors = normalized.filter((entry) => entry.level === 'ERROR')
  const unresolvedError = normalized[normalized.length - 1]?.level === 'ERROR'
  if (unresolvedError && !result.some((item) => item.state === 'failed')) {
    const lastError = errors[errors.length - 1]
    result.push({ id: `summary-error-${lastError.id}`, title: '执行异常', detail: lastError.line, timestamp: lastError.timestamp, state: 'failed' })
  }
  const completed = normalized.some((entry) => /rollout complete|successfully rolled out|release target=.*completed/i.test(entry.line))
  if (!result.length) {
    const last = normalized[normalized.length - 1]
    return [{ id: `summary-running-${last.id}`, title: '准备发布', detail: last.line, timestamp: normalized[0].timestamp, state: unresolvedError ? 'failed' : completed ? 'done' : 'active' }]
  }
  if (!completed && !result.some((item) => item.state === 'failed')) result[result.length - 1].state = 'active'
  return result
}
