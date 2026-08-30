import { Alert, Button, Card, Col, Empty, Row, Space, Tag, Typography, message } from 'antd'
import { AppstoreOutlined, ClusterOutlined, DeploymentUnitOutlined, EditOutlined, LinkOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { createCluster, getClusterMetrics, getClusters, getProjects, testCluster, updateCluster } from '../services/api'
import ClusterForm from './ClusterForm'
import MonitorDashboard from './MonitorDashboard'

function valueOrDash(value, suffix = '') {
  return value === null || value === undefined ? '--' : `${value}${suffix}`
}

function metricValue(value, suffix, metricsAvailable) {
  return metricsAvailable === false ? '暂无' : valueOrDash(value, suffix)
}

function statusLabel(value) {
  return ({ active: '正常', ready: '正常', healthy: '正常', draining: '迁移中', offline: '离线' })[value] || '未知'
}

function statusColor(value) {
  return ({ active: 'green', ready: 'green', healthy: 'green', draining: 'orange', offline: 'red' })[value] || 'default'
}

export default function ClusterOverview({ onOpenProjects, canManageClusters = true }) {
  const [clusters, setClusters] = useState([])
  const [projects, setProjects] = useState([])
  const [metrics, setMetrics] = useState({})
  const [selectedClusterID, setSelectedClusterID] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editingCluster, setEditingCluster] = useState(null)
  const [saving, setSaving] = useState(false)
  const [testingClusterID, setTestingClusterID] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    const [clusterResult, projectResult] = await Promise.allSettled([getClusters(), getProjects()])
    const failures = []
    let nextClusters = clusters
    if (clusterResult.status === 'fulfilled') {
      nextClusters = clusterResult.value || []
      setClusters(nextClusters)
      setSelectedClusterID((current) => current && nextClusters.some((cluster) => cluster.id === current) ? current : nextClusters[0]?.id || '')
    } else {
      failures.push(`集群：${clusterResult.reason?.message || '加载失败'}`)
    }
    if (projectResult.status === 'fulfilled') setProjects(projectResult.value || [])
    else failures.push(`项目：${projectResult.reason?.message || '加载失败'}`)

    if (nextClusters.length) {
      const metricResults = await Promise.allSettled(nextClusters.map((cluster) => getClusterMetrics(cluster.id)))
      const nextMetrics = {}
      metricResults.forEach((result, index) => {
        if (result.status === 'fulfilled') nextMetrics[nextClusters[index].id] = result.value
        else failures.push(`${nextClusters[index].name}：监控数据暂不可用`)
      })
      setMetrics(nextMetrics)
    } else {
      setMetrics({})
    }
    if (failures.length) setError(failures.join('；'))
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  const openCreate = () => {
    if (!canManageClusters) {
      message.warning('当前角色没有管理集群的权限')
      return
    }
    setEditingCluster(null)
    setFormOpen(true)
  }

  const openEdit = (cluster) => {
    if (!canManageClusters) {
      message.warning('当前角色没有管理集群的权限')
      return
    }
    setEditingCluster(cluster)
    setFormOpen(true)
  }

  const saveCluster = async (values) => {
    if (!canManageClusters) return
    setSaving(true)
    try {
      const result = editingCluster
        ? await updateCluster(editingCluster.id, Object.fromEntries(Object.entries(values).filter(([key, value]) => key !== 'kubeconfig_path' || String(value || '').trim() || values.connection_mode === 'in_cluster')))
        : await createCluster(values)
      setFormOpen(false)
      setEditingCluster(null)
      if (result.connected) message.success('集群配置已保存，连接成功')
      else message.warning(result.message || '配置已保存，但连接失败')
      await load()
    } catch (saveError) {
      message.error(saveError.message || '保存集群配置失败')
    } finally {
      setSaving(false)
    }
  }

  const checkCluster = async (cluster) => {
    if (!canManageClusters) {
      message.warning('当前角色没有管理集群的权限')
      return
    }
    setTestingClusterID(cluster.id)
    try {
      const result = await testCluster(cluster.id)
      if (result.connected) message.success(`${cluster.name} 连接成功`)
      else message.warning(result.message || `${cluster.name} 连接失败`)
      await load()
    } catch (testError) {
      message.error(testError.message || '测试连接失败')
    } finally {
      setTestingClusterID('')
    }
  }

  const totalPods = clusters.reduce((total, cluster) => total + (metrics[cluster.id]?.pod_count || 0), 0)
  const visiblePodCount = Object.keys(metrics).length ? totalPods : '--'
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterID) || clusters[0]
  const selectedMetrics = selectedCluster ? metrics[selectedCluster.id] : null

  return <div className="page-wrap">
    <div className="page-heading">
      <div>
        <Typography.Text className="page-kicker">工作空间 · 基础设施</Typography.Text>
        <Typography.Title level={2}>集群管理</Typography.Title>
        <Typography.Paragraph type="secondary">先在这里登记 Kubernetes 集群，再把项目发布到对应集群。</Typography.Paragraph>
      </div>
      <Space>
        {canManageClusters && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加集群</Button>}
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新数据</Button>
      </Space>
    </div>
    {error && <Alert className="cluster-error" type="warning" showIcon message={error} />}
    <Row gutter={[16, 16]} className="summary-row">
      <Col xs={24} sm={8}><SummaryCard label="已登记集群" value={clusters.length} icon={<ClusterOutlined />} tone="green" /></Col>
      <Col xs={24} sm={8}><SummaryCard label="运行中 Pod" value={visiblePodCount} icon={<DeploymentUnitOutlined />} tone="green" /></Col>
      <Col xs={24} sm={8}><SummaryCard label="空间项目" value={projects.length} icon={<AppstoreOutlined />} tone="orange" /></Col>
    </Row>
    {!loading && !clusters.length && <Card bordered={false} className="empty-panel"><Empty description="当前空间还没有连接集群" /></Card>}
    <div className="cluster-list">
      {clusters.map((cluster) => {
        const item = metrics[cluster.id]
        const clusterProjects = projects.filter((project) => project.cluster_id === cluster.id)
        return <Card key={cluster.id} bordered={false} className="cluster-list-card">
          <div className="cluster-row">
            <div className="cluster-symbol"><ClusterOutlined /></div>
            <div className="cluster-info"><Typography.Title level={4}>{cluster.name}</Typography.Title><Typography.Text type="secondary">{cluster.id} · {cluster.type === 'kubernetes' ? 'Kubernetes' : (cluster.type || 'Kubernetes')}</Typography.Text><Typography.Text type="secondary" className="cluster-endpoint"><LinkOutlined /> {cluster.api_endpoint || 'API 地址未填写，使用 kubeconfig 地址'}</Typography.Text><Typography.Text type="secondary" className="cluster-connection">{cluster.connection_mode === 'in_cluster' ? '集群内身份' : cluster.kubeconfig_configured ? 'kubeconfig 已配置' : '等待 kubeconfig'}</Typography.Text></div>
            <Tag color={statusColor(cluster.status)}>{statusLabel(cluster.status)}</Tag>
            <div className="cluster-stat"><span>CPU</span><strong>{metricValue(item?.cpu_used_percent, '%', item?.metrics_available)}</strong></div>
            <div className="cluster-stat"><span>内存</span><strong>{metricValue(item?.memory_used_percent, '%', item?.metrics_available)}</strong></div>
            <div className="cluster-row-actions"><Button type="link" onClick={() => setSelectedClusterID(cluster.id)}>查看监控</Button>{canManageClusters && <><Button type="link" icon={<EditOutlined />} onClick={() => openEdit(cluster)}>编辑配置</Button><Button type="link" loading={testingClusterID === cluster.id} onClick={() => checkCluster(cluster)}>测试连接</Button></>}<Button type="link" onClick={onOpenProjects}>查看 {clusterProjects.length} 个项目 →</Button></div>
          </div>
        </Card>
      })}
    </div>
    {selectedCluster && <div className="cluster-monitor-wrap"><MonitorDashboard scope="cluster" cluster={selectedCluster} metrics={selectedMetrics} onRefresh={load} /></div>}
    {canManageClusters && <ClusterForm open={formOpen} cluster={editingCluster} onCancel={() => { setFormOpen(false); setEditingCluster(null) }} onSubmit={saveCluster} loading={saving} />}
  </div>
}

function SummaryCard({ label, value, icon, tone }) {
  return <Card bordered={false} className={`summary-card tone-${tone}`}><div className="summary-icon">{icon}</div><div><Typography.Text type="secondary">{label}</Typography.Text><Typography.Title level={3}>{value}</Typography.Title></div></Card>
}
