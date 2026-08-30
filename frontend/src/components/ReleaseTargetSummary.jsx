import { Button, Tag, Tooltip, Typography } from 'antd'
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloudUploadOutlined,
  LoadingOutlined,
  ReloadOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import {
  environmentGate,
  environmentSlotProgress,
  environmentSlotStatus,
  getEnvironmentSlots,
  normalizeReleaseStatus,
  targetStatus,
} from './ReleaseProgress'

const STATUS_LABELS = {
  pending: ['default', '待发布'],
  queued: ['warning', '排队中'],
  preparing: ['processing', '准备中'],
  running: ['processing', '发布中'],
  building: ['processing', '构建中'],
  testing: ['processing', '检查中'],
  pushing: ['processing', '推送镜像'],
  deploying: ['processing', '更新 Pod'],
  checking: ['processing', '检查 Pod'],
  switching: ['processing', '切换流量'],
  succeeded: ['success', '已完成'],
  failed: ['error', '失败'],
  cancelled: ['default', '已取消'],
  canceled: ['default', '已取消'],
  unconfigured: ['default', '未选择'],
}

const STATUS_ORDER = {
  failed: 8,
  cancelled: 7,
  canceled: 7,
  running: 6,
  preparing: 6,
  building: 6,
  testing: 6,
  pushing: 6,
  deploying: 6,
  checking: 6,
  switching: 6,
  queued: 5,
  pending: 4,
  succeeded: 1,
}

function targetID(target) {
  return String(target?.id || target?.target_id || target?.targetId || '').trim()
}

function targetForSlot(slot) {
  const targets = Array.isArray(slot?.targets) ? slot.targets : []
  if (!targets.length) return null
  return targets.reduce((current, candidate) => {
    const currentRank = STATUS_ORDER[targetStatus(current)] || 0
    const candidateRank = STATUS_ORDER[targetStatus(candidate)] || 0
    return candidateRank > currentRank ? candidate : current
  })
}

function statusMeta(status) {
  return STATUS_LABELS[status] || ['default', '待发布']
}

function isBusy(value, id) {
  if (!value || !id) return false
  if (value === true) return true
  if (typeof value === 'string' || typeof value === 'number') {
    const current = String(value)
    return current === id || current.endsWith(`:${id}`)
  }
  if (Array.isArray(value)) return value.some((item) => isBusy(item, id))
  return isBusy(value.id || value.target_id || value.targetId || value.release_id, id)
}

function allowedToPublish(canPublishTarget, target, slot) {
  if (typeof canPublishTarget === 'function') return canPublishTarget(target, slot) !== false
  return canPublishTarget !== false
}

function actionCallback(onPublishTarget, onPublishEnvironment) {
  return onPublishTarget || onPublishEnvironment
}

function actionLabel(status) {
  return ['failed', 'cancelled', 'canceled'].includes(status) ? '重新发布到此环境' : '发布到此环境'
}

function statusIcon(status, configured) {
  if (!configured) return <ClockCircleOutlined />
  if (status === 'succeeded') return <CheckCircleOutlined />
  if (['running', 'queued', 'preparing', 'building', 'testing', 'pushing', 'deploying', 'checking', 'switching'].includes(status)) return <LoadingOutlined spin />
  if (status === 'failed' || status === 'cancelled' || status === 'canceled') return <WarningOutlined />
  return <CloudUploadOutlined />
}

function statusDescription(status, configured, slot, gate) {
  if (!configured) return '未纳入本次发布'
  if (status === 'succeeded') return '这个环境已经发布完成'
  if (['running', 'preparing', 'building', 'testing', 'pushing', 'deploying', 'checking', 'switching'].includes(status)) return '正在发布，请稍候'
  if (status === 'queued') return '正在排队，稍后开始'
  if (status === 'failed') return gate.canPublish ? '可以重新发布这个环境' : gate.reason
  if (status === 'cancelled' || status === 'canceled') return gate.canPublish ? '可以重新发布这个环境' : gate.reason
  return gate.canPublish ? `可以开始发布到 ${slot.label}` : gate.reason
}

