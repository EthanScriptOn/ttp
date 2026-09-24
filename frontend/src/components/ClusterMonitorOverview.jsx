import { Alert, Button, Card, Empty, Modal, Progress, Select, Space, Spin, Tag, Typography, message } from 'antd'
import { ClusterOutlined, LinkOutlined, ReloadOutlined, SettingOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { getClusterMetrics, getClusters, installClusterMonitoring } from '../services/api'
import MonitorDashboard from './MonitorDashboard'

function mergeMetricHistory(previous, next) {
  if (!next || !Array.isArray(next.series) || next.series.length === 0) return next
  const history = [...(Array.isArray(previous?.series) ? previous.series : []), ...next.series]
  const unique = new Map(history.map((point) => [String(point.timestamp), point]))
  return { ...next, series: [...unique.values()].slice(-120) }
}

function needsMonitoringInstall(monitoring) {
  const dependencies = monitoring?.dependencies || []
  return dependencies.length
    ? dependencies.some((item) => !item.installed)
    : Boolean(monitoring && !monitoring.installed)
}

function dependencyState(dependency) {
  if (dependency?.state) return dependency.state
  if (dependency?.available) return 'ready'
  if (dependency?.installed) return 'installing'
  return 'missing'
}

const MONITORING_STATE = {
  ready: { label: '已就绪', color: 'success' },
  installing: { label: '安装中', color: 'processing' },
  failed: { label: '异常', color: 'error' },
  missing: { label: '未安装', color: 'default' },
}

export default function ClusterMonitorOverview({ onOpenConnections, canManageClusters = false }) {
  const [clusters, setClusters] = useState([])
  const [selectedClusterID, setSelectedClusterID] = useState('')
  const [metrics, setMetrics] = useState({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [monitoringPrompt, setMonitoringPrompt] = useState(null)
  const [installingMonitoring, setInstallingMonitoring] = useState(false)
  const [checkingMonitoring, setCheckingMonitoring] = useState(false)
  const [retentionDays, setRetentionDays] = useState(7)

  const load = async (preferredID = selectedClusterID) => {
    setLoading(true)
    setError('')
    try {
      const nextClusters = (await getClusters()) || []
      setClusters(nextClusters)
      const nextID = nextClusters.some((cluster) => cluster.id === preferredID) ? preferredID : nextClusters[0]?.id || ''
      setSelectedClusterID(nextID)
      if (!nextID) {
        setMetrics({})
        return
      }
      const result = await getClusterMetrics(nextID)
      setMetrics((old) => ({ ...old, [nextID]: mergeMetricHistory(old[nextID], result) }))
      promptMonitoringStatus({ cluster: nextClusters.find((cluster) => cluster.id === nextID), monitoring: result?.monitoring })
    } catch (loadError) {
      setError(loadError.message || '集群监控加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const promptMonitoringStatus = ({ cluster, monitoring }) => {
    const needsInstall = needsMonitoringInstall(monitoring)
    if (!cluster?.id || !monitoring || monitoring.available || (needsInstall && (!canManageClusters || !monitoring.installable))) {
      setMonitoringPrompt(null)
      return
    }
    setMonitoringPrompt({ cluster, monitoring })
  }

  const installMonitoring = async () => {
    const cluster = monitoringPrompt?.cluster
    if (!cluster?.id) return
    setInstallingMonitoring(true)
    try {
      const result = await installClusterMonitoring(cluster.id, retentionDays)
      message.success(result.monitoring?.message || '监控组件安装请求已提交')
      setMonitoringPrompt({ cluster, monitoring: result.monitoring })
      setMetrics((old) => ({ ...old, [cluster.id]: { ...(old[cluster.id] || {}), monitoring: result.monitoring } }))
      await refreshMonitoringProgress(cluster, true)
    } catch (installError) {
      message.error(installError.message || '安装监控组件失败')
    } finally {
      setInstallingMonitoring(false)
    }
  }

  const refreshMonitoringProgress = async (cluster, silent = false) => {
    if (!cluster?.id) return
    if (!silent) setCheckingMonitoring(true)
    try {
      const result = await getClusterMetrics(cluster.id)
      setMetrics((old) => ({ ...old, [cluster.id]: mergeMetricHistory(old[cluster.id], result) }))
      if (result.monitoring?.available) {
        setMonitoringPrompt(null)
        message.success('集群监控组件已全部就绪')
        return
      }
      setMonitoringPrompt({ cluster, monitoring: result.monitoring })
    } catch (refreshError) {
      if (!silent) message.error(refreshError.message || '监控安装状态检查失败')
    } finally {
      if (!silent) setCheckingMonitoring(false)
    }
  }

  const promptNeedsInstall = needsMonitoringInstall(monitoringPrompt?.monitoring)
  const promptDependencies = monitoringPrompt?.monitoring?.dependencies || []
  const promptHasFailure = monitoringPrompt?.monitoring?.state === 'failed' || promptDependencies.some((item) => dependencyState(item) === 'failed')
  useEffect(() => {
    if (!monitoringPrompt?.cluster?.id || promptNeedsInstall || promptHasFailure || monitoringPrompt.monitoring?.available) return undefined
    const timer = window.setTimeout(() => refreshMonitoringProgress(monitoringPrompt.cluster, true), 3000)
    return () => window.clearTimeout(timer)
  }, [monitoringPrompt, promptNeedsInstall, promptHasFailure])

  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterID)
  const selectedMetrics = selectedClusterID ? metrics[selectedClusterID] : null
  const promptReadyCount = promptDependencies.filter((item) => dependencyState(item) === 'ready').length
  const promptPercent = promptDependencies.length ? Math.round(promptReadyCount / promptDependencies.length * 100) : 0

  return (
    <div className="page-wrap cluster-monitor-page">
      <div className="page-heading cluster-monitor-heading">
        <div>
          <Typography.Text className="page-kicker">工作空间 · 运行状态</Typography.Text>
          <Typography.Title level={2}>集群监控</Typography.Title>
        </div>
        <Space wrap>
          <Select
            value={selectedClusterID || undefined}
            placeholder="选择集群"
            loading={loading && !clusters.length}
            options={clusters.map((cluster) => ({ value: cluster.id, label: cluster.name }))}
            onChange={(value) => load(value)}
            style={{ minWidth: 180 }}
          />
          <Button icon={<ReloadOutlined />} onClick={() => load()} loading={loading}>刷新</Button>
          {onOpenConnections && <Button icon={<SettingOutlined />} onClick={onOpenConnections}>管理连接</Button>}
        </Space>
      </div>

      {error && <Alert className="cluster-monitor-alert" type="warning" showIcon message={error} />}
      {!loading && !clusters.length && <Card className="infrastructure-empty-card" variant="borderless"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前空间还没有可监控的集群">{onOpenConnections && <Button type="primary" icon={<LinkOutlined />} onClick={onOpenConnections}>去添加集群连接</Button>}</Empty></Card>}
      {loading && !selectedCluster && <div className="space-settings-loading"><Spin /><Typography.Text type="secondary">加载集群监控...</Typography.Text></div>}
      {selectedCluster && <div className="cluster-monitor-content">
        <div className="cluster-monitor-context">
          <div><span className="cluster-monitor-context-icon"><ClusterOutlined /></span><div><Typography.Text strong>{selectedCluster.name}</Typography.Text><Typography.Text type="secondary">{selectedCluster.api_endpoint || selectedCluster.id}</Typography.Text></div></div>
          <Typography.Text type="secondary">只读监控视图</Typography.Text>
        </div>
        <MonitorDashboard
          scope="cluster"
          cluster={selectedCluster}
          metrics={selectedMetrics}
          onRefresh={() => load(selectedCluster.id)}
          onMonitoringAction={!selectedMetrics?.monitoring?.available && selectedMetrics?.monitoring
            ? () => setMonitoringPrompt({ cluster: selectedCluster, monitoring: selectedMetrics.monitoring })
            : undefined}
          monitoringActionLabel={needsMonitoringInstall(selectedMetrics?.monitoring) ? '安装组件' : '查看安装状态'}
        />
      </div>}

      {monitoringPrompt && <Modal
        title={promptNeedsInstall ? '安装集群监控组件' : promptHasFailure ? '监控组件安装异常' : '正在安装集群监控组件'}
        open
        onCancel={() => setMonitoringPrompt(null)}
        onOk={promptNeedsInstall ? installMonitoring : undefined}
        okText={promptNeedsInstall ? '确认安装' : undefined}
        cancelText={promptNeedsInstall ? '暂不安装' : undefined}
        confirmLoading={installingMonitoring}
        footer={promptNeedsInstall ? undefined : [
          <Button key="close" onClick={() => setMonitoringPrompt(null)}>关闭</Button>,
          <Button key="refresh" icon={<ReloadOutlined />} loading={checkingMonitoring} onClick={() => refreshMonitoringProgress(monitoringPrompt.cluster)}>重新检查</Button>,
          promptHasFailure && canManageClusters ? <Button key="retry" type="primary" loading={installingMonitoring} onClick={installMonitoring}>重新安装</Button> : null,
        ]}
        destroyOnHidden
      >
        {promptNeedsInstall ? <>
          <Typography.Paragraph>{monitoringPrompt.cluster.name} 尚未接入平台所需的资源监控组件。</Typography.Paragraph>
          <Alert
            type="info"
            showIcon
            message="监控依赖"
            description={promptDependencies.filter((item) => !item.installed).map((item) => `${item.display_name}${item.install_version ? ` (${item.install_version})` : ''}`).join('；') || monitoringPrompt.monitoring.message}
          />
          <Space align="center" style={{ marginTop: 16 }}>
            <Typography.Text>历史数据保存</Typography.Text>
            <Select value={retentionDays} onChange={setRetentionDays} options={[3, 7, 14, 30].map((days) => ({ value: days, label: `${days} 天` }))} />
          </Space>
        </> : <div className="monitoring-install-progress">
          <Alert
            type={promptHasFailure ? 'error' : 'info'}
            showIcon
            message={monitoringPrompt.monitoring.message || (promptHasFailure ? '安装过程中出现异常' : '监控组件正在安装')}
            description={promptHasFailure ? '请根据下方组件原因处理问题，然后点击“重新检查”。' : '页面每 3 秒自动检查一次，全部组件就绪后会自动关闭。'}
          />
          <div className="monitoring-install-progress-bar"><Progress percent={promptPercent} status={promptHasFailure ? 'exception' : 'active'} /><span>{promptReadyCount} / {promptDependencies.length} 个组件就绪</span></div>
          <div className="monitoring-install-dependencies">
            {promptDependencies.map((dependency) => {
              const state = dependencyState(dependency)
              const stateMeta = MONITORING_STATE[state] || MONITORING_STATE.installing
              return <div className={`monitoring-install-dependency is-${state}`} key={dependency.component}>
                <div><strong>{dependency.display_name || dependency.component}</strong>{dependency.install_version && <small>{dependency.install_version}</small>}</div>
                <Tag color={stateMeta.color}>{stateMeta.label}</Tag>
                <p>{dependency.message || '等待状态更新'}</p>
                {dependency.image_source && <div className="monitoring-install-source">
                  <span>镜像源：{dependency.image_source}</span>
                  {dependency.source_count > 0 && <span>源 {dependency.source_index || 1}/{dependency.source_count}</span>}
                  {state === 'installing' && dependency.retry_limit > 0 && <span>本源重试 {dependency.retry_count || 0}/{dependency.retry_limit}</span>}
                </div>}
              </div>
            })}
          </div>
        </div>}
      </Modal>}
    </div>
  )
}
