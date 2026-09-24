import { Alert, Button, Card, DatePicker, Select, Space, Switch, Table, Tag, Typography } from 'antd'
import { CheckCircleOutlined, ReloadOutlined, WarningOutlined } from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import MetricChart from './MetricChart'
import { DeploymentTargetSelect } from './DeploymentTargets'

const RANGE_OPTIONS = [
  { value: '15m', label: '近 15 分钟', duration: 15 * 60 * 1000 },
  { value: '1h', label: '近 1 小时', duration: 60 * 60 * 1000 },
  { value: '6h', label: '近 6 小时', duration: 6 * 60 * 60 * 1000 },
  { value: '24h', label: '近 24 小时', duration: 24 * 60 * 60 * 1000 },
  { value: 'custom', label: '自定义时间' },
]

const COLORS = {
  primary: '#07a957',
  teal: '#2f9d6b',
  orange: '#c4862d',
  red: '#c65a5a',
  purple: '#7a62b8',
  cyan: '#3277b8',
}

function isNumber(value) {
  return typeof value === 'number' && Number.isFinite(value)
}

function formatMetric(value, unit = '', precision = 1) {
  if (!isNumber(value)) return '暂无'
  return `${value.toLocaleString('zh-CN', { maximumFractionDigits: precision, minimumFractionDigits: precision })}${unit}`
}

function integerMetric(value, unit = '') {
  if (!isNumber(value)) return '暂无'
  return `${Math.round(value).toLocaleString('zh-CN')}${unit}`
}

function countFromPods(pods, predicate) {
  return Array.isArray(pods) ? pods.filter(predicate).length : 0
}

function restartCountFromPods(pods) {
  return Array.isArray(pods) ? pods.reduce((total, pod) => total + Number(pod.restart_count ?? pod.restarts ?? 0), 0) : 0
}

function OverviewMetric({ label, value, note, tone = 'normal' }) {
  return <div className={`monitor-overview-metric status-${tone}`}>
    <span>{label}</span>
    <strong>{value}</strong>
    {note && <small>{note}</small>}
  </div>
}

function NodeValue({ value, unit = '%', precision = 1, available = true }) {
  return available && isNumber(value) ? formatMetric(value, unit, precision) : '暂无'
}

