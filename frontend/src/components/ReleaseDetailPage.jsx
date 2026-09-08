import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  BranchesOutlined,
  CodeOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  RollbackOutlined,
} from '@ant-design/icons'
import { useMemo, useState } from 'react'
import {
  batchBranchOf,
  batchIdOf,
  commitMessage,
  commitOf,
  formatClock,
  formatTime,
  getEnvironments,
  makeLogs,
  releaseOwner,
  targetStatus,
} from './ReleaseCenter'
import { ReleaseLogTerminal } from './ReleaseLogs'
import './ReleaseDetailPage.css'

const STRATEGIES = [
  { key: 'rolling', label: '滚动', title: '逐步替换 Pod' },
  { key: 'canary', label: '灰度', title: '逐步切流量' },
  { key: 'blue_green', label: '蓝绿', title: '新旧版本切换' },
]

function strategyOf(release, target) {
  return target?.deploy_strategy || release.strategy || release.plan?.strategy || 'rolling'
}

function strategyLabel(value) {
  return STRATEGIES.find((item) => item.key === value)?.label || '滚动'
}

function targetCommit(target, release) {
  const value = target?.commit_sha || target?.commitSha || target?.sha || target?.commit?.sha
  return value ? value.slice(0, 7) : commitOf(release)
}

function statusText(release, state) {
  if (state === 'done') return '已完成'
  if (state === 'active') return release.status === 'queued' ? '排队中' : '进行中'
  if (state === 'failed') return '失败'
  return '未开始'
}

function statusClass(state) {
  return state === 'done' ? 'done' : state === 'active' ? 'progress' : state === 'failed' ? 'failed' : 'muted'
}

function podVersion(pod, release) {
  return pod.labels?.version || pod.labels?.['app.kubernetes.io/version'] || commitOf(release)
}

function podTraffic(pod) {
  return (pod.traffic_percent ?? pod.trafficPercent ?? pod.labels?.traffic_percent ?? pod.labels?.traffic) ?? '-'
}

function trafficFor(strategy, release) {
  const traffic = release.plan?.traffic || release.traffic || {}
  if (strategy === 'canary') {
    const candidate = Number(traffic.candidate_percent ?? traffic.candidatePercent ?? 1)
    return { primary: Math.max(0, 100 - candidate), secondary: Math.min(100, candidate), primaryLabel: '稳定版本', secondaryLabel: '灰度版本', primaryColor: 'blue', secondaryColor: 'green' }
  }
  if (strategy === 'blue_green') {
    const green = Number(traffic.green_percent ?? traffic.greenPercent ?? 1)
    return { primary: Math.max(0, 100 - green), secondary: Math.min(100, green), primaryLabel: '蓝环境', secondaryLabel: '绿环境', primaryColor: 'blue', secondaryColor: 'green' }
  }
  return { primary: 100, secondary: 0, primaryLabel: '当前服务', secondaryLabel: '新 Pod', primaryColor: 'blue', secondaryColor: 'green' }
}

