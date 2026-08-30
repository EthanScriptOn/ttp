import { Alert, Progress, Tag, Typography } from 'antd'
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  LoadingOutlined,
  MinusCircleOutlined,
  WarningOutlined,
} from '@ant-design/icons'

export const RELEASE_ENVIRONMENTS = [
  { key: 'dev', label: 'DEV', name: '开发环境' },
  { key: 'uat', label: 'UAT', name: '测试环境' },
  { key: 'pre', label: 'PRE', name: '预发布环境' },
  { key: 'prod', label: 'PROD', name: '生产环境' },
]

const STATUS_ALIASES = {
  success: 'succeeded',
  successful: 'succeeded',
  complete: 'succeeded',
  completed: 'succeeded',
  in_progress: 'running',
  processing: 'running',
  canceled: 'cancelled',
  error: 'failed',
  errored: 'failed',
  waiting: 'pending',
}

const STAGE_LABELS = {
  queued: '排队中',
  preparing: '准备中',
  building: '构建镜像',
  testing: '版本确认',
  pushing: '推送镜像',
  deploying: '更新 Pod',
  checking: '检查 Pod',
  switching: '切换流量',
  succeeded: '已完成',
  failed: '执行失败',
  cancelled: '已取消',
  draft: '草稿',
  waiting: '等待推进',
}

const STATUS_COLORS = {
  queued: 'warning',
  preparing: 'processing',
  running: 'processing',
  succeeded: 'success',
  failed: 'error',
  cancelled: 'default',
  draft: 'default',
}

const RUNNING_STATUSES = new Set([
  'queued',
  'preparing',
  'building',
  'testing',
  'pushing',
  'deploying',
  'checking',
  'switching',
  'running',
])

function valueText(value, fallback = '') {
  if (value === undefined || value === null) return fallback
  const result = String(value).trim()
  return result || fallback
}

export function normalizeReleaseStatus(value, fallback = 'unknown') {
  const raw = value && typeof value === 'object'
    ? value.status ?? value.state ?? value.phase
    : value
  const normalized = valueText(raw, fallback).toLowerCase().replace(/[\s-]+/g, '_')
  return STATUS_ALIASES[normalized] || normalized || fallback
}

export function targetStatus(target) {
  return normalizeReleaseStatus(target?.status ?? target?.state, 'pending')
}

export function targetProgress(target) {
  const status = targetStatus(target)
  if (status === 'succeeded') return 100
  const value = Number(target?.progress ?? target?.progress_percent ?? target?.progressPercent)
  if (Number.isFinite(value)) return Math.max(0, Math.min(100, Math.round(value)))
  return status === 'pending' || status === 'queued' ? 0 : null
}

export function environmentKey(value) {
  const source = value && typeof value === 'object'
    ? value.environment || value.env || value.name || value.target_name || value.targetName
    : value
  const raw = valueText(source).toLowerCase()
  if (!raw) return ''
  if (['dev', 'develop', 'development'].includes(raw) || raw.includes('开发')) return 'dev'
  if (['uat', 'qa', 'test', 'testing'].includes(raw) || raw.includes('测试')) return 'uat'
  if (['pre', 'stage', 'staging', 'preprod', 'pre-production'].includes(raw) || raw.includes('预发布')) return 'pre'
  if (['prod', 'pro', 'production'].includes(raw) || raw.includes('生产')) return 'prod'
  return ''
}

function targetName(target, fallback = '未命名环境') {
  return valueText(target?.name || target?.environment || target?.env || target?.target_name || target?.targetName, fallback)
}

function targetEnvironmentKey(target, index = 0) {
  return environmentKey(target) || `custom-${index + 1}`
}

function isKnownEnvironment(key) {
  return RELEASE_ENVIRONMENTS.some((item) => item.key === key)
}

export function getEnvironmentSlots(targets = []) {
  const list = Array.isArray(targets) ? targets.filter(Boolean) : []
  const buckets = new Map(RELEASE_ENVIRONMENTS.map((item) => [item.key, []]))
  const custom = []

  list.forEach((target, index) => {
    const key = targetEnvironmentKey(target, index)
    if (isKnownEnvironment(key)) {
      buckets.get(key).push(target)
      return
    }
    custom.push({
      key,
      label: valueText(target?.environment || target?.env, targetName(target, `环境 ${index + 1}`)).toUpperCase(),
      name: targetName(target, `环境 ${index + 1}`),
      targets: [target],
    })
  })

  return [
    ...RELEASE_ENVIRONMENTS.map((definition) => ({
      ...definition,
      targets: buckets.get(definition.key) || [],
    })),
    ...custom,
  ]
}

export function environmentSlotStatus(slot) {
  const targets = Array.isArray(slot?.targets) ? slot.targets : []
  if (!targets.length) return 'unconfigured'
  const statuses = targets.map(targetStatus)
  if (statuses.some((status) => status === 'failed')) return 'failed'
  if (statuses.some((status) => status === 'cancelled')) return 'cancelled'
  if (statuses.some((status) => RUNNING_STATUSES.has(status))) return 'running'
  if (statuses.some((status) => status === 'pending' || status === 'unknown')) return 'pending'
  if (statuses.every((status) => status === 'succeeded')) return 'succeeded'
  return statuses[0] || 'pending'
}

export function environmentSlotProgress(slot) {
  const targets = Array.isArray(slot?.targets) ? slot.targets : []
  if (!targets.length) return null
  const values = targets.map(targetProgress).filter((value) => value !== null)
  if (!values.length) return null
  return Math.round(values.reduce((sum, value) => sum + value, 0) / values.length)
}

