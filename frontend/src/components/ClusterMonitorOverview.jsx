import { Alert, Button, Card, Empty, Modal, Select, Space, Spin, Typography, message } from 'antd'
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

export default function ClusterMonitorOverview({ onOpenConnections }) {
  const [clusters, setClusters] = useState([])
  const [selectedClusterID, setSelectedClusterID] = useState('')
  const [metrics, setMetrics] = useState({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [monitoringPrompt, setMonitoringPrompt] = useState(null)
  const [installingMonitoring, setInstallingMonitoring] = useState(false)
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
      promptMonitoringInstall({ cluster: nextClusters.find((cluster) => cluster.id === nextID), monitoring: result?.monitoring })
    } catch (loadError) {
      setError(loadError.message || '集群监控加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const promptMonitoringInstall = ({ cluster, monitoring }) => {
    const missingDependency = (monitoring?.dependencies || []).find((item) => !item.available)
    if (!cluster?.id || !monitoring || (!missingDependency && (monitoring.available || monitoring.installed)) || !monitoring.installable) return
    setMonitoringPrompt({ cluster, monitoring })
  }

  const installMonitoring = async () => {
    const cluster = monitoringPrompt?.cluster
    if (!cluster?.id) return
    setInstallingMonitoring(true)
    try {
      const result = await installClusterMonitoring(cluster.id, retentionDays)
      message.success(result.monitoring?.message || '监控组件安装请求已提交')
      setMonitoringPrompt(null)
      await load(cluster.id)
    } catch (installError) {
      message.error(installError.message || '安装监控组件失败')
    } finally {
      setInstallingMonitoring(false)
    }
  }

  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterID)
  const selectedMetrics = selectedClusterID ? metrics[selectedClusterID] : null

  return (
    <div className="page-wrap cluster-monitor-page">
      <div className="page-heading cluster-monitor-heading">
        <div>
          <Typography.Text className="page-kicker">工作空间 · 运行状态</Typography.Text>
          <Typography.Title level={2}>集群监控</Typography.Title>
          <Typography.Paragraph type="secondary">监控节点、Pod、资源曲线和集群运行状态。集群连接本身请在“连接管理”中维护。</Typography.Paragraph>
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
        <MonitorDashboard scope="cluster" cluster={selectedCluster} metrics={selectedMetrics} onRefresh={() => load(selectedCluster.id)} />
      </div>}

      {monitoringPrompt && <Modal
        title="安装集群监控组件"
        open
        onCancel={() => setMonitoringPrompt(null)}
        onOk={installMonitoring}
        okText="确认安装"
        cancelText="暂不安装"
        confirmLoading={installingMonitoring}
        destroyOnHidden
      >
        <Typography.Paragraph>{monitoringPrompt.cluster.name} 尚未接入平台所需的资源监控组件。</Typography.Paragraph>
        <Alert
          type="info"
          showIcon
          message="监控依赖"
          description={(monitoringPrompt.monitoring.dependencies || []).filter((item) => !item.available).map((item) => `${item.display_name}${item.install_version ? ` (${item.install_version})` : ''}`).join('；') || monitoringPrompt.monitoring.message}
        />
        <Space align="center" style={{ marginTop: 16 }}>
          <Typography.Text>历史数据保存</Typography.Text>
          <Select value={retentionDays} onChange={setRetentionDays} options={[3, 7, 14, 30].map((days) => ({ value: days, label: `${days} 天` }))} />
        </Space>
      </Modal>}
    </div>
  )
}