function actionHint({ status, gate, hasCallback, allowed, configured }) {
  if (!configured) return ''
  if (!['pending', 'failed', 'cancelled', 'canceled'].includes(status)) return ''
  if (!gate.canPublish) return gate.reason
  if (!allowed) return '当前账号没有发布权限'
  if (!hasCallback) return '当前页面还没有接入这个环境的发布动作'
  return ''
}

function targetAction({ target, slot, index, status, gate, release, onPublish, publishingTarget, canPublishTarget, actionReason }) {
  if (!target || !['pending', 'failed', 'cancelled', 'canceled'].includes(status)) return null
  const id = targetID(target)
  const busy = isBusy(publishingTarget, id)
  const allowed = allowedToPublish(canPublishTarget, target, slot)
  const retryable = ['failed', 'cancelled', 'canceled'].includes(status)
  const actionable = ['pending', 'failed', 'cancelled', 'canceled'].includes(status)
    && gate.canPublish
    && allowed
    && typeof onPublish === 'function'

  return <Tooltip title={actionable ? undefined : actionReason || '当前操作暂不可用'}>
    <span className="release-target-action-wrap">
      <Button
        type="link"
        size="small"
        icon={retryable ? <ReloadOutlined /> : <CloudUploadOutlined />}
        loading={busy}
        disabled={!actionable || busy}
        onClick={() => {
          if (!actionable) return
          onPublish(target, {
            action: retryable ? 'retry' : 'publish',
            environment: slot.key,
            environment_name: slot.name,
            environment_index: index,
            release,
          })
        }}
      >
        {actionLabel(status)}
      </Button>
    </span>
  </Tooltip>
}

export default function ReleaseTargetSummary({
  targets = [],
  canRetry = false,
  onRetryTarget,
  retryingTarget = '',
  onPublishTarget,
  onPublishEnvironment,
  publishingTarget = '',
  canPublishTarget = true,
  sequential = true,
  release,
}) {
  const targetList = Array.isArray(targets) ? targets.filter(Boolean) : []
  if (!targetList.length) return null

  const slots = getEnvironmentSlots(targetList)
  const publishAction = actionCallback(onPublishTarget, onPublishEnvironment)

  return <div className="release-target-summary">
    <div className="release-target-summary-head">
      <div>
        <div className="release-target-summary-label">发布环境</div>
      </div>
    </div>
    <div className="release-target-status-list release-target-flow-list">
      {slots.map((slot, index) => {
        const target = targetForSlot(slot)
        const configured = Boolean(target)
        const status = environmentSlotStatus(slot)
        const progress = environmentSlotProgress(slot)
        const gate = environmentGate(slots, index, sequential)
        const currentReleaseStatus = normalizeReleaseStatus(release?.status || release?.release_status, '')
        const releaseAllowsAction = !currentReleaseStatus || ['running', 'failed'].includes(currentReleaseStatus)
        const allowed = configured && releaseAllowsAction && allowedToPublish(canPublishTarget, target, slot)
        const retryAction = canRetry && typeof onRetryTarget === 'function' && ['failed', 'cancelled', 'canceled'].includes(status)
        const publishHandler = retryAction ? (currentTarget, context) => onRetryTarget(currentTarget, context) : publishAction
        const hint = actionHint({ status, gate, hasCallback: typeof publishHandler === 'function', allowed, configured })
        const [color, label] = statusMeta(configured ? status : 'unconfigured')
        const detail = configured ? '已配置部署目标' : '未选择'
        const action = targetAction({ target, slot, index, status, gate, release, onPublish: publishHandler, publishingTarget: retryAction ? retryingTarget : publishingTarget, canPublishTarget, actionReason: hint })
        const statusHint = hint || statusDescription(status, configured, slot, gate)
        return <div className={`release-target-status release-target-flow-item status-${status} ${configured ? '' : 'is-unconfigured'}`} key={slot.key} title={statusHint}>
          <span className="release-target-status-mark">{statusIcon(status, configured)}</span>
          <div className="release-target-status-copy">
            <div className="release-target-flow-name"><strong>{slot.label}</strong><small>{slot.name}</small></div>
            <small>{detail}</small>
          </div>
          <Tag color={color}>{label}{progress !== null && ['running', 'succeeded'].includes(status) ? ` ${progress}%` : ''}</Tag>
          {action}
        </div>
      })}
    </div>
  </div>
}