export function environmentGate(slots, index, sequential = true) {
  const slot = slots[index]
  if (!slot || !slot.targets?.length) {
    return { canPublish: false, configured: false, reason: '本次没有选择这个环境' }
  }
  if (!sequential) return { canPublish: true, configured: true, reason: `可以发布到 ${slot.label}` }

  let previous = null
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    if (slots[cursor]?.targets?.length) {
      previous = slots[cursor]
      break
    }
  }
  if (!previous) return { canPublish: true, configured: true, reason: `可以开始发布到 ${slot.label}` }

  const previousStatus = environmentSlotStatus(previous)
  if (previousStatus === 'succeeded') {
    return { canPublish: true, configured: true, reason: `可以发布到 ${slot.label}` }
  }
  if (previousStatus === 'running' || previousStatus === 'queued') {
    return { canPublish: false, configured: true, reason: `${previous.label} 正在发布，完成后才能发布到 ${slot.label}` }
  }
  if (previousStatus === 'failed') {
    return { canPublish: false, configured: true, reason: `先处理 ${previous.label} 的失败发布，再发布到 ${slot.label}` }
  }
  if (previousStatus === 'cancelled') {
    return { canPublish: false, configured: true, reason: `先重新发布 ${previous.label}，才能发布到 ${slot.label}` }
  }
  return { canPublish: false, configured: true, reason: `先把 ${previous.label} 发布完成，才能发布到 ${slot.label}` }
}

function releaseProgressValue(release) {
  const value = Number(release?.progress ?? release?.progress_percent ?? release?.progressPercent)
  if (!Number.isFinite(value)) return null
  return Math.max(0, Math.min(100, Math.round(value)))
}

function releaseStatus(release) {
  return normalizeReleaseStatus(release?.status || release?.release_status, 'unknown')
}

function displayStage(release, status) {
  const stage = valueText(release?.stage || release?.current_stage || release?.currentStage).toLowerCase()
  return STAGE_LABELS[stage] || valueText(release?.stage, '') || STAGE_LABELS[status] || (status === 'running' ? '发布中' : '发布状态')
}

function statusColor(status) {
  return STATUS_COLORS[status] || (RUNNING_STATUSES.has(status) ? 'processing' : 'default')
}

function environmentIcon(status, configured) {
  if (!configured) return <MinusCircleOutlined />
  if (status === 'succeeded') return <CheckCircleOutlined />
  if (status === 'failed') return <CloseCircleOutlined />
  if (status === 'cancelled') return <WarningOutlined />
  if (RUNNING_STATUSES.has(status)) return <LoadingOutlined spin />
  return <ClockCircleOutlined />
}

function environmentStatusLabel(status, configured) {
  if (!configured) return '未选择'
  return ({
    pending: '待发布',
    queued: '排队中',
    preparing: '准备中',
    running: '发布中',
    building: '构建中',
    testing: '检查中',
    pushing: '推送镜像',
    deploying: '更新 Pod',
    checking: '检查 Pod',
    switching: '切换流量',
    succeeded: '已完成',
    failed: '失败',
    cancelled: '已取消',
  })[status] || '待发布'
}

function flowSummary(slots) {
  const configured = slots.filter((slot) => slot.targets?.length)
  return { configured }
}

export default function ReleaseProgress({ release, compact = false }) {
  const source = release || {}
  const status = releaseStatus(source)
  const progress = releaseProgressValue(source)
  const targets = Array.isArray(source.targets) ? source.targets : []
  const slots = getEnvironmentSlots(targets)
  const { configured } = flowSummary(slots)
  const active = RUNNING_STATUSES.has(status)
  const rawMessage = valueText(source.message || source.status_message || source.statusMessage)
  const message = /等待推进环境|等待确认下一环境|等待推进到/.test(rawMessage) ? '' : rawMessage
  const error = valueText(source.error || source.error_message || source.errorMessage)
  const hasFlow = configured.length > 0
  const hasFlowError = configured.some((slot) => ['failed', 'cancelled'].includes(environmentSlotStatus(slot)))

  if (!active && progress === null && !message && !error && !hasFlow) return null

  const displayedFlowProgress = progress === null && hasFlow
    ? Math.round(configured.reduce((sum, slot) => sum + (environmentSlotProgress(slot) || 0), 0) / configured.length)
    : progress
  return <div className={`release-progress ${status === 'failed' || hasFlowError ? 'has-error' : ''} ${compact ? 'is-compact' : ''}`}>
    <div className="release-progress-head">
      <div className="release-progress-stage">
        <Tag color={statusColor(status)}>{displayStage(source, status)}</Tag>
      </div>
      {displayedFlowProgress === null ? <Typography.Text type="secondary">进度待上报</Typography.Text> : <Typography.Text type="secondary">{displayedFlowProgress}%</Typography.Text>}
    </div>
    {displayedFlowProgress === null ? <div className="release-progress-unavailable">暂无进度信息</div> : <Progress percent={displayedFlowProgress} size="small" status={status === 'failed' || hasFlowError ? 'exception' : status === 'succeeded' ? 'success' : 'active'} showInfo={false} />}
    {hasFlow && <div className="release-progress-environment-flow" aria-label="逐级发布进度">
      {slots.map((slot) => {
        const slotStatus = environmentSlotStatus(slot)
        const configuredSlot = slot.targets.length > 0
        return <div className={`release-progress-environment status-${slotStatus}`} key={slot.key}>
          <span className="release-progress-environment-icon">{environmentIcon(slotStatus, configuredSlot)}</span>
          <span className="release-progress-environment-copy"><strong>{slot.label}</strong><small>{environmentStatusLabel(slotStatus, configuredSlot)}</small></span>
        </div>
      })}
    </div>}
    {message && <Typography.Text type="secondary" className="release-progress-message">{message}</Typography.Text>}
    {error && <Alert className="release-progress-error" type="error" showIcon message={error} />}
  </div>
}
