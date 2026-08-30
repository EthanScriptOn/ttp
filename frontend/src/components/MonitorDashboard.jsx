import { Alert, Button, Card, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import { CheckCircleOutlined, ClusterOutlined, DeploymentUnitOutlined, ReloadOutlined, WarningOutlined } from '@ant-design/icons'
import { useEffect, useMemo, useRef, useState } from 'react'
import { DeploymentTargetSelect } from './DeploymentTargets'
import MetricChart from './MetricChart'

function phaseLabel(phase) {
  return ({
    Pending: '等待中',
    Running: '运行中',
    Succeeded: '已完成',
    Failed: '失败',
    Unknown: '未知',
  })[phase] || phase || '未知'
}

function phaseColor(phase, ready) {
  if (ready) return 'green'
  if (phase === 'Failed') return 'red'
  if (phase === 'Pending') return 'orange'
  return 'default'
}

function podTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', { hour12: false })
}

function formatCPU(pod) {
  return pod.metrics_available ? `${Number(pod.cpu_millicores || 0).toLocaleString()}m` : '-'
}

function formatMemory(pod) {
  if (!pod.metrics_available) return '-'
  const bytes = Number(pod.memory_bytes || 0)
  const gibibytes = bytes / (1024 ** 3)
  if (gibibytes >= 1) return `${gibibytes.toFixed(gibibytes >= 10 ? 1 : 2)} GiB`
  return `${(bytes / (1024 ** 2)).toFixed(1)} MiB`
}

function PodMetric({ label, value, tone = 'normal' }) {
  return <div className={`monitor-overview-metric status-${tone}`}>
    <span>{label}</span>
    <strong>{value}</strong>
  </div>
}

const podMetricLines = [
  { key: 'total', label: 'Pod 总数', color: '#167c72' },
  { key: 'ready', label: '已就绪', color: '#3a9b69' },
  { key: 'running', label: '运行中', color: '#397db5' },
  { key: 'pending', label: '等待中', color: '#d28a32' },
  { key: 'failed', label: '失败', color: '#c85c5c' },
  { key: 'restarts', label: '重启次数', color: '#8968b5' },
]

