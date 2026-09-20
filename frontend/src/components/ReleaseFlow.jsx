import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  BranchesOutlined,
  CloudUploadOutlined,
  CodeOutlined,
  DashboardOutlined,
  DeleteOutlined,
  EnvironmentOutlined,
  EyeOutlined,
  LockOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SearchOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { message } from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ReleaseLogTerminal } from './ReleaseLogs'
import { getReleaseTargetLogs } from '../services/api'
import {
  batchIdOf,
  batchBranchOf,
  commitMessage,
  commitOf,
  formatClock,
  formatTime,
  getEnvironments,
  releaseOwner,
  targetStatus,
} from './ReleaseCenter'
import './ReleaseFlow.css'

const STRATEGIES = [
  { key: 'rolling', label: '滚动', title: '逐批替换 Pod' },
  { key: 'blue_green', label: '蓝绿', title: '新旧版本并行' },
  { key: 'canary', label: '灰度', title: '按比例切流量' },
]

const STRATEGY_COPY = {
  rolling: '服务流量保持 100%，Pod 按批次替换。新 Pod 健康后，才继续替换下一批。',
  blue_green: '同时准备新旧两套 Pod，新版本从 1% 流量开始，确认稳定后逐步切到 100%。',
  canary: '新旧版本同时接收请求，新版本从 1% 流量开始，可按比例逐步扩大。',
}

function stageOf(value) {
  const text = `${value || ''}`.toLowerCase()
  return ['dev', 'uat', 'pre', 'prod'].find((stage) => text.includes(stage)) || ''
}

function branchVersion(branch) {
  const head = branch?.head
  return head?.short_sha || head?.shortSha || head?.sha?.slice(0, 7) || branch?.short || '-'
}

function branchMessage(branch) {
  return branch?.message || branch?.head?.message || '暂无提交说明'
}

function environmentTarget(environment) {
  return environment?.target || null
}

function strategyOf(release, environment) {
  return environment?.target?.deploy_strategy || release?.strategy || release?.plan?.strategy || 'rolling'
}

function strategyLabel(value) {
  return STRATEGIES.find((item) => item.key === value)?.label || '滚动'
}

function targetCommit(target, release) {
  const value = target?.commit_sha || target?.commitSha || target?.sha || target?.commit?.sha
  return value ? value.slice(0, 7) : commitOf(release)
}

function statusLabel(release, state) {
  if (state === 'done') return '已完成'
  if (state === 'active') return release.status === 'queued' ? '排队中' : '进行中'
  if (state === 'failed') return '失败'
  return '未开始'
}

function statusClass(state) {
  return state === 'done' ? 'done' : state === 'active' ? 'active' : state === 'failed' ? 'failed' : 'pending'
}

function isEnvironmentUnlocked(release, environments, index) {
  if (!release || index === 0) return true
  return targetStatus(release, environments[index - 1]).state === 'done'
}

function hasEnvironmentRelease(release, environment) {
  if (!release || !environment) return false
  const state = targetStatus(release, environment)
  return state.state === 'active' || state.state === 'done' || state.state === 'failed'
}

function ReleaseStepBar({ page, onChange, canOpen }) {
  const stepsRef = useRef(null)
  const currentRef = useRef(null)
  const steps = [
    ['branches', '选择分支'],
    ['create', '新建发布单'],
    ['order', '发布单详情'],
    ['config', '环境发布配置'],
    ['deploy', '环境发布详情'],
  ]
  const currentIndex = Math.max(0, steps.findIndex(([key]) => key === page))
  useEffect(() => {
    const container = stepsRef.current
    const current = currentRef.current
    if (!container || !current || container.scrollWidth <= container.clientWidth) return
    const left = current.offsetLeft - (container.clientWidth - current.offsetWidth) / 2
    container.scrollTo({ left: Math.max(0, Math.min(left, container.scrollWidth - container.clientWidth)), behavior: 'smooth' })
  }, [page])
  return <nav ref={stepsRef} className="release-flow-steps" aria-label="发布流程"><span className="release-flow-steps-label">发布流程</span>{steps.map(([key, label], index) => { const current = page === key; const completed = index < currentIndex; const state = completed ? 'done' : current ? 'current' : 'upcoming'; const disabled = canOpen ? !canOpen(key) : false; return <button ref={current ? currentRef : undefined} key={key} type="button" className={`${state} ${disabled ? 'unavailable' : ''}`} onClick={() => onChange(key)} disabled={disabled} aria-current={current ? 'step' : undefined}><span>{index + 1}</span>{label}</button> })}</nav>
}

