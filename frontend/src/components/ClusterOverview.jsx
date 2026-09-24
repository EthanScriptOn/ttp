import { Alert, Button, Card, Empty, Space, Tag, Typography, message } from 'antd'
import { CheckCircleOutlined, ClusterOutlined, EditOutlined, LinkOutlined, MonitorOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { createCluster, getClusters, testCluster, updateCluster } from '../services/api'
import ClusterForm from './ClusterForm'

function statusLabel(value) {
  return ({ active: '已连接', ready: '已连接', healthy: '已连接', draining: '维护中', offline: '离线' })[value] || '未测试'
}

function statusColor(value) {
  return ({ active: 'green', ready: 'green', healthy: 'green', draining: 'orange', offline: 'red' })[value] || 'default'
}

function connectionLabel(cluster) {
  if (cluster.connection_mode === 'in_cluster') return '集群内身份'
  if (cluster.kubeconfig_configured) return 'kubeconfig 已配置'
  return '等待 kubeconfig'
}

function checkedAt(cluster) {
  if (!cluster.updated_at) return '尚未测试'
  const value = new Date(cluster.updated_at)
  return Number.isNaN(value.valueOf()) ? '最近状态未知' : `最近更新 ${value.toLocaleString('zh-CN', { dateStyle: 'short', timeStyle: 'short' })}`
}

export default function ClusterOverview({ onOpenMonitor, canManageClusters = true, embedded = false }) {
  const [clusters, setClusters] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editingCluster, setEditingCluster] = useState(null)
  const [saving, setSaving] = useState(false)
  const [testingClusterID, setTestingClusterID] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setClusters((await getClusters()) || [])
    } catch (loadError) {
      setError(loadError.message || '集群连接加载失败')
    } finally {
      setLoading(false)
    }
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
      const payload = editingCluster
        ? Object.fromEntries(Object.entries(values).filter(([key, value]) => key !== 'kubeconfig_path' || String(value || '').trim() || values.connection_mode === 'in_cluster'))
        : values
      const result = editingCluster ? await updateCluster(editingCluster.id, payload) : await createCluster(values)
      setFormOpen(false)
      setEditingCluster(null)
      message[result.connected ? 'success' : 'warning'](result.connected ? '集群连接已保存并测试成功' : (result.message || '集群配置已保存，但连接测试未通过'))
      await load()
    } catch (saveError) {
      message.error(saveError.message || '保存集群连接失败')
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
      message[result.connected ? 'success' : 'warning'](result.connected ? `${cluster.name} 连接成功` : (result.message || `${cluster.name} 连接失败`))
      await load()
    } catch (testError) {
      message.error(testError.message || '测试集群连接失败')
    } finally {
      setTestingClusterID('')
    }
  }

  return (
    <Card
      className={`space-settings-section infrastructure-connection-section ${embedded ? 'is-embedded' : ''}`}
      title={<span className="infrastructure-card-title"><ClusterOutlined /> {embedded ? 'Kubernetes 集群' : '集群连接'}</span>}
      extra={<Space>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        {canManageClusters && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加连接</Button>}
      </Space>}
      variant="borderless"
    >
      {error && <Alert className="infrastructure-section-alert" type="warning" showIcon message={error} />}
      {!loading && !clusters.length && <Card className="infrastructure-empty-card" variant="borderless"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前空间还没有 Kubernetes 集群连接">{canManageClusters && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加第一个连接</Button>}</Empty></Card>}
      {clusters.length > 0 && <div className="infrastructure-connection-list">
        {clusters.map((cluster) => <Card key={cluster.id} className="infrastructure-connection-card" variant="borderless">
          <div className="infrastructure-connection-card-head">
            <div className="infrastructure-connection-identity">
              <span className="infrastructure-connection-icon is-cluster"><ClusterOutlined /></span>
              <div><Typography.Title level={4}>{cluster.name}</Typography.Title><Typography.Text type="secondary">{cluster.id} · Kubernetes</Typography.Text></div>
            </div>
            <Tag color={statusColor(cluster.status)} icon={cluster.status === 'active' || cluster.status === 'healthy' ? <CheckCircleOutlined /> : undefined}>{statusLabel(cluster.status)}</Tag>
          </div>
          <div className="infrastructure-connection-details">
            <div><span>API 地址</span><strong title={cluster.api_endpoint || undefined}><LinkOutlined /> {cluster.api_endpoint || '从 kubeconfig 读取'}</strong></div>
            <div><span>连接方式</span><strong>{connectionLabel(cluster)}</strong></div>
            <div><span>状态时间</span><strong>{checkedAt(cluster)}</strong></div>
          </div>
          <div className="infrastructure-connection-actions">
            {onOpenMonitor && <Button type="link" icon={<MonitorOutlined />} onClick={() => onOpenMonitor(cluster)}>查看监控</Button>}
            {canManageClusters && <Button type="link" icon={<EditOutlined />} onClick={() => openEdit(cluster)}>编辑</Button>}
            {canManageClusters && <Button type="link" loading={testingClusterID === cluster.id} onClick={() => checkCluster(cluster)}>测试连接</Button>}
          </div>
        </Card>)}
      </div>}

      {canManageClusters && <ClusterForm open={formOpen} cluster={editingCluster} onCancel={() => { setFormOpen(false); setEditingCluster(null) }} onSubmit={saveCluster} loading={saving} />}
    </Card>
  )
}
