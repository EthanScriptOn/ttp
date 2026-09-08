import { Alert, Progress, Tag, Typography } from 'antd'

const stageLabels = {
  queued: '排队中',
  preparing: '准备中',
  building: '构建镜像',
  testing: '发布前检查',
  pushing: '推送镜像',
  deploying: '更新 Pod',
  checking: '检查 Pod',
  switching: '切换流量',
  succeeded: '已完成',
  failed: '执行失败',
  cancelled: '已取消',
}

const statusColors = {
  queued: 'warning',
  running: 'processing',
  succeeded: 'success',
  failed: 'error',
  cancelled: 'default',
}

function displayStage(release) {
  return stageLabels[release.stage] || release.stage || ({ queued: '排队中', running: '发布中' }[release.status] || '发布状态')
}

function progressValue(release) {
  return typeof release.progress === 'number' && Number.isFinite(release.progress)
    ? Math.min(100, Math.max(0, release.progress))
    : null
}

export default function ReleaseProgress({ release }) {
  const progress = progressValue(release)
  const active = ['queued', 'running'].includes(release.status)
  const color = statusColors[release.status] || 'default'
  const message = release.message || ''

  if (!active && progress === null && !message && !release.error) return null

  return (
    <div className={`release-progress ${release.status === 'failed' ? 'has-error' : ''}`}>
      <div className="release-progress-head">
        <Tag color={color}>{displayStage(release)}</Tag>
        {progress === null ? <Typography.Text type="secondary">进度待上报</Typography.Text> : <Typography.Text type="secondary">{progress}%</Typography.Text>}
      </div>
      {progress === null ? <div className="release-progress-unavailable">暂无进度信息</div> : <Progress percent={progress} size="small" status={release.status === 'failed' ? 'exception' : release.status === 'succeeded' ? 'success' : 'active'} showInfo={false} />}
      {message && <Typography.Text type="secondary" className="release-progress-message">{message}</Typography.Text>}
      {release.error && <Alert className="release-progress-error" type="error" showIcon message={release.error} />}
    </div>
  )
}
