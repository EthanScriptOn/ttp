import { Button, Tag } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'

const STATUS_LABELS = {
  pending: ['default', '待发布'],
  running: ['processing', '发布中'],
  succeeded: ['success', '已完成'],
  failed: ['error', '失败'],
  cancelled: ['default', '已取消'],
}

function targetStatus(target) {
  const status = String(target?.status || 'pending').toLowerCase()
  return STATUS_LABELS[status] || STATUS_LABELS.pending
}

function targetProgress(target) {
  const value = Number(target?.progress)
  if (!Number.isFinite(value)) return null
  return Math.max(0, Math.min(100, Math.round(value)))
}

export default function ReleaseTargetSummary({ targets = [], canRetry = false, onRetryTarget, retryingTarget = '' }) {
  if (!targets.length) return null

  return <div className="release-target-summary">
    <div className="release-target-summary-label">发布环境</div>
    <div className="release-target-status-list">
      {targets.map((target, index) => {
        const [color, label] = targetStatus(target)
        const progress = targetProgress(target)
        const status = String(target?.status || 'pending').toLowerCase()
        const detail = target.error || (status === 'cancelled' ? target.message : '')
        return <div className={`release-target-status status-${status}`} key={target.id || `${target.name}-${index}`}>
          <span className="release-target-status-mark" />
          <div className="release-target-status-copy">
            <strong>{target.name || target.environment || '未命名环境'}</strong>
            <small>{target.cluster_id || '未配置集群'} / {target.namespace || '未配置 namespace'}</small>
            {detail && <small className="release-target-error" title={detail}>{detail}</small>}
          </div>
          <Tag color={color}>{label}{progress !== null && (status === 'running' || status === 'succeeded') ? ` ${progress}%` : ''}</Tag>
          {canRetry && ['failed', 'cancelled'].includes(status) && <Button type="link" size="small" icon={<ReloadOutlined />} loading={retryingTarget === target.id} onClick={() => onRetryTarget?.(target)}>重试此环境</Button>}
        </div>
      })}
    </div>
  </div>
}