function PageFrame({ project, page, onChange, children, onBack, canOpen, embedded = false }) {
  const pageTitles = { branches: '选择分支', create: '新建发布单', order: '发布单详情', config: '环境发布配置', deploy: '环境发布详情' }
  const stepPage = page
  return <div className={`release-flow-page${embedded ? ' is-embedded' : ''}`}>
    {!embedded && <div className="release-flow-page-top"><div className="release-flow-breadcrumb"><button type="button" onClick={onBack}>项目</button><ArrowRightOutlined /><strong>{project.name}</strong><ArrowRightOutlined /><span>{pageTitles[page] || '发布流程'}</span></div><button className="release-flow-back" type="button" onClick={onBack}><ArrowLeftOutlined />返回项目</button></div>}
    <ReleaseStepBar page={stepPage} onChange={onChange} canOpen={canOpen} />
    {children}
  </div>
}

function FlowUnavailablePage({ title, message, onGoBranches }) {
  return <section className="release-flow-unavailable"><div className="release-flow-unavailable-icon">!</div><h1>{title}</h1><p>{message}</p><button className="release-flow-button primary" type="button" onClick={onGoBranches}><BranchesOutlined />去选择分支</button></section>
}

function BranchPage({ project, branches, releases, search, onSearch, onCreate, onOpen, canCreateRelease, branchLoading }) {
  return <>
    <section className="release-flow-section release-flow-branch-section">
      <div className="release-flow-section-head"><div><h2>可发布分支 <span>{branches.length}</span></h2><p>发布时会读取分支当前最新版本。</p></div><div className="release-flow-branch-search"><SearchOutlined /><input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索分支、提交说明或提交人" /></div></div>
      {branchLoading ? <div className="release-flow-empty">正在读取分支...</div> : branches.length ? <div className="release-flow-branch-list">{branches.map((item) => {
        const existing = releases.find((release) => release.branch === item.name)
        return <article className="release-flow-branch-row" key={item.name}><div className="release-flow-branch-identity"><span className="release-flow-branch-icon"><BranchesOutlined /></span><div><span className="release-flow-label">分支</span><strong>{item.name}</strong></div><div><span className="release-flow-label">最新版本</span><code>{branchVersion(item)}</code></div><div className="release-flow-commit"><span className="release-flow-label">提交说明</span><span>{branchMessage(item)}</span></div></div>{existing ? <button className="release-flow-button" type="button" onClick={() => onOpen(existing, 'dev')}><EyeOutlined />进入发布单</button> : <button className="release-flow-button primary" type="button" onClick={() => onCreate(item)} disabled={!canCreateRelease}><CloudUploadOutlined />创建发布单</button>}</article>
      })}</div> : <div className="release-flow-empty">没有找到匹配的分支</div>}
    </section>
  </>
}

