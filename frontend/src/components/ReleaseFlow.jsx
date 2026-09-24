import {
  ArrowLeftOutlined,
  ArrowRightOutlined,
  BranchesOutlined,
  CloudUploadOutlined,
  CloseOutlined,
  CodeOutlined,
  DashboardOutlined,
  DeleteOutlined,
  DownOutlined,
  EnvironmentOutlined,
  EyeOutlined,
  FileTextOutlined,
  HistoryOutlined,
  LockOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SearchOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { message, Pagination } from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { normalizeExecutionLogs, ReleaseLogTerminal } from './ReleaseLogs'
import { getReleaseTargetLogs } from '../services/api'
import { executionStageFromLogs } from '../services/release-log-summary'
import {
  batchIdOf,
  batchBranchOf,
  commitMessage,
  commitOf,
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

function environmentStage(environment) {
  const target = environmentTarget(environment)
  return stageOf(target?.environment_stage || target?.stage || target?.environment || target?.name || environment?.label || environment?.name)
}

function resolveEnvironment(value, environments) {
  if (!value) return environments[0] || null
  const raw = typeof value === 'object'
    ? value.key || value.id || value.target?.id || value.label || value.name
    : value
  const text = `${raw || ''}`.trim().toLowerCase()
  return environments.find((item) => {
    const target = environmentTarget(item)
    return item.key === raw
      || target?.id === raw
      || `${item.name || ''}`.trim().toLowerCase() === text
      || `${item.label || ''}`.trim().toLowerCase() === text
      || (stageOf(text) && environmentStage(item) === stageOf(text))
  }) || environments[0] || null
}

function strategyOf(release, environment) {
  // A release keeps the strategy that was captured when it was created.
  // Environment settings may change later, but must not rewrite the meaning
  // of an existing release or expose controls its runtime never created.
  return release?.plan?.strategy || release?.strategy || environment?.target?.deploy_strategy || 'rolling'
}

function strategyLabel(value) {
  return STRATEGIES.find((item) => item.key === value)?.label || '滚动'
}

function resourceQuotaLabel(quota) {
  const parts = [
    quota?.cpu_limit ? `CPU ${quota.cpu_limit}` : '',
    quota?.memory_limit ? `内存 ${quota.memory_limit}` : '',
    Number(quota?.pods) > 0 ? `Pod ${quota.pods}` : '',
  ].filter(Boolean)
  return parts.join(' · ') || '由空间配额管理'
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

function PageFrame({ project, page, children, onBack, embedded = false }) {
  const pageTitles = { branches: '发布', create: '新建发布单', orders: '发布单列表', workspace: '发布工作区' }
  return <div className={`release-flow-page${embedded ? ' is-embedded' : ''}`}>
    {!embedded && <div className="release-flow-page-top"><div className="release-flow-breadcrumb"><button type="button" onClick={onBack}>项目</button><ArrowRightOutlined /><strong>{project.name}</strong><ArrowRightOutlined /><span>{pageTitles[page] || '发布'}</span></div><button className="release-flow-back" type="button" onClick={onBack}><ArrowLeftOutlined />返回项目</button></div>}
    {children}
  </div>
}

function FlowUnavailablePage({ title, message, onGoOrders }) {
  return <section className="release-flow-unavailable"><div className="release-flow-unavailable-icon">!</div><h1>{title}</h1><p>{message}</p><button className="release-flow-button primary" type="button" onClick={onGoOrders}><ArrowLeftOutlined />返回发布单列表</button></section>
}

function BranchPickerModal({ open, branches, releases, releaseFlow, search, onSearch, onClose, onChoose, onOpenRelease, canCreateRelease, branchLoading }) {
  if (!open) return null
  return <div className="release-flow-modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose?.()}>
    <section className="release-flow-modal release-flow-branch-modal" role="dialog" aria-modal="true" aria-labelledby="release-branch-picker-title">
      <header className="release-flow-modal-head">
        <div>
          <h2 id="release-branch-picker-title">选择发布分支</h2>
        </div>
        <button className="release-flow-modal-close" type="button" onClick={onClose} aria-label="关闭"><CloseOutlined /></button>
      </header>
      <div className="release-flow-modal-body">
        <div className="release-flow-branch-search"><SearchOutlined /><input autoFocus value={search} onChange={(event) => onSearch(event.target.value)} placeholder="搜索分支、提交说明或提交人" /></div>
        {branchLoading
          ? <div className="release-flow-empty">正在读取分支...</div>
          : branches.length
            ? <div className="release-flow-modal-branch-list">{branches.map((item) => {
              const existing = releaseFlow?.current_release || releases.find((release) => release.id === releaseFlow?.current_release_id)
              const active = releaseFlow?.participants?.some((participant) => participant.active !== false && participant.branch?.toLowerCase() === item.name?.toLowerCase())
              return <div className="release-flow-modal-branch-row" key={item.name}>
                <span className="release-flow-branch-icon"><BranchesOutlined /></span>
                <div className="release-flow-modal-branch-main">
                  <strong>{item.name}</strong>
                  <span><code>{branchVersion(item)}</code>{branchMessage(item)}</span>
                </div>
                {active
                  ? <button className="release-flow-button" type="button" onClick={() => existing && onOpenRelease?.(existing, 'dev')} disabled={!existing}><EyeOutlined />进入当前流程</button>
                  : <button className="release-flow-button primary" type="button" onClick={() => onChoose?.(item)} disabled={!canCreateRelease}><CloudUploadOutlined />选择此分支</button>}
              </div>
            })}</div>
            : <div className="release-flow-empty">没有找到匹配的分支</div>}
      </div>
    </section>
  </div>
}

function releaseStatusLabel(release) {
  return ({
    draft: '草稿',
    queued: '排队中',
    running: '发布中',
    succeeded: '已完成',
    failed: '失败',
    cancelled: '已取消',
  })[release?.status] || '未知状态'
}

function RemoveReleaseModal({ release, environments, open, submitting, onClose, onConfirm }) {
  if (!open || !release) return null
  const publishedEnvironments = (release.targets || [])
    .filter((target) => target.status === 'succeeded')
    .map((target) => target.name || target.environment_stage || target.environment)
    .filter(Boolean)
  const active = release.status === 'queued' || release.status === 'running'
  const draft = release.status === 'draft'
  const replacement = !draft
  return <div className="release-flow-modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && !submitting && onClose?.()}>
    <section className="release-flow-modal release-flow-remove-modal" role="dialog" aria-modal="true" aria-labelledby="release-remove-title">
      <header className="release-flow-modal-head">
        <div className="release-flow-remove-title"><span className="release-flow-remove-icon"><WarningOutlined /></span><div><h2 id="release-remove-title">{draft ? '移除草稿' : '移除发布单'}</h2><p>{draft ? '请确认是否从当前发布流程中移除这条草稿。' : '将该分支移出当前发布流程，并生成新的发布快照。'}</p></div></div>
        <button className="release-flow-modal-close" type="button" onClick={onClose} disabled={submitting} aria-label="关闭"><CloseOutlined /></button>
      </header>
      <div className="release-flow-modal-body release-flow-remove-body">
        <div className="release-flow-remove-summary">
          <div><span>发布单</span><strong>{release.id}</strong></div>
          <div><span>分支</span><strong>{release.branch || '未命名分支'}</strong></div>
          <div><span>状态</span><strong>{releaseStatusLabel(release)}</strong></div>
          <div><span>已发布环境</span><strong>{publishedEnvironments.length ? publishedEnvironments.join('、') : '无'}</strong></div>
        </div>
        <div className="release-flow-remove-notice">
          <strong>{draft ? '草稿将被移除' : '将分支移出当前发布流程'}</strong>
          <p>{draft ? '这不会删除代码仓库中的开发分支。' : '系统会基于当前流程剩余的参与分支创建新的不可变发布单并重新部署。Git 分支不会删除，旧发布单会保留在历史中；只有新发布成功后，当前流程才会切换到新发布单。'}</p>
        </div>
        {active && <div className="release-flow-remove-warning">这条发布单正在执行，必须先取消发布后才能操作。</div>}
      </div>
      <footer className="release-flow-modal-foot">
        <button className="release-flow-button" type="button" onClick={onClose} disabled={submitting}>取消</button>
        <button className="release-flow-button danger" type="button" onClick={onConfirm} disabled={submitting || active}><DeleteOutlined />{submitting ? '处理中...' : replacement ? '移除发布单' : '确认移除'}</button>
      </footer>
    </section>
  </div>
}

function CreateReleasePage({ branch, project, environments, submitting, onBack, onOpenTargets, onSubmit, canCreateRelease }) {
  const releaseEnvironments = (environments || []).filter(Boolean)
  const hasReleaseEnvironment = releaseEnvironments.length > 0
  return <>
    <header className="release-flow-heading release-flow-heading-no-title"><div><p>确认发布来源后提交，发布单会进入发布单列表。</p></div></header>
    <div className="release-flow-create-grid">
      <section className="release-flow-section release-flow-card"><div className="release-flow-section-head"><div><h2>发布来源</h2><p>本次发布使用分支当前版本。</p></div></div><div className="release-flow-source-grid"><div><span className="release-flow-label">发布分支</span><strong>{branch?.name || '-'}</strong></div><div><span className="release-flow-label">当前版本</span><code>{branchVersion(branch)}</code></div><div><span className="release-flow-label">最近提交</span><strong>{branchMessage(branch)}</strong></div><div><span className="release-flow-label">提交人</span><strong>{branch?.author || branch?.owner || '仓库提交人'}</strong></div></div><div className="release-flow-environment-route"><div className="release-flow-route-head"><strong>发布环境</strong><span>{releaseEnvironments.length} 个，按顺序推进</span></div><div className="release-flow-environment-sequence">{hasReleaseEnvironment ? releaseEnvironments.map((environment, index) => { const target = environmentTarget(environment); const displayName = target?.name || environment.name || environment.label; const stage = target?.environment_stage || target?.stage || target?.environment || environment.label; const destination = target ? `${target.cluster_id || '未配置集群'} / ${target.namespace || '未配置命名空间'}` : '未配置发布目标'; return <div className="release-flow-environment-sequence-row" key={environment.key || target?.id || displayName}><span className="release-flow-sequence-number">{index + 1}</span><div className="release-flow-sequence-name"><strong>{displayName}</strong><small>{stage}</small></div><span className="release-flow-sequence-target">{destination}</span><span className="release-flow-sequence-state">{index === 0 ? '首个环境' : `等待第 ${index} 个环境完成`}</span></div> }) : <div className="release-flow-sequence-empty">暂无已配置的发布环境</div>}</div></div></section>
      <aside className="release-flow-section release-flow-card release-flow-permission-card"><h2>提交发布单</h2>{hasReleaseEnvironment ? <><p>创建后可在发布单列表中选择并进入发布工作区。</p><div className="release-flow-permission"><div><strong>当前账号</strong><span>可提交</span></div><small>执行发布需要“执行发布”权限。</small></div></> : <div className="release-flow-missing-target"><EnvironmentOutlined /><strong>还没有可用的发布环境</strong><span>部署配置中的资源文件不会自动创建发布环境，请先添加并启用 DEV 环境。</span><button className="release-flow-button" type="button" onClick={onOpenTargets}><EnvironmentOutlined />去配置发布环境</button></div>}</aside>
    </div>
    <div className="release-flow-footer"><button className="release-flow-button primary" type="button" onClick={onSubmit} disabled={submitting || !canCreateRelease || !hasReleaseEnvironment}>{submitting ? '创建中...' : '提交发布单'}</button></div>
  </>
}

function ReleaseListNavigation({ active, onChange, historyCount = 0 }) {
  return <nav className="release-flow-list-navigation" aria-label="发布视图">
    <button className={active === 'current' ? 'active' : ''} type="button" onClick={() => onChange?.('current')}><BranchesOutlined />当前发布流程</button>
    <button className={active === 'history' ? 'active' : ''} type="button" onClick={() => onChange?.('history')}><HistoryOutlined />发布历史{historyCount > 0 && <span>{historyCount}</span>}</button>
  </nav>
}

function CurrentReleaseFlowPage({ flow, releases, environments, historyCount = 0, loading, onViewHistory, onNew, onOpenWorkspace, onRemove, onRefresh, canCreateRelease, canRemove, canRepublish }) {
  const participants = (flow?.participants || []).filter((participant) => participant.active !== false && participant.branch)
  const participantReleases = flow?.participant_releases || releases
  const releaseForBranch = (branch) => participantReleases.find((release) => release.branch?.toLowerCase() === branch.toLowerCase())
  const [branchSearch, setBranchSearch] = useState('')
  const [branchPage, setBranchPage] = useState(1)
  const branchPageSize = 5
  const filteredParticipants = useMemo(() => {
    const term = branchSearch.trim().toLowerCase()
    if (!term) return participants
    return participants.filter((participant) => {
      const branch = participant.branch || ''
      const head = participant.head || {}
      const release = releaseForBranch(branch)
      return `${branch} ${head.message || ''} ${head.sha || ''} ${head.short_sha || ''} ${release?.id || ''} ${release ? releaseOwner(release) : ''} ${release ? commitMessage(release) : ''}`.toLowerCase().includes(term)
    })
  }, [branchSearch, participants, participantReleases])
  const branchPageCount = Math.max(1, Math.ceil(filteredParticipants.length / branchPageSize))
  const visibleParticipants = filteredParticipants.slice((branchPage - 1) * branchPageSize, branchPage * branchPageSize)
  useEffect(() => {
    setBranchPage(1)
  }, [branchSearch])
  useEffect(() => {
    if (branchPage > branchPageCount) setBranchPage(branchPageCount)
  }, [branchPage, branchPageCount])
  const initialLoading = loading && !flow
  return <>
    <ReleaseListNavigation active="current" onChange={(value) => value === 'history' && onViewHistory?.()} historyCount={historyCount} />
    {initialLoading
      ? <section className="release-flow-section release-flow-orders-section"><div className="release-flow-orders-loading" role="status"><span className="release-flow-loading-spinner" aria-hidden="true" />正在加载当前发布流程...</div></section>
      : !flow
        ? <section className="release-flow-section release-flow-current-list-section release-flow-current-list-empty">
          <div className="release-flow-current-list-toolbar">
            <div className="release-flow-current-list-toolbar-actions">
              <button className="release-flow-button" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />刷新</button>
              <button className="release-flow-button primary" type="button" onClick={onNew} disabled={!canCreateRelease}><CloudUploadOutlined />创建发布单</button>
            </div>
          </div>
          <div className="release-flow-orders-empty"><div className="release-flow-orders-empty-icon" aria-hidden="true"><BranchesOutlined /></div><strong>当前还没有发布流程</strong><span>选择一个分支创建发布单后，这里会展示当前参与发布的分支和生效版本。</span></div>
        </section>
        : <section className="release-flow-section release-flow-current-list-section">
          <div className="release-flow-current-list-toolbar">
            <div className="release-flow-current-list-toolbar-actions">
              <label className="release-flow-current-branch-search"><SearchOutlined /><input value={branchSearch} onChange={(event) => setBranchSearch(event.target.value)} placeholder="搜索分支、提交说明或发布单" aria-label="搜索当前发布流程" /></label>
              <button className="release-flow-button" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />刷新</button>
              <button className="release-flow-button primary" type="button" onClick={onNew} disabled={!canCreateRelease}><CloudUploadOutlined />创建发布单</button>
            </div>
          </div>
          <div className="release-flow-current-card-list">{visibleParticipants.length ? visibleParticipants.map((participant) => {
              const branch = participant.branch
              const branchRelease = releaseForBranch(branch)
              const isBase = branch.toLowerCase() === `${flow.base_branch || ''}`.toLowerCase()
              const environmentStates = environments.map((environment) => ({ environment, state: branchRelease ? targetStatus(branchRelease, environment) : { state: 'pending', label: '未开放', target: null } }))
              const doneCount = environmentStates.filter(({ state }) => state.state === 'done').length
              const failed = environmentStates.find(({ state }) => state.state === 'failed')
              const active = environmentStates.find(({ state }) => state.state === 'active')
              const status = failed
                ? { label: `${failed.environment.label} 失败`, className: 'failed' }
                : active
                  ? { label: `${active.environment.label} 发布中`, className: 'active' }
                  : branchRelease?.status === 'succeeded'
                    ? { label: '已完成', className: 'done' }
                    : { label: '待发布', className: 'pending' }
              const hasDeployedVersion = doneCount > 0 && branchRelease
              const deployedCommit = hasDeployedVersion ? targetCommit(environmentStates.find(({ state }) => state.state === 'done')?.state.target, branchRelease) : ''
              const canRemoveBranch = Boolean(branchRelease) && !isBase && (branchRelease.status === 'draft' ? canRemove : canRepublish)
              const releaseDate = branchRelease?.created_at ? new Date(branchRelease.created_at) : null
              const releaseDateLabel = releaseDate && !Number.isNaN(releaseDate.getTime())
                ? `${releaseDate.getFullYear()}-${String(releaseDate.getMonth() + 1).padStart(2, '0')}-${String(releaseDate.getDate()).padStart(2, '0')} ${String(releaseDate.getHours()).padStart(2, '0')}:${String(releaseDate.getMinutes()).padStart(2, '0')}`
                : '-'
              return <article className="release-flow-current-card" key={branch}>
                <header className="release-flow-current-card-head">
                  <div className="release-flow-current-card-branch"><strong>{branch}</strong></div>
                  <span className={`release-flow-current-card-status ${status.className}`}><i />{status.label}</span>
                </header>
                <div className="release-flow-current-card-body">
                  <div className="release-flow-current-card-details">
                    <p className="release-flow-current-card-meta">当前发布单 · 由 {branchRelease ? releaseOwner(branchRelease) : '—'} 创建 · {releaseDateLabel}</p>
                    <div className="release-flow-current-card-main-row">
                      <div className="release-flow-current-card-version">
                        <div><span>当前生效版本</span><strong>{hasDeployedVersion ? deployedCommit : '暂无已部署版本'}</strong></div>
                        <ArrowRightOutlined aria-hidden="true" />
                        <div><span>发布单快照</span><strong>{branchRelease ? `${branchRelease.id} · ${commitOf(branchRelease)}` : '尚未创建发布单'}</strong></div>
                      </div>
                      <div className="release-flow-current-card-environments">
                        {environmentStates.map(({ environment, state }) => <span className={`release-flow-current-card-environment ${state.state}`} key={environment.key}>{environment.label} · {state.state === 'pending' ? (state.target ? '待发布' : '未开放') : state.label}</span>)}
                      </div>
                    </div>
                  </div>
                  <aside className="release-flow-current-card-action-panel" aria-label="发布单操作">
                    <div className="release-flow-current-card-actions">
                      <button className="release-flow-button" type="button" disabled={!branchRelease} onClick={() => branchRelease && onOpenWorkspace?.(branchRelease, environments[0]?.key || 'dev')}><EyeOutlined />进入发布工作区</button>
                      <button className="release-flow-button danger" type="button" disabled={!canRemoveBranch} title={isBase ? '基础分支不可移出当前流程' : canRemoveBranch ? '从当前发布流程移出该分支' : '当前账号没有移出该分支的权限'} onClick={() => branchRelease && canRemoveBranch && onRemove?.(branchRelease)}><DeleteOutlined />移出当前流程</button>
                    </div>
                  </aside>
                </div>
              </article>
            }) : <div className="release-flow-empty">{branchSearch ? '没有找到匹配的参与分支' : '当前流程没有参与分支'}</div>}</div>
          {filteredParticipants.length > 0 && <div className="release-flow-pagination"><Pagination current={branchPage} pageSize={branchPageSize} total={filteredParticipants.length} showSizeChanger={false} showTotal={(count) => `共 ${count} 个分支`} onChange={setBranchPage} /></div>}
        </section>}
  </>
}

function ReleaseOrderListPage({ releases, environments, removed, page, pageSize, total, search = '', onPageChange, onSearch, onOpenWorkspace, onRefresh, loading, onViewCurrent }) {
  const [searchDraft, setSearchDraft] = useState(search)
  const hasSearch = search.trim().length > 0
  useEffect(() => {
    setSearchDraft(search)
  }, [search])
  const submitSearch = (event) => {
    event.preventDefault()
    onSearch?.(searchDraft)
  }
  const clearSearch = () => {
    setSearchDraft('')
    onSearch?.('')
  }
  const initialLoading = loading && releases.length === 0 && total === 0
  return <>
    <ReleaseListNavigation active="history" onChange={(value) => value === 'current' && onViewCurrent?.()} historyCount={total} />
    <header className="release-flow-heading release-flow-orders-heading release-flow-heading-no-title">
      <form className="release-flow-order-search" role="search" onSubmit={submitSearch}>
        <div className="release-flow-order-search-field">
          <SearchOutlined aria-hidden="true" />
          <input value={searchDraft} onChange={(event) => setSearchDraft(event.target.value)} placeholder="搜索发布单、分支、版本或发布人" aria-label="搜索发布单" />
          {searchDraft && <button type="button" className="release-flow-order-search-clear" aria-label="清空搜索" onClick={clearSearch}><CloseOutlined /></button>}
        </div>
        <button className="release-flow-button" type="submit">搜索</button>
      </form>
      <div className="release-flow-heading-actions">
        <button className="release-flow-button" type="button" onClick={onRefresh} disabled={loading}><ReloadOutlined />刷新</button>
      </div>
    </header>
    <section className="release-flow-section release-flow-orders-section">
      {initialLoading
        ? <div className="release-flow-orders-loading" role="status"><span className="release-flow-loading-spinner" aria-hidden="true" />正在加载发布单...</div>
        : releases.length
        ? <div className="release-flow-order-list">{releases.map((release) => <ReleaseOrderListCard key={release.id} release={release} environments={environments} removed={removed.has(release.id)} onOpen={onOpenWorkspace} />)}</div>
        : <div className="release-flow-orders-empty">
          <div className="release-flow-orders-empty-icon" aria-hidden="true">{hasSearch ? <SearchOutlined /> : <CloudUploadOutlined />}</div>
          <strong>{hasSearch ? '没有匹配的发布单' : '还没有发布单'}</strong>
          <span>{hasSearch ? `没有找到与“${search}”匹配的结果。` : '当前没有历史记录，请到当前发布流程创建发布单。'}</span>
          {hasSearch
            ? <button className="release-flow-button" type="button" onClick={clearSearch}>清除搜索</button>
            : <button className="release-flow-button" type="button" onClick={onViewCurrent}>查看当前发布流程</button>}
        </div>}
      {total > 0 && <div className="release-flow-pagination"><Pagination current={page} pageSize={pageSize} total={total} showSizeChanger={false} showTotal={(count) => `共 ${count} 条`} disabled={loading} onChange={onPageChange} /></div>}
    </section>
  </>
}

function ReleaseOrderListCard({ release, environments, removed, onOpen, onRemove, canRemove, canRepublish }) {
  const environmentStates = environments.map((environment) => ({ environment, state: targetStatus(release, environment) }))
  const doneCount = environmentStates.filter(({ state }) => state.state === 'done').length
  const active = environmentStates.find(({ state }) => state.state === 'active')
  const failed = environmentStates.find(({ state }) => state.state === 'failed')
  const overall = removed
    ? { label: '已移出批次', className: 'muted' }
    : failed
      ? { label: `${failed.environment.label} 失败`, className: 'failed' }
      : active
        ? { label: `${active.environment.label} 发布中`, className: 'active' }
        : doneCount > 0 && doneCount === environments.length
          ? { label: '全部完成', className: 'done' }
          : doneCount > 0
            ? { label: `${environments[doneCount]?.label || '下一环境'} 待发布`, className: 'pending' }
            : { label: '待发布', className: 'pending' }
  const openWorkspace = (environment) => onOpen?.(release, environment?.key || 'dev')
  const activeRelease = release.status === 'queued' || release.status === 'running'
  const draft = release.status === 'draft'
  const replaced = Boolean(release.replaced_by_release_id || release.replacedByReleaseId || release.replacement_state === 'replaced')
  const canAction = draft ? canRemove : canRepublish
  const actionTitle = activeRelease
    ? '运行中的发布单不能移除，请先取消发布'
    : draft
      ? canRemove ? '移除草稿' : '当前账号没有移除发布单的权限'
      : replaced ? '该发布单已被新的发布单替代' : canRepublish ? '移出当前流程并重新发布' : '当前账号没有移除发布单的权限'
  return <article className={`release-flow-order-row ${removed ? 'removed' : ''}`}>
    <div className="release-flow-order-row-main">
      <div className="release-flow-order-id"><strong>{release.id}</strong><span>{formatTime(release.created_at)}</span></div>
      <h3>{release.branch || '未命名分支'}</h3>
      <p>{releaseOwner(release)} · 当前版本 <code>{commitOf(release)}</code></p>
    </div>
    <div className="release-flow-order-row-message">
      <span>提交说明</span>
      <strong>{commitMessage(release)}</strong>
    </div>
    <div className="release-flow-order-row-status">
      <span className={`release-flow-status ${overall.className}`}><i />{overall.label}</span>
      {replaced && <small className="release-flow-order-replaced">已被 {release.replaced_by_release_id || release.replacedByReleaseId} 替代</small>}
      <small>{doneCount} / {Math.max(1, environments.length)} 个环境完成</small>
    </div>
    <div className="release-flow-order-row-actions">
      <button className="release-flow-text-button release-flow-history-workspace-link" type="button" onClick={() => openWorkspace(environments[0])}><EyeOutlined />查看工作区</button>
      {canAction && !replaced && <button className="release-flow-button danger" type="button" title={actionTitle} onClick={() => onRemove?.(release)} disabled={removed || activeRelease}><DeleteOutlined />{draft ? '移除草稿' : '移出并重新发布'}</button>}
    </div>
  </article>
}

function ReleaseWorkspacePage({
  release,
  environments,
  environment,
  status,
  logs,
  logsLoading,
  logsError,
  pods,
  activeTab,
  onTabChange,
  onBack,
  onRefresh,
  onPublish,
  onUpdateTraffic,
  onRetryTarget,
  retryingTarget,
  publishing,
  canPublishRelease,
  onEnvironmentChange,
  onOpenPod,
  onOpenMonitor,
  onOpenTerminal,
  onOpenPodLogs,
  canOpenTerminal,
  canOpenPodLogs,
  loading,
}) {
  const target = status.target || environmentTarget(environment)
  const configTarget = environmentTarget(environment) || status.target || {}
  const strategy = strategyOf(release, environment)
  const commit = targetCommit(target, release)
  const replicas = configTarget.replicas || target?.replicas || 1
  const resourceQuota = configTarget.resource_quota || {}
  const [actionSubmitting, setActionSubmitting] = useState(false)
  const actionSubmittingRef = useRef(false)
  const releaseRunning = ['queued', 'running'].includes(`${release.status || ''}`.toLowerCase())
  const actionBusy = publishing || actionSubmitting || Boolean(retryingTarget) || releaseRunning
  const locked = status.state === 'pending' && !isEnvironmentUnlocked(release, environments, environments.indexOf(environment))
  const canAction = canPublishRelease && !locked && !actionBusy && !loading
  const actionLabel = status.state === 'failed' ? `重试 ${environment.label}` : status.state === 'done' ? `重新发布 ${environment.label}` : `发布 ${environment.label}`
  const runAction = async () => {
    if (!canAction || actionSubmittingRef.current) return
    actionSubmittingRef.current = true
    setActionSubmitting(true)
    onTabChange('logs')
    try {
      if (status.state === 'failed' && status.target) await onRetryTarget?.(release, status.target)
      else await onPublish?.(release, environment)
    } finally {
      actionSubmittingRef.current = false
      setActionSubmitting(false)
    }
  }
  const executionActive = actionSubmitting || releaseRunning || status.state === 'active'
  const statusText = locked ? '未解锁' : executionActive ? status.state === 'active' ? status.label : '进行中' : status.state === 'done' ? '已完成' : status.state === 'failed' ? '发布失败' : '待发布'
  const statusTone = locked ? 'locked' : executionActive ? 'active' : status.state === 'failed' ? 'failed' : status.state === 'done' ? 'done' : ''
  const currentStage = status.target?.stage || (executionActive ? 'preparing' : '')
  const logStage = useMemo(() => executionStageFromLogs(normalizeExecutionLogs(logs)), [logs])
  const visibleStage = logStage === 'source'
    ? 'preparing'
    : logStage === 'build' || logStage === 'registry'
      ? 'building'
      : logStage === 'k8s'
        ? 'deploying'
        : logStage === 'pod'
          ? 'checking'
          : currentStage
  const progressStages = [
    { key: 'source', label: '读取代码' },
    { key: 'build', label: '构建镜像' },
    { key: 'k8s', label: '应用 Kubernetes' },
    { key: 'pod', label: '等待 Pod 就绪' },
  ]
  const stageIndex = visibleStage === 'preparing'
    ? 0
    : visibleStage === 'building'
      ? 1
      : visibleStage === 'deploying'
        ? 2
        : visibleStage === 'checking'
        ? 3
        : -1
  const progressItems = progressStages.map((item, index) => {
    const state = status.state === 'done'
      ? 'done'
      : status.state === 'failed' && index === Math.max(0, stageIndex)
        ? 'failed'
        : index < stageIndex
          ? 'done'
          : index === stageIndex
            ? 'active'
            : 'pending'
    return { ...item, state }
  })
  const planTraffic = release.plan?.traffic || {}
  const secondaryTraffic = strategy === 'blue_green'
    ? Number.isFinite(Number(planTraffic.green_percent)) ? Number(planTraffic.green_percent) : 0
    : Number.isFinite(Number(planTraffic.candidate_percent)) ? Number(planTraffic.candidate_percent) : 0
  const traffic = strategy === 'rolling'
    ? { primary: 100, secondary: 0, primaryLabel: '当前服务', secondaryLabel: '新 Pod' }
    : strategy === 'blue_green'
      ? { primary: 100 - secondaryTraffic, secondary: secondaryTraffic, primaryLabel: '蓝版本', secondaryLabel: '绿版本' }
      : { primary: 100 - secondaryTraffic, secondary: secondaryTraffic, primaryLabel: '稳定版本', secondaryLabel: '灰度版本' }
  const trafficTargetID = target?.id || configTarget?.id || environment?.key
  const canAdjustTraffic = canPublishRelease && strategy !== 'rolling' && ['active', 'done'].includes(status.state) && Boolean(trafficTargetID)
  return <section className="release-workspace-panel">
      <section className="release-workspace-main">
        <header className="release-workspace-main-head">
          <div className="release-workspace-main-copy">
            <div className="release-workspace-main-title"><h2>环境发布</h2><span className={`release-flow-status ${statusTone}`}>{statusText}</span></div>
            <div className="release-workspace-release-meta"><span>发布单 <code>{release.id}</code></span><span>分支 <code>{release.branch || '-'}</code></span><span>代码版本 <code>{commit}</code></span><span>发布人 {releaseOwner(release)}</span></div>
          </div>
          <div className="release-workspace-main-actions">
            <label className="release-workspace-environment-switch">
              <span>环境</span>
              <select
                aria-label="切换发布环境"
                value={environment?.key || ''}
                disabled={loading || environments.length < 2}
                onChange={(event) => onEnvironmentChange?.(event.target.value)}
              >
                {environments.map((item) => <option key={item.key} value={item.key}>{item.label}</option>)}
              </select>
              <DownOutlined />
            </label>
            <button className="release-flow-button" type="button" onClick={onBack}>返回列表</button>
            <button className="release-flow-button" type="button" onClick={onRefresh} disabled={loading || actionBusy}><ReloadOutlined />{loading ? '刷新中' : '刷新'}</button>
            <button className="release-flow-button primary" type="button" onClick={runAction} disabled={!canAction}><PlayCircleOutlined />{actionBusy ? '提交中...' : locked ? '等待前置环境完成' : actionLabel}</button>
          </div>
        </header>

        <nav className="release-workspace-tabs" role="tablist" aria-label="环境详情">
          <button className={activeTab === 'config' ? 'active' : ''} type="button" onClick={() => onTabChange('config')}>发布配置</button>
          <button className={activeTab === 'logs' ? 'active' : ''} type="button" onClick={() => onTabChange('logs')}>执行日志 <span>{logs.length}</span></button>
          <button className={activeTab === 'pods' ? 'active' : ''} type="button" onClick={() => onTabChange('pods')}>Pod <span>{pods.length}</span></button>
          <button className={activeTab === 'traffic' ? 'active' : ''} type="button" onClick={() => onTabChange('traffic')}>流量</button>
        </nav>

        <div className="release-workspace-body">
          {activeTab === 'config' && <div className="release-workspace-config">
            <section><h3>发布配置</h3><div className="release-workspace-config-grid">
              <div><span>集群</span><strong>{configTarget.cluster_id || target?.cluster_id || '未配置集群'}</strong></div>
              <div><span>命名空间</span><strong>{configTarget.namespace || target?.namespace || '未配置命名空间'}</strong></div>
              <div><span>发布策略</span><strong>{strategyLabel(strategy)}</strong></div>
              <div><span>副本数</span><strong>{replicas}</strong></div>
              <div><span>资源限额</span><strong>{resourceQuotaLabel(resourceQuota)}</strong></div>
            </div></section>
          </div>}
          {activeTab === 'logs' && <div className="release-workspace-logs"><div><div className="release-workspace-log-title"><strong>{environment?.label || '当前环境'} 执行日志</strong><span>{logsLoading ? '加载中...' : logsError ? '加载失败' : `${logs.length} 条`}</span></div><ReleaseLogTerminal logs={logs} emptyText={logsError || (executionActive ? '正在等待执行输出...' : '尚未产生执行输出')} /></div><aside><h3>执行进度</h3>{progressItems.map((item) => <div className={`release-workspace-progress-item ${item.state}`} key={item.key}><i />{item.label}</div>)}</aside></div>}
          {activeTab === 'pods' && <PodList pods={pods} release={release} onOpenPod={onOpenPod} onOpenMonitor={onOpenMonitor} onOpenTerminal={onOpenTerminal} onOpenPodLogs={onOpenPodLogs} canOpenTerminal={canOpenTerminal} canOpenPodLogs={canOpenPodLogs} />}
          {activeTab === 'traffic' && <TrafficView
            traffic={traffic}
            strategy={strategy}
            canEdit={canPublishRelease}
            canAdjust={canAdjustTraffic}
            saving={publishing}
            onApply={(payload) => onUpdateTraffic?.(release, trafficTargetID, payload)}
          />}
        </div>
      </section>
  </section>
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
    <div className="release-flow-config-grid"><section className="release-flow-section release-flow-card"><div className="release-flow-section-head"><div><h2>发布目标</h2><p>目标集群和命名空间来自环境配置，副本、端口和探针等工作负载规格来自资源文件。</p></div></div><div className="release-flow-target-grid"><div><span className="release-flow-label">环境发布单</span><strong>{target?.release_id || `${release.id}-${environment.label}`}</strong></div><div><span className="release-flow-label">集群</span><strong>{target?.cluster_id || '未配置集群'}</strong></div><div><span className="release-flow-label">命名空间</span><strong>{target?.namespace || '未配置命名空间'}</strong></div><div><span className="release-flow-label">工作负载规格</span><strong>资源文件决定</strong></div></div><div className="release-flow-mode-panel"><div className="release-flow-mode-head"><strong>发布方式</strong><span>来自环境配置</span></div><div className="release-flow-mode-readonly"><strong>{strategyLabel(activeStrategy)}</strong></div><div className="release-flow-config-fields"><div className={`release-flow-traffic ${activeStrategy === 'rolling' ? 'disabled' : ''}`}><div><span>新版本流量</span><strong>{activeStrategy === 'rolling' ? '不涉及' : `${config.traffic}%`}</strong></div><input type="range" min="1" max="100" value={activeStrategy === 'rolling' ? 1 : config.traffic} disabled={activeStrategy === 'rolling'} onChange={(event) => onConfigChange({ traffic: Number(event.target.value) })} /><small><span>新版本 {activeStrategy === 'rolling' ? '—' : `${config.traffic}%`}</span><span>旧版本 {activeStrategy === 'rolling' ? '—' : `${100 - config.traffic}%`}</span></small></div></div></div></section><aside className="release-flow-section release-flow-card release-flow-review-card"><h2>提交前确认</h2><p>提交后生成 {environment.label} 环境发布单。</p><div><span>代码版本</span><strong>{commitOf(release)}</strong></div><div><span>目标环境</span><strong>{environment.label}</strong></div><div><span>发布方式</span><strong>{strategyLabel(activeStrategy)}</strong></div><div><span>起始流量</span><strong>{activeStrategy === 'rolling' ? '不涉及' : `${config.traffic}%`}</strong></div><div className="release-flow-submit-note">当前账号可提交；没有执行权限时，提交后等待发布员执行。</div></aside></div>
  </>
}

function EnvironmentDetailPage({ project, release, environment, status, logs, logsLoading, logsError, pods, activeTab, onTabChange, onBack, onRefresh, onRepublish, onRetryTarget, retryingTarget, publishing, canPublishRelease, onOpenPod, onOpenMonitor, onOpenTerminal, onOpenPodLogs, canOpenTerminal, canOpenPodLogs, loading }) {
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
    <section className={`release-flow-section release-flow-runtime-card is-${activeTab}`}><nav className="release-flow-detail-tabs"><button className={activeTab === 'logs' ? 'active' : ''} type="button" onClick={() => onTabChange('logs')}>执行日志</button><button className={activeTab === 'pods' ? 'active' : ''} type="button" onClick={() => onTabChange('pods')}>Pod 列表 <span>{pods.length}</span></button><button className={activeTab === 'traffic' ? 'active' : ''} type="button" onClick={() => onTabChange('traffic')}>流量变化</button></nav>{activeTab === 'logs' && <ReleaseLogTerminal logs={logs} emptyText={logsLoading ? '正在读取执行日志...' : logsError ? '执行日志暂时无法加载' : '尚未产生执行输出'} />}{activeTab === 'pods' && <PodList pods={pods} release={release} onOpenPod={onOpenPod} onOpenMonitor={onOpenMonitor} onOpenTerminal={onOpenTerminal} onOpenPodLogs={onOpenPodLogs} canOpenTerminal={canOpenTerminal} canOpenPodLogs={canOpenPodLogs} />}{activeTab === 'traffic' && <TrafficView traffic={traffic} strategy={strategy} />}</section>
  </>
}

function PodList({ pods, release, onOpenPod, onOpenMonitor, onOpenTerminal, onOpenPodLogs, canOpenTerminal, canOpenPodLogs = true }) {
  return <div className="release-flow-pod-table"><div className="release-flow-pod-head"><span>Pod</span><span>版本</span><span>节点</span><span>状态</span><span>操作</span></div>{pods.length ? pods.map((pod) => <div className="release-flow-pod-row" key={pod.name}><button type="button" onClick={() => onOpenPod?.(pod)}>{pod.name}</button><code>{pod.labels?.version || pod.labels?.['app.kubernetes.io/version'] || commitOf(release)}</code><span>{pod.node_name || '-'}</span><span className={pod.ready ? 'ready' : 'starting'}>{pod.ready ? 'Ready' : pod.phase || '处理中'}</span><div className="release-flow-pod-actions"><button type="button" onClick={() => onOpenMonitor?.(pod)}><DashboardOutlined />监控</button><button type="button" disabled={!canOpenTerminal} title={canOpenTerminal ? '进入 Pod Terminal' : '当前账号没有进入 Pod 终端的权限'} onClick={() => onOpenTerminal?.(pod)}><CodeOutlined />Terminal</button><button type="button" disabled={!canOpenPodLogs} title={canOpenPodLogs ? '查看 Pod 标准输出和标准错误日志' : '当前账号没有查看 Pod 日志的权限'} onClick={() => onOpenPodLogs?.(pod)}><FileTextOutlined />日志</button></div></div>) : <div className="release-flow-empty">当前环境还没有可展示的 Pod</div>}</div>
}

function TrafficView({ traffic, strategy, canEdit = false, canAdjust = false, saving = false, onApply }) {
  const [draft, setDraft] = useState(traffic.secondary)
  const adjustable = strategy !== 'rolling'
  const editable = canEdit && canAdjust && adjustable
  useEffect(() => {
    setDraft(traffic.secondary)
  }, [traffic.secondary, strategy])
  const nextTraffic = adjustable
    ? { ...traffic, primary: 100 - draft, secondary: draft }
    : traffic
  const changed = adjustable && draft !== traffic.secondary
  const apply = async () => {
    if (!editable || !changed || saving || !onApply) return
    const payload = strategy === 'blue_green'
      ? { stable_percent: 0, candidate_percent: 0, blue_percent: 100 - draft, green_percent: draft }
      : { stable_percent: 100 - draft, candidate_percent: draft, blue_percent: 0, green_percent: 0 }
    const updated = await onApply(payload)
    if (!updated) setDraft(traffic.secondary)
  }
  const helperText = !canEdit
    ? '当前角色没有调整发布流量的权限'
    : !adjustable
      ? '滚动发布不使用独立流量比例'
      : !canAdjust
        ? '发布完成或开始执行后才可以调整流量'
        : '调整后会立即作用于当前环境'
  return <div className="release-flow-traffic-view">
    <div className="release-flow-traffic-title"><strong>{strategyLabel(strategy)} · 当前流量</strong><span>{helperText}</span></div>
    <div className="release-flow-traffic-grid">
      <div><span>{nextTraffic.primaryLabel}</span><strong>{nextTraffic.primary}%</strong><div className="release-flow-traffic-bar"><i style={{ width: `${nextTraffic.primary}%` }} /></div><small>旧版本 / 稳定版本</small></div>
      <div><span>{nextTraffic.secondaryLabel}</span><strong>{nextTraffic.secondary}%</strong><div className="release-flow-traffic-bar candidate"><i style={{ width: `${nextTraffic.secondary}%` }} /></div><small>新版本</small></div>
    </div>
    <div className={`release-flow-traffic-editor ${editable ? '' : 'is-readonly'}`}>
      <div className="release-flow-traffic-editor-head"><span>流量调整</span><strong>{adjustable ? `${draft}%` : '不涉及'}</strong></div>
      <input aria-label="新版本流量比例" title={helperText} type="range" min={0} max={100} value={adjustable ? draft : 0} disabled={!editable || saving} onChange={(event) => setDraft(Number(event.target.value))} />
      <div className="release-flow-traffic-editor-scale"><span>{adjustable ? '0%' : '滚动发布'}</span><span>{adjustable ? '100%' : '无独立流量比例'}</span></div>
      <div className="release-flow-traffic-editor-actions">
        <small>{adjustable && canEdit ? '按比例调整新旧版本接收的请求' : helperText}</small>
        <button className="release-flow-button primary" type="button" title={helperText} disabled={!editable || !changed || saving} onClick={apply}>{saving ? '应用中...' : adjustable ? '应用流量' : '不支持调整'}</button>
      </div>
    </div>
  </div>
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
  releaseFlow = null,
  releasePage = 1,
  releasePageSize = 10,
  releaseTotal = 0,
  releaseSearch = '',
  loading = false,
  onRefresh,
  onReleasePageChange,
  onReleaseSearch,
  onCreateRelease,
  onPublishEnvironment,
  onUpdateTraffic,
  onRetryTarget,
  onLoadReleaseTargetLogs,
  onEnvironmentChange,
  onOpenPod,
  onOpenMonitor,
  onOpenTerminal,
  onOpenPodLogs,
  canOpenTerminal = true,
  canOpenPodLogs = true,
  pods = [],
  retryingTarget = '',
  canCreateRelease = true,
  canUpdateRelease = true,
  canPublishRelease = true,
  publishing = false,
  onRemoveRelease,
  targets = [],
  embedded = false,
}) {
  const [page, setPage] = useState('orders')
  const [releaseView, setReleaseView] = useState('current')
  const [branchSearch, setBranchSearch] = useState('')
  const [branchPickerOpen, setBranchPickerOpen] = useState(false)
  const [selectedBranch, setSelectedBranch] = useState(null)
  const [selectedReleaseId, setSelectedReleaseId] = useState(releases[0]?.id || '')
  const [selectedReleaseSnapshot, setSelectedReleaseSnapshot] = useState(null)
  const [selectedEnvironmentKey, setSelectedEnvironmentKey] = useState('')
  const [activeDetailTab, setActiveDetailTab] = useState('config')
  const [environmentLogs, setEnvironmentLogs] = useState([])
  const [environmentLogsLoading, setEnvironmentLogsLoading] = useState(false)
  const [environmentLogsError, setEnvironmentLogsError] = useState('')
  const [removedIds, setRemovedIds] = useState(() => new Set())
  const [removeCandidate, setRemoveCandidate] = useState(null)
  const [removeSubmitting, setRemoveSubmitting] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [environmentConfig, setEnvironmentConfig] = useState({ traffic: 1 })
  const environments = useMemo(() => getEnvironments(targets), [targets])
  const selectedRelease = releases.find((item) => item.id === selectedReleaseId) || (selectedReleaseSnapshot?.id === selectedReleaseId ? selectedReleaseSnapshot : null) || releases[0] || null
  const selectedEnvironment = environments.find((item) => item.key === selectedEnvironmentKey) || environments[0]
  const selectedStatus = selectedRelease && selectedEnvironment ? targetStatus(selectedRelease, selectedEnvironment) : { state: 'pending', target: null }
  const selectedTargetId = selectedStatus.target?.id || ''
  const loadTargetLogs = useCallback((releaseId, targetId) => onLoadReleaseTargetLogs ? onLoadReleaseTargetLogs(releaseId, targetId) : getReleaseTargetLogs(project.id, releaseId, targetId), [onLoadReleaseTargetLogs, project.id])
  const visibleBranches = useMemo(() => {
    const term = branchSearch.trim().toLowerCase()
    const source = branches
    return source.filter((item) => !/^batch(?:[-/]|$)/i.test(item.name || '')).filter((item) => `${item.name || ''} ${item.author || item.owner || ''} ${branchVersion(item)} ${branchMessage(item)}`.toLowerCase().includes(term))
  }, [branches, branch, branchSearch])
  const openCreate = (item) => {
    setSelectedBranch(item)
    setBranchPickerOpen(false)
    setPage('create')
  }
  const openNewReleaseForm = () => {
    setBranchSearch('')
    setBranchPickerOpen(true)
  }
  const openCurrentFlow = () => {
    setReleaseView('current')
    if (releaseSearch && onReleaseSearch) onReleaseSearch('')
    else if (releasePage !== 1) onReleasePageChange?.(1)
  }
  const openReleaseHistory = () => {
    setReleaseView('history')
    if (releasePage !== 1 && !releaseSearch) onReleasePageChange?.(1)
  }
  const notifyEnvironmentChange = (environment) => {
    const target = environmentTarget(environment)
    onEnvironmentChange?.(target?.id || environmentStage(environment) || environment?.key)
  }
  const openRelease = (release, environment = 'dev') => {
    if (!release?.id) return
    setBranchPickerOpen(false)
    const nextEnvironment = resolveEnvironment(environment, environments)
    setSelectedReleaseId(release.id)
    setSelectedReleaseSnapshot(release)
    setSelectedEnvironmentKey(nextEnvironment?.key || '')
    setActiveDetailTab('config')
    if (nextEnvironment) notifyEnvironmentChange(nextEnvironment)
    setPage('workspace')
  }
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
      if (created?.id) {
        setSelectedReleaseId(created.id)
        setSelectedReleaseSnapshot(created)
        setSelectedEnvironmentKey(environments[0]?.key || '')
        setReleaseView('current')
        setPage('orders')
      }
    } finally { setSubmitting(false) }
  }
  const openEnvironment = (release, environment) => {
    const nextEnvironment = resolveEnvironment(environment, environments)
    setSelectedReleaseId(release.id)
    setSelectedReleaseSnapshot(release)
    setSelectedEnvironmentKey(nextEnvironment?.key || '')
    setActiveDetailTab('config')
    if (nextEnvironment) notifyEnvironmentChange(nextEnvironment)
    setPage('workspace')
  }
  const switchEnvironment = (value) => {
    const nextEnvironment = resolveEnvironment(value, environments)
    if (!nextEnvironment || nextEnvironment.key === selectedEnvironment?.key) return
    setSelectedEnvironmentKey(nextEnvironment.key)
    notifyEnvironmentChange(nextEnvironment)
  }
  useEffect(() => {
    if (page !== 'workspace' || !selectedRelease?.id || !selectedTargetId) {
      setEnvironmentLogs([])
      setEnvironmentLogsLoading(false)
      setEnvironmentLogsError('')
      return undefined
    }
    let active = true
    let timer
    setEnvironmentLogs([])
    setEnvironmentLogsLoading(true)
    setEnvironmentLogsError('')
    const poll = async (initial = false) => {
      try {
        const nextLogs = await loadTargetLogs(selectedRelease.id, selectedTargetId)
        if (active) {
          setEnvironmentLogs(Array.isArray(nextLogs) ? nextLogs : [])
          setEnvironmentLogsError('')
        }
      } catch (error) {
        if (active) setEnvironmentLogsError(error.message || '执行日志加载失败')
      } finally {
        if (active && initial) setEnvironmentLogsLoading(false)
        if (active && activeDetailTab === 'logs' && (selectedStatus.state === 'active' || selectedStatus.state === 'pending')) {
          timer = window.setTimeout(() => poll(false), 800)
        }
      }
    }
    poll(true)
    return () => {
      active = false
      window.clearTimeout(timer)
    }
  }, [page, activeDetailTab, selectedRelease?.id, selectedTargetId, selectedStatus.state, loadTargetLogs])
  const submitEnvironment = async () => {
    if (!selectedRelease || !selectedEnvironment || !onPublishEnvironment) return
    setSubmitting(true)
    try {
      const updated = await onPublishEnvironment(selectedRelease, selectedEnvironment, environmentConfig)
      if (updated?.id) {
        setSelectedReleaseId(updated.id)
        setPage('workspace')
      }
    } catch (error) {
      message.error(error.message || '提交环境发布单失败')
    } finally { setSubmitting(false) }
  }
  const requestRemoveRelease = (release) => {
    if (!release?.id) return
    const draft = release.status === 'draft'
    if (draft && !canUpdateRelease) {
      message.warning('当前角色没有移除发布单的权限')
      return
    }
    if (!draft && (!canUpdateRelease || !canPublishRelease)) {
      message.warning('当前角色没有移除发布单的权限')
      return
    }
    setRemoveCandidate(release)
  }
  const confirmRemoveRelease = async () => {
    if (!removeCandidate || removeSubmitting || !onRemoveRelease) return
    setRemoveSubmitting(true)
    try {
      const result = await onRemoveRelease(removeCandidate)
      if (result?.archived) {
        setRemovedIds((old) => new Set([...old, removeCandidate.id]))
        message.success(`草稿 ${removeCandidate.id} 已移除`)
        setRemoveCandidate(null)
      } else if (result?.republished) {
        message.success(`已提交移出分支 ${result.removed_branch || removeCandidate.branch || '当前分支'} 的重新发布；成功后当前流程会切换到新发布单`)
        setRemoveCandidate(null)
      }
    } finally {
      setRemoveSubmitting(false)
    }
  }
  const pageContent = page === 'workspace' && (!selectedRelease || !selectedEnvironment)
    ? <FlowUnavailablePage title="还没有可用的发布工作区" message="请先从发布单列表选择一条发布单和发布环境。" onGoOrders={() => setPage('orders')} />
    : <>
      {page === 'create' && <CreateReleasePage project={project} branch={selectedBranch} environments={environments} submitting={submitting} onBack={() => setPage('orders')} onOpenTargets={onOpenTargets || onBack} onSubmit={submitCreate} canCreateRelease={canCreateRelease} />}
      {page === 'orders' && (releaseView === 'current'
        ? <CurrentReleaseFlowPage flow={releaseFlow} releases={releases.filter((item) => !removedIds.has(item.id))} environments={environments} historyCount={releaseTotal} loading={loading} onViewHistory={openReleaseHistory} onNew={openNewReleaseForm} onOpenWorkspace={openRelease} onRemove={requestRemoveRelease} onRefresh={onRefresh} canCreateRelease={canCreateRelease} canRemove={canUpdateRelease} canRepublish={canUpdateRelease && canPublishRelease} />
        : <ReleaseOrderListPage releases={releases.filter((item) => !removedIds.has(item.id))} environments={environments} removed={removedIds} page={releasePage} pageSize={releasePageSize} total={releaseTotal} search={releaseSearch} onPageChange={onReleasePageChange} onSearch={onReleaseSearch} onOpenWorkspace={openRelease} onRefresh={onRefresh} loading={loading} onViewCurrent={openCurrentFlow} />)}
      {page === 'workspace' && selectedRelease && selectedEnvironment && <ReleaseWorkspacePage release={selectedRelease} environments={environments} environment={selectedEnvironment} status={selectedStatus} logs={environmentLogs} logsLoading={environmentLogsLoading} logsError={environmentLogsError} pods={pods} activeTab={activeDetailTab} onTabChange={setActiveDetailTab} onBack={() => setPage('orders')} onRefresh={onRefresh} onPublish={onPublishEnvironment} onUpdateTraffic={onUpdateTraffic} onRetryTarget={onRetryTarget} retryingTarget={retryingTarget} publishing={publishing} canPublishRelease={canPublishRelease} onEnvironmentChange={switchEnvironment} onOpenPod={onOpenPod} onOpenMonitor={onOpenMonitor} onOpenTerminal={onOpenTerminal} onOpenPodLogs={onOpenPodLogs} canOpenTerminal={canOpenTerminal} canOpenPodLogs={canOpenPodLogs} loading={loading} />}
    </>

  return <div className={`release-flow-portal${embedded ? ' is-embedded' : ''}`}>
    <PageFrame project={project} page={page} onBack={onBack} embedded={embedded}>
      {pageContent}
    </PageFrame>
    <BranchPickerModal
      open={branchPickerOpen}
      branches={visibleBranches}
      releases={releases.filter((item) => !removedIds.has(item.id))}
      releaseFlow={releaseFlow}
      search={branchSearch}
      onSearch={setBranchSearch}
      onClose={() => setBranchPickerOpen(false)}
      onChoose={openCreate}
      onOpenRelease={openRelease}
      canCreateRelease={canCreateRelease}
      branchLoading={branchLoading}
    />
    <RemoveReleaseModal
      open={Boolean(removeCandidate)}
      release={removeCandidate}
      environments={environments}
      submitting={removeSubmitting}
      onClose={() => setRemoveCandidate(null)}
      onConfirm={confirmRemoveRelease}
    />
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
