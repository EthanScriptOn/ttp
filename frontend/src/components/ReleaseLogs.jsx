import { useMemo, useState } from 'react'
import './ReleaseLogs.css'

const LOG_LEVELS = ['全部级别', 'INFO', 'WARN', 'ERROR']

const SUMMARY_STEPS = [
  { key: 'source', title: '读取代码', match: (line) => /git (fetch|checkout|rev-parse|diff)/i.test(line) },
  { key: 'batch', title: '合入批次分支', match: (line) => /(merge|batch|批次)/i.test(line) },
  { key: 'build', title: '构建镜像', match: (line, entry) => entry.source === 'builder' || /(build image|successfully built)/i.test(line) },
  { key: 'registry', title: '推送镜像', match: (line, entry) => entry.source === 'registry' || /\b(push|pushed|digest)\b/i.test(line) },
  { key: 'k8s', title: '更新 Kubernetes', match: (line) => /\b(apply|configured|resourceversion|generation)\b/i.test(line) },
  { key: 'pod', title: '等待 Pod 就绪', match: (line) => /\b(pod\/|availablereplicas|rollout status|rollout complete|successfully rolled out|ready=true|\bready$)/i.test(line) },
]

function asText(value) {
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function firstValue(value, keys) {
  for (const key of keys) {
    const candidate = value?.[key]
    if (candidate !== undefined && candidate !== null) return candidate
  }
  return ''
}

function logLines(value) {
  if (typeof value === 'string') return value.split(/\r?\n/).filter((line) => line.length > 0)
  if (Array.isArray(value)) return value.flatMap((item) => logLines(item))
  if (value && typeof value === 'object') {
    const nested = firstValue(value, ['lines', 'entries', 'items', 'logs'])
    if (nested) return logLines(nested)
    const line = firstValue(value, ['line', 'message', 'text', 'output', 'content'])
    return asText(line) ? asText(line).split(/\r?\n/).filter((item) => item.length > 0).map((item) => ({ ...value, line: item })) : []
  }
  return []
}

function formatTime(value) {
  if (!value) return '--:--:--.---'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return asText(value)
  return `${date.toLocaleTimeString('zh-CN', { hour12: false })}.${String(date.getMilliseconds()).padStart(3, '0')}`
}

function levelOf(value) {
  const level = asText(firstValue(value, ['level', 'log_level', 'logLevel'])).toUpperCase()
  if (['ERROR', 'ERR', 'FATAL'].includes(level)) return 'ERROR'
  if (['WARN', 'WARNING'].includes(level)) return 'WARN'
  return 'INFO'
}

export function normalizeExecutionLogs(value) {
  return logLines(value).map((item, index) => {
    const entry = typeof item === 'string' ? { line: item } : item
    const line = asText(firstValue(entry, ['line', 'message', 'text', 'output', 'content']))
    if (!line) return null
    return {
      id: asText(firstValue(entry, ['id', 'event_id', 'eventId'])) || `log-${index}`,
      timestamp: formatTime(firstValue(entry, ['timestamp', 'time', 'created_at', 'createdAt'])),
      source: asText(firstValue(entry, ['source', 'component', 'service', 'step'])) || 'executor',
      stream: asText(firstValue(entry, ['stream', 'channel'])) || (levelOf(entry) === 'ERROR' ? 'stderr' : 'stdout'),
      level: levelOf(entry),
      line,
    }
  }).filter(Boolean)
}

export function executionLogsForTarget(target, release) {
  const targetLogs = firstValue(target, ['logs', 'execution_logs', 'executionLogs', 'raw_logs', 'rawLogs', 'output'])
  if (targetLogs) return normalizeExecutionLogs(targetLogs)
  const releaseLogs = firstValue(release, ['execution_logs', 'executionLogs', 'logs'])
  return releaseLogs ? normalizeExecutionLogs(releaseLogs) : []
}

function summaryDetail(step, entries) {
  const lines = entries.map((entry) => entry.line)
  const last = lines[lines.length - 1] || ''
  if (step.key === 'source') {
    const branch = last.match(/refs\/heads\/([^\s]+)/i)?.[1] || last.match(/checkout\s+([^\s]+)/i)?.[1]
    const sha = last.match(/(?:HEAD|commit)\s*(?:-?>|=)?\s*([0-9a-f]{7,})/i)?.[1]
    return [branch && `分支 ${branch}`, sha && `版本 ${sha.slice(0, 10)}`].filter(Boolean).join(' · ') || '已读取代码仓库和当前分支'
  }
  if (step.key === 'build') return last.replace(/^.*?Successfully built\s+/i, '镜像 ').replace(/^.*?build image\s+/i, '镜像 ') || '镜像构建完成'
  if (step.key === 'registry') return /digest/i.test(last) ? '镜像已推送，已取得镜像摘要' : '镜像已推送到镜像仓库'
  if (step.key === 'k8s') return last.replace(/^.*?apply\s+/i, '已应用 ').replace(/^.*?(deployment\.apps\/[^\s]+) configured.*$/i, '$1 已更新') || '已应用 Kubernetes 配置'
  if (step.key === 'pod') return last.replace(/^.*?(deployment\/[^:]+:\s*)/i, '').replace(/^.*?pod\//i, 'Pod ') || '正在等待 Pod 通过就绪检查'
  if (step.key === 'batch') return last.replace(/^.*?(merge|合入)\s*/i, '已合入 ') || '代码已进入当前批次'
  return last
}

function summarizeExecutionLogs(normalized) {
  const result = []
  const consumed = new Set()
  SUMMARY_STEPS.forEach((step) => {
    const entries = normalized.filter((entry, index) => {
      if (consumed.has(index)) return false
      if (!step.match(entry.line, entry)) return false
      consumed.add(index)
      return true
    })
    if (!entries.length) return
    const failed = entries.some((entry) => entry.level === 'ERROR')
    result.push({
      id: `summary-${step.key}-${entries[0].id}`,
      title: step.title,
      detail: summaryDetail(step, entries),
      timestamp: entries[0].timestamp,
      state: failed ? 'failed' : 'done',
    })
  })
  const errors = normalized.filter((entry) => entry.level === 'ERROR')
  if (errors.length && !result.some((item) => item.state === 'failed')) {
    result.push({ id: `summary-error-${errors[0].id}`, title: '执行异常', detail: errors[errors.length - 1].line, timestamp: errors[0].timestamp, state: 'failed' })
  }
  if (result.length && !normalized.some((entry) => /rollout complete|successfully rolled out|release target=.*completed/i.test(entry.line)) && !result.some((item) => item.state === 'failed')) {
    result[result.length - 1].state = 'active'
  }
  return result
}

export function ReleaseLogTerminal({ logs = [], emptyText = '尚未产生执行输出' }) {
  const [search, setSearch] = useState('')
  const [level, setLevel] = useState('全部级别')
  const [view, setView] = useState('summary')
  const normalized = useMemo(() => normalizeExecutionLogs(logs), [logs])
  const summary = useMemo(() => summarizeExecutionLogs(normalized), [normalized])
  const visible = useMemo(() => {
    const term = search.trim().toLowerCase()
    return normalized.filter((item) => (level === '全部级别' || item.level === level) && (!term || `${item.source} ${item.stream} ${item.line}`.toLowerCase().includes(term)))
  }, [normalized, search, level])
  const visibleSummary = useMemo(() => {
    const term = search.trim().toLowerCase()
    return summary.filter((item) => {
      const levelMatches = level === '全部级别' || (level === 'ERROR' && item.state === 'failed') || (level !== 'ERROR' && item.state !== 'failed')
      return levelMatches && (!term || `${item.title} ${item.detail}`.toLowerCase().includes(term))
    })
  }, [summary, search, level])

  return <div className="release-log-terminal">
    <div className="release-log-toolbar">
      <div className="release-log-toolbar-title"><span className="release-log-live-dot" /><strong>{view === 'summary' ? '执行概览' : '原始输出'}</strong><span>{view === 'summary' ? `${visibleSummary.length} / ${summary.length} 步骤` : `${visible.length} / ${normalized.length} 行`}</span></div>
      <div className="release-log-view-tabs" role="tablist" aria-label="日志视图"><button type="button" className={view === 'summary' ? 'active' : ''} onClick={() => setView('summary')}>执行概览</button><button type="button" className={view === 'raw' ? 'active' : ''} onClick={() => setView('raw')}>原始日志</button></div>
      <div className="release-log-toolbar-tools"><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索日志内容" aria-label="搜索日志内容" /><select value={level} onChange={(event) => setLevel(event.target.value)} aria-label="筛选日志级别">{LOG_LEVELS.map((item) => <option key={item}>{item}</option>)}</select></div>
    </div>
    {view === 'summary' ? <div className="release-log-summary-body" role="list" aria-label="执行概览">{visibleSummary.length ? visibleSummary.map((item) => <div className={`release-log-summary-line ${item.state}`} key={item.id}><span className="release-log-summary-marker">{item.state === 'failed' ? '!' : item.state === 'active' ? '…' : '✓'}</span><div><strong>{item.title}</strong><span>{item.detail}</span></div><time>{item.timestamp}</time><b>{item.state === 'failed' ? '失败' : item.state === 'active' ? '进行中' : '已完成'}</b></div>) : <div className="release-log-empty">{summary.length ? '没有匹配的步骤' : emptyText}</div>}</div> : <div className="release-log-terminal-body" role="log" aria-live="polite">{visible.length ? visible.map((item) => <div className={`release-log-line ${item.level.toLowerCase()}`} key={item.id}><time>{item.timestamp}</time><span className="release-log-source">{item.source}</span><span className="release-log-level">{item.level}</span><code>{item.line}</code></div>) : <div className="release-log-empty">{normalized.length ? '没有匹配的日志' : emptyText}</div>}</div>}
  </div>
}