function CreateReleasePage({ branch, project, environments, submitting, onBack, onOpenTargets, onSubmit, canCreateRelease }) {
  const releaseEnvironments = (environments || []).filter(Boolean)
  const hasReleaseEnvironment = releaseEnvironments.length > 0
  return <>
    <header className="release-flow-heading release-flow-heading-no-title"><div><p>确认发布来源后提交，环境发布在下一步单独配置。</p></div></header>
    <div className="release-flow-create-grid">
      <section className="release-flow-section release-flow-card"><div className="release-flow-section-head"><div><h2>发布来源</h2><p>本次发布使用分支当前版本。</p></div></div><div className="release-flow-source-grid"><div><span className="release-flow-label">发布分支</span><strong>{branch?.name || '-'}</strong></div><div><span className="release-flow-label">当前版本</span><code>{branchVersion(branch)}</code></div><div><span className="release-flow-label">最近提交</span><strong>{branchMessage(branch)}</strong></div><div><span className="release-flow-label">提交人</span><strong>{branch?.author || branch?.owner || '仓库提交人'}</strong></div></div><div className="release-flow-environment-route"><div className="release-flow-route-head"><strong>发布环境</strong><span>{releaseEnvironments.length} 个，按顺序推进</span></div><div className="release-flow-environment-sequence">{hasReleaseEnvironment ? releaseEnvironments.map((environment, index) => { const target = environmentTarget(environment); const displayName = target?.name || environment.name || environment.label; const stage = target?.environment_stage || target?.stage || target?.environment || environment.label; const destination = target ? `${target.cluster_id || '未配置集群'} / ${target.namespace || '未配置命名空间'}` : '未配置发布目标'; return <div className="release-flow-environment-sequence-row" key={environment.key || target?.id || displayName}><span className="release-flow-sequence-number">{index + 1}</span><div className="release-flow-sequence-name"><strong>{displayName}</strong><small>{stage}</small></div><span className="release-flow-sequence-target">{destination}</span><span className="release-flow-sequence-state">{index === 0 ? '首个环境' : `等待第 ${index} 个环境完成`}</span></div> }) : <div className="release-flow-sequence-empty">暂无已配置的发布环境</div>}</div></div></section>
      <aside className="release-flow-section release-flow-card release-flow-permission-card"><h2>提交发布单</h2>{hasReleaseEnvironment ? <><p>提交后进入当前开放批次。</p><div className="release-flow-permission"><div><strong>当前账号</strong><span>可提交</span></div><small>执行发布需要“执行发布”权限。</small></div></> : <div className="release-flow-missing-target"><EnvironmentOutlined /><strong>还没有可用的发布环境</strong><span>部署配置中的资源文件不会自动创建发布环境，请先添加并启用 DEV 环境。</span><button className="release-flow-button" type="button" onClick={onOpenTargets}><EnvironmentOutlined />去配置发布环境</button></div>}</aside>
    </div>
    <div className="release-flow-footer"><button className="release-flow-button primary" type="button" onClick={onSubmit} disabled={submitting || !canCreateRelease || !hasReleaseEnvironment}>{submitting ? '创建中...' : '提交发布单'}</button></div>
  </>
}

function OrderPage({ project, release, environments, removed, onOpenEnvironment, onBack, onRefresh, loading, onRemove }) {
  if (!release) return <div className="release-flow-empty">没有找到这条发布单</div>
  return <>
    <section className="release-flow-order-summary"><div className="release-flow-summary-content"><div className="release-flow-summary-primary"><div className="release-flow-order-title"><h2>{release.id}</h2><span className={removed ? 'release-flow-status muted' : 'release-flow-status'}>{removed ? '已移出批次' : release.status === 'succeeded' ? '生产成功' : release.status === 'failed' ? '发布失败' : release.status === 'running' || release.status === 'queued' ? '发布中' : '待发布'}</span></div><p><code>{release.branch || '未命名分支'}</code>　·　当前版本 <code>{commitOf(release)}</code>　·　{releaseOwner(release)}</p></div><div className="release-flow-summary-meta"><span>所属批次 <strong>{batchIdOf(release)}</strong></span><span>批次分支 <strong>{batchBranchOf(release)}</strong></span><span>创建于 <strong>{formatTime(release.created_at)}</strong></span></div></div><div className="release-flow-heading-actions release-flow-order-actions"><button className="release-flow-button danger" type="button" onClick={() => onRemove(release)} disabled={removed || release.status === 'running'}><DeleteOutlined />移出批次</button><button className="release-flow-button" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />刷新</button></div></section>
    <div className="release-flow-order-head"><div><h2>环境发布单</h2></div></div>
    <section className="release-flow-environment-list">{environments.map((environment, index) => { const state = targetStatus(release, environment); const unlocked = isEnvironmentUnlocked(release, environments, index); const published = hasEnvironmentRelease(release, environment); const target = environmentTarget(environment); return <article className={`release-flow-environment-row ${state.state === 'active' ? 'current' : ''} ${!unlocked ? 'locked' : ''}`} key={environment.key}><div className="release-flow-environment-name"><strong>{environment.label}</strong><small>{environment.name}</small></div><div><span className="release-flow-label">环境发布单</span><strong>{target?.release_id || `${release.id}-${environment.label}`}</strong><small>{target?.cluster_id || '未配置集群'} / {target?.namespace || '未配置命名空间'}</small></div><div><span className="release-flow-label">发布模式</span><strong>{strategyLabel(strategyOf(release, environment))}</strong></div><div className={`release-flow-environment-state ${statusClass(state.state)}`}><i />{!unlocked ? `完成 ${environments[index - 1]?.label} 后开放` : statusLabel(release, state.state)}</div>{unlocked ? <button className="release-flow-button primary" type="button" onClick={() => onOpenEnvironment(release, environment, published ? 'deploy' : 'config')}>{published ? '查看详情' : '配置并发布'}</button> : <span className="release-flow-locked"><LockOutlined />未解锁</span>}</article> })}</section>
  </>
}

