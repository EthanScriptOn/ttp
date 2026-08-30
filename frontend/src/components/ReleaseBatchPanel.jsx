import {
  Button,
  Empty,
  Popconfirm,
  Progress,
  Skeleton,
  Space,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import {
  BranchesOutlined,
  CheckCircleOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  CloudUploadOutlined,
  ExclamationCircleOutlined,
  ReloadOutlined,
  SyncOutlined,
} from '@ant-design/icons'
import {
  environmentGate,
  environmentSlotProgress,
  environmentSlotStatus,
  getEnvironmentSlots,
  normalizeReleaseStatus,
  targetStatus,
} from './ReleaseProgress'

const RELEASE_STATUS = {
  draft: ['default', '草稿'],
  queued: ['warning', '排队中'],
  preparing: ['processing', '准备中'],
  building: ['processing', '构建中'],
  testing: ['processing', '检查中'],
  pushing: ['processing', '推送镜像'],
  deploying: ['processing', '更新 Pod'],
  checking: ['processing', '检查 Pod'],
  switching: ['processing', '切换流量'],
  running: ['processing', '发布中'],
  succeeded: ['success', '发布成功'],
  failed: ['error', '发布失败'],
  cancelled: ['default', '已取消'],
  canceled: ['default', '已取消'],
}

const TARGET_STATUS = {
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

const ACTIONABLE_TARGET_STATUS = new Set(['pending', 'failed', 'cancelled', 'canceled'])
const ACTIVE_TARGET_STATUS = new Set(['queued', 'preparing', 'running', 'building', 'testing', 'pushing', 'deploying', 'checking', 'switching'])

function text(value, fallback = '') {
  if (value === undefined || value === null) return fallback
  const result = String(value).trim()
  return result || fallback
}

function numberValue(value) {
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}

function releaseID(item) {
  return text(item?.release_id || item?.releaseId || item?.id)
}

function targetID(target) {
  return text(target?.id || target?.target_id || target?.targetId)
}

function releaseForItem(item, releases) {
  const id = releaseID(item)
  return releases.find((release) => text(release?.id || release?.release_id) === id) || null
}

function batchItems(batch, releases) {
  const sourceItems = Array.isArray(batch?.items) ? batch.items : []
  const items = sourceItems.map((item) => ({ item, release: releaseForItem(item, releases) }))
  const knownIDs = new Set(items.map(({ item }) => releaseID(item)).filter(Boolean))

  releases
    .filter((release) => text(release?.batch_id || release?.batchId) === text(batch?.id))
    .filter((release) => !knownIDs.has(text(release?.id || release?.release_id)))
    .forEach((release) => items.push({
      item: {
        release_id: release.id,
        owner_id: release.owner_id,
        owner_name: release.owner_name,
        branch: release.branch,
        source_branch: release.source_branch,
        commits: release.commits,
        targets: release.targets,
        release_status: release.status,
        main_merge_status: release.main_merge_status,
        main_merge_sha: release.main_merge_sha,
        batch_snapshot_sha: release.batch_snapshot_sha,
        batch_snapshot_revision: release.batch_snapshot_revision,
        completed_environments: release.completed_environments,
        environment_count: release.environment_count,
      },
      release,
    }))

  return items
}

function readTargets(item, release) {
  const source = Array.isArray(release?.targets) && release.targets.length
    ? release.targets
    : Array.isArray(item?.targets) && item.targets.length
      ? item.targets
      : []
  if (source.length) return source

  const progressMap = release?.environment_progress || item?.environment_progress
  if (!progressMap || typeof progressMap !== 'object' || Array.isArray(progressMap)) return []
  return Object.entries(progressMap).map(([environment, value]) => ({
    ...(value && typeof value === 'object' ? value : { progress: value }),
    environment,
  }))
}

function commitsForItem(item, release) {
  const commits = Array.isArray(item?.commits) && item.commits.length
    ? item.commits
    : Array.isArray(release?.commits) && release.commits.length
      ? release.commits
      : []
  if (commits.length) return commits

  const sha = text(item?.commit_sha || item?.commitSha || item?.sha || release?.commit_sha || release?.sha)
  return sha ? [{ sha, short_sha: sha.slice(0, 7) }] : []
}

function commitSHA(commit) {
  return typeof commit === 'string' || typeof commit === 'number'
    ? text(commit)
    : text(commit?.sha || commit?.id || commit?.commit_sha)
}

function commitShortSHA(commit) {
  return text(commit?.short_sha || commit?.shortSha, commitSHA(commit).slice(0, 7) || '-')
}

function commitMessage(commit) {
  return text(commit?.message || commit?.title || commit?.subject)
}

function batchSnapshot(item, release) {
  return {
    sha: text(item?.batch_snapshot_sha || item?.batchSnapshotSHA || release?.batch_snapshot_sha || release?.batchSnapshotSHA),
    revision: numberValue(
      item?.batch_snapshot_revision ?? item?.batchSnapshotRevision ?? release?.batch_snapshot_revision ?? release?.batchSnapshotRevision,
    ),
  }
}

function ownerForItem(item, release) {
  return text(item?.owner_name || item?.ownerName || release?.owner_name || release?.ownerName || release?.operator, '未记录')
}

function releaseStatusForItem(item, release) {
  return normalizeReleaseStatus(release?.status || item?.release_status || item?.releaseStatus, 'unknown')
}

function mergeStatusForItem(item, release, merging) {
  if (merging) return 'merging'
  const itemStatus = normalizeReleaseStatus(item?.main_merge_status || item?.mainMergeStatus, '')
  const releaseStatus = normalizeReleaseStatus(release?.main_merge_status || release?.mainMergeStatus, '')
  if (itemStatus && itemStatus !== 'pending' && itemStatus !== 'unknown') return itemStatus
  return releaseStatus || itemStatus || 'pending'
}

function batchItemCanClose(item, release) {
  return ['merged', 'cancelled', 'canceled', 'terminated'].includes(mergeStatusForItem(item, release, false))
}

function productionSucceeded(targets, releaseStatus) {
  if (normalizeReleaseStatus(releaseStatus, '') !== 'succeeded') return false
  const slots = getEnvironmentSlots(targets)
  const production = slots.find((slot) => slot.key === 'prod')
  if (production?.targets.length) return environmentSlotStatus(production) === 'succeeded'
  return false
}

function statusMeta(map, status, fallback = ['default', '未知']) {
  return map[status] || fallback
}

function isBusy(value, id) {
  if (!value || !id) return false
  if (value === true) return true
  if (typeof value === 'string' || typeof value === 'number') {
    const current = String(value)
    return current === id || current.endsWith(`:${id}`)
  }
  if (Array.isArray(value)) return value.some((item) => isBusy(item, id))
  return isBusy(value.id || value.release_id || value.target_id, id)
}

function targetForSlot(slot) {
  const targets = Array.isArray(slot?.targets) ? slot.targets : []
  if (!targets.length) return null
  const rank = {
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
  return targets.reduce((current, candidate) => (rank[targetStatus(candidate)] || 0) > (rank[targetStatus(current)] || 0) ? candidate : current)
}

function allowedToPublish(value, target, slot) {
  return typeof value === 'function' ? value(target, slot) !== false : value !== false
}

function publishCallback(onPublishTarget, onPublishEnvironment) {
  return onPublishTarget || onPublishEnvironment
}

function actionLabel(status) {
  return ['failed', 'cancelled', 'canceled'].includes(status) ? '重试' : '发布'
}

function environmentDescription(status, configured, slot, gate) {
  if (!configured) return '未纳入本次发布'
  if (status === 'succeeded') return '这个环境已经发布完成'
  if (['running', 'preparing', 'building', 'testing', 'pushing', 'deploying', 'checking', 'switching'].includes(status)) return '正在发布，请稍候'
  if (status === 'queued') return '正在排队，稍后开始'
  return gate.canPublish ? `可以开始发布到 ${slot.label}` : gate.reason
}

function EnvironmentProgress({
  slot,
  index,
  slots,
  batch,
  item,
  release,
  canPublishTarget,
  onPublishTarget,
  onPublishEnvironment,
  publishingTarget,
  sequential,
}) {
  const target = targetForSlot(slot)
  const configured = Boolean(target)
  const batchClosed = normalizeReleaseStatus(batch?.status, 'open') === 'closed'
  const status = environmentSlotStatus(slot)
  const progress = environmentSlotProgress(slot)
  const gate = environmentGate(slots, index, sequential)
  const handler = publishCallback(onPublishTarget, onPublishEnvironment)
  const permission = configured && allowedToPublish(canPublishTarget, target, slot)
  const currentReleaseStatus = normalizeReleaseStatus(release?.status || item?.release_status, '')
  const releaseAllowsAction = !currentReleaseStatus || ['running', 'failed'].includes(currentReleaseStatus)
  const showAction = !batchClosed && configured && releaseAllowsAction && ACTIONABLE_TARGET_STATUS.has(status)
  const canAct = showAction && gate.canPublish && permission && typeof handler === 'function'
  const busy = isBusy(publishingTarget, targetID(target))
  const actionReason = batchClosed
    ? '这个批次已经关闭'
    : !configured
    ? ''
    : !gate.canPublish
      ? gate.reason
      : !permission
        ? '当前账号没有发布权限'
        : typeof handler !== 'function'
          ? '当前页面还没有接入这个环境的发布动作'
          : ''
  const [, label] = statusMeta(TARGET_STATUS, configured ? status : 'unconfigured')
  const isRetry = ['failed', 'cancelled', 'canceled'].includes(status)
  const marker = !configured
    ? <span className="release-batch-flow-marker-empty">-</span>
    : status === 'succeeded'
      ? <CheckCircleOutlined />
      : status === 'failed'
        ? <CloseCircleOutlined />
      : ACTIVE_TARGET_STATUS.has(status)
          ? <SyncOutlined spin />
          : <ClockCircleOutlined />

  return <div className={`release-batch-environment release-batch-flow-step status-${status} ${configured ? '' : 'is-unconfigured'}`} role="listitem" title={configured ? `${slot.name} · 已配置` : `${slot.name} · 未选择`}>
    <div className="release-batch-flow-step-head">
      <span className="release-batch-flow-marker">{marker}</span>
      <span className="release-batch-flow-step-copy"><strong>{slot.label}</strong><small>{configured ? label : '未选择'}</small></span>
    </div>
    <div className="release-batch-flow-step-progress">
      {progress !== null ? <Progress percent={progress} size="small" showInfo={false} status={status === 'failed' ? 'exception' : status === 'succeeded' ? 'success' : 'active'} /> : <span className="release-batch-environment-empty">--</span>}
      {progress !== null && <strong>{progress}%</strong>}
    </div>
    {showAction && <Tooltip title={canAct ? undefined : actionReason}>
      <span className="release-batch-environment-action-wrap">
        <Button
          type="link"
          size="small"
          icon={isRetry ? <ReloadOutlined /> : <CloudUploadOutlined />}
          disabled={!canAct}
          loading={busy}
          onClick={() => {
            if (!canAct) return
            handler(target, {
              action: isRetry ? 'retry' : 'publish',
              batch,
              item,
              release,
              environment: slot.key,
              environment_name: slot.name,
              environment_index: index,
            })
          }}
        >
          {actionLabel(status)}
        </Button>
      </span>
    </Tooltip>}
    {!showAction && configured && status !== 'succeeded' && <small className="release-batch-flow-step-hint">{batchClosed ? '批次已关闭' : environmentDescription(status, configured, slot, gate)}</small>}
  </div>
}

function MainMergeState({ batch, item, release, canPublish, onMergeMain, mergingRelease }) {
  const id = releaseID(item)
  const merging = isBusy(mergingRelease, id)
  const status = mergeStatusForItem(item, release, merging)
  const persistedStatus = mergeStatusForItem(item, release, false)
  const targets = readTargets(item, release)
  const ready = productionSucceeded(targets, releaseStatusForItem(item, release))
  const batchClosed = normalizeReleaseStatus(batch?.status, 'open') === 'closed'
  const mergePayload = release || { ...item, id }

  // Keep the column quiet until production makes the merge action available.
  if (batchClosed || !ready || typeof onMergeMain !== 'function' || !['pending', 'conflict'].includes(persistedStatus)) return null

  return <div className={`release-batch-merge release-batch-merge-cell status-${status}`}>
    {persistedStatus === 'pending' && <Tooltip title={!canPublish ? '当前账号没有合入 main 的权限' : undefined}>
      <Button type="primary" size="small" icon={<CheckOutlined />} disabled={!canPublish} loading={merging} onClick={() => onMergeMain(mergePayload)}>合入 main</Button>
    </Tooltip>}
    {persistedStatus === 'conflict' && <Tooltip title={!canPublish ? '当前账号没有合入 main 的权限' : undefined}>
      <Button type="primary" size="small" icon={<BranchesOutlined />} disabled={!canPublish} loading={merging} onClick={() => onMergeMain(mergePayload)}>重新合入 main</Button>
    </Tooltip>}
  </div>
}

function ReleaseBatchItem({
  batch,
  item,
  release,
  index,
  canPublish,
  canPublishTarget,
  onMergeMain,
  mergingRelease,
  onPublishTarget,
  onPublishEnvironment,
  publishingTarget,
  sequential,
}) {
  const id = releaseID(item) || `item-${index + 1}`
  const owner = ownerForItem(item, release)
  const commits = commitsForItem(item, release)
  const targets = readTargets(item, release)
  const slots = getEnvironmentSlots(targets)
  const snapshot = batchSnapshot(item, release)
  const releaseStatus = releaseStatusForItem(item, release)
  const [statusColor, statusLabel] = statusMeta(RELEASE_STATUS, releaseStatus)
  const visibleCommits = commits.slice(0, 2)

  return <article className="release-batch-item release-batch-row" role="row">
    <div className="release-batch-release-cell" role="cell">
      <div className="release-batch-release-head">
        <span className="release-batch-item-index">{String(index + 1).padStart(2, '0')}</span>
        <div className="release-batch-release-copy">
          <div className="release-batch-release-id" title={`发布人：${owner}`}><Typography.Text strong>{id}</Typography.Text><Tag color={statusColor}>{statusLabel}</Tag></div>
        </div>
      </div>
    </div>

    <div className="release-batch-code-cell" role="cell" aria-label={`${commits.length} 个 commit`}>
      {visibleCommits.length ? <div className="release-batch-commit-list">
        {visibleCommits.map((commit, commitIndex) => {
          const sha = commitSHA(commit)
          const message = commitMessage(commit)
          return <div className="release-batch-commit" key={`${sha || 'commit'}-${commitIndex}`}>
            <Typography.Text code title={sha}>{commitShortSHA(commit)}</Typography.Text>
            {message && <Typography.Text ellipsis={{ tooltip: message }}>{message}</Typography.Text>}
          </div>
        })}
        {commits.length > visibleCommits.length && <Typography.Text type="secondary" className="release-batch-more-commits">还有 {commits.length - visibleCommits.length} 个 commit</Typography.Text>}
      </div> : <Typography.Text type="secondary">未记录 commit</Typography.Text>}
      <div className="release-batch-code-meta" aria-label="测试版本">
        {snapshot.sha ? <Typography.Text code title={snapshot.sha}>{snapshot.sha.slice(0, 12)}</Typography.Text> : <Typography.Text type="secondary">等待固定</Typography.Text>}
        {snapshot.revision !== null && snapshot.revision > 0 && <Tag>批次版本 r{snapshot.revision}</Tag>}
      </div>
    </div>

    <div className="release-batch-flow-cell" role="cell">
      <div className="release-batch-flow-track" role="list" aria-label="各环境发布进度">
      {slots.map((slot, slotIndex) => <EnvironmentProgress
        key={slot.key}
        slot={slot}
        index={slotIndex}
        slots={slots}
        batch={batch}
        item={item}
        release={release}
        canPublishTarget={canPublishTarget}
        onPublishTarget={onPublishTarget}
        onPublishEnvironment={onPublishEnvironment}
        publishingTarget={publishingTarget}
        sequential={sequential}
      />)}
      </div>
    </div>

    <div className="release-batch-merge-wrap" role="cell">
      <MainMergeState batch={batch} item={item} release={release} canPublish={canPublish} onMergeMain={onMergeMain} mergingRelease={mergingRelease} />
    </div>
  </article>
}

function ReleaseBatch({
  batch,
  releases,
  canPublish,
  canPublishTarget,
  onMergeMain,
  mergingRelease,
  onPublishTarget,
  onPublishEnvironment,
  publishingTarget,
  sequential,
  onCloseBatch,
  closingBatch,
  onRefresh,
  loading = false,
}) {
  const closed = normalizeReleaseStatus(batch?.status, 'open') === 'closed'
  const [statusColor, statusLabel] = closed ? ['default', '已关闭'] : ['success', '开放中']
  const items = batchItems(batch, releases)
  const batchID = text(batch?.id, '未命名批次')
  const closeBusy = isBusy(closingBatch, batchID)
  const closeReady = items.length > 0 && items.every(({ item, release }) => batchItemCanClose(item, release))
  const closeReason = !canPublish
    ? '当前账号没有关闭批次的权限'
    : closeReady
      ? '关闭后不再接收新发布，也不能继续推进环境或合入 main；已发布的 Pod 和批次分支会保留。'
      : '所有发布项完成合入 main、取消或终止后才能关闭。关闭不会删除已发布的 Pod 或批次分支。'
  const batchBranch = text(batch?.branch, batchID)
  const baseSHA = text(batch?.base_sha, '-')
  const headSHA = text(batch?.head_sha, '-')
  const currentMainSHA = text(batch?.current_main_sha, '-')

  return <article className={`release-batch-card ${closed ? 'is-closed' : 'is-open'}`}>
    <div className="release-batch-card-head">
      <div className="release-batch-card-title">
        <div className="release-batch-card-icon"><BranchesOutlined /></div>
        <div className="release-batch-card-title-copy">
          <div className="release-batch-card-name">
            <Typography.Title level={5}>{batchID}</Typography.Title>
            <Tag color={statusColor}>{statusLabel}</Tag>
            <span className="release-batch-item-count">{items.length} 个发布项</span>
            <div className="release-batch-inline-meta" aria-label="批次信息">
              <span><small>批次分支</small><Typography.Text code title={batchBranch}>{batchBranch}</Typography.Text></span>
              <span><small>初始 main</small><Typography.Text code title={baseSHA}>{baseSHA.slice(0, 12)}</Typography.Text></span>
              <span><small>批次 HEAD</small><Typography.Text code title={headSHA}>{headSHA.slice(0, 12)}</Typography.Text></span>
              <span><small>revision</small><strong>r{text(batch?.revision, '0')}</strong></span>
              <span><small>当前 main</small><Typography.Text code title={currentMainSHA}>{currentMainSHA.slice(0, 12)}</Typography.Text></span>
            </div>
          </div>
        </div>
      </div>
      <Space size={8} className="release-batch-card-actions">
        {typeof onRefresh === 'function' && <Button size="small" icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>刷新批次</Button>}
        {!closed && typeof onCloseBatch === 'function' && <Tooltip title={closeReason}>
          <span className="release-batch-action-wrap">
            <Popconfirm
              title="确定关闭这个发布批次吗？"
              description={<span className="release-batch-close-description">已发布的 Pod 会继续运行；批次分支会保留但不能再发布或合入 main。关闭后，未完成的发布项也不能继续推进。</span>}
              okText="关闭批次"
              cancelText="取消"
              onConfirm={() => onCloseBatch(batch)}
            >
              <Button size="small" loading={closeBusy} disabled={!canPublish || !closeReady}>关闭批次</Button>
            </Popconfirm>
          </span>
        </Tooltip>}
      </Space>
    </div>

    {items.length ? <div className="release-batch-table" role="table" aria-label={`${batchID} 内的发布项`}>
      <div className="release-batch-items">
        {items.map(({ item, release }, index) => <ReleaseBatchItem
          key={`${batchID}-${releaseID(item) || index}`}
          batch={batch}
          item={item}
          release={release}
          index={index}
          canPublish={canPublish}
          canPublishTarget={canPublishTarget}
          onMergeMain={onMergeMain}
          mergingRelease={mergingRelease}
          onPublishTarget={onPublishTarget}
          onPublishEnvironment={onPublishEnvironment}
          publishingTarget={publishingTarget}
          sequential={sequential}
        />)}
      </div>
    </div> : <div className="release-batch-items-empty"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这个批次还没有发布项" /></div>}
  </article>
}

export default function ReleaseBatchPanel({
  batches = [],
  releases = [],
  loading = false,
  canPublish = false,
  onRefresh,
  onMergeMain,
  mergingRelease,
  onCloseBatch,
  closingBatch,
  onPublishTarget,
  onPublishEnvironment,
  publishingTarget = '',
  canPublishTarget = true,
  sequential = true,
}) {
  const batchList = Array.isArray(batches) ? batches : batches ? [batches] : []
  const releaseList = Array.isArray(releases) ? releases : []

  return <section className="release-batch-panel release-items" aria-label="发布批次">
    {loading && !batchList.length ? <div className="release-batch-loading"><Skeleton active paragraph={{ rows: 5 }} /></div>
      : batchList.length ? <div className="release-batch-list">
        {batchList.map((batch, index) => <ReleaseBatch
          key={text(batch?.id, `batch-${index}`)}
          batch={batch}
          releases={releaseList}
          canPublish={canPublish}
          canPublishTarget={canPublishTarget}
          onMergeMain={onMergeMain}
          mergingRelease={mergingRelease}
          onPublishTarget={onPublishTarget}
          onPublishEnvironment={onPublishEnvironment}
          publishingTarget={publishingTarget}
          sequential={sequential}
          onCloseBatch={onCloseBatch}
          closingBatch={closingBatch}
          onRefresh={onRefresh}
          loading={loading}
        />)}
      </div>
      : <div className="release-batch-empty"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有发布批次" /></div>}

    {!canPublish && batchList.length > 0 && <div className="release-batch-readonly"><ExclamationCircleOutlined /> 当前账号只能查看批次，合入和关闭操作需要发布权限。</div>}
  </section>
}
