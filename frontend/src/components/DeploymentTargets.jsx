import {
  Button,
  Checkbox,
  Col,
  Collapse,
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
  QuestionCircleOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import { DEFAULT_NAMESPACE_QUOTA, normalizeNamespaceQuota } from '../services/deployment-api-helpers'

const DEFAULT_VALUES = {
  name: '',
  environment: 'dev',
  stage: 'dev',
  sort_order: 1,
  cluster_id: '',
  deploy_strategy: 'rolling',
  enabled: true,
  resource_quota: { ...DEFAULT_NAMESPACE_QUOTA },
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

const TARGET_HELP = {
  namespace: '命名规则：TTP 空间 slug + 环境标识；同一集群中的不同 TTP 空间不会共用 namespace。',
  quota: '配额属于 TTP 空间 + 集群 + 环境。同一环境下的多个项目共享这份预算，避免一个项目耗尽整个集群的资源；如果该环境已经存在共享预算，新项目会沿用已有值，请编辑已有环境来调整。',
  containerDefaults: '每个容器的默认 request/limit 会由 TTP 写入 LimitRange；未填写时使用 100m/500m CPU、128Mi/512Mi 内存和 256Mi/1Gi 临时盘。',
}

function TargetHelp({ label, title }) {
  return <Tooltip overlayClassName="deployment-target-help-tooltip" placement="top" title={title}>
    <button type="button" className="deployment-target-help" aria-label={label} onClick={(event) => event.stopPropagation()}>
      <QuestionCircleOutlined />
    </button>
  </Tooltip>
}

function targetHealth(target) {
  if (target.health === 'healthy' || target.healthy_pod_count > 0 && target.healthy_pod_count === target.pod_count) return ['success', '运行正常']
  if (target.health === 'degraded') return ['warning', '部分异常']
  return ['default', target.pod_count ? '状态未知' : '尚未发布']
}

function quotaSummary(quota) {
  const value = normalizeNamespaceQuota(quota)
  return `CPU ${value.cpu_limit} · 内存 ${value.memory_limit} · 临时盘 ${value.ephemeral_storage_limit} · 持久盘 ${value.storage}`
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
      deploy_strategy: editing.deploy_strategy,
      enabled: editing.enabled,
      resource_quota: normalizeNamespaceQuota(editing.resource_quota),
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
        resource_quota: normalizeNamespaceQuota(values.resource_quota),
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
        <Typography.Paragraph type="secondary">每个环境单独设置集群、namespace、资源配额和发布方式；副本、端口、探针等工作负载规格由资源文件维护。</Typography.Paragraph>
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
            <div className="deployment-target-quota"><span>资源配额</span><strong>{quotaSummary(target.resource_quota)}</strong><small>Pod {normalizeNamespaceQuota(target.resource_quota).pods} · PVC {normalizeNamespaceQuota(target.resource_quota).persistent_volume_claims}</small></div>
            <div className="deployment-target-stats"><div><span>Pod</span><strong>{target.healthy_pod_count} / {target.pod_count}</strong><small>健康 / 总数</small></div><div><span>发布策略</span><strong>{target.deploy_strategy === 'blue_green' ? '蓝绿' : target.deploy_strategy === 'canary' ? '灰度' : '滚动'}</strong><small>流程策略</small></div><div><span>最近版本</span><strong>{target.last_commit || '-'}</strong><small>{target.last_release || '暂无发布'}</small></div></div>
            <div className="deployment-target-actions"><Tooltip title={lockTitle}><span><Button type="link" size="small" icon={target.enabled ? <StopOutlined /> : <CheckCircleOutlined />} onClick={() => toggleEnabled(target, !target.enabled)} disabled={!canEdit || targetLocked}>{target.enabled ? '停用' : '启用'}</Button></span></Tooltip><Space size={2}>{canEdit && <Tooltip title={lockTitle}><span><Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(target)} disabled={targetLocked}>编辑</Button></span></Tooltip>}{canEdit && <Tooltip title={lockTitle}><span><Popconfirm title="删除这个发布环境？" description="删除后不会影响已完成的历史发布；该环境由 TTP 管理的 PVC 及其中数据也会被删除。" okText="删除" cancelText="取消" onConfirm={() => remove(target)} disabled={targetLocked}><Button type="link" danger size="small" icon={<DeleteOutlined />} loading={deleting === target.id} disabled={targetLocked}>删除</Button></Popconfirm></span></Tooltip>}</Space></div>
          </div>
        </Col>
      })}
    </Row>}
    <Modal title={editing ? '编辑发布环境' : '添加发布环境'} open={modalOpen} onCancel={closeModal} onOk={() => form.submit()} confirmLoading={saving} okText="保存" cancelText="取消" destroyOnHidden width="min(680px, calc(100vw - 32px))">
      <Form form={form} layout="vertical" onFinish={submit} initialValues={DEFAULT_VALUES} className="deployment-target-form" requiredMark>
        <Row gutter={[16, 0]}><Col xs={24} sm={12}><Form.Item label="显示名称" name="name" rules={[{ required: true, message: '请输入环境名称' }, { max: 120, message: '名称不能超过 120 个字符' }]}><Input placeholder="例如：验收环境" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="环境标识" name="environment" rules={[{ required: true, message: '请输入环境标识' }, { pattern: /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/, message: '只能使用小写字母、数字和短横线' }]}><Input placeholder="例如：release" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label={<span>阶段类型 <Tooltip title="阶段类型决定发布流程，不看环境名称。发布必须从 DEV 开始。"><InfoCircleOutlined /></Tooltip></span>} name="stage" rules={[{ required: true, message: '请选择阶段类型' }]}><Select options={STAGE_OPTIONS} /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label={<span>发布顺序 <Tooltip title="数字越小越先发布。顺序 1 必须是 DEV。"><InfoCircleOutlined /></Tooltip></span>} name="sort_order" rules={[{ required: true, message: '请输入发布顺序' }, { type: 'number', min: 1, max: 99, message: '发布顺序范围为 1 到 99' }]}><InputNumber min={1} max={99} precision={0} style={{ width: '100%' }} /></Form.Item></Col><Col xs={24}><Form.Item label="部署集群" name="cluster_id" rules={[{ required: true, message: '请选择部署集群' }]}><Select options={clusterOptions} placeholder="选择集群" /></Form.Item></Col><Col xs={24} sm={12}><Form.Item label={<span>Kubernetes namespace <TargetHelp label="查看 namespace 命名规则" title={TARGET_HELP.namespace} /></span>}><Typography.Text code>{editing?.namespace || '保存后自动生成'}</Typography.Text></Form.Item></Col><Col xs={24} sm={12}><Form.Item label="发布策略" name="deploy_strategy"><Select options={STRATEGIES} /></Form.Item></Col></Row>
        <Collapse
          className="deployment-target-quota-collapse"
          defaultActiveKey={['quota']}
          items={[{
            key: 'quota',
            label: <span>环境资源配额 <TargetHelp label="查看环境资源配额说明" title={TARGET_HELP.quota} /></span>,
            children: <div className="deployment-target-quota-form">
              <Row gutter={[16, 0]}>
                <Col xs={24} sm={12}><Form.Item label="CPU request 总量" name={['resource_quota', 'cpu_request']} rules={[{ required: true, message: '请输入 CPU request 配额' }]}><Input placeholder="例如 2" /></Form.Item></Col>
                <Col xs={24} sm={12}><Form.Item label="CPU limit 总量" name={['resource_quota', 'cpu_limit']} rules={[{ required: true, message: '请输入 CPU limit 配额' }]}><Input placeholder="例如 4" /></Form.Item></Col>
                <Col xs={24} sm={12}><Form.Item label="内存 request 总量" name={['resource_quota', 'memory_request']} rules={[{ required: true, message: '请输入内存 request 配额' }]}><Input placeholder="例如 2Gi" /></Form.Item></Col>
                <Col xs={24} sm={12}><Form.Item label="内存 limit 总量" name={['resource_quota', 'memory_limit']} rules={[{ required: true, message: '请输入内存 limit 配额' }]}><Input placeholder="例如 4Gi" /></Form.Item></Col>
                <Col xs={24} sm={12}><Form.Item label="临时磁盘 request 总量" name={['resource_quota', 'ephemeral_storage_request']} rules={[{ required: true, message: '请输入临时磁盘 request 配额' }]}><Input placeholder="例如 10Gi" /></Form.Item></Col>
                <Col xs={24} sm={12}><Form.Item label="临时磁盘 limit 总量" name={['resource_quota', 'ephemeral_storage_limit']} rules={[{ required: true, message: '请输入临时磁盘 limit 配额' }]}><Input placeholder="例如 20Gi" /></Form.Item></Col>
                <Col xs={24} sm={12}><Form.Item label="持久化存储总量" name={['resource_quota', 'storage']} rules={[{ required: true, message: '请输入持久化存储配额' }]}><Input placeholder="例如 50Gi" /></Form.Item></Col>
                <Col xs={24} sm={6}><Form.Item label="Pod 数量上限" name={['resource_quota', 'pods']} rules={[{ required: true, type: 'number', min: 1, max: 10000, message: 'Pod 上限为 1 到 10000' }]}><InputNumber min={1} max={10000} precision={0} style={{ width: '100%' }} /></Form.Item></Col>
                <Col xs={24} sm={6}><Form.Item label="PVC 数量上限" name={['resource_quota', 'persistent_volume_claims']} rules={[{ required: true, type: 'number', min: 1, max: 1000, message: 'PVC 上限为 1 到 1000' }]}><InputNumber min={1} max={1000} precision={0} style={{ width: '100%' }} /></Form.Item></Col>
              </Row>
              <div className="deployment-target-default-help"><Typography.Text type="secondary">容器默认值</Typography.Text><TargetHelp label="查看容器默认 request 和 limit 说明" title={TARGET_HELP.containerDefaults} /></div>
            </div>,
          }]}
        />
        <div className="deployment-target-form-switches"><Form.Item label="启用这个环境" name="enabled" valuePropName="checked"><Switch /></Form.Item></div>
      </Form>
    </Modal>
  </section>
}