function EnvironmentConfigPage({ release, environment, config, onConfigChange, onSubmit, submitting, publishing, canPublishRelease }) {
  const target = environmentTarget(environment)
  const activeStrategy = strategyOf(release, environment)
  return <>
    <section className="release-flow-order-summary release-flow-config-summary"><div className="release-flow-summary-content"><div className="release-flow-summary-primary"><div className="release-flow-order-title"><h2>{release.id}</h2><span className="release-flow-status active">待提交</span></div><p><code>{release.branch || '未命名分支'}</code>　·　当前版本 <code>{commitOf(release)}</code>　·　{releaseOwner(release)}</p></div><div className="release-flow-summary-meta"><span>目标环境 <strong>{environment.label}</strong></span><span>发布方式 <strong>{strategyLabel(activeStrategy)}</strong></span><span>所属批次 <strong>{batchIdOf(release)}</strong></span></div></div><div className="release-flow-heading-actions release-flow-order-actions"><button className="release-flow-button primary" type="button" onClick={onSubmit} disabled={submitting || publishing || !canPublishRelease}>{submitting || publishing ? '提交中...' : '提交环境发布单'}</button></div></section>
    <div className="release-flow-config-grid"><section className="release-flow-section release-flow-card"><div className="release-flow-section-head"><div><h2>发布目标</h2><p>目标集群和命名空间来自环境配置，副本、端口和探针等工作负载规格来自资源文件。</p></div></div><div className="release-flow-target-grid"><div><span className="release-flow-label">环境发布单</span><strong>{target?.release_id || `${release.id}-${environment.label}`}</strong></div><div><span className="release-flow-label">集群</span><strong>{target?.cluster_id || '未配置集群'}</strong></div><div><span className="release-flow-label">命名空间</span><strong>{target?.namespace || '未配置命名空间'}</strong></div><div><span className="release-flow-label">工作负载规格</span><strong>资源文件决定</strong></div></div><div className="release-flow-mode-panel"><div className="release-flow-mode-head"><strong>发布方式</strong><span>来自环境配置</span></div><div className="release-flow-mode-readonly"><strong>{strategyLabel(activeStrategy)}</strong><span>{STRATEGY_COPY[activeStrategy]}</span></div><div className="release-flow-config-fields"><div className={`release-flow-traffic ${activeStrategy === 'rolling' ? 'disabled' : ''}`}><div><span>新版本流量</span><strong>{activeStrategy === 'rolling' ? '不涉及' : `${config.traffic}%`}</strong></div><input type="range" min="1" max="100" value={activeStrategy === 'rolling' ? 1 : config.traffic} disabled={activeStrategy === 'rolling'} onChange={(event) => onConfigChange({ traffic: Number(event.target.value) })} /><small><span>新版本 {activeStrategy === 'rolling' ? '—' : `${config.traffic}%`}</span><span>旧版本 {activeStrategy === 'rolling' ? '—' : `${100 - config.traffic}%`}</span></small></div></div></div></section><aside className="release-flow-section release-flow-card release-flow-review-card"><h2>提交前确认</h2><p>提交后生成 {environment.label} 环境发布单。</p><div><span>代码版本</span><strong>{commitOf(release)}</strong></div><div><span>目标环境</span><strong>{environment.label}</strong></div><div><span>发布方式</span><strong>{strategyLabel(activeStrategy)}</strong></div><div><span>起始流量</span><strong>{activeStrategy === 'rolling' ? '不涉及' : `${config.traffic}%`}</strong></div><div className="release-flow-submit-note">当前账号可提交；没有执行权限时，提交后等待发布员执行。</div></aside></div>
  </>
}