export default function MonitorDashboard({ scope = 'project', pods = [], project, cluster, targets = [], selectedTargetId, onTargetChange, onRefresh, onOpenPod, onOpenCluster, error = '' }) {
  const [autoRefresh, setAutoRefresh] = useState(false)
  const isCluster = scope === 'cluster'
  const selectedTarget = targets.find((item) => item.id === selectedTargetId) || targets.find((item) => item.enabled !== false) || targets[0]
  const targetName = isCluster ? (cluster?.name || '当前集群') : (project?.name || '当前项目')
  const podItems = useMemo(() => (Array.isArray(pods) ? pods : []).filter((pod) => pod?.name), [pods])
  const runningPods = podItems.filter((pod) => String(pod.phase).toLowerCase() === 'running').length
  const readyPods = podItems.filter((pod) => pod.ready).length
  const pendingPods = podItems.filter((pod) => String(pod.phase).toLowerCase() === 'pending').length
  const failedPods = podItems.filter((pod) => ['failed', 'error'].includes(String(pod.phase).toLowerCase())).length
  const restartCount = podItems.reduce((total, pod) => total + Number(pod.restart_count ?? pod.restarts ?? 0), 0)
  const hasIssues = pendingPods > 0 || failedPods > 0 || readyPods < podItems.length
  const statusState = error ? 'unknown' : !podItems.length ? 'unknown' : hasIssues ? 'warning' : 'healthy'
  const statusLabel = error ? '读取失败' : statusState === 'healthy' ? '运行正常' : statusState === 'warning' ? '存在异常' : '暂无 Pod'
  const [history, setHistory] = useState([])
  const previousTargetRef = useRef(selectedTarget?.id)
  const lastSnapshotRef = useRef(null)

  useEffect(() => {
    if (previousTargetRef.current !== selectedTarget?.id) {
      previousTargetRef.current = selectedTarget?.id
      lastSnapshotRef.current = null
      setHistory([])
      return
    }
    if (!podItems.length || error) return
    const snapshot = {
      timestamp: Date.now(),
      total: podItems.length,
      ready: readyPods,
      running: runningPods,
      pending: pendingPods,
      failed: failedPods,
      restarts: restartCount,
    }
    const previous = lastSnapshotRef.current
    const sameValues = previous && Object.keys(snapshot).every((key) => key === 'timestamp' || snapshot[key] === previous[key])
    if (sameValues && snapshot.timestamp - previous.timestamp < 1000) return
    lastSnapshotRef.current = snapshot
    setHistory((current) => [...current, snapshot].slice(-120))
  }, [error, failedPods, pendingPods, podItems, readyPods, restartCount, runningPods, selectedTarget?.id])

  useEffect(() => {
    if (!autoRefresh || typeof onRefresh !== 'function') return undefined
    const timer = window.setInterval(onRefresh, 30000)
    return () => window.clearInterval(timer)
  }, [autoRefresh, onRefresh])

  const columns = [
    {
      title: 'Pod',
      dataIndex: 'name',
      ellipsis: true,
      render: (value, pod) => onOpenPod
        ? <Button type="link" className="pod-link" onClick={() => onOpenPod(pod)}>{value}</Button>
        : value,
    },
    { title: '状态', dataIndex: 'phase', width: 92, render: (value, pod) => <Tag color={phaseColor(value, pod.ready)}>{pod.ready ? '就绪' : phaseLabel(value)}</Tag> },
    { title: '命名空间', dataIndex: 'namespace', width: 120, ellipsis: true },
    { title: 'Pod IP', dataIndex: 'pod_ip', width: 128, render: (value) => value || '-' },
    { title: '节点', dataIndex: 'node_name', width: 190, ellipsis: true, render: (value) => value || '-' },
    { title: 'CPU', dataIndex: 'cpu_millicores', width: 90, render: (_, pod) => formatCPU(pod) },
    { title: '内存', dataIndex: 'memory_bytes', width: 100, render: (_, pod) => formatMemory(pod) },
    { title: <Tooltip title="需要接入 Prometheus/cAdvisor 才能读取 Pod 磁盘使用量">磁盘</Tooltip>, width: 82, render: () => <Typography.Text type="secondary">未接入</Typography.Text> },
    { title: '重启次数', dataIndex: 'restart_count', width: 90, render: (value, pod) => value ?? pod.restarts ?? 0 },
    { title: '启动时间', dataIndex: 'started_at', width: 160, render: podTime },
  ]

  return <div className={`monitor-dashboard ${isCluster ? 'is-cluster' : 'is-project'}`}>
    <div className="monitor-toolbar">
      <div className="monitor-heading-copy">
        <Typography.Title level={4}>{isCluster ? '集群 Pod' : '项目 Pod'}</Typography.Title>
        <Typography.Text type="secondary">{targetName}{!isCluster && selectedTarget?.name ? ` · ${selectedTarget.name}` : ''}</Typography.Text>
      </div>
      <Space wrap className="monitor-toolbar-actions">
        {!isCluster && onOpenCluster && <Button icon={<ClusterOutlined />} onClick={onOpenCluster}>查看集群详情</Button>}
        {!isCluster && targets.length > 0 && <DeploymentTargetSelect className="monitor-target-select" targets={targets} value={selectedTarget?.id} onChange={onTargetChange} disabled={typeof onTargetChange !== 'function'} />}
        <span className="monitor-auto-refresh"><span>自动刷新</span><Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} /></span>
        <Button icon={<ReloadOutlined />} onClick={onRefresh}>刷新</Button>
      </Space>
    </div>

    {error && <Alert className="monitor-source-alert" type="error" showIcon message="无法读取真实 Pod" description={error} />}

    <section className={`monitor-status-overview status-state-${statusState}`}>
      <div className="monitor-status-summary">
        <div className="monitor-status-mark">{statusState === 'healthy' ? <CheckCircleOutlined /> : <WarningOutlined />}</div>
        <div className="monitor-status-copy"><span>运行状态</span><strong>{statusLabel}</strong></div>
      </div>
      <div className="monitor-overview-content">
        <div className="monitor-overview-metrics">
          <PodMetric label="Pod" value={podItems.length} tone={podItems.length ? 'good' : 'normal'} />
          <PodMetric label="已就绪" value={readyPods} tone={readyPods === podItems.length && podItems.length ? 'good' : 'warning'} />
          <PodMetric label="异常" value={pendingPods + failedPods} tone={pendingPods + failedPods ? 'danger' : 'good'} />
          <PodMetric label="重启" value={restartCount} tone={restartCount ? 'warning' : 'good'} />
        </div>
      </div>
    </section>

    <MetricChart title="Pod 趋势" series={history} lines={podMetricLines} emptyText="刷新后开始记录" />

    <Card bordered={false} className="monitor-detail-card monitor-pod-card">
      <div className="monitor-card-heading">
        <div><Typography.Title level={5}>Pod 列表</Typography.Title><Typography.Text type="secondary">数据来自当前 Kubernetes 环境</Typography.Text></div>
        <DeploymentUnitOutlined className="monitor-heading-icon" />
      </div>
      <Table className="monitor-pod-table" rowKey="name" size="small" columns={columns} dataSource={podItems} pagination={false} scroll={{ x: 1160 }} locale={{ emptyText: error ? '暂时无法读取 Pod' : '当前环境没有 Pod' }} />
    </Card>
  </div>
}
