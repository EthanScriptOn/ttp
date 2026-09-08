import {
  ArrowDownOutlined,
  ArrowRightOutlined,
  BranchesOutlined,
  CheckCircleOutlined,
  CloseOutlined,
  CloudUploadOutlined,
  ClusterOutlined,
  CodeOutlined,
  DeleteOutlined,
  DeploymentUnitOutlined,
  EyeOutlined,
  FileTextOutlined,
  LockOutlined,
  PlusOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SearchOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import { message } from 'antd'
import { useMemo, useState } from 'react'
import { executionLogsForTarget, ReleaseLogTerminal } from './ReleaseLogs'
import BrandLogo from './BrandLogo'
import './ReleaseCenter.css'

const STATUS_TEXT = {
  draft: '待发布',
  queued: '排队中',
  running: 'DEV 验证中',
  succeeded: '生产成功',
  failed: '发布失败',
  cancelled: '已取消',
}

const STATUS_CLASS = {
  draft: 'muted',
  queued: 'warn',
  running: 'warn',
  succeeded: '',
  failed: 'danger',
  cancelled: 'muted',
}

function asStage(value) {
  const text = `${value || ''}`.toLowerCase()
  return ['dev', 'uat', 'pre', 'prod'].find((stage) => text.includes(stage)) || ''
}

function environmentKey(target) {
  return asStage(`${target?.environment_stage || ''} ${target?.stage || ''} ${target?.environment || ''} ${target?.name || ''}`)
}

export function releaseOwner(release) {
  return release.created_by_name || release.creator_name || release.author || release.created_by || '发布人未返回'
}

export function commitOf(value) {
  const commit = value?.commits?.[0] || value?.commit
  return commit?.short_sha || commit?.shortSha || commit?.sha?.slice(0, 7) || value?.short || '-'
}

export function commitMessage(value) {
  return value?.commits?.[0]?.message || value?.message || '无提交说明'
}

export function formatTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function formatClock(value) {
  if (!value) return '--:--:--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleTimeString('zh-CN', { hour12: false })
}

export function targetStatus(release, environment) {
  const target = (release.targets || []).find((item) => {
    if (environment.target?.id && item.id === environment.target.id) return true
    if (item.id && item.id === environment.key) return true
    return environmentKey(item) === environmentKey(environment.target || environment)
  })
  if (!target) return { state: 'pending', label: '未开始', target: null }
  const status = `${target.status || ''}`.toLowerCase()
  if (status === 'succeeded' || status === 'success' || status === 'completed') return { state: 'done', label: '已完成', target }
  if (status === 'failed') return { state: 'failed', label: '失败', target }
  if (status === 'running' || status === 'queued') return { state: 'active', label: status === 'queued' ? '排队中' : '进行中', target }
  return { state: 'pending', label: '未开始', target }
}

function branchHead(branch) {
  const head = branch?.head
  return head?.short_sha || head?.shortSha || head?.sha?.slice(0, 7) || '-'
}

export function batchIdOf(release) {
  return release.batch_id || release.batchId || release.batch || 'batch-000001'
}

export function batchBranchOf(release) {
  return release.batch_branch || release.batchBranch || release.temporary_branch || `batch/${(release.created_at || '').slice(0, 10) || 'current'}`
}

function IconStep({ type }) {
  const icons = {
    ttp: <CloudUploadOutlined />,
    git: <BranchesOutlined />,
    build: <CodeOutlined />,
    k8s: <DeploymentUnitOutlined />,
    pod: <ClusterOutlined />,
  }
  return <span className={`release-v4-log-icon ${type}`}>{icons[type] || <FileTextOutlined />}</span>
}

export function getEnvironments(targets) {
  const enabled = (targets || []).filter((target) => target.enabled !== false).sort((left, right) => (left.sort_order || 0) - (right.sort_order || 0))
  return enabled.map((target) => {
    const stage = `${target.stage || target.environment || ''}`.trim()
    const label = stage && stage.toLowerCase() !== 'custom' ? stage.toUpperCase() : target.name || '环境'
    return {
      key: target.id || environmentKey(target) || target.environment || target.name,
      label,
      name: target.name || stage || '环境',
      target,
    }
  })
}

export function makeLogs(project, release, environment, status) {
  return executionLogsForTarget(status?.target || environment?.target, release)
}

function buildRecords(releases, environments, removedIds) {
  return releases.flatMap((release) => environments.map((environment) => {
    const status = targetStatus(release, environment)
    if (status.state === 'pending') return null
    return {
      id: status.target?.release_id || `${release.id}-${environment.key}`,
      release,
      environment,
      status,
      sha: commitOf(release),
      message: commitMessage(release),
      result: removedIds.has(release.id) ? '已移出批次' : status.state === 'done' ? '成功' : status.state === 'failed' ? '失败' : '进行中',
      time: formatTime(status.target?.finished_at || status.target?.started_at || release.created_at),
    }
  }).filter(Boolean))
}

export default function ReleaseCenter({
  project,
  spaceName = 'Android 逆向实验室',
  userName = '平台管理员',
  onBack,
  branch,
  branches = [],
  branchLoading = false,
  onBranchChange,
  commits = [],
  releases = [],
  loading = false,
  onRefresh,
  onPublish,
  onRepublish,
  onRetryTarget,
  onOpenRelease,
  retryingTarget = '',
  canCreateRelease = true,
  canPublishRelease = true,
  targets = [],
}) {
  const [view, setView] = useState(releases.length ? 'orders' : 'branches')
  const [branchSearch, setBranchSearch] = useState('')
  const [orderSearch, setOrderSearch] = useState('')
  const [recordFilter, setRecordFilter] = useState('all')
  const [removedIds, setRemovedIds] = useState(() => new Set())

  const environments = useMemo(() => getEnvironments(targets), [targets])
  const records = useMemo(() => buildRecords(releases, environments, removedIds), [releases, environments, removedIds])
  const visibleBranches = useMemo(() => {
    const term = branchSearch.trim().toLowerCase()
    return branches
      .filter((item) => !/^batch(?:[-/]|$)/i.test(item.name || ''))
      .filter((item) => `${item.name || ''} ${item.author || item.owner || ''} ${branchHead(item)}`.toLowerCase().includes(term))
  }, [branches, branchSearch])
  const visibleOrders = useMemo(() => {
    const term = orderSearch.trim().toLowerCase()
    return releases.filter((release) => [release.id, release.branch, releaseOwner(release), commitOf(release), commitMessage(release), batchIdOf(release)].join(' ').toLowerCase().includes(term))
  }, [releases, orderSearch])
  const filteredRecords = useMemo(() => records.filter((record) => {
    return recordFilter === 'all' || (recordFilter === 'active' && record.result === '进行中') || (recordFilter === 'success' && record.result === '成功') || (recordFilter === 'failed' && record.result === '失败')
  }), [records, recordFilter])

  const createReleaseFromBranch = async (nextBranch) => {
    if (!nextBranch) {
      message.warning('请选择一个发布分支')
      return
    }
    if (nextBranch !== branch && onBranchChange) await onBranchChange(nextBranch)
    setView('orders')
    onPublish?.(nextBranch)
  }

  const removeFromBatch = (release) => {
    setRemovedIds((old) => new Set([...old, release.id]))
    message.success(`${release.branch || '这个分支'} 已移出当前批次，发布记录仍保留`)
  }

  const filteredBranchRows = visibleBranches.length ? visibleBranches : (branch && !branchSearch ? [{ name: branch, head: { sha: commits[0]?.sha, short_sha: commitOf(commits[0]) }, message: commitMessage(commits[0]) }] : [])

  return <div className="release-v4-portal">
    <div className="release-v4-shell">
      <header className="release-v4-header">
        <div className="release-v4-brand"><BrandLogo tone="dark" compact /></div>
        <div className="release-v4-header-right"><button className="release-v4-space" type="button" onClick={() => message.info('请从顶部空间菜单切换空间')}><span>▱</span><strong>{spaceName}</strong><span>⌄</span></button><div className="release-v4-user"><span className="release-v4-avatar">{userName.slice(0, 1)}</span><span>{userName}</span><span>⌄</span></div></div>
      </header>
      <main className="release-v4-main">
        <div className="release-v4-breadcrumb"><button type="button" onClick={onBack}>项目</button><ArrowRightOutlined /><strong>{project.name}</strong><ArrowRightOutlined /><span>{view === 'branches' ? '新建发布单' : view === 'orders' ? '发布单' : '发布记录'}</span></div>
        {view === 'branches' && <BranchSelectionPage project={project} branches={filteredBranchRows} releases={releases} removedIds={removedIds} search={branchSearch} loading={branchLoading} canCreateRelease={canCreateRelease} onSearch={setBranchSearch} onCreate={createReleaseFromBranch} onOpenRelease={onOpenRelease} onBack={() => setView('orders')} />}
        {view === 'orders' && <ReleaseOrdersPage project={project} releases={visibleOrders} environments={environments} search={orderSearch} removedIds={removedIds} canCreateRelease={canCreateRelease} onSearch={setOrderSearch} onNew={() => setView('branches')} onOpenRelease={onOpenRelease} onRemove={removeFromBatch} onRecords={() => setView('records')} onRefresh={onRefresh} loading={loading} />}
        {view === 'records' && <RecordsView records={filteredRecords} allCount={records.length} search="" filter={recordFilter} onSearch={() => {}} onFilter={setRecordFilter} onOpen={(record) => onOpenRelease?.(record.release, record.environment.key)} />}
      </main>
    </div>
  </div>
}

function BranchSelectionPage({ project, branches, releases, removedIds, search, loading, canCreateRelease, onSearch, onCreate, onOpenRelease, onBack }) {
  return <>
    <div className="release-v4-heading-row"><div className="release-v4-heading"><h1>新建发布单</h1></div><button className="release-v4-button" type="button" onClick={onBack}>返回发布单</button></div>
    <section className="release-v4-branch-page">
      <div className="release-v4-page-section-head"><div><h2>可发布分支</h2><span>选择分支，发布时会读取它的最新提交</span></div><label className="release-v4-search"><SearchOutlined /><input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索分支、提交人或提交编号" /></label></div>
      <div className="release-v4-branch-page-list">{branches.length ? branches.map((item) => { const existingRelease = releases.find((release) => release.branch === item.name && !removedIds.has(release.id)); const action = existingRelease ? <button className="release-v4-button" type="button" onClick={() => onOpenRelease?.(existingRelease, 'dev')}><EyeOutlined />进入发布单</button> : <button className="release-v4-button primary" type="button" disabled={!canCreateRelease || loading} onClick={() => onCreate(item.name)}><PlusOutlined />创建发布单</button>; return <article className="release-v4-branch-row" key={item.name}><div className="release-v4-branch-identity"><span className="release-v4-branch-icon"><BranchesOutlined /></span><div className="release-v4-branch-main"><span className="release-v4-field-label">分支</span><div className="release-v4-branch-name-line"><strong>{item.name}</strong>{existingRelease && <em>当前批次</em>}</div></div><div className="release-v4-branch-commit"><span className="release-v4-field-label">最新提交</span><code>{branchHead(item)}</code></div><div className="release-v4-branch-message"><span className="release-v4-field-label">提交说明</span><p>{item.message || item.head?.message || '暂无提交说明'}</p></div></div>{action}</article> }) : <div className="release-v4-empty">没有找到匹配的分支</div>}</div>
    </section>
  </>
}

function ReleaseOrdersPage({ project, releases, environments, search, removedIds, canCreateRelease, onSearch, onNew, onOpenRelease, onRemove, onRecords, onRefresh, loading }) {
  return <>
    <div className="release-v4-heading-row"><div className="release-v4-heading"><h1>发布单</h1><p>{project.name} · 每条发布单独立推进 DEV、UAT、PRE、PROD</p></div><div className="release-v4-heading-actions"><button className="release-v4-button" type="button" onClick={onRecords}>发布记录</button><button className="release-v4-button" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />刷新</button><button className="release-v4-button primary" type="button" disabled={!canCreateRelease} onClick={onNew}><PlusOutlined />新建发布单</button></div></div>
    <section className="release-v4-orders-page">
      <div className="release-v4-page-section-head"><div><h2>全部发布单 <span>{releases.length}</span></h2><span>点击环境查看该环境的日志、Pod 和流量</span></div><label className="release-v4-search"><SearchOutlined /><input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索发布单、分支、发布人或 SHA" /></label></div>
      <div className="release-v4-order-list">{releases.length ? releases.map((release) => <ReleaseOrderCard key={release.id} release={release} environments={environments} removed={removedIds.has(release.id)} onOpenRelease={onOpenRelease} onRemove={onRemove} />) : <div className="release-v4-orders-empty"><strong>还没有发布单</strong><span>选择一个分支，创建第一条发布单</span><button className="release-v4-button primary" type="button" disabled={!canCreateRelease} onClick={onNew}><PlusOutlined />选择分支</button></div>}</div>
    </section>
  </>
}

function ReleaseOrderCard({ release, environments, removed, onOpenRelease, onRemove }) {
  const status = release.status || 'draft'
  const environmentStates = environments.map((environment) => ({ environment, state: targetStatus(release, environment) }))
  const doneCount = environmentStates.filter(({ state }) => state.state === 'done').length
  const percentage = Math.round((doneCount / Math.max(1, environments.length)) * 100)
  const activeState = environmentStates.find(({ state }) => state.state === 'active')
  const failedState = environmentStates.find(({ state }) => state.state === 'failed')
  const derivedLabel = removed ? '已移出批次' : failedState ? `${failedState.environment.label} 失败` : activeState ? `${activeState.environment.label} ${activeState.state === 'active' ? '验证中' : '进行中'}` : doneCount === environments.length ? '全部完成' : doneCount > 0 ? `${environments[doneCount]?.label || '下一环境'} 待发布` : STATUS_TEXT[status] || '待发布'
  const derivedClass = failedState ? 'danger' : activeState || (doneCount < environments.length && doneCount > 0) ? 'warn' : removed || derivedLabel === '待发布' ? 'muted' : ''
  return <article className={`release-v4-order-card ${removed ? 'removed' : ''}`}><header><div className="release-v4-order-primary"><span className={`release-v4-avatar ${status === 'succeeded' ? 'gold' : ''}`}>{releaseOwner(release).slice(0, 1)}</span><div><div className="release-v4-order-id"><strong>{release.id}</strong>{removed && <span>已移出批次</span>}</div><h2>{release.branch || '未命名分支'}</h2><p>{releaseOwner(release)} · 批次 {batchIdOf(release)}</p></div></div><span className={`release-v4-order-status ${derivedClass}`}><i />{derivedLabel}</span></header><div className="release-v4-order-version"><span>本次发布版本</span><code>{commitOf(release)}</code><small>{commitMessage(release)}</small></div><div className="release-v4-order-environments">{environmentStates.map(({ environment, state }) => <button type="button" key={environment.key} className={`release-v4-order-environment ${state.state}`} onClick={() => onOpenRelease?.(release, environment.key)}><span><i />{environment.label}</span><strong>{state.label}</strong></button>)}</div><footer><span>{percentage}% 完成 · {formatTime(release.created_at)}</span><div><button className="release-v4-text-action" type="button" onClick={() => onOpenRelease?.(release, 'dev')}><EyeOutlined />查看发布单详情</button>{!removed && <button className="release-v4-text-action danger" type="button" onClick={() => onRemove(release)}><DeleteOutlined />移出批次</button>}</div></footer></article>
}

function BatchView({ currentBatch, batches, batchItems, environments, currentHead, onSelectBatch, onSelectItem, onSelectEnv, onAddBranch, onRemove, onOpenRelease }) {
  return <>
    <div className="release-v4-rulebar"><div><BranchesOutlined /><strong>分支发布</strong><span>批次里保留分支；每次发布环境，都重新读取分支当前版本。</span></div><b>发布项 = 分支 · 记录 = 快照</b></div>
    <div className="release-v4-batch-layout release-v4-list-only">
      <aside className="release-v4-batch-nav"><div className="release-v4-section-head"><h2>发布批次</h2><span>{batches.length} 个</span></div><div className="release-v4-batch-list">{batches.map((batch) => <button key={batch.id} className={`release-v4-batch-item ${batch.id === currentBatch.id ? 'active' : ''}`} type="button" onClick={() => onSelectBatch(batch.id)}><i className={batch.open ? '' : 'closed'} /><span><strong>{batch.id}</strong><small>{batch.open ? '开放中' : '已关闭'} · {batch.items.length} 个分支</small></span><time>{batch.time}</time></button>)}</div><div className="release-v4-relation"><div className="release-v4-section-head"><h3>批次结构</h3><span>基于 {currentBatch.base}</span></div><div className="release-v4-relation-line"><div className="release-v4-relation-node"><BranchesOutlined /><span><strong>{currentBatch.base}</strong><small>{currentHead} · 稳定基准</small></span></div><ArrowDownOutlined /><div className="release-v4-relation-node batch"><BranchesOutlined /><span><strong>{currentBatch.id}</strong><small>{currentBatch.branch}</small></span></div></div></div><button className="release-v4-add" type="button" onClick={onAddBranch}><PlusOutlined />加入一个分支</button></aside>
      <section className="release-v4-members"><div className="release-v4-members-head"><h2>{currentBatch.id} · 发布项</h2><span>{batchItems.length} 个分支</span></div><div className="release-v4-member-list">{batchItems.length ? batchItems.map((release) => <MemberCard key={release.id} release={release} environments={environments} onSelect={() => onSelectItem(release, 'dev')} onSelectEnv={(environment) => onSelectEnv(release, environment.key)} onRemove={onRemove} onOpenRelease={onOpenRelease} />) : <div className="release-v4-empty">批次里还没有分支</div>}</div></section>
    </div>
  </>
}

function MemberCard({ release, environments, onSelect, onSelectEnv, onRemove, onOpenRelease }) {
  const status = release.status || 'draft'
  const doneCount = environments.filter((environment) => targetStatus(release, environment).state === 'done').length
  const percentage = Math.round((doneCount / Math.max(1, environments.length)) * 100)
  const openDetail = onOpenRelease ? () => onOpenRelease(release, 'dev') : onSelect
  return <article className="release-v4-member"><div className="release-v4-member-head"><button className="release-v4-member-main" type="button" onClick={openDetail}><span className={`release-v4-avatar ${status === 'succeeded' ? 'gold' : ''}`}>{releaseOwner(release).slice(0, 1)}</span><span><strong>{release.branch || '未命名分支'}</strong><small>{releaseOwner(release)} · {release.id}</small></span></button><span className="release-v4-member-status"><i className={STATUS_CLASS[status] || ''} />{STATUS_TEXT[status] || '进行中'}</span></div><div className="release-v4-branch-line"><CodeOutlined /><span>本次发布版本</span><code>{commitOf(release)}</code><span>· {commitMessage(release)}</span></div><div className="release-v4-stages">{environments.map((environment) => { const state = targetStatus(release, environment); return <button className={`release-v4-stage ${state.state}`} key={environment.key} type="button" onClick={() => onOpenRelease ? onOpenRelease(release, environment.key) : onSelectEnv(environment)}><i /><strong>{environment.label}</strong><small>{state.state === 'done' ? commitOf(release) : state.label}</small></button> })}</div><div className="release-v4-member-foot"><span>{percentage}% 完成 · {batchIdOf(release)}</span><div><button className="release-v4-text-action" type="button" onClick={openDetail}><EyeOutlined />查看发布单详情</button><button className="release-v4-text-action danger" type="button" onClick={() => onRemove(release)}><DeleteOutlined />移出批次</button></div></div></article>
}

function ExecutionPanel({ project, release, selectedEnv, environments, onSelectEnv, onPublish, onRefresh, loading, onRetryTarget, retryingTarget, canPublishRelease }) {
  const environment = environments.find((item) => item.key === selectedEnv) || environments[0]
  const status = targetStatus(release, environment)
  const snapshot = status.target ? commitOf(release) : ''
  const logs = makeLogs(project, release, environment, status)
  const canAction = canPublishRelease && !['queued', 'running'].includes(release.status)
  const actionLabel = status.state === 'failed' ? `重试 ${environment.label}` : status.state === 'pending' ? `发布 ${environment.label}` : `重新发布 ${environment.label}`
  const action = status.state === 'failed' && onRetryTarget ? () => onRetryTarget(release, status.target) : () => onPublish(release, environment)
  return <aside className="release-v4-execution"><div className="release-v4-execution-head"><div><h2>{release.branch || '未命名分支'}</h2><span>{releaseOwner(release)} · {release.id} · 当前查看 <code>{environment.label}</code></span></div><span className="release-v4-execution-state"><i className={STATUS_CLASS[release.status] || ''} />{STATUS_TEXT[release.status] || '进行中'}</span></div><div className="release-v4-snapshot"><div><strong>{environment.label} 代码快照</strong><span>点击环境时重新读取分支</span></div><div className="release-v4-snapshot-grid"><div><label>当前环境运行</label><code>{snapshot || '—'}</code><small>{snapshot ? '这次部署实际使用的提交' : '还没有部署'}</small></div><div className="latest"><label>分支最新提交</label><code>{commitOf(release)}</code><small>{snapshot && snapshot === commitOf(release) ? '与当前环境一致' : '发布时会重新读取'}</small></div></div></div><div className="release-v4-env-tabs">{environments.map((item) => { const itemStatus = targetStatus(release, item); return <button key={item.key} className={item.key === selectedEnv ? 'active' : ''} type="button" onClick={() => onSelectEnv(item)}><strong>{item.label}</strong><span>{itemStatus.state === 'done' ? commitOf(release) : itemStatus.label}</span></button> })}</div><div className="release-v4-env-action"><div><strong>{environment.label} · {status.state === 'active' ? '发布中' : status.state === 'done' ? '当前版本' : status.state === 'failed' ? '发布失败' : '等待发布'}</strong><span>{status.target ? `Pod 使用 ${commitOf(release)}` : `将读取 ${release.branch || '当前分支'} 的最新提交`}</span></div><button className="release-v4-button" type="button" disabled={!canAction || loading} onClick={action}>{status.state === 'failed' ? <UndoOutlined /> : status.state === 'pending' ? <PlayCircleOutlined /> : <ReloadOutlined />}{actionLabel}</button></div><div className="release-v4-flow">{['ttp', 'git', 'build', 'k8s', 'pod'].map((item, index) => <div className={`release-v4-flow-step ${index < 3 && status.state !== 'pending' ? 'done' : index === 3 && status.state === 'active' ? 'active' : ''}`} key={item}><span><IconStep type={item} /></span><small>{item === 'ttp' ? 'TTP' : item === 'git' ? 'Git' : item === 'build' ? '镜像' : item === 'k8s' ? 'K8s' : 'Pod'}</small></div>)}</div><div className="release-v4-log-head"><div><h3>{environment.label} 执行日志</h3><span>{logs.length} 条</span></div><span>TTP · Git · K8s · Pod</span></div><ReleaseLogTerminal logs={logs} emptyText="尚未产生执行输出" /><div className="release-v4-log-summary"><span>构建产物 <code>{project.name}:{commitOf(release)}</code></span><span>{status.target ? `Pod 快照 ${commitOf(release)}` : '等待第一次执行'}</span></div>{onRefresh && <button className="release-v4-refresh-corner" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />{loading ? '刷新中' : '刷新'}</button>}</aside>
}

function RecordsView({ records, allCount, search, filter, onSearch, onFilter, onOpen }) {
  return <section className="release-v4-records"><div className="release-v4-records-head"><div><h2>发布记录</h2><p>每次环境发布都是固定快照，历史不会被覆盖</p></div><div className="release-v4-record-tools"><label><SearchOutlined /><input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索发布项、分支或 commit" /></label><select value={filter} onChange={(event) => onFilter(event.target.value)}><option value="all">全部</option><option value="active">进行中</option><option value="success">已完成</option><option value="failed">失败</option></select><span><LockOutlined />只读</span></div></div><div className="release-v4-record-table"><div className="release-v4-record-header"><span>发布记录</span><span>实际代码快照</span><span>环境</span><span>发布人 / 分支</span><span>结果</span><span>时间</span></div>{records.length ? records.map((record) => <button className="release-v4-record-row" type="button" key={record.id} onClick={() => onOpen(record)}><span className="release-v4-record-primary"><span className="release-v4-avatar">{releaseOwner(record.release).slice(0, 1)}</span><span><strong>{record.id}</strong><small>{batchIdOf(record.release)}</small></span></span><span><code>{record.sha}</code><small>{record.message}</small></span><span><strong><i className={record.result === '进行中' ? 'warn' : record.result === '失败' ? 'danger' : ''} />{record.environment.label}</strong><small>部署快照</small></span><span>{releaseOwner(record.release)} · {record.release.branch || '-'}</span><span className={`release-v4-record-result ${record.result === '进行中' ? 'pending' : record.result === '已移出批次' ? 'moved' : ''}`}><CheckCircleOutlined />{record.result}</span><time>{record.time}</time></button>) : <div className="release-v4-empty">没有匹配的发布记录</div>}</div><div className="release-v4-record-foot">共 {allCount} 条记录。这里只能查看详情，不能取消、移出或重新发布。</div></section>
}

function Overlay({ overlay, project, branch, branches, environments, onClose, onChooseBranch, onConfirmPublish, onConfirmRemove }) {
  if (overlay.type === 'record-detail') return <RecordDrawer record={overlay.record} project={project} environments={environments} onClose={onClose} />
  const release = overlay.release
  if (overlay.type === 'branch-picker') return <div className="release-v4-overlay" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><section className="release-v4-dialog"><header><div><h2>选择发布分支</h2><p>发布时会读取所选分支的最新提交</p></div><button type="button" onClick={onClose}><CloseOutlined /></button></header><div className="release-v4-dialog-body"><div className="release-v4-branch-list">{branches.length ? branches.map((item) => <div className="release-v4-branch-option" key={item.name}><span><strong>{item.name}</strong><small>最新提交 <code>{branchHead(item)}</code> · {item.message || item.head?.message || '暂无提交说明'}</small></span><button className="release-v4-button primary" type="button" onClick={() => onChooseBranch(item.name)}>使用这个分支</button></div>) : <div className="release-v4-empty">当前没有读取到分支</div>}</div></div><footer><button className="release-v4-button" type="button" onClick={onClose}>取消</button></footer></section></div>
  if (!release) return null
  if (overlay.type === 'remove') return <div className="release-v4-overlay" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><section className="release-v4-dialog"><header><div><h2>从批次移出这个分支？</h2><p>{release.branch || '未命名分支'} · {releaseOwner(release)}</p></div><button type="button" onClick={onClose}><CloseOutlined /></button></header><div className="release-v4-dialog-body"><div className="release-v4-dialog-note danger"><strong>只解除批次关系</strong><span>历史发布记录和已经部署的 Pod 都保留。</span></div></div><footer><button className="release-v4-button" type="button" onClick={onClose}>取消</button><button className="release-v4-button primary" type="button" onClick={onConfirmRemove}>确认移出</button></footer></section></div>
  const environment = overlay.environment
  const status = targetStatus(release, environment)
  const title = status.state === 'failed' ? '重试' : status.state === 'pending' ? '发布' : '重新发布'
  return <div className="release-v4-overlay" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><section className="release-v4-dialog"><header><div><h2>{title} {environment.label}？</h2><p>{release.branch || branch || '当前分支'} · 版本 {commitOf(release)}</p></div><button type="button" onClick={onClose}><CloseOutlined /></button></header><div className="release-v4-dialog-body"><div className="release-v4-dialog-note"><strong>发布时重新读取分支</strong><span>执行 Git、构建镜像和 K8s 部署，新增一条发布记录。</span></div><div className="release-v4-dialog-facts"><span>环境<strong>{environment.label}</strong></span><span>版本<strong>{commitOf(release)}</strong></span><span>命名空间<strong>{status.target?.namespace || '按环境配置'}</strong></span></div></div><footer><button className="release-v4-button" type="button" onClick={onClose}>取消</button><button className="release-v4-button primary" type="button" onClick={onConfirmPublish}>确认{title}</button></footer></section></div>
}

function RecordDrawer({ record, project, environments, onClose }) {
  const logs = makeLogs(project, record.release, record.environment, record.status)
  return <div className="release-v4-overlay" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><aside className="release-v4-drawer"><header><div><small>发布记录 · 只读</small><h2>{record.id}</h2><span><LockOutlined />历史记录不可修改</span></div><button type="button" onClick={onClose}><CloseOutlined /></button></header><div className="release-v4-drawer-body"><div className="release-v4-drawer-owner"><span className="release-v4-avatar">{releaseOwner(record.release).slice(0, 1)}</span><div><strong>{releaseOwner(record.release)} · {record.release.branch || '-'}</strong><small>{record.message} · {record.time}</small></div></div><div className="release-v4-detail-grid"><div><label>实际代码快照</label><code>{record.sha}</code></div><div><label>发布环境</label><strong>{record.environment.label}</strong></div><div><label>所属批次</label><strong>{batchIdOf(record.release)}</strong></div><div><label>发布结果</label><strong>{record.result}</strong></div></div><div className="release-v4-drawer-section"><h3>环境快照</h3><span>只读</span></div><div className="release-v4-record-snapshot-list">{environments.map((environment) => { const state = targetStatus(record.release, environment); return <div key={environment.key}><strong>{environment.label}</strong><code>{state.target ? record.sha : '—'}</code><span>{state.target ? state.label : '未开始'}</span></div> })}</div><div className="release-v4-drawer-section"><h3>执行日志</h3><span>{record.environment.label} · {logs.length} 行</span></div><ReleaseLogTerminal logs={logs} emptyText="这条记录没有执行输出" /></div><footer><button className="release-v4-button" type="button" onClick={onClose}>关闭</button></footer></aside></div>
}
