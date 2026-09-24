import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, Input, Pagination, Select, Space, Spin, Tag } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { getPodLogs } from '../services/api'

const emptyPod = {}
const LOG_PAGE_SIZE = 50

function logLevel(line) {
  if (/\b(error|fatal|panic|critical|crit)\b/i.test(line)) return 'ERROR'
  if (/\b(warn|warning)\b/i.test(line)) return 'WARN'
  if (/\b(info|notice)\b/i.test(line)) return 'INFO'
  if (/\b(debug|trace)\b/i.test(line)) return 'DEBUG'
  return 'OTHER'
}

function parseLogs(value) {
  const lines = String(value || '').split(/\r?\n/)
  if (lines[lines.length - 1] === '') lines.pop()
  return lines.map((line, index) => ({ id: index + 1, line, level: logLevel(line) }))
}

export default function PodDrawer({ project, pod: podValue, targetId = '', open, onClose, onPodNotFound }) {
  const pod = podValue || emptyPod
  const [logs, setLogs] = useState('')
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [keyword, setKeyword] = useState('')
  const [level, setLevel] = useState('ALL')
  const [page, setPage] = useState(1)

  useEffect(() => {
    if (!open || !pod) {
      setLoading(false)
      setLoadError('')
      setLogs('')
      return
    }
    let active = true
    setLoading(true)
    setLoadError('')
    setKeyword('')
    setLevel('ALL')
    setPage(1)

    const load = async () => {
      try {
        const nextLogs = await getPodLogs(project.id, pod.name, pod.container, targetId, 0)
        if (!active) return
        setLogs(nextLogs || '')
      } catch (error) {
        if (!active) return
        setLogs('')
        if (error?.code === 'not_found') {
          onPodNotFound?.()
          setLoadError('Pod 已完成滚动更新，运行态列表正在刷新')
        } else {
          setLoadError(error?.message || 'Pod 详情和日志加载失败，请稍后重试')
        }
      } finally {
        if (active) setLoading(false)
      }
    }

    load()
    return () => { active = false }
  }, [open, pod, project, targetId])

  const parsedLogs = useMemo(() => parseLogs(logs), [logs])
  const filteredLogs = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    return parsedLogs.filter((item) => {
      if (level !== 'ALL' && item.level !== level) return false
      return !query || item.line.toLowerCase().includes(query)
    })
  }, [keyword, level, parsedLogs])
  const pageCount = Math.max(1, Math.ceil(filteredLogs.length / LOG_PAGE_SIZE))
  const visibleLogs = filteredLogs.slice((page - 1) * LOG_PAGE_SIZE, page * LOG_PAGE_SIZE)

  useEffect(() => {
    setPage(1)
  }, [keyword, level])

  useEffect(() => {
    if (page > pageCount) setPage(pageCount)
  }, [page, pageCount])

  const refreshLogs = async () => {
    if (!project?.id || !pod?.name) return
    setLoading(true)
    setLoadError('')
    try {
      const nextLogs = await getPodLogs(project.id, pod.name, pod.container, targetId, 0)
      setLogs(nextLogs || '')
      setPage(1)
    } catch (error) {
      if (error?.code === 'not_found') {
        onPodNotFound?.()
        setLoadError('Pod 已完成滚动更新，运行态列表正在刷新')
      } else {
        setLoadError(error?.message || 'Pod 日志加载失败，请稍后重试')
      }
    } finally {
      setLoading(false)
    }
  }

  return <Drawer title={<div className="pod-drawer-title"><span>{pod?.name}</span><Tag color="green">运行中</Tag></div>} width={700} open={open} onClose={onClose} destroyOnHidden>
    {loading && !logs ? <div className="drawer-loading"><Spin /></div> : loadError ? <Alert type="error" showIcon message="Pod 日志加载失败" description={loadError} /> : <section className="pod-log-viewer">
      <div className="pod-log-toolbar">
        <Space.Compact className="pod-log-filters">
          <Input allowClear prefix={<SearchOutlined />} placeholder="查询日志内容" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
          <Select value={level} onChange={setLevel} options={[{ value: 'ALL', label: '全部级别' }, { value: 'ERROR', label: 'ERROR' }, { value: 'WARN', label: 'WARN' }, { value: 'INFO', label: 'INFO' }, { value: 'DEBUG', label: 'DEBUG' }, { value: 'OTHER', label: '其他' }]} />
        </Space.Compact>
        <Button icon={<ReloadOutlined />} loading={loading} onClick={refreshLogs}>刷新</Button>
        <span className="pod-log-count">{filteredLogs.length} / {parsedLogs.length} 行</span>
      </div>
      <div className="pod-logs" role="log" aria-label="Pod 日志">
        {visibleLogs.length ? visibleLogs.map((item) => <div className={`pod-log-line ${item.level.toLowerCase()}`} key={item.id}><span>{item.id}</span><code>{item.line || ' '}</code></div>) : <div className="pod-log-empty">{parsedLogs.length ? '没有匹配的日志' : '暂无日志'}</div>}
      </div>
      {filteredLogs.length > 0 && <div className="pod-log-pagination"><Pagination current={page} pageSize={LOG_PAGE_SIZE} total={filteredLogs.length} showSizeChanger={false} showQuickJumper size="small" onChange={setPage} /></div>}
    </section>}
  </Drawer>
}
