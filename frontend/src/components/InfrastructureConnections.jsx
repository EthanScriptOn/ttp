import { Alert, Button, Card, Select, Space, Statistic, Tabs, Tag, Typography } from 'antd'
import { CheckCircleOutlined, ClusterOutlined, LinkOutlined, ReloadOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import { getClusters, getImageRegistryConnections, testCluster, testImageRegistryConnection } from '../services/api'
import ClusterOverview from './ClusterOverview'
import ImageRegistryConnections from './ImageRegistryConnections'

export default function InfrastructureConnections({
  canReadClusters = false,
  canManageClusters = false,
  canReadRegistry = false,
  canManageRegistry = false,
}) {
  const [tab, setTab] = useState('connections')
  const [clusters, setClusters] = useState([])
  const [registries, setRegistries] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    const results = await Promise.allSettled([
      canReadClusters ? getClusters() : Promise.resolve([]),
      canReadRegistry ? getImageRegistryConnections() : Promise.resolve([]),
    ])
    const failures = []
    if (results[0].status === 'fulfilled') setClusters(results[0].value || [])
    else failures.push(`集群：${results[0].reason?.message || '加载失败'}`)
    if (results[1].status === 'fulfilled') setRegistries(results[1].value || [])
    else failures.push(`镜像仓库：${results[1].reason?.message || '加载失败'}`)
    if (failures.length) setError(failures.join('；'))
    setLoading(false)
  }

  useEffect(() => { load() }, [canReadClusters, canReadRegistry])
  const availableCount = useMemo(
    () => clusters.filter((item) => ['active', 'ready', 'healthy'].includes(String(item.status || '').toLowerCase())).length
      + registries.filter((item) => item.status === 'active').length,
    [clusters, registries],
  )

  if (!canReadClusters && !canReadRegistry) return null

  return <div className="page-wrap infrastructure-page">
    <div className="page-heading infrastructure-page-heading">
      <div>
        <Typography.Text className="page-kicker">工作空间 · 连接</Typography.Text>
        <Typography.Title level={2}>连接管理</Typography.Title>
        <Typography.Paragraph type="secondary">这里只维护可被项目环境引用的 Kubernetes 集群和镜像仓库。项目 Git 凭证在项目设置中按项目权限隔离，集群监控在“集群监控”中查看。</Typography.Paragraph>
      </div>
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
    </div>
    <div className="infrastructure-summary-grid">
      <Card variant="borderless"><Statistic title="空间级连接" value={clusters.length + registries.length} suffix="个" /><Typography.Text type="secondary">Kubernetes 集群与镜像仓库</Typography.Text></Card>
      <Card variant="borderless"><Statistic title="当前可用" value={availableCount} suffix="个" /><Typography.Text type="secondary">最近一次连接测试通过</Typography.Text></Card>
      <Card variant="borderless"><Statistic title="项目 Git" value="项目设置" /><Typography.Text type="secondary">按项目成员权限单独维护</Typography.Text></Card>
    </div>
    {error && <Alert type="warning" showIcon message={error} />}
    <Tabs activeKey={tab} onChange={setTab} items={[
      {
        key: 'connections',
        label: <span><LinkOutlined /> 空间连接</span>,
        children: <div className="infrastructure-tab-content">
          {canReadClusters && <ClusterOverview embedded canManageClusters={canManageClusters} />}
          {canReadRegistry && <div className="infrastructure-registry-section"><ImageRegistryConnections canRead={canReadRegistry} canManage={canManageRegistry} /></div>}
        </div>,
      },
      {
        key: 'path',
        label: <span><SafetyCertificateOutlined /> 发布链路预检</span>,
        children: <ConnectionPathCheck clusters={clusters} registries={registries} canManageClusters={canManageClusters} canManageRegistry={canManageRegistry} />,
      },
    ]} />
  </div>
}