export default function MonitorDashboard({ scope = 'project', metrics, pods = [], project, cluster, targets = [], selectedTargetId, onTargetChange, onRefresh, onOpenCluster, onMonitoringAction, monitoringActionLabel = '查看安装状态', focusPod, onClearPod }) {
  const [range, setRange] = useState('1h')
  const [customRange, setCustomRange] = useState(null)
  const [autoRefresh, setAutoRefresh] = useState(false)
  const isCluster = scope === 'cluster'
  const selectedTarget = targets.find((item) => item.id === selectedTargetId) || targets[0]
  const targetClusterID = selectedTarget?.cluster_id || project?.cluster_id || metrics?.cluster_id || '未配置集群'
  const targetNamespace = selectedTarget?.namespace || project?.namespace || '未配置 namespace'
  const targetName = isCluster ? (cluster?.name || cluster?.id || '当前集群') : (project?.name || '当前项目')
  const targetContext = isCluster
    ? `${cluster?.id || metrics?.cluster_id || '未配置集群'} · ${cluster?.type || 'Kubernetes'}`
    : `${targetClusterID} / ${targetNamespace}`
  const focusedPodStatus = focusPod?.ready ? '运行中' : focusPod?.phase || '处理中'
  const focusedPodVersion = focusPod?.labels?.version || focusPod?.labels?.['app.kubernetes.io/version'] || '-'
  const focusedPodRestarts = focusPod ? (focusPod.restart_count ?? focusPod.restarts ?? 0) : 0

  useEffect(() => {
    if (!autoRefresh || typeof onRefresh !== 'function') return undefined
    const timer = window.setInterval(onRefresh, 30000)
    return () => window.clearInterval(timer)
  }, [autoRefresh, onRefresh])

  const series = useMemo(() => {
    const source = Array.isArray(metrics?.series) ? metrics.series : []
    if (!source.length) return []
    if (range === 'custom') {
      const start = customRange?.[0]?.valueOf()
      const end = customRange?.[1]?.valueOf()
      if (!isNumber(start) || !isNumber(end)) return []
      const lower = Math.min(start, end)
      const upper = Math.max(start, end)
      return source.filter((item) => {
        const timestamp = new Date(item.timestamp).getTime()
        return Number.isFinite(timestamp) && timestamp >= lower && timestamp <= upper
      })
    }
    const selectedRange = RANGE_OPTIONS.find((item) => item.value === range) || RANGE_OPTIONS[1]
    const timestamps = source.map((item) => new Date(item.timestamp).getTime()).filter(Number.isFinite)
    const latest = timestamps.length ? Math.max(...timestamps) : Date.now()
    const cutoff = latest - selectedRange.duration
    return source.filter((item) => {
      const timestamp = new Date(item.timestamp).getTime()
      return Number.isFinite(timestamp) && timestamp >= cutoff
    })
  }, [customRange, metrics?.series, range])

  const totalPods = isNumber(metrics?.pod_count) ? metrics.pod_count : pods.length
  const healthyPods = isNumber(metrics?.healthy_pod_count) ? metrics.healthy_pod_count : countFromPods(pods, (pod) => pod.ready)
  const snapshotUnavailable = metrics?.metrics_available === false
  const restartCount = !snapshotUnavailable && isNumber(metrics?.pod_restart_count) ? metrics.pod_restart_count : restartCountFromPods(pods)
  const pendingPods = !snapshotUnavailable && isNumber(metrics?.pending_pod_count) ? metrics.pending_pod_count : countFromPods(pods, (pod) => String(pod.phase).toLowerCase() === 'pending')
  const failedPods = !snapshotUnavailable && isNumber(metrics?.failed_pod_count) ? metrics.failed_pod_count : countFromPods(pods, (pod) => ['failed', 'error'].includes(String(pod.phase).toLowerCase()))
  const nodeCount = isNumber(metrics?.node_count) && metrics.node_count > 0 ? metrics.node_count : metrics?.nodes?.length || 0
  const readyNodeCount = isNumber(metrics?.ready_node_count) && metrics.ready_node_count > 0 ? metrics.ready_node_count : metrics?.nodes?.filter((node) => node.ready).length || 0
  const desired = isNumber(metrics?.deployment_desired) && metrics.deployment_desired > 0 ? metrics.deployment_desired : totalPods
  const available = isNumber(metrics?.deployment_available) && metrics.deployment_available > 0 ? metrics.deployment_available : healthyPods
  const nodeHealthPercent = nodeCount > 0 ? Math.round((readyNodeCount / nodeCount) * 100) : null
  const oomCount = snapshotUnavailable ? null : metrics?.oom_killed_count
  const crashLoopCount = snapshotUnavailable ? null : metrics?.crash_loop_count
  const hasStatusData = totalPods > 0 || nodeCount > 0
  const hasIssues = failedPods > 0 || pendingPods > 0 || (crashLoopCount || 0) > 0 || (oomCount || 0) > 0 || (desired > 0 && available < desired)
  const statusState = !hasStatusData ? 'unknown' : hasIssues ? 'warning' : 'healthy'
  const statusLabel = statusState === 'healthy' ? '运行正常' : statusState === 'warning' ? '存在异常' : '暂无运行实例'
  const hasNodeDetails = Array.isArray(metrics?.nodes) && metrics.nodes.length > 0
  const statusDescription = statusState === 'warning'
    ? hasNodeDetails ? '有 Pod 或部署状态需要关注。' : '有 Pod 或部署状态需要关注，但当前没有可用的节点明细。'
    : statusState === 'unknown'
      ? '当前还没有可观测的 Pod 或节点。'
      : snapshotUnavailable
        ? 'Pod 和节点状态正常，资源指标尚未接入。'
        : ''
  const statusItems = isCluster
    ? [
        { label: '健康节点', value: nodeCount ? `${readyNodeCount} / ${nodeCount}` : '暂无', note: nodeHealthPercent === null ? '暂无节点' : `${nodeHealthPercent}% 可用`, tone: readyNodeCount < nodeCount ? 'warning' : 'good' },
        { label: '运行 Pod', value: totalPods ? `${healthyPods} / ${totalPods}` : '暂无', note: '健康 / 总数', tone: healthyPods < totalPods ? 'warning' : 'good' },
        { label: '异常节点', value: `${Math.max(0, nodeCount - readyNodeCount)} 个`, note: '未就绪节点', tone: nodeCount > readyNodeCount ? 'danger' : 'good' },
        { label: '重启次数', value: integerMetric(restartCount, ' 次'), note: crashLoopCount === null || crashLoopCount === undefined ? '异常重启暂无' : `异常重启 ${crashLoopCount} 次`, tone: restartCount ? 'warning' : 'good' },
        { label: '等待 / 失败', value: `${pendingPods} / ${failedPods}`, note: 'Pod 数量', tone: pendingPods || failedPods ? 'danger' : 'good' },
        { label: 'OOMKilled', value: oomCount === null || oomCount === undefined ? '暂无' : integerMetric(oomCount, ' 次'), note: snapshotUnavailable ? '等待指标接入' : '最近采样', tone: oomCount ? 'danger' : 'good' },
      ]
    : [
        { label: '健康 Pod', value: totalPods ? `${healthyPods} / ${totalPods}` : '暂无', note: '健康 / 总数', tone: healthyPods < totalPods ? 'warning' : 'good' },
        { label: '等待 / 失败', value: `${pendingPods} / ${failedPods}`, note: 'Pod 数量', tone: pendingPods || failedPods ? 'danger' : 'good' },
        { label: '异常重启', value: crashLoopCount === null || crashLoopCount === undefined ? '暂无' : integerMetric(crashLoopCount, ' 次'), note: snapshotUnavailable ? '等待指标接入' : 'CrashLoopBackOff', tone: crashLoopCount ? 'danger' : 'good' },
        { label: '重启次数', value: integerMetric(restartCount, ' 次'), note: crashLoopCount === null || crashLoopCount === undefined ? '异常重启暂无' : `异常重启 ${crashLoopCount} 次`, tone: restartCount ? 'warning' : 'good' },
        { label: 'OOMKilled', value: oomCount === null || oomCount === undefined ? '暂无' : integerMetric(oomCount, ' 次'), note: snapshotUnavailable ? '等待指标接入' : '最近采样', tone: oomCount ? 'danger' : 'good' },
        { label: '涉及节点', value: nodeCount ? `${readyNodeCount} / ${nodeCount}` : '暂无', note: '正常 / 总数', tone: readyNodeCount < nodeCount ? 'warning' : 'good' },
      ]

  const nodeColumns = [
    { title: '节点', dataIndex: 'name', ellipsis: true },
    { title: '状态', dataIndex: 'ready', width: 78, render: (value) => <Tag color={value ? 'green' : 'red'}>{value ? '正常' : '异常'}</Tag> },
    { title: 'CPU', dataIndex: 'cpu_used_percent', width: 82, render: (value) => <span>{NodeValue({ value, available: metrics?.metrics_available !== false })}</span> },
    { title: '内存', dataIndex: 'memory_used_percent', width: 82, render: (value) => <span>{NodeValue({ value, available: metrics?.metrics_available !== false })}</span> },
    { title: 'Swap', dataIndex: 'swap_used_percent', width: 82, render: (value) => <span>{NodeValue({ value, available: metrics?.metrics_available !== false })}</span> },
    { title: '磁盘', dataIndex: 'disk_used_percent', width: 82, render: (value) => <span>{NodeValue({ value, available: metrics?.metrics_available !== false })}</span> },
    { title: '负载', dataIndex: 'load_1m', width: 72, render: (value) => <span>{NodeValue({ value, unit: '', precision: 2, available: metrics?.metrics_available !== false })}</span> },
    { title: '入网', dataIndex: 'network_receive_mbps', width: 82, render: (value) => <span>{NodeValue({ value, unit: ' Mbps', precision: 0, available: metrics?.metrics_available !== false })}</span> },
    { title: '出网', dataIndex: 'network_transmit_mbps', width: 82, render: (value) => <span>{NodeValue({ value, unit: ' Mbps', precision: 0, available: metrics?.metrics_available !== false })}</span> },
    { title: 'Pod', dataIndex: 'pod_count', width: 62, render: (value) => integerMetric(value) },
  ]
  const podColumns = [
    { title: 'Pod', dataIndex: 'name', ellipsis: true },
    { title: '命名空间', dataIndex: 'namespace', width: 130, ellipsis: true },
    { title: '状态', dataIndex: 'ready', width: 78, render: (value) => <Tag color={value ? 'green' : 'orange'}>{value ? '正常' : '处理中'}</Tag> },
    { title: '重启', dataIndex: 'restart_count', width: 70, render: (value) => integerMetric(value, ' 次') },
    { title: 'Pod IP', dataIndex: 'pod_ip', width: 125, render: (value) => value || '-' },
  ]

  const trendGroups = [
    {
      key: 'resources',
      charts: [
        { title: '资源使用率', max: 100, lines: [
          { key: 'cpu_used_percent', label: 'CPU', color: COLORS.primary, unit: '%', precision: 1 },
          { key: 'memory_used_percent', label: '内存', color: COLORS.teal, unit: '%', precision: 1 },
          { key: 'swap_used_percent', label: 'Swap', color: COLORS.purple, unit: '%', precision: 1 },
          { key: 'disk_used_percent', label: '磁盘', color: COLORS.orange, unit: '%', precision: 1 },
        ] },
        { title: '网络带宽', lines: [
          { key: 'network_receive_mbps', label: '接收', color: COLORS.cyan, unit: ' Mbps', precision: 0 },
          { key: 'network_transmit_mbps', label: '发送', color: COLORS.purple, unit: ' Mbps', precision: 0 },
        ] },
        { title: '磁盘 IO', lines: [
          { key: 'disk_read_mbps', label: '读取', color: COLORS.primary, unit: ' MB/s', precision: 0 },
          { key: 'disk_write_mbps', label: '写入', color: COLORS.orange, unit: ' MB/s', precision: 0 },
        ] },
        { title: '系统负载', lines: [
          { key: 'load_1m', label: '1 分钟', color: COLORS.red, precision: 2 },
          { key: 'load_5m', label: '5 分钟', color: COLORS.orange, precision: 2 },
          { key: 'load_15m', label: '15 分钟', color: COLORS.primary, precision: 2 },
        ] },
      ],
    },
    {
      key: 'workload',
      charts: [
        { title: '请求量', lines: [{ key: 'request_rate_rps', label: '请求量', color: COLORS.primary, unit: ' req/s', precision: 0 }] },
        { title: '错误率', max: 5, lines: [{ key: 'error_rate_percent', label: '错误率', color: COLORS.red, unit: '%', precision: 2 }] },
        { title: '响应延迟', lines: [
          { key: 'latency_p50_ms', label: 'P50', color: COLORS.teal, unit: ' ms', precision: 0 },
          { key: 'latency_p95_ms', label: 'P95', color: COLORS.orange, unit: ' ms', precision: 0 },
          { key: 'latency_p99_ms', label: 'P99', color: COLORS.red, unit: ' ms', precision: 0 },
        ] },
        { title: 'Pod 数量', lines: [
          { key: 'pod_count', label: '总 Pod', color: COLORS.primary, unit: ' 个', precision: 0 },
          { key: 'healthy_pod_count', label: '健康 Pod', color: COLORS.teal, unit: ' 个', precision: 0 },
        ] },
      ],
    },
  ]

  const customRangeReady = isNumber(customRange?.[0]?.valueOf()) && isNumber(customRange?.[1]?.valueOf())

  return <div className={`monitor-dashboard ${isCluster ? 'is-cluster' : 'is-project'}`}>
    <div className="monitor-toolbar">
      <div className="monitor-heading-copy">
        <div className="monitor-heading-line"><Typography.Title level={4}>{isCluster ? `集群监控 · ${cluster?.name || cluster?.id || '当前集群'}` : '项目监控'}</Typography.Title></div>
      </div>
      <Space wrap className="monitor-toolbar-actions">
        {!isCluster && onOpenCluster && <Button type="link" onClick={onOpenCluster}>查看集群详情</Button>}
        {!isCluster && targets.length > 0 && <DeploymentTargetSelect className="monitor-target-select" targets={targets} value={selectedTarget?.id} onChange={onTargetChange} disabled={typeof onTargetChange !== 'function'} />}
        <Select value={range} onChange={setRange} options={RANGE_OPTIONS.map(({ value, label }) => ({ value, label }))} aria-label="监控时间范围" />
        {range === 'custom' && <DatePicker.RangePicker
          value={customRange}
          onChange={setCustomRange}
          showTime={{ format: 'HH:mm' }}
          format="YYYY-MM-DD HH:mm"
          allowClear
          aria-label="自定义监控时间范围"
        />}
        <span className="monitor-auto-refresh"><span>自动刷新</span><Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} /></span>
        <Button icon={<ReloadOutlined />} onClick={onRefresh}>刷新</Button>
      </Space>
    </div>

    {focusPod && <div className="monitor-focus-bar">
      <div className="monitor-focus-main"><span className="monitor-focus-label">Pod 监控</span><strong title={focusPod.name}>{focusPod.name}</strong><Tag color={focusPod.ready ? 'green' : 'orange'}>{focusedPodStatus}</Tag><small>{targetName} · {targetContext}</small></div>
      <div className="monitor-focus-facts"><span>版本 <b>{focusedPodVersion}</b></span><span>节点 <b>{focusPod.node_name || '-'}</b></span><span>重启 <b>{focusedPodRestarts} 次</b></span></div>
      <Button type="link" onClick={onClearPod}>查看环境监控</Button>
    </div>}

    {focusPod && <div className="monitor-focus-note">曲线按当前环境统计；Pod 状态、版本、节点和重启次数为当前 Pod 信息。</div>}

    {metrics?.metrics_available === false && <Alert
      className="monitor-source-alert"
      type="warning"
      showIcon
      message="当前能看到 Pod 和集群基础信息，资源曲线还没有数据"
      description={metrics.monitoring?.message || metrics.metrics_message || '请确认 Prometheus、node-exporter 和 kube-state-metrics 已安装并正常运行。'}
      action={onMonitoringAction ? <Button size="small" type="primary" onClick={onMonitoringAction}>{monitoringActionLabel}</Button> : undefined}
    />}
    {metrics?.metrics_available !== false && metrics?.monitoring?.history_available === false && <Alert className="monitor-source-alert" type="info" showIcon message="Prometheus 历史数据正在准备" description="Prometheus 采集链路就绪后，平台会保存并读取历史资源数据；强制刷新页面不会清空历史记录。" />}

    <section className={`monitor-status-overview status-state-${statusState}`}>
      <div className="monitor-status-summary">
        <div className="monitor-status-mark">{statusState === 'healthy' ? <CheckCircleOutlined /> : <WarningOutlined />}</div>
        <div className="monitor-status-copy"><span>运行状态</span><strong>{statusLabel}</strong>{statusDescription && <Typography.Text type="secondary">{statusDescription}</Typography.Text>}</div>
      </div>
      <div className="monitor-overview-content">
        <div className="monitor-overview-metrics">{statusItems.map((item) => <OverviewMetric key={item.label} {...item} />)}</div>
      </div>
    </section>

    <div className="monitor-trend-groups">{trendGroups.map((group) => <section className="monitor-trend-group" key={group.key}><div className="monitor-chart-grid">{group.charts.map((chart) => <MetricChart key={chart.title} {...chart} series={series} emptyText={range === 'custom' && !customRangeReady ? '请选择开始和结束时间' : undefined} />)}</div></section>)}</div>

    <div className="monitor-detail-grid">
      <Card variant="borderless" className="monitor-detail-card monitor-node-card monitor-node-card-wide">
        <div className="monitor-card-heading"><Typography.Title level={5}>{isCluster ? '节点明细' : '承载节点'}</Typography.Title></div>
        {metrics?.nodes?.length ? <Table className="monitor-node-table" size="small" rowKey="name" columns={nodeColumns} dataSource={metrics.nodes} pagination={false} scroll={{ x: 760 }} expandable={{ rowExpandable: (node) => node.pods?.length > 0, expandedRowRender: (node) => <Table size="small" rowKey="name" columns={podColumns} dataSource={node.pods} pagination={false} showHeader /> }} /> : <div className="monitor-table-empty">当前数据源没有节点明细</div>}
      </Card>
    </div>

  </div>
}