export default function ReleaseDetailPage({
  project,
  release,
  targets = [],
  selectedEnvironment = 'dev',
  pods = [],
  loading = false,
  onBack,
  onRefresh,
  onEnvironmentChange,
  onRepublish,
  onRetryTarget,
  onOpenPod,
  retryingTarget = '',
  canPublishRelease = true,
}) {
  const environments = useMemo(() => getEnvironments(targets), [targets])
  const environment = environments.find((item) => item.key === selectedEnvironment) || environments[0]
  const status = targetStatus(release, environment)
  const target = status.target || environment.target || null
  const strategy = strategyOf(release, target)
  const [displayStrategy, setDisplayStrategy] = useState(strategy)
  const traffic = trafficFor(displayStrategy, release)
  const logs = makeLogs(project, release, environment, status)
  const commit = targetCommit(target, release)
  const actionLabel = status.state === 'failed' ? `重试 ${environment.label}` : status.state === 'pending' ? `发布 ${environment.label}` : `重新发布 ${environment.label}`
  const canAction = canPublishRelease && !['queued', 'running'].includes(release.status)
  const targetRetryKey = target ? `${release.id}:${target.id}` : ''
  const podRows = pods || []

  const changeEnvironment = (nextEnvironment) => {
    setDisplayStrategy(strategyOf(release, nextEnvironment.target))
    onEnvironmentChange?.(nextEnvironment.key)
  }

  const runAction = () => {
    if (status.state === 'failed' && status.target && onRetryTarget) onRetryTarget(release, status.target)
    else onRepublish?.(release)
  }

  return <section className="release-detail-page">
    <div className="release-detail-breadcrumb"><button type="button" onClick={onBack}>发布中心</button><ArrowRightOutlined /><strong>{release.id}</strong><span>发布单详情</span></div>

    <header className="release-detail-header">
      <div className="release-detail-heading">
        <span className="release-detail-eyebrow">RELEASE ORDER</span>
        <div className="release-detail-title-line"><h1>{release.branch || '未命名分支'}</h1><span className={`release-detail-status ${statusClass(release.status === 'succeeded' ? 'done' : release.status === 'failed' ? 'failed' : release.status === 'running' || release.status === 'queued' ? 'progress' : 'muted')}`}>{release.status === 'succeeded' ? '生产成功' : release.status === 'failed' ? '发布失败' : release.status === 'running' ? '发布中' : release.status === 'queued' ? '排队中' : release.status === 'cancelled' ? '已取消' : '待发布'}</span></div>
        <div className="release-detail-release-line"><strong>{release.id}</strong><span>·</span><span>{releaseOwner(release)} 创建</span><span>·</span><span>本次发布版本</span><code>{commit}</code><span>{commitMessage(release)}</span></div>
      </div>
      <button className="release-detail-back" type="button" onClick={onBack}><ArrowLeftOutlined />返回发布中心</button>
    </header>

    <div className="release-detail-meta">
      <span><BranchesOutlined /> 批次 <strong>{batchIdOf(release)}</strong></span>
      <span><CodeOutlined /> 批次分支 <code>{batchBranchOf(release)}</code></span>
      <span>创建时间 <strong>{formatTime(release.created_at)}</strong></span>
      <span>发布方式 <strong>{strategyLabel(strategy)}</strong></span>
    </div>

    <div className="release-detail-env-nav" aria-label="发布环境">
      {environments.map((item) => {
        const itemStatus = targetStatus(release, item)
        return <button key={item.key} type="button" className={`release-detail-env-card ${item.key === environment.key ? 'active' : ''}`} onClick={() => changeEnvironment(item)}>
          <span className="release-detail-env-card-head"><strong>{item.label}</strong><span className={`release-detail-env-state ${statusClass(itemStatus.state)}`}>{statusText(release, itemStatus.state)}</span></span>
          <span className="release-detail-env-name">{item.name}</span>
          <span className="release-detail-env-detail">{item.target?.cluster_id || '未配置集群'} / {item.target?.namespace || '未配置 namespace'}</span>
        </button>
      })}
    </div>

    <section className="release-detail-section release-detail-runtime">
      <div className="release-detail-section-head">
        <div><div className="release-detail-env-heading"><h2>{environment.label}</h2><span className={`release-detail-env-state ${statusClass(status.state)}`}>{statusText(release, status.state)}</span></div><span className="release-detail-section-subtitle">{environment.name} · {target?.cluster_id || '未配置集群'} · namespace {target?.namespace || '未配置'}</span></div>
        <div className="release-detail-actions"><button className="release-detail-button" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />{loading ? '刷新中' : '刷新'}</button><button className="release-detail-button primary" type="button" onClick={runAction} disabled={!canAction || loading || Boolean(retryingTarget)}>{status.state === 'failed' ? <RollbackOutlined /> : status.state === 'pending' ? <PlayCircleOutlined /> : <ReloadOutlined />}{retryingTarget === targetRetryKey ? '重试中' : actionLabel}</button></div>
      </div>
      <div className="release-detail-runtime-grid">
        <div className="release-detail-runtime-panel">
          <div className="release-detail-runtime-title"><strong>流量与发布方式</strong><span>{target ? `实际版本 ${commit}` : '尚未发布'}</span></div>
          <div className="release-detail-mode-switch" aria-label="查看发布方式"><span>查看：</span>{STRATEGIES.map((item) => <button key={item.key} type="button" className={displayStrategy === item.key ? 'active' : ''} onClick={() => setDisplayStrategy(item.key)}>{item.label}</button>)}</div>
          <div className="release-detail-traffic-values"><div className={`release-detail-traffic-value ${traffic.primaryColor}`}><span>{traffic.primaryLabel}</span><strong>{traffic.primary}%</strong></div><div className={`release-detail-traffic-value ${traffic.secondaryColor}`}><span>{traffic.secondaryLabel}</span><strong>{traffic.secondary}%</strong></div></div>
          <div className="release-detail-traffic-bar"><span className={traffic.primaryColor} style={{ width: `${traffic.primary}%` }} /><span className={traffic.secondaryColor} style={{ width: `${traffic.secondary}%` }} /></div>
          <div className="release-detail-traffic-legend"><span><i className={traffic.primaryColor} />{traffic.primaryLabel}</span><span><i className={traffic.secondaryColor} />{traffic.secondaryLabel}</span></div>
          <div className="release-detail-rollout-note"><strong>{strategyLabel(displayStrategy)}：</strong>{displayStrategy === 'rolling' ? '服务流量保持 100%，Pod 按顺序替换。' : displayStrategy === 'canary' ? '灰度版本从少量流量开始，确认正常后再逐步扩大。' : '新旧两套 Pod 并行，确认新版本正常后一次切换流量。'}</div>
        </div>
        <div className="release-detail-runtime-panel">
          <div className="release-detail-pod-toolbar"><div className="release-detail-runtime-title"><strong>Pod 列表</strong><span>{podRows.length} 个 Pod</span></div><span className="release-detail-pod-version">当前版本 {commit}</span></div>
          <div className="release-detail-pod-table-wrap"><table className="release-detail-pod-table"><thead><tr><th>Pod</th><th>版本</th><th>状态</th><th>就绪</th><th>CPU / 内存</th><th>流量</th><th /></tr></thead><tbody>{podRows.length ? podRows.map((pod) => <tr key={pod.name}><td><button type="button" className="release-detail-pod-link" onClick={() => onOpenPod?.(pod, target)}>{pod.name}</button></td><td><code>{podVersion(pod, release)}</code></td><td><span className={`release-detail-pod-state ${pod.ready ? '' : 'starting'}`}>{pod.ready ? '运行中' : pod.phase || '处理中'}</span></td><td>{pod.ready ? 'Ready' : '等待'}</td><td>{pod.cpu_usage || pod.cpu || '-'} / {pod.memory_usage || pod.memory || '-'}</td><td>{podTraffic(pod)}{podTraffic(pod) === '-' ? '' : '%'}</td><td><button type="button" className="release-detail-pod-action" onClick={() => onOpenPod?.(pod, target)}>详情</button></td></tr>) : <tr><td colSpan="7" className="release-detail-table-empty">当前环境还没有可展示的 Pod</td></tr>}</tbody></table></div>
        </div>
      </div>
    </section>

    <section className="release-detail-section release-detail-logs">
      <div className="release-detail-section-head"><div><div className="release-detail-log-heading"><h2><FileTextOutlined /> 执行日志</h2><span>{logs.length} 条</span></div><span className="release-detail-section-subtitle">{environment.label} 的 TTP、Git、镜像、K8s 和 Pod 操作</span></div><span className="release-detail-log-context">{formatClock(target?.started_at || release.created_at)} 开始</span></div>
      <ReleaseLogTerminal logs={logs} />
    </section>
  </section>
}