function ConnectionPathCheck({ clusters = [], registries = [], canManageClusters = false, canManageRegistry = false }) {
  const [clusterID, setClusterID] = useState('')
  const [registryID, setRegistryID] = useState('')
  const [running, setRunning] = useState(false)
  const [steps, setSteps] = useState([])

  useEffect(() => {
    if (!clusters.some((item) => item.id === clusterID)) setClusterID(clusters[0]?.id || '')
  }, [clusters, clusterID])

  useEffect(() => {
    if (!registries.some((item) => item.id === registryID)) setRegistryID(registries[0]?.id || '')
  }, [registries, registryID])

  const run = async () => {
    if (!clusterID || !registryID) return
    setRunning(true)
    setSteps([
      { key: 'cluster', label: 'TTP → Kubernetes API', state: 'running', message: '正在检查集群连接…' },
      { key: 'registry', label: 'TTP → 镜像仓库', state: 'pending', message: '等待检查' },
    ])
    let clusterOK = false
    try {
      const result = await testCluster(clusterID)
      clusterOK = Boolean(result.connected)
      setSteps((current) => current.map((step) => step.key === 'cluster' ? { ...step, state: clusterOK ? 'success' : 'error', message: result.message || (clusterOK ? '连接成功' : '连接失败') } : step))
    } catch (error) {
      setSteps((current) => current.map((step) => step.key === 'cluster' ? { ...step, state: 'error', message: error.message || '连接失败' } : step))
    }
    if (clusterOK) {
      setSteps((current) => current.map((step) => step.key === 'registry' ? { ...step, state: 'running', message: '正在检查仓库凭证和 Registry API…' } : step))
      try {
        const result = await testImageRegistryConnection(registryID)
        setSteps((current) => current.map((step) => step.key === 'registry' ? { ...step, state: 'success', message: result.message || '连接成功' } : step))
      } catch (error) {
        setSteps((current) => current.map((step) => step.key === 'registry' ? { ...step, state: 'error', message: error.message || '连接失败' } : step))
      }
    } else {
      setSteps((current) => current.map((step) => step.key === 'registry' ? { ...step, state: 'blocked', message: '集群连接失败，未继续检查' } : step))
    }
    setRunning(false)
  }

  const canRun = canManageClusters && canManageRegistry && Boolean(clusterID) && Boolean(registryID)
  const stateTag = (state) => {
    if (state === 'success') return <Tag color="green" icon={<CheckCircleOutlined />}>通过</Tag>
    if (state === 'error') return <Tag color="red">失败</Tag>
    if (state === 'blocked') return <Tag>未执行</Tag>
    if (state === 'running') return <Tag color="processing">检查中</Tag>
    return <Tag>等待</Tag>
  }

  return <Card variant="borderless" className="infrastructure-path-card">
    <div className="infrastructure-section-heading">
      <div className="infrastructure-section-title">
        <span className="infrastructure-section-icon is-cluster"><ClusterOutlined /></span>
        <div>
          <Typography.Title level={3}>发布链路预检</Typography.Title>
          <Typography.Text type="secondary">确认 TTP 可以同时访问目标集群和镜像仓库，再开始发布。</Typography.Text>
        </div>
      </div>
      <Button type="primary" onClick={run} loading={running} disabled={!canRun}>开始预检</Button>
    </div>
    <Space wrap className="infrastructure-path-selects">
      <Select value={clusterID || undefined} onChange={setClusterID} placeholder="选择目标集群" options={clusters.map((item) => ({ value: item.id, label: item.name }))} style={{ minWidth: 220 }} disabled={running || !clusters.length} />
      <Select value={registryID || undefined} onChange={setRegistryID} placeholder="选择镜像仓库" options={registries.map((item) => ({ value: item.id, label: `${item.name} · ${item.registry}` }))} style={{ minWidth: 280 }} disabled={running || !registries.length} />
    </Space>
    {!canManageClusters || !canManageRegistry ? <Alert type="info" showIcon message="需要集群管理和镜像仓库管理权限才能执行预检。" /> : (!clusters.length || !registries.length) ? <Alert type="warning" showIcon message="请先在空间连接中配置并测试集群、镜像仓库。" /> : <Typography.Paragraph type="secondary" className="infrastructure-path-note">此处检查的是平台侧连通性和凭证有效性；Kubernetes 节点在真实发布时还会通过 imagePullSecret 验证镜像拉取。</Typography.Paragraph>}
    {steps.length > 0 && <div className="infrastructure-path-steps">{steps.map((step) => <div className="infrastructure-path-step" key={step.key}><div><Typography.Text strong>{step.label}</Typography.Text><Typography.Text type="secondary">{step.message}</Typography.Text></div>{stateTag(step.state)}</div>)}</div>}
  </Card>
}
