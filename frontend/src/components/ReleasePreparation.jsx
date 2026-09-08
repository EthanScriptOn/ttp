import {
  CheckCircleOutlined,
  EyeOutlined,
  ExclamationCircleOutlined,
  FileSearchOutlined,
  FileTextOutlined,
  LoadingOutlined,
  LockOutlined,
  MergeCellsOutlined,
  ReloadOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { Alert, Button, Divider, Input, Select, Space, Spin, Tag, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'

const statusAliases = {
  ok: 'ready',
  pass: 'ready',
  passed: 'ready',
  can_publish: 'ready',
  canpublish: 'ready',
  success: 'ready',
  need_merge: 'needs_merge',
  needsmerge: 'needs_merge',
  merge_required: 'needs_merge',
  requires_merge: 'needs_merge',
  behind: 'needs_merge',
  conflicts: 'conflict',
  merge_conflict: 'conflict',
  merge_conflicts: 'conflict',
  mergeconflict: 'conflict',
  not_supported: 'unsupported',
  unsupported_repository: 'unsupported',
  checking: 'loading',
  pending: 'loading',
}

function firstValue(source, ...keys) {
  if (!source || typeof source !== 'object') return undefined
  for (const key of keys) {
    if (source[key] !== undefined && source[key] !== null) return source[key]
  }
  return undefined
}

function hasOwn(source, key) {
  return Object.prototype.hasOwnProperty.call(source, key)
}

function unwrapPreparation(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  const nested = firstValue(value, 'data', 'result', 'payload')
  if (nested && typeof nested === 'object' && !Array.isArray(nested)) return { ...value, ...nested }
  return value
}

function textValue(value, fallback = '') {
  if (value === undefined || value === null) return fallback
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  if (typeof value === 'object') {
    const text = firstValue(value, 'text', 'message', 'detail', 'description')
    if (text !== undefined && text !== null) return textValue(text, fallback)
    try {
      return JSON.stringify(value)
    } catch {
      return fallback
    }
  }
  return fallback
}

function booleanValue(value) {
  if (typeof value === 'boolean') return value
  if (typeof value === 'number') return value !== 0
  if (typeof value !== 'string') return undefined
  const normalized = value.trim().toLowerCase()
  if (['true', '1', 'yes', 'y'].includes(normalized)) return true
  if (['false', '0', 'no', 'n'].includes(normalized)) return false
  return undefined
}

function numberValue(value) {
  if (value === undefined || value === null || value === '') return null
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}

function normalizeStatus(value) {
  const raw = typeof value === 'object' ? firstValue(value, 'status', 'state', 'phase') : value
  if (raw === undefined || raw === null || raw === '') return ''
  const normalized = String(raw).trim().toLowerCase().replace(/[\s-]+/g, '_')
  return statusAliases[normalized] || normalized
}

function normalizeBranches(branches, selectedBranch) {
  const values = Array.isArray(branches) ? branches : []
  const options = values.map((branch) => {
    if (typeof branch === 'string' || typeof branch === 'number') {
      const value = String(branch)
      return { value, label: value }
    }
    if (!branch || typeof branch !== 'object') return null
    const value = textValue(firstValue(branch, 'value', 'name', 'branch', 'ref', 'branch_name', 'branchName'))
    if (!value) return null
    const label = textValue(firstValue(branch, 'label', 'display_name', 'displayName', 'name', 'branch'), value)
    return { value, label }
  }).filter(Boolean)

  if (selectedBranch && !options.some((option) => option.value === selectedBranch)) {
    options.unshift({ value: selectedBranch, label: selectedBranch })
  }

  return options.filter((option, index, all) => all.findIndex((item) => item.value === option.value) === index)
}

function conflictPath(value, index) {
  if (typeof value === 'string' || typeof value === 'number') return String(value)
  if (!value || typeof value !== 'object') return `冲突文件 ${index + 1}`
  return textValue(firstValue(value, 'path', 'file_path', 'filePath', 'file', 'filename', 'name'), `冲突文件 ${index + 1}`)
}

function conflictResolved(value) {
  if (!value || typeof value !== 'object') return false
  return booleanValue(firstValue(value, 'resolved', 'is_resolved', 'isResolved')) === true
}

function conflictSideContent(value, side) {
  if (!value || typeof value !== 'object') return ''
  if (side === 'base') {
    return textValue(firstValue(
      value,
      'base_content',
      'baseContent',
      'target_content',
      'targetContent',
      'incoming_content',
      'incomingContent',
      'theirs',
      'base',
    ))
  }
  return textValue(firstValue(
    value,
    'source_content',
    'sourceContent',
    'head_content',
    'headContent',
    'current_content',
    'currentContent',
    'ours',
    'source',
  ))
}

function conflictContent(value) {
  if (!value || typeof value !== 'object') return ''
  const content = firstValue(value, 'content', 'merged_content', 'mergedContent', 'resolved_content', 'resolvedContent', 'resolution')
  if (content !== undefined) return textValue(content)
  return conflictSideContent(value, 'source')
}

function conflictReason(value) {
  if (!value || typeof value !== 'object') return ''
  return textValue(firstValue(value, 'reason', 'message', 'description'))
}

function rawConflictEntries(preparation) {
  const candidates = [preparation?.conflicts, preparation?.conflict_files, preparation?.conflictFiles]
  const raw = candidates.find((value) => Array.isArray(value) && value.length > 0)
    ?? candidates.find((value) => value && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length > 0)
    ?? candidates.find((value) => Array.isArray(value))

  if (Array.isArray(raw)) return raw
  if (!raw || typeof raw !== 'object') return []

  const nested = firstValue(raw, 'files', 'items', 'conflicts', 'conflict_files', 'conflictFiles')
  if (Array.isArray(nested)) return nested
  return Object.entries(raw).map(([path, content]) => ({ path, content }))
}

function normalizeConflictFiles(preparation) {
  return rawConflictEntries(preparation).map((entry, index) => ({
    key: `${conflictPath(entry, index)}-${index}`,
    path: conflictPath(entry, index),
    content: typeof entry === 'string' || typeof entry === 'number' ? '' : conflictContent(entry),
    sourceContent: typeof entry === 'string' || typeof entry === 'number' ? '' : conflictSideContent(entry, 'source'),
    baseContent: typeof entry === 'string' || typeof entry === 'number' ? '' : conflictSideContent(entry, 'base'),
    reason: typeof entry === 'string' || typeof entry === 'number' ? '' : conflictReason(entry),
    resolved: conflictResolved(entry),
  }))
}

function statusFrom(preparation, status, conflictFiles) {
  const explicit = normalizeStatus(status)
  if (explicit === 'loading') return explicit

  // A file list is authoritative: never show a green result while conflicts exist.
  if (conflictFiles.length) return 'conflict'
  if (explicit) return explicit

  const prepared = normalizeStatus(firstValue(preparation, 'status', 'state', 'phase'))
  if (prepared) return prepared
  const canPublish = booleanValue(firstValue(preparation, 'can_publish', 'canPublish'))
  return canPublish === true ? 'ready' : ''
}

function countText(value, suffix) {
  const number = numberValue(value)
  return number === null ? '-' : `${number}${suffix}`
}

function resolvePayload(files, editedFiles) {
  return files.map((file) => ({
    path: file.path,
    content: editedFiles[file.key] ?? file.content,
  }))
}

function hasConflictMarkers(content) {
  return /^\s*(?:<{7}|={7}|>{7})(?:\s|$)/m.test(content || '')
}

export default function ReleasePreparation({
  sourceBranch = '',
  baseBranch = '',
  onBaseBranchChange,
  branches = [],
  status,
  loading = false,
  preparation,
  onPrepare,
  onResolve,
}) {
  const prepared = useMemo(() => unwrapPreparation(preparation), [preparation])
  const preparedSourceBranch = textValue(firstValue(prepared, 'source_branch', 'sourceBranch'), textValue(sourceBranch))
  const preparedBaseBranch = textValue(firstValue(prepared, 'base_branch', 'baseBranch'), textValue(baseBranch))
  const selectedBaseBranch = textValue(baseBranch, preparedBaseBranch)
  const branchOptions = useMemo(() => normalizeBranches(branches, selectedBaseBranch), [branches, selectedBaseBranch])
  const conflictFiles = useMemo(() => normalizeConflictFiles(prepared), [prepared])
  const currentStatus = loading ? 'loading' : statusFrom(prepared, status, conflictFiles)
  const summary = textValue(firstValue(prepared, 'summary', 'message', 'detail', 'description'))
  const canPublish = booleanValue(firstValue(prepared, 'can_publish', 'canPublish'))
  const tempBranch = textValue(firstValue(prepared, 'temp_branch', 'tempBranch', 'temporary_branch', 'temporaryBranch'))
  const capabilities = firstValue(prepared, 'capabilities', 'git_capabilities', 'gitCapabilities') || {}
  const supportsConflictResolution = booleanValue(firstValue(
    prepared,
    'supports_conflict_resolution',
    'supportsConflictResolution',
    'can_resolve_conflicts',
    'canResolveConflicts',
  )) ?? booleanValue(firstValue(
    capabilities,
    'can_resolve_conflicts',
    'canResolveConflicts',
    'supports_conflict_resolution',
    'supportsConflictResolution',
  ))
  const readOnlyCapability = booleanValue(firstValue(prepared, 'read_only', 'readOnly'))
    ?? booleanValue(firstValue(capabilities, 'read_only', 'readOnly'))
  const canWriteGit = booleanValue(firstValue(
    prepared,
    'can_write_git',
    'canWriteGit',
    'can_write',
    'canWrite',
  )) ?? booleanValue(firstValue(capabilities, 'can_write_git', 'canWriteGit', 'can_write', 'canWrite'))
  // Require an explicit writable capability. A returned conflict list alone is never permission to write.
  const canPersistConflictResolution = supportsConflictResolution === true && canWriteGit === true && readOnlyCapability !== true
  const readOnlyMode = !canPersistConflictResolution
  const sourceAhead = firstValue(prepared, 'source_ahead', 'sourceAhead', 'ahead')
  const baseBehind = firstValue(prepared, 'base_behind', 'baseBehind', 'behind')
  const [editedFiles, setEditedFiles] = useState({})
  const [resolvedFiles, setResolvedFiles] = useState({})
  const [activeFileKey, setActiveFileKey] = useState('')
  const [previewComplete, setPreviewComplete] = useState(false)

  useEffect(() => {
    setEditedFiles((current) => {
      const next = {}
      conflictFiles.forEach((file) => {
        next[file.key] = hasOwn(current, file.key) ? current[file.key] : file.content
      })
      return next
    })
    setResolvedFiles((current) => {
      const next = {}
      conflictFiles.forEach((file) => {
        next[file.key] = hasOwn(current, file.key) ? current[file.key] : file.resolved
      })
      return next
    })
    setActiveFileKey((current) => conflictFiles.some((file) => file.key === current) ? current : conflictFiles[0]?.key || '')
    setPreviewComplete(false)
  }, [conflictFiles])

  const updateFile = (file, value) => {
    setEditedFiles((current) => ({ ...current, [file.key]: value }))
    setResolvedFiles((current) => ({ ...current, [file.key]: false }))
    setPreviewComplete(false)
  }

  const toggleFileResolved = (file) => {
    const content = editedFiles[file.key] ?? file.content
    const resolved = !resolvedFiles[file.key]
    setResolvedFiles((current) => ({ ...current, [file.key]: resolved }))
    onResolve?.({
      action: resolved ? 'resolve' : 'unresolve',
      file: file.path,
      content,
      readOnly: readOnlyMode,
      canWrite: canPersistConflictResolution,
    })
  }

  const finishConflictHandling = (files, allResolved) => {
    if (!allResolved) return
    if (readOnlyMode) {
      // Do not emit the legacy "continue" action here: App treats it as permission to publish.
      setPreviewComplete(true)
      onResolve?.({ action: 'preview-complete', files, readOnly: true, canWrite: false })
      return
    }
    onResolve?.({ action: 'continue', files, readOnly: false, canWrite: true })
  }

  const openConflictResolution = () => {
    onResolve?.({
      action: 'open',
      tempBranch,
      sourceBranch: preparedSourceBranch,
      baseBranch: preparedBaseBranch,
      canResolve: canPersistConflictResolution,
      readOnly: readOnlyMode,
    })
  }

  const renderContext = () => (
    <div className="release-preparation-context">
      <Typography.Text type="secondary">源分支</Typography.Text>
      <Typography.Text code>{preparedSourceBranch || '未选择'}</Typography.Text>
      <span>→</span>
      <Typography.Text type="secondary">基准分支</Typography.Text>
      <Typography.Text code>{selectedBaseBranch || '未选择'}</Typography.Text>
    </div>
  )

  const renderConflict = () => {
    if (!conflictFiles.length) {
      return (
        <Alert
          type="warning"
          showIcon
          message="接口声明有冲突，但没有返回冲突文件"
          description="当前只能查看检查结果，平台没有足够信息让你逐文件处理。请在 Git 仓库完成合并后重新检查。"
        />
      )
    }

    const resolvedCount = conflictFiles.filter((file) => resolvedFiles[file.key]).length
    const allResolved = resolvedCount === conflictFiles.length
    const editedCount = conflictFiles.filter((file) => (editedFiles[file.key] ?? file.content) !== file.content).length
    const activeFile = conflictFiles.find((file) => file.key === activeFileKey) || conflictFiles[0]
    const activeContent = editedFiles[activeFile.key] ?? activeFile.content
    const activeResolved = Boolean(resolvedFiles[activeFile.key])
    const activeHasMarkers = hasConflictMarkers(activeContent)
    const hasReferenceContent = Boolean(activeFile.sourceContent || activeFile.baseContent)

    return (
      <div className="conflict-resolution">
        <Alert
          type="error"
          showIcon
          icon={<WarningOutlined />}
          message={`发现 ${conflictFiles.length} 个冲突文件`}
          description={summary || '逐个查看文件，编辑合并后的最终内容，再标记处理状态。'}
        />

        <div className={`conflict-mode-notice ${readOnlyMode ? 'is-read-only' : 'is-writable'}`}>
          <div className="conflict-mode-icon">{readOnlyMode ? <LockOutlined /> : <CheckCircleOutlined />}</div>
          <div>
            <div className="conflict-mode-title">
              <Tag color={readOnlyMode ? 'warning' : 'success'}>{readOnlyMode ? '只读' : '可写'}</Tag>
              <Typography.Text strong>{readOnlyMode ? '当前账号不能写回仓库' : '平台已获得 Git 冲突写入能力'}</Typography.Text>
            </div>
            <Typography.Text type="secondary">
              {readOnlyMode
                ? '编辑内容和“已解决”标记只保存在当前弹窗，不会写回 Git、创建提交或解除发布拦截。请在代码仓库真正解决后重新检查。'
                : '标记全部文件后可以提交解决结果；最终是否允许发布仍以后端返回的检查结果为准。'}
            </Typography.Text>
          </div>
        </div>

        <div className="conflict-workspace">
          <div className="conflict-file-panel">
            <div className="conflict-file-panel-head">
              <Typography.Text strong>冲突文件</Typography.Text>
              <Typography.Text type="secondary">{resolvedCount}/{conflictFiles.length}</Typography.Text>
            </div>
            <div className="conflict-file-list" role="tablist" aria-label="冲突文件列表">
              {conflictFiles.map((file, index) => {
                const resolved = Boolean(resolvedFiles[file.key])
                return (
                  <button
                    key={file.key}
                    type="button"
                    role="tab"
                    aria-selected={file.key === activeFile.key}
                    className={`conflict-file-item ${file.key === activeFile.key ? 'is-active' : ''} ${resolved ? 'is-resolved' : ''}`}
                    onClick={() => setActiveFileKey(file.key)}
                  >
                    <span className="conflict-file-number">{resolved ? <CheckCircleOutlined /> : index + 1}</span>
                    <span className="conflict-file-path" title={file.path}>{file.path}</span>
                    <span className="conflict-file-state">{resolved ? (readOnlyMode ? '本地已标记' : '已解决') : '待处理'}</span>
                  </button>
                )
              })}
            </div>
          </div>

          <div className="conflict-editor-panel" role="tabpanel">
            <div className="conflict-editor-head">
              <div className="conflict-editor-title">
                <FileTextOutlined />
                <Typography.Text code title={activeFile.path}>{activeFile.path}</Typography.Text>
              </div>
              <Tag color={activeResolved ? 'success' : 'error'}>{activeResolved ? (readOnlyMode ? '本地已标记' : '已解决') : '待处理'}</Tag>
            </div>

            {activeFile.reason && <Typography.Text className="conflict-file-reason" type="secondary">冲突原因：{activeFile.reason}</Typography.Text>}

            <div className="conflict-editor-toolbar">
              <Typography.Text type="secondary">编辑合并后的最终文件内容</Typography.Text>
              <Space size={6} wrap>
                {activeFile.sourceContent && <Button size="small" onClick={() => updateFile(activeFile, activeFile.sourceContent)}>采用源分支</Button>}
                {activeFile.baseContent && <Button size="small" onClick={() => updateFile(activeFile, activeFile.baseContent)}>采用基准分支</Button>}
              </Space>
            </div>

            {hasReferenceContent && (
              <details className="conflict-reference">
                <summary><EyeOutlined /> 查看双方原始内容</summary>
                <div className="conflict-reference-grid">
                  <section>
                    <div>源分支 <Typography.Text code>{preparedSourceBranch || '未选择'}</Typography.Text></div>
                    <pre>{activeFile.sourceContent || '后端未返回源分支内容'}</pre>
                  </section>
                  <section>
                    <div>基准分支 <Typography.Text code>{selectedBaseBranch || '未选择'}</Typography.Text></div>
                    <pre>{activeFile.baseContent || '后端未返回基准分支内容'}</pre>
                  </section>
                </div>
              </details>
            )}

            <Input.TextArea
              className="conflict-content-editor"
              aria-label={`编辑冲突文件 ${activeFile.path}`}
              value={activeContent}
              onChange={(event) => updateFile(activeFile, event.target.value)}
              rows={12}
              spellCheck={false}
              placeholder="请输入解决冲突后的完整文件内容"
            />

            <div className="conflict-editor-footer">
              <div className="conflict-editor-state">
                {activeHasMarkers
                  ? <Typography.Text type="danger"><WarningOutlined /> 仍包含 Git 冲突标记，请先清理</Typography.Text>
                  : (activeContent !== activeFile.content
                    ? <Typography.Text type="secondary">内容已在本地修改，切换文件不会丢失</Typography.Text>
                    : <Typography.Text type="secondary">当前使用后端返回的初始内容</Typography.Text>)}
              </div>
              <Button
                type={activeResolved ? 'default' : 'primary'}
                icon={activeResolved ? <ReloadOutlined /> : <CheckCircleOutlined />}
                disabled={!activeResolved && activeHasMarkers}
                onClick={() => toggleFileResolved(activeFile)}
              >
                {activeResolved
                  ? (readOnlyMode ? '取消本地标记' : '改为待处理')
                  : (readOnlyMode ? '标记为本地已解决' : '标记已解决')}
              </Button>
            </div>
          </div>
        </div>

        <div className="conflict-resolution-footer">
          <div>
            <Typography.Text strong>已处理 {resolvedCount}/{conflictFiles.length} 个文件</Typography.Text>
            <Typography.Text type="secondary">，本地修改 {editedCount} 个</Typography.Text>
          </div>
          <Space wrap>
            {readOnlyMode && (
              <Button
                icon={<CheckCircleOutlined />}
                disabled={!allResolved || previewComplete}
                onClick={() => finishConflictHandling(resolvePayload(conflictFiles, editedFiles), allResolved)}
              >
                {previewComplete ? '已处理' : '标记为已处理'}
              </Button>
            )}
            <Button
              type="primary"
              icon={readOnlyMode ? <ReloadOutlined /> : <CheckCircleOutlined />}
              disabled={readOnlyMode ? false : !allResolved}
              onClick={readOnlyMode
                ? () => onPrepare?.(selectedBaseBranch)
                : () => finishConflictHandling(resolvePayload(conflictFiles, editedFiles), allResolved)}
            >
              {readOnlyMode ? '重新检查仓库' : '提交解决结果'}
            </Button>
          </Space>
        </div>

        {readOnlyMode && previewComplete && (
          <Alert
            type="info"
            showIcon
            message="本地处理已完成，发布仍保持锁定"
            description="这些编辑和标记没有保存到 Git。请在代码仓库完成真实合并，再点击“重新检查仓库”。"
          />
        )}
      </div>
    )
  }

  const renderResult = () => {
    if (currentStatus === 'loading') {
      return (
        <div className="release-preparation-loading">
          <Spin indicator={<LoadingOutlined spin />} />
          <Typography.Text>正在检查分支关系，请稍候...</Typography.Text>
        </div>
      )
    }

    if (!currentStatus) {
      return (
        <div className="release-preparation-empty">
          <FileSearchOutlined />
          <Typography.Text type="secondary">选择基准分支后点击“检查”</Typography.Text>
        </div>
      )
    }

    if (currentStatus === 'ready') {
      return (
        <Alert
          className="release-preparation-status-alert"
          type={canPublish === false ? 'warning' : 'success'}
          showIcon
          icon={canPublish === false ? <ExclamationCircleOutlined /> : <CheckCircleOutlined />}
          message={canPublish === false ? '检查完成，但当前版本暂不可直接发布' : '检查通过，可以发布'}
          description={summary || '源分支与基准分支状态兼容，不需要额外合并。'}
        />
      )
    }

    if (currentStatus === 'needs_merge') {
      return (
        <div className="release-preparation-result">
          <Alert className="release-preparation-status-alert" type="warning" showIcon icon={<MergeCellsOutlined />} message="需要先合并基准分支" description={summary || '源分支和基准分支存在差异，请先完成合并后再发布。'} />
          <div className="release-preparation-stats">
            <div>
              <Typography.Text type="secondary">源分支独有提交</Typography.Text>
              <div><Typography.Text strong>{countText(sourceAhead, ' 个提交')}</Typography.Text></div>
            </div>
            <div>
              <Typography.Text type="secondary">基准分支待合入提交</Typography.Text>
              <div><Typography.Text strong>{countText(baseBehind, ' 个提交')}</Typography.Text></div>
            </div>
          </div>
          <div className="release-preparation-followup">
            <div>
              <Typography.Text type="secondary">临时发布分支：{tempBranch ? <Typography.Text code copyable>{tempBranch}</Typography.Text> : '暂未生成'}</Typography.Text>
            </div>
            <Button type={canPersistConflictResolution ? 'primary' : 'default'} icon={<MergeCellsOutlined />} onClick={openConflictResolution}>
              {canPersistConflictResolution ? '开始合并并检查冲突' : '查看处理说明'}
            </Button>
          </div>
        </div>
      )
    }

    if (currentStatus === 'conflict') return renderConflict()

    if (currentStatus === 'unsupported') {
      return <Alert className="release-preparation-status-alert" type="warning" showIcon icon={<ExclamationCircleOutlined />} message="暂不支持自动代码检查" description={summary || '当前仓库或分支类型不支持自动合并，请确认仓库配置后重试。'} />
    }

    return <Alert className="release-preparation-status-alert" type="info" showIcon message={`检查状态：${currentStatus}`} description={summary || '暂时无法识别该检查状态，请重新检查。'} />
  }

  return (
    <div className="release-preparation">
      <div className="release-preparation-header">
        <div className="release-preparation-heading">
          <Typography.Text strong>发布前代码检查</Typography.Text>
          {renderContext()}
        </div>
        <Space.Compact>
          <Select
            aria-label="选择基准分支"
            value={selectedBaseBranch || undefined}
            onChange={onBaseBranchChange}
            options={branchOptions}
            placeholder="选择基准分支"
            style={{ minWidth: 190 }}
            disabled={loading}
            showSearch
            optionFilterProp="label"
          />
          <Button
            type="primary"
            icon={<ReloadOutlined />}
            loading={loading}
            disabled={!preparedSourceBranch || !selectedBaseBranch}
            onClick={() => onPrepare?.(selectedBaseBranch)}
          >
            检查
          </Button>
        </Space.Compact>
      </div>
      <Divider className="release-preparation-divider" style={{ margin: '2px 0 0' }} />
      {renderResult()}
    </div>
  )
}
