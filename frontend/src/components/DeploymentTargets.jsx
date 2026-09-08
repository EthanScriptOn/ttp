import {
  Button,
  Checkbox,
  Col,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd'
import {
  CheckCircleOutlined,
  ClusterOutlined,
  DeleteOutlined,
  EditOutlined,
  EnvironmentOutlined,
  InfoCircleOutlined,
  PlusOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'

const DEFAULT_VALUES = {
  name: '',
  environment: 'dev',
  stage: 'dev',
  sort_order: 1,
  cluster_id: '',
  namespace: '',
  replicas: 2,
  container_port: 8080,
  deploy_strategy: 'rolling',
  enabled: true,
}

export const STAGE_OPTIONS = [
  { value: 'dev', label: 'DEV · 开发' },
  { value: 'uat', label: 'UAT · 验收' },
  { value: 'pre', label: 'PRE · 预发布' },
  { value: 'prod', label: 'PROD · 生产' },
  { value: 'custom', label: '自定义阶段' },
]

export function stageLabel(stage) {
  return STAGE_OPTIONS.find((item) => item.value === stage)?.label || '自定义阶段'
}

function orderTargets(targets = []) {
  return [...targets].sort((a, b) => {
    const order = Number(a?.sort_order || 1) - Number(b?.sort_order || 1)
    return order || String(a?.name || '').localeCompare(String(b?.name || ''), 'zh-CN')
  })
}

const STRATEGIES = [
  { value: 'rolling', label: '滚动发布' },
  { value: 'canary', label: '灰度发布' },
  { value: 'blue_green', label: '蓝绿发布' },
]

function targetHealth(target) {
  if (target.health === 'healthy' || target.healthy_pod_count > 0 && target.healthy_pod_count === target.pod_count) return ['success', '运行正常']
  if (target.health === 'degraded') return ['warning', '部分异常']
  return ['default', target.pod_count ? '状态未知' : '尚未发布']
}

export function targetDisplay(target, clusters = []) {
  const cluster = clusters.find((item) => item.id === target?.cluster_id)
  return {
    clusterName: cluster?.name || target?.cluster_id || '未配置集群',
    context: `${cluster?.name || target?.cluster_id || '未配置集群'} / ${target?.namespace || '未配置 namespace'}`,
  }
}

export function DeploymentTargetSelect({ targets = [], value, onChange, disabled = false, className }) {
  const options = orderTargets(targets).map((target) => ({
    value: target.id,
    label: <span>#{target.sort_order} {target.name}</span>,
    disabled: !target.enabled,
  }))
  return <Select
    className={className}
    value={value || undefined}
    onChange={onChange}
    disabled={disabled}
    placeholder="选择发布环境"
    options={options}
    optionFilterProp="label"
  />
}

export function DeploymentTargetPicker({ targets = [], value = [], onChange, disabled = false }) {
  const selected = new Set(Array.isArray(value) ? value : [])
  const toggle = (target, checked) => {
    const next = new Set(selected)
    if (checked) next.add(target.id)
    else next.delete(target.id)
    onChange?.(Array.from(next))
  }
  if (!targets.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有可用的发布环境" />
  return <div className="target-picker-list">
    {orderTargets(targets).map((target) => {
      const [color, label] = targetHealth(target)
      return <label className={`target-picker-item ${selected.has(target.id) ? 'is-selected' : ''} ${!target.enabled ? 'is-disabled' : ''}`} key={target.id}>
        <Checkbox checked={selected.has(target.id)} disabled={disabled || !target.enabled} onChange={(event) => toggle(target, event.target.checked)} />
        <span className="target-picker-order">{target.sort_order}</span><span className="target-picker-main"><strong>{target.name}</strong><small>{stageLabel(target.stage)} · {target.cluster_id} / {target.namespace}</small></span>
        <Tag color={color}>{label}</Tag>
      </label>
    })}
  </div>
}

function activeReleaseForTarget(target, releases = []) {
  return releases.find((release) => {
    if (!['queued', 'running'].includes(release?.status)) return false
    return (release.targets || []).some((item) => item.id === target.id && ['pending', 'running'].includes(item.status))
  }) || null
}

export default function DeploymentTargets({
  project,
  targets = [],
  clusters = [],
  loading = false,
  canEdit = true,
  releases = [],
  onReload,
  onCreate,
  onUpdate,
  onDelete,
}) {
  const [form] = Form.useForm()
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState(null)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState('')
  const clusterOptions = useMemo(() => {
    const options = clusters.map((cluster) => ({
      value: cluster.id,
      label: `${cluster.name || cluster.id} (${cluster.type || 'Kubernetes'})`,
      disabled: ['offline', 'unavailable'].includes(cluster.status),
    }))
    if (editing?.cluster_id && !options.some((item) => item.value === editing.cluster_id)) {
      options.unshift({ value: editing.cluster_id, label: `当前集群 (${editing.cluster_id})` })
    }
    return options
  }, [clusters, editing])

  useEffect(() => {
    if (!modalOpen) return
    form.resetFields()
    form.setFieldsValue(editing ? {
      name: editing.name,
      environment: editing.environment,
      stage: editing.stage || 'custom',
      sort_order: editing.sort_order || 1,
      cluster_id: editing.cluster_id,
      namespace: editing.namespace,
      replicas: editing.replicas,
      container_port: editing.container_port,
      deploy_strategy: editing.deploy_strategy,
      enabled: editing.enabled,
    } : { ...DEFAULT_VALUES, sort_order: Math.max(1, ...targets.map((target) => Number(target.sort_order) || 0)) + 1 })
  }, [editing, form, modalOpen, targets])

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }

  const openEdit = (target) => {
    setEditing(target)
    setModalOpen(true)
  }

  const closeModal = () => {
    if (saving) return
    setModalOpen(false)
    setEditing(null)
  }

  const submit = async (values) => {
    setSaving(true)
    try {
      const payload = {
        ...values,
        name: values.name.trim(),
        environment: values.environment.trim().toLowerCase(),
        stage: values.stage,
        sort_order: Number(values.sort_order),
        namespace: values.namespace.trim().toLowerCase(),
        replicas: Number(values.replicas),
        container_port: Number(values.container_port),
      }
      if (editing) await onUpdate?.(editing, payload)
      else await onCreate?.(payload)
      closeModal()
    } catch (error) {
      message.error(error?.message || (editing ? '发布环境保存失败' : '发布环境创建失败'))
    } finally {
      setSaving(false)
    }
  }

  const toggleEnabled = async (target, enabled) => {
    try {
      await onUpdate?.(target, { enabled })
    } catch (error) {
      message.error(error?.message || '发布环境状态更新失败')
    }
  }

  const remove = async (target) => {
    setDeleting(target.id)
    try {
      await onDelete?.(target)
      message.success('发布环境已删除')
    } catch (error) {
      message.error(error?.message || '发布环境删除失败')
    } finally {
      setDeleting('')
    }
  }

  return <section className="deployment-targets-page">
    <div className="deployment-targets-heading">
      <div>
        <Typography.Title level={3}>发布环境</Typography.Title>
        <Typography.Paragraph type="secondary">每个环境单独设置集群、namespace、实例数和发布方式；发布时选择一个或多个环境。</Typography.Paragraph>
      </div>
      <Space wrap>
        {canEdit && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>添加环境</Button>}
        <Button onClick={onReload}>刷新</Button>
      </Space>
    </div>
    <div className="deployment-targets-context"><EnvironmentOutlined /><span>{project?.name || '当前项目'}</span><span className="context-separator">·</span><span>共 {targets.length} 个发布环境</span></div>
    {loading ? <div className="deployment-targets-loading"><Spin /><Typography.Text type="secondary">加载发布环境...</Typography.Text></div> : !targets.length ? <div className="deployment-targets-empty"><Empty description="还没有发布环境">{canEdit && <Button type="primary" onClick={openCreate}>添加第一个环境</Button>}</Empty></div> : <Row gutter={[14, 14]} className="deployment-target-grid">
      {orderTargets(targets).map((target) => {
        const [healthColor, healthLabel] = targetHealth(target)
        const { context } = targetDisplay(target, clusters)
        const activeRelease = activeReleaseForTarget(target, releases)
        const targetLocked = Boolean(activeRelease)
        const lockTitle = targetLocked ? `发布单 ${activeRelease.id} 正在发布，完成后才能修改环境配置` : undefined
        return <Col xs={24} md={12} xl={8} key={target.id}>
          <div className={`deployment-target-card ${!target.enabled ? 'is-disabled' : ''}`}>
            <div className="deployment-target-card-head"><div className="deployment-target-icon"><EnvironmentOutlined /></div><div className="deployment-target-title"><strong><span className="deployment-target-order">#{target.sort_order}</span><span className="deployment-target-name">{target.name}</span></strong></div><Space size={4}><Tag color={healthColor}>{healthLabel}</Tag></Space></div>
            <div className="deployment-target-context"><ClusterOutlined /><span title={context}>{context}</span></div>
            <div className="deployment-target-stats"><div><span>Pod</span><strong>{target.healthy_pod_count} / {target.pod_count}</strong><small>健康 / 总数</small></div><div><span>副本</span><strong>{target.replicas}</strong><small>{target.deploy_strategy === 'blue_green' ? '蓝绿' : target.deploy_strategy === 'canary' ? '灰度' : '滚动'}</small></div><div><span>最近版本</span><strong>{target.last_commit || '-'}</strong><small>{target.last_release || '暂无发布'}</small></div></div>
            <div className="deployment-target-actions"><Tooltip title={lockTitle}><span><Button type="link" size="small" icon={target.enabled ? <StopOutlined /> : <CheckCircleOutlined />} onClick={() => toggleEnabled(target, !target.enabled)} disabled={!canEdit || targetLocked}>{target.enabled ? '停用' : '启用'}</Button></span></Tooltip><Space size={2}>{canEdit && <Tooltip title={lockTitle}><span><Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(target)} disabled={targetLocked}>编辑</Button></span></Tooltip>}{canEdit && <Tooltip title={lockTitle}><span><Popconfirm title="删除这个发布环境？" description="删除后不会影响已经完成的历史发布。" okText="删除" cancelText="取消" onConfirm={() => remove(target)} disabled={targetLocked}><Button type="link" danger size="small" icon={<DeleteOutlined />} loading={deleting === target.id} disabled={targetLocked}>删除</Button></Popconfirm></span></Tooltip>}</Space></div>
          </div>
        </Col>
      })}
    </Row>}
    <Modal title={editing ? '编辑发布环境' : '添加发布环境'} open={modalOpen} onCancel={closeModal} onOk={() => form.submit()} confirmLoading={saving} okText="保存" cancelText="取消" destroyOnHidden width="min(680px, calc(100vw - 32px))">
      <Form form={form} layout="vertical" onFinish={submit} initialValues={DEFAULT_VALUES} className="deployment-target-form" requiredMark>
        <Row gutter={[16, 0]}><Col xs={24} sm={12}><Form.Item label="显示名称" name="name" rules={[{ required: true, message: '请输入环境名称' }, { max: 120, message: '名称不能超过 120 个字符' }]}><Input placeholder="例如：验收环境" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="环境标识" name="environment" rules={[{ required: true, message: '请输入环境标识' }, { pattern: /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/, message: '只能使用小写字母、数字和短横线' }]}><Input placeholder="例如：release" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label={<span>阶段类型 <Tooltip title="阶段类型决定发布流程，不看环境名称。发布必须从 DEV 开始。"><InfoCircleOutlined /></Tooltip></span>} name="stage" rules={[{ required: true, message: '请选择阶段类型' }]}><Select options={STAGE_OPTIONS} /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label={<span>发布顺序 <Tooltip title="数字越小越先发布。顺序 1 必须是 DEV。"><InfoCircleOutlined /></Tooltip></span>} name="sort_order" rules={[{ required: true, message: '请输入发布顺序' }, { type: 'number', min: 1, max: 99, message: '发布顺序范围为 1 到 99' }]}><InputNumber min={1} max={99} precision={0} style={{ width: '100%' }} /></Form.Item></Col><Col xs={24}><Form.Item label="部署集群" name="cluster_id" rules={[{ required: true, message: '请选择部署集群' }]}><Select options={clusterOptions} placeholder="选择集群" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="Kubernetes namespace" name="namespace" rules={[{ required: true, message: '请输入 namespace' }, { max: 63, message: 'namespace 最多 63 个字符' }, { pattern: /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/, message: '只能使用小写字母、数字和短横线' }]}><Input placeholder="例如：uat" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="发布策略" name="deploy_strategy"><Select options={STRATEGIES} /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="副本数" name="replicas" rules={[{ required: true, message: '请输入副本数' }, { type: 'number', min: 1, max: 100, message: '副本数范围为 1 到 100' }]}><InputNumber min={1} max={100} precision={0} style={{ width: '100%' }} /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="容器端口" name="container_port" rules={[{ required: true, message: '请输入容器端口' }, { type: 'number', min: 1, max: 65535, message: '端口范围为 1 到 65535' }]}><InputNumber min={1} max={65535} precision={0} style={{ width: '100%' }} /></Form.Item></Col></Row>
        <div className="deployment-target-form-switches"><Form.Item label="启用这个环境" name="enabled" valuePropName="checked"><Switch /></Form.Item></div>
      </Form>
    </Modal>
  </section>
}
