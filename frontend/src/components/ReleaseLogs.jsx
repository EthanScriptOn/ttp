import { useEffect, useMemo, useRef, useState } from 'react'
import { sourceForExecutionLog, summarizeExecutionLogs } from '../services/release-log-summary'
import './ReleaseLogs.css'

const LOG_LEVELS = ['全部级别', 'INFO', 'WARN', 'ERROR']

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
    const source = asText(firstValue(entry, ['source', 'component', 'service', 'step'])) || 'executor'
    return {
      id: asText(firstValue(entry, ['id', 'event_id', 'eventId'])) || `log-${index}`,
      timestamp: formatTime(firstValue(entry, ['timestamp', 'time', 'created_at', 'createdAt'])),
      source: sourceForExecutionLog(source, line),
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

export function ReleaseLogTerminal({ logs = [], emptyText = '尚未产生执行输出' }) {
  const [search, setSearch] = useState('')
  const [level, setLevel] = useState('全部级别')
  const [view, setView] = useState('summary')
  const rawBodyRef = useRef(null)
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

  useEffect(() => {
    if (view !== 'raw' || !rawBodyRef.current || !normalized.length) return
    rawBodyRef.current.scrollTop = rawBodyRef.current.scrollHeight
  }, [normalized.length, view])

  return <div className={`release-log-terminal is-linux ${view === 'raw' ? 'is-raw' : 'is-summary'}`}>
    <div className="release-log-toolbar">
      <div className="release-log-toolbar-title"><span className="release-log-live-dot" /><strong>{view === 'summary' ? '执行概览' : '原始输出'}</strong><span>{view === 'summary' ? `${visibleSummary.length} / ${summary.length} 步骤` : `${visible.length} / ${normalized.length} 行`}</span></div>
      <div className="release-log-view-tabs" role="tablist" aria-label="日志视图"><button type="button" className={view === 'summary' ? 'active' : ''} onClick={() => setView('summary')}>执行概览</button><button type="button" className={view === 'raw' ? 'active' : ''} onClick={() => setView('raw')}>原始日志</button></div>
      <div className="release-log-toolbar-tools"><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索日志内容" aria-label="搜索日志内容" /><span className="release-log-level-select"><select value={level} onChange={(event) => setLevel(event.target.value)} aria-label="筛选日志级别">{LOG_LEVELS.map((item) => <option key={item}>{item}</option>)}</select></span></div>
    </div>
    {view === 'summary' ? <div className="release-log-summary-body" role="list" aria-label="执行概览">{visibleSummary.length ? visibleSummary.map((item) => <div className={`release-log-summary-line ${item.state}`} key={item.id}><span className="release-log-summary-marker">{item.state === 'failed' ? '!' : item.state === 'active' ? '…' : '✓'}</span><div><strong>{item.title}</strong><span>{item.detail}</span></div><time>{item.timestamp}</time><b>{item.state === 'failed' ? '失败' : item.state === 'active' ? '进行中' : '已完成'}</b></div>) : <div className="release-log-empty">{summary.length ? '没有匹配的步骤' : emptyText}</div>}</div> : <div ref={rawBodyRef} className="release-log-terminal-body" role="log" aria-live="polite">{visible.length ? visible.map((item) => <div className={`release-log-line ${item.level.toLowerCase()}`} key={item.id}><time>{item.timestamp}</time><span className="release-log-source">{item.source}</span><span className="release-log-level">{item.level}</span><code>{item.line}</code></div>) : <div className="release-log-empty">{normalized.length ? '没有匹配的日志' : emptyText}</div>}</div>}
  </div>
}