function EnvironmentDetailPage({ project, release, environment, status, logs, logsLoading, logsError, pods, activeTab, onTabChange, onBack, onRefresh, onRepublish, onRetryTarget, retryingTarget, publishing, canPublishRelease, onOpenPod, onOpenMonitor, onOpenTerminal, canOpenTerminal, loading }) {
  const target = status.target || environmentTarget(environment)
  const commit = targetCommit(target, release)
  const strategy = strategyOf(release, environment)
  const traffic = strategy === 'rolling' ? { primary: 100, secondary: 0, primaryLabel: '当前服务', secondaryLabel: '新 Pod' } : strategy === 'blue_green' ? { primary: 100 - (release.plan?.traffic?.green_percent || 1), secondary: release.plan?.traffic?.green_percent || 1, primaryLabel: '蓝版本', secondaryLabel: '绿版本' } : { primary: 100 - (release.plan?.traffic?.candidate_percent || 1), secondary: release.plan?.traffic?.candidate_percent || 1, primaryLabel: '稳定版本', secondaryLabel: '灰度版本' }
  const actionLabel = status.state === 'failed' ? `重试 ${environment.label}` : status.state === 'done' ? '重新发布' : `发布 ${environment.label}`
  const [actionSubmitting, setActionSubmitting] = useState(false)
  const actionSubmittingRef = useRef(false)
  const canAction = canPublishRelease && status.state !== 'active' && !['queued', 'running'].includes(release.status)
  const actionBusy = publishing || actionSubmitting || Boolean(retryingTarget)
  const runAction = async () => {
    if (!canAction || loading || actionBusy || actionSubmittingRef.current) return
    actionSubmittingRef.current = true
    setActionSubmitting(true)
    try {
      if (status.state === 'failed' && status.target) await onRetryTarget?.(release, status.target)
      else await onRepublish?.(release, environment)
    } finally {
      actionSubmittingRef.current = false
      setActionSubmitting(false)
    }
  }
  return <>
    <section className="release-flow-environment-overview">
      <div className="release-flow-environment-overview-copy"><h2>{release.id} · {strategyLabel(strategy)}发布</h2><p className="release-flow-environment-release-meta"><code>{release.branch}</code>　·　当前版本 <code>{commit}</code>　·　{releaseOwner(release)}</p></div>
      <div className="release-flow-environment-overview-actions"><button className="release-flow-button primary" type="button" disabled={!canAction || loading || actionBusy} onClick={runAction}>{actionBusy ? <ReloadOutlined spin /> : status.state === 'failed' ? <WarningOutlined /> : status.state === 'done' ? <ReloadOutlined /> : <PlayCircleOutlined />}{actionSubmitting || publishing ? '提交中...' : retryingTarget ? '处理中...' : actionLabel}</button><button className="release-flow-button" type="button" onClick={onRefresh} disabled={loading || actionBusy}><ReloadOutlined />{loading ? '刷新中' : '刷新'}</button></div>
    </section>
    <section className={`release-flow-section release-flow-runtime-card is-${activeTab}`}><nav className="release-flow-detail-tabs"><button className={activeTab === 'logs' ? 'active' : ''} type="button" onClick={() => onTabChange('logs')}>执行日志</button><button className={activeTab === 'pods' ? 'active' : ''} type="button" onClick={() => onTabChange('pods')}>Pod 列表 <span>{pods.length}</span></button><button className={activeTab === 'traffic' ? 'active' : ''} type="button" onClick={() => onTabChange('traffic')}>流量变化</button></nav>{activeTab === 'logs' && <ReleaseLogTerminal logs={logs} emptyText={logsLoading ? '正在读取执行日志...' : logsError ? '执行日志暂时无法加载' : '尚未产生执行输出'} />}{activeTab === 'pods' && <PodList pods={pods} release={release} onOpenPod={onOpenPod} onOpenMonitor={onOpenMonitor} onOpenTerminal={onOpenTerminal} canOpenTerminal={canOpenTerminal} />}{activeTab === 'traffic' && <TrafficView traffic={traffic} strategy={strategy} />}</section>
  </>
}

function PodList({ pods, release, onOpenPod, onOpenMonitor, onOpenTerminal, canOpenTerminal }) {
  return <div className="release-flow-pod-table"><div className="release-flow-pod-head"><span>Pod</span><span>版本</span><span>节点</span><span>状态</span><span>操作</span></div>{pods.length ? pods.map((pod) => <div className="release-flow-pod-row" key={pod.name}><button type="button" onClick={() => onOpenPod?.(pod)}>{pod.name}</button><code>{pod.labels?.version || pod.labels?.['app.kubernetes.io/version'] || commitOf(release)}</code><span>{pod.node_name || '-'}</span><span className={pod.ready ? 'ready' : 'starting'}>{pod.ready ? 'Ready' : pod.phase || '处理中'}</span><div className="release-flow-pod-actions"><button type="button" onClick={() => onOpenMonitor?.(pod)}><DashboardOutlined />监控</button><button type="button" disabled={!canOpenTerminal} title={canOpenTerminal ? '进入 Pod Terminal' : '当前账号没有进入 Pod 终端的权限'} onClick={() => onOpenTerminal?.(pod)}><CodeOutlined />Terminal</button></div></div>) : <div className="release-flow-empty">当前环境还没有可展示的 Pod</div>}</div>
}

function TrafficView({ traffic, strategy }) {
  return <div className="release-flow-traffic-view"><div className="release-flow-traffic-title"><strong>{strategyLabel(strategy)} · 当前流量</strong><span>新版本从 1% 开始</span></div><div className="release-flow-traffic-grid"><div><span>{traffic.primaryLabel}</span><strong>{traffic.primary}%</strong><div className="release-flow-traffic-bar"><i style={{ width: `${traffic.primary}%` }} /></div><small>旧版本 / 稳定版本</small></div><div><span>{traffic.secondaryLabel}</span><strong>{traffic.secondary}%</strong><div className="release-flow-traffic-bar candidate"><i style={{ width: `${traffic.secondary}%` }} /></div><small>新版本</small></div></div></div>
}

export default function ReleaseFlow({
  project,
  spaceName = '',
  userName = '平台管理员',
  onBack,
  onOpenTargets,
  branch,
  branches = [],
  branchLoading = false,
  releases = [],
  loading = false,
  onRefresh,
  onCreateRelease,
  onPublishEnvironment,
  onRetryTarget,
  onLoadReleaseTargetLogs,
  onEnvironmentChange,
  onOpenPod,
  onOpenMonitor,
  onOpenTerminal,
  canOpenTerminal = true,
  pods = [],
  retryingTarget = '',
  canCreateRelease = true,
  canPublishRelease = true,
  publishing = false,
  targets = [],
  embedded = false,
}) {
  const [page, setPage] = useState('branches')
  const [branchSearch, setBranchSearch] = useState('')
  const [selectedBranch, setSelectedBranch] = useState(null)
  const [selectedReleaseId, setSelectedReleaseId] = useState(releases[0]?.id || '')
  const [selectedEnvironmentKey, setSelectedEnvironmentKey] = useState('')
  const [activeDetailTab, setActiveDetailTab] = useState('logs')
  const [environmentLogs, setEnvironmentLogs] = useState([])
  const [environmentLogsLoading, setEnvironmentLogsLoading] = useState(false)
  const [environmentLogsError, setEnvironmentLogsError] = useState('')
  const [removedIds, setRemovedIds] = useState(() => new Set())
  const [submitting, setSubmitting] = useState(false)
  const [environmentConfig, setEnvironmentConfig] = useState({ traffic: 1 })
  const environments = useMemo(() => getEnvironments(targets), [targets])
  const selectedRelease = releases.find((item) => item.id === selectedReleaseId) || releases[0] || null
  const selectedEnvironment = environments.find((item) => item.key === selectedEnvironmentKey) || environments[0]
  const selectedStatus = selectedRelease && selectedEnvironment ? targetStatus(selectedRelease, selectedEnvironment) : { state: 'pending', target: null }
  const selectedTargetId = selectedStatus.target?.id || selectedEnvironment?.target?.id || ''
  const selectedLogRevision = selectedStatus.target?.updated_at || selectedStatus.target?.finished_at || selectedStatus.target?.started_at || selectedRelease?.updated_at || selectedStatus.state
  const loadTargetLogs = useCallback((releaseId, targetId) => onLoadReleaseTargetLogs ? onLoadReleaseTargetLogs(releaseId, targetId) : getReleaseTargetLogs(project.id, releaseId, targetId), [onLoadReleaseTargetLogs, project.id])
  const visibleBranches = useMemo(() => {
    const term = branchSearch.trim().toLowerCase()
    const source = branches
    return source.filter((item) => !/^batch(?:[-/]|$)/i.test(item.name || '')).filter((item) => `${item.name || ''} ${item.author || item.owner || ''} ${branchVersion(item)} ${branchMessage(item)}`.toLowerCase().includes(term))
  }, [branches, branch, branchSearch])
  const openCreate = (item) => { setSelectedBranch(item); setPage('create') }
  const openRelease = (release, environment = 'dev', nextPage = 'order') => { setSelectedReleaseId(release.id); setSelectedEnvironmentKey(environment.key || environment); setActiveDetailTab('logs'); onEnvironmentChange?.(environment.key || environment); setPage(nextPage) }
  const submitCreate = async () => {
    if (!selectedBranch || !onCreateRelease) return
    if (!environments.length) {
      message.warning('请先添加并启用 DEV 发布环境，再创建发布单')
      onOpenTargets?.()
      return
    }
    setSubmitting(true)
    try {
      const created = await onCreateRelease({ branch: selectedBranch.name })
      if (created?.id) { setSelectedReleaseId(created.id); setPage('order') }
    } finally { setSubmitting(false) }
  }
  const openEnvironment = (release, environment, nextPage) => { setSelectedReleaseId(release.id); setSelectedEnvironmentKey(environment.key); onEnvironmentChange?.(environment.key); const strategy = strategyOf(release, environment); setEnvironmentConfig({ traffic: strategy === 'rolling' ? 0 : Math.max(1, release.plan?.traffic?.candidate_percent || release.plan?.traffic?.green_percent || 1) }); setPage(nextPage) }
  useEffect(() => {
    if (page !== 'deploy' || !selectedRelease?.id || !selectedTargetId) {
      setEnvironmentLogs([])
      setEnvironmentLogsLoading(false)
      setEnvironmentLogsError('')
      return undefined
    }
    let active = true
    setEnvironmentLogs([])
    setEnvironmentLogsLoading(true)
    setEnvironmentLogsError('')
    loadTargetLogs(selectedRelease.id, selectedTargetId)
      .then((nextLogs) => {
        if (active) setEnvironmentLogs(Array.isArray(nextLogs) ? nextLogs : [])
      })
      .catch((error) => {
        if (active) setEnvironmentLogsError(error.message || '执行日志加载失败')
      })
      .finally(() => {
        if (active) setEnvironmentLogsLoading(false)
      })
    return () => { active = false }
  }, [page, selectedRelease?.id, selectedTargetId, selectedLogRevision, loadTargetLogs])
  const submitEnvironment = async () => {
    if (!selectedRelease || !selectedEnvironment || !onPublishEnvironment) return
    setSubmitting(true)
    try {
      const updated = await onPublishEnvironment(selectedRelease, selectedEnvironment, environmentConfig)
      if (updated?.id) {
        setSelectedReleaseId(updated.id)
        setPage('deploy')
      }
    } catch (error) {
      message.error(error.message || '提交环境发布单失败')
    } finally { setSubmitting(false) }
  }
  const removeFromBatch = (release) => { setRemovedIds((old) => new Set([...old, release.id])); message.success(`${release.branch || '这个分支'} 已移出批次，发布记录仍保留`) }
  const canOpenPage = (nextPage) => {
    const pageIndex = { branches: 0, create: 1, order: 2, config: 3, deploy: 4 }
    const currentPage = page
    const nextIndex = pageIndex[nextPage]
    if (nextPage === 'branches') return true
    if (nextIndex > pageIndex[currentPage]) return false
    if (nextPage === 'create') return Boolean(selectedBranch)
    if (nextPage === 'order') return Boolean(selectedRelease)
    if (nextPage === 'config') return Boolean(selectedRelease && selectedEnvironment)
    if (nextPage === 'deploy') return hasEnvironmentRelease(selectedRelease, selectedEnvironment)
    return false
  }
  const pageChange = (nextPage) => {
    if (!canOpenPage(nextPage)) return
    if (nextPage === 'config' && selectedRelease && selectedEnvironment) {
      openEnvironment(selectedRelease, selectedEnvironment, 'config')
      return
    }
    setPage(nextPage)
  }

  const pageContent = page === 'config' && (!selectedRelease || !selectedEnvironment)
    ? <FlowUnavailablePage title="还没有环境发布配置" message="请先选择分支并创建发布单，再配置要发布的环境。" onGoBranches={() => setPage('branches')} />
    : page === 'deploy' && !hasEnvironmentRelease(selectedRelease, selectedEnvironment)
      ? <FlowUnavailablePage title="还没有环境发布详情" message="当前环境还没有执行记录。先从发布单详情配置并发布，完成后这里才会有日志、Pod 和流量信息。" onGoBranches={() => setPage('branches')} />
      : <>
        {page === 'branches' && <BranchPage project={project} branches={visibleBranches} releases={releases.filter((item) => !removedIds.has(item.id))} search={branchSearch} onSearch={setBranchSearch} onCreate={openCreate} onOpen={openRelease} canCreateRelease={canCreateRelease} branchLoading={branchLoading} />}
        {page === 'create' && <CreateReleasePage project={project} branch={selectedBranch} environments={environments} submitting={submitting} onBack={() => setPage('branches')} onOpenTargets={onOpenTargets || onBack} onSubmit={submitCreate} canCreateRelease={canCreateRelease} />}
        {page === 'order' && <OrderPage project={project} release={selectedRelease} environments={environments} removed={selectedRelease ? removedIds.has(selectedRelease.id) : false} onOpenEnvironment={openEnvironment} onBack={() => setPage('branches')} onRefresh={onRefresh} loading={loading} onRemove={removeFromBatch} />}
        {page === 'config' && selectedRelease && selectedEnvironment && <EnvironmentConfigPage release={selectedRelease} environment={selectedEnvironment} config={environmentConfig} onConfigChange={(next) => setEnvironmentConfig((old) => ({ ...old, ...next }))} onSubmit={submitEnvironment} submitting={submitting} publishing={publishing} canPublishRelease={canPublishRelease} />}
        {page === 'deploy' && selectedRelease && selectedEnvironment && <EnvironmentDetailPage project={project} release={selectedRelease} environment={selectedEnvironment} status={selectedStatus} logs={environmentLogs} logsLoading={environmentLogsLoading} logsError={environmentLogsError} pods={pods} activeTab={activeDetailTab} onTabChange={setActiveDetailTab} onBack={() => setPage('order')} onRefresh={onRefresh} onRepublish={onPublishEnvironment} onRetryTarget={onRetryTarget} retryingTarget={retryingTarget} publishing={publishing} canPublishRelease={canPublishRelease} onOpenPod={onOpenPod} onOpenMonitor={onOpenMonitor} onOpenTerminal={onOpenTerminal} canOpenTerminal={canOpenTerminal} loading={loading} />}
      </>

  return <div className={`release-flow-portal${embedded ? ' is-embedded' : ''}`}>
    <PageFrame project={project} page={page} onChange={pageChange} onBack={onBack} canOpen={canOpenPage} embedded={embedded}>
      {pageContent}
    </PageFrame>
  </div>
}

function OrderListCard({ release, environments, removed, onOpen, onRemove }) {
  const states = environments.map((environment) => ({ environment, state: targetStatus(release, environment) }))
  const active = states.find((item) => item.state.state === 'active')
  const failed = states.find((item) => item.state.state === 'failed')
  const doneCount = states.filter((item) => item.state.state === 'done').length
  const label = removed ? '已移出批次' : failed ? `${failed.environment.label} 失败` : active ? `${active.environment.label} 进行中` : doneCount === environments.length ? '全部完成' : doneCount ? `${environments[doneCount]?.label || '下一环境'} 待发布` : '待发布'
  return <article className={`release-flow-order-card ${removed ? 'removed' : ''}`}><div className="release-flow-order-card-top"><div><div className="release-flow-order-id"><strong>{release.id}</strong><span>{formatTime(release.created_at)}</span></div><h3>{release.branch || '未命名分支'}</h3><p>{releaseOwner(release)}　·　{batchIdOf(release)}</p></div><span className={`release-flow-status ${failed ? 'failed' : active ? 'active' : removed ? 'muted' : ''}`}>{label}</span></div><div className="release-flow-order-version"><span>当前版本</span><code>{commitOf(release)}</code><span>{commitMessage(release)}</span></div><div className="release-flow-card-environments">{states.map(({ environment, state }) => <button key={environment.key} className={statusClass(state.state)} type="button" onClick={() => onOpen(release, environment, state.state === 'done' ? 'deploy' : 'config')}><span>{environment.label}</span><small>{state.state === 'pending' ? '未开始' : statusLabel(release, state.state)}</small></button>)}</div><div className="release-flow-order-card-foot"><span>{doneCount} / {environments.length} 环境完成</span><div><button className="release-flow-text-button" type="button" onClick={() => onOpen(release, 'dev', 'order')}><EyeOutlined />查看发布单详情</button><button className="release-flow-text-button danger" type="button" onClick={() => onRemove(release)} disabled={removed}><DeleteOutlined />移出批次</button></div></div></article>
}
