import {
	Alert,
	Button,
  Card,
  Empty,
  Form,
  Input,
  Radio,
  Select,
  Slider,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  ArrowLeftOutlined,
  CheckCircleOutlined,
  ExperimentOutlined,
  PauseCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import './ABExperiment.css'

const STATUS_META = {
  running: { label: '运行中', color: 'green' },
  stopped: { label: '已停止', color: 'default' },
  finished: { label: '已结束', color: 'blue' },
}

function shortSha(version) {
  return version?.short_sha || version?.commit_sha?.slice(0, 10) || '-'
}

function versionText(version) {
  return `${version?.branch || '-'} · ${shortSha(version)}`
}

function releaseLabel(release) {
  const commit = release?.commits?.[0]
  const sha = commit?.short_sha || commit?.shortSha || commit?.sha?.slice(0, 10) || '-'
  return `${release?.id || '-'} · ${release?.branch || '-'} · ${sha}`
}

function targetLabel(target) {
  return target?.name || target?.environment?.toUpperCase() || target?.environment || '未命名环境'
}

function releaseSucceededForTarget(release, targetId) {
  return release?.status === 'succeeded' && release.targets?.some((target) => target.id === targetId && target.status === 'succeeded')
}

function releaseCommitShort(release) {
  const commit = release?.commits?.[0]
  return commit?.short_sha || commit?.shortSha || commit?.sha?.slice(0, 10) || ''
}

function releaseCanBeCandidate(release) {
  return Boolean(release?.id && release?.commits?.length) && !['failed', 'cancelled'].includes(release.status)
}

function statusTag(status) {
  const meta = STATUS_META[status] || { label: status || '未知', color: 'default' }
  return <Tag color={meta.color}>{meta.label}</Tag>
}

function VersionBlock({ label, version, traffic, tone }) {
  return <div className={`ab-version-block ${tone}`}>
    <div className="ab-version-label"><span>{label}</span><strong>{traffic}%</strong></div>
    <Typography.Text strong>{versionText(version)}</Typography.Text>
    <Typography.Text type="secondary" ellipsis>{version?.message || version?.release_id || '版本信息不可用'}</Typography.Text>
    <div className="ab-traffic-bar"><i style={{ width: `${traffic}%` }} /></div>
  </div>
}

function PodTable({ pods, emptyText }) {
  const columns = [
    { title: 'Pod', dataIndex: 'name', key: 'name', ellipsis: true },
    { title: '版本', dataIndex: 'version', key: 'version', render: (value) => <code>{value || '-'}</code> },
    { title: '节点', dataIndex: 'node_name', key: 'node_name', ellipsis: true },
    { title: '状态', dataIndex: 'ready', key: 'ready', render: (ready, item) => <Tag color={ready ? 'green' : 'orange'}>{ready ? 'Ready' : item.phase || '处理中'}</Tag> },
    { title: '重启', dataIndex: 'restart_count', key: 'restart_count', width: 70 },
  ]
  return <Table className="ab-pod-table" rowKey="name" size="small" columns={columns} dataSource={pods || []} pagination={false} locale={{ emptyText }} />
}

function StatsBlock({ label, stats }) {
  const available = stats?.metrics_available
  return <div className="ab-stats-block">
    <div className="ab-stats-head"><strong>{label}</strong><span>{available ? '指标已接入' : '暂无流量指标'}</span></div>
    <div className="ab-stats-grid">
      <div><span>请求量</span><strong>{available ? `${stats.request_rate_rps ?? 0} req/s` : '-'}</strong></div>
      <div><span>错误率</span><strong>{available ? `${stats.error_rate_percent ?? 0}%` : '-'}</strong></div>
      <div><span>P95 延迟</span><strong>{available ? `${stats.latency_p95_ms ?? 0} ms` : '-'}</strong></div>
    </div>
    {!available && <Typography.Text type="secondary">{stats?.metrics_message || '等待监控数据源提供该实验的分组指标'}</Typography.Text>}
  </div>
}

function ExperimentList({ experiments, onOpen, onCreate, onRefresh, loading, canCreate }) {
  const columns = [
    { title: '实验', key: 'name', render: (_, item) => <button type="button" className="ab-list-link" onClick={() => onOpen(item)}><strong>{item.name}</strong><span>{item.id}</span></button> },
    { title: '环境', key: 'environment', render: (_, item) => <span>{item.environment?.toUpperCase() || '-'}<small>{item.namespace}</small></span> },
    { title: 'A 版本', key: 'a_version', render: (_, item) => <code>{versionText(item.a_version)}</code> },
    { title: 'B 版本', key: 'b_version', render: (_, item) => <code>{versionText(item.b_version)}</code> },
    { title: '流量', key: 'traffic', render: (_, item) => <div className="ab-list-traffic"><span>A {item.a_traffic}%</span><span>B {item.b_traffic}%</span></div> },
    { title: '状态', dataIndex: 'status', key: 'status', render: statusTag },
  ]
  return <section className="ab-page">
    <div className="ab-page-header"><div><Typography.Title level={3}>A/B 实验</Typography.Title><Typography.Text type="secondary">让稳定版本和候选版本在同一环境并行运行</Typography.Text></div><Space><Button icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>刷新</Button><Button type="primary" icon={<PlusOutlined />} onClick={onCreate} disabled={!canCreate}>新建实验</Button></Space></div>
    <div className="ab-list-summary"><div><span>实验总数</span><strong>{experiments.length}</strong></div><div><span>运行中</span><strong>{experiments.filter((item) => item.status === 'running').length}</strong></div><div><span>环境</span><strong>{new Set(experiments.map((item) => item.target_id).filter(Boolean)).size}</strong></div></div>
    <Card className="ab-list-card" variant="borderless"><Table rowKey="id" columns={columns} dataSource={experiments} loading={loading} pagination={{ pageSize: 8, hideOnSinglePage: true }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无 A/B 实验" /> }} /></Card>
  </section>
}

function CreateExperiment({ targets, releases, onBack, onSubmit, submitting, canCreate }) {
  const [form] = Form.useForm()
  const targetId = Form.useWatch('target_id', form)
  const availableTargets = targets.filter((item) => item.enabled && item.status === 'active')
  const target = availableTargets.find((item) => item.id === targetId) || availableTargets[0]
  const successfulReleases = useMemo(() => releases.filter((item) => releaseSucceededForTarget(item, target?.id)), [releases, target?.id])
  const stableRelease = useMemo(() => {
    const currentCommit = target?.last_commit
    if (currentCommit) {
      const matched = successfulReleases.find((item) => releaseCommitShort(item).toLowerCase() === currentCommit.toLowerCase())
      if (matched) return matched
    }
    return successfulReleases[0]
  }, [successfulReleases, target?.last_commit])
  const bOptions = useMemo(() => releases.filter((item) => releaseCanBeCandidate(item) && item.id !== stableRelease?.id), [releases, stableRelease?.id])
	const assignment = Form.useWatch('assignment', form) ?? 'percentage'
	const canSubmit = canCreate && Boolean(target?.id) && Boolean(stableRelease?.id) && bOptions.length > 0
  useEffect(() => {
    if (!targetId && target?.id) form.setFieldValue('target_id', target.id)
    if (stableRelease?.id) form.setFieldValue('a_release_id', stableRelease.id)
    else form.resetFields(['a_release_id', 'b_release_id'])
    if (targetId) form.resetFields(['b_release_id'])
  }, [form, targetId, target?.id, stableRelease?.id])
  const bTraffic = Form.useWatch('b_traffic', form) ?? 1
  return <section className="ab-page">
    <div className="ab-page-header"><div><Button type="link" icon={<ArrowLeftOutlined />} onClick={onBack}>A/B 实验</Button><Typography.Title level={3}>新建实验</Typography.Title></div></div>
    {!availableTargets.length && <Alert type="warning" showIcon message="当前项目没有可用的发布环境" />}
    {availableTargets.length > 0 && !stableRelease && <Alert type="info" showIcon message="该环境还没有稳定版本，请先完成一次普通发布" />}
    {stableRelease && !bOptions.length && <Alert type="info" showIcon message="请先创建一个包含候选 commit 的发布单" />}
    {!canCreate && <Alert type="warning" showIcon message="当前账号没有创建 A/B 实验的权限" />}
	    <Form form={form} layout="vertical" className="ab-create-form" initialValues={{ b_traffic: 1, assignment: 'percentage', routing_rule: { path: '$.wx_id' } }} onFinish={onSubmit}>
      <Card className="ab-form-card" variant="borderless"><Typography.Title level={5}>实验范围</Typography.Title><div className="ab-form-grid">
        <Form.Item label="实验名称" name="name" rules={[{ required: true, message: '请输入实验名称' }]}><Input placeholder="例如：新版结算流程" /></Form.Item>
        <Form.Item label="发布环境" name="target_id" rules={[{ required: true, message: '请选择环境' }]}><Select options={availableTargets.map((item) => ({ value: item.id, label: targetLabel(item) }))} disabled={!availableTargets.length} /></Form.Item>
      </div></Card>
      <Card className="ab-form-card" variant="borderless"><Typography.Title level={5}>版本</Typography.Title><div className="ab-version-select-grid">
        <Form.Item label="A 版本 · 当前稳定版本" name="a_release_id"><Select disabled options={stableRelease ? [{ value: stableRelease.id, label: releaseLabel(stableRelease) }] : []} placeholder="该环境暂无稳定版本" /></Form.Item>
        <Form.Item label="B 版本 · 候选版本" name="b_release_id" rules={[{ required: true, message: '请选择 B 版本' }]}><Select options={bOptions.map((item) => ({ value: item.id, label: releaseLabel(item) }))} disabled={!bOptions.length} placeholder="选择要部署到该环境的候选发布单" /></Form.Item>
      </div><div className="ab-create-version-note"><ExperimentOutlined /> A/B 只绑定选中的发布单版本，开发分支后续提交不会改变实验。</div></Card>
	      <Card className="ab-form-card" variant="borderless"><Typography.Title level={5}>分组规则</Typography.Title><Form.Item label="用户分组" name="assignment"><Radio.Group><Radio value="percentage">按比例分流</Radio><Radio value="user_id">按 JSON 字段固定分组</Radio></Radio.Group></Form.Item>{assignment === 'user_id' && <Form.Item label="请求体字段路径" name={['routing_rule', 'path']} rules={[{ required: true, message: '请输入 JSON 字段路径' }]}><Input placeholder="$.wx_id" /></Form.Item>}{assignment === 'user_id' && <Typography.Text type="secondary" className="ab-routing-note">读取普通 HTTP JSON 请求体；字段不存在时进入 A 版本。</Typography.Text>}<Form.Item label={`B 版本起始流量 ${bTraffic}%`} name="b_traffic"><Slider min={1} max={100} marks={{ 1: '1%', 50: '50%', 100: '100%' }} /></Form.Item><div className="ab-traffic-preview"><span>A 版本 <strong>{100 - bTraffic}%</strong></span><span>B 版本 <strong>{bTraffic}%</strong></span></div></Card>
      <div className="ab-form-actions"><Button onClick={onBack}>取消</Button><Button type="primary" htmlType="submit" loading={submitting} disabled={!canSubmit} icon={<PlusOutlined />}>创建实验</Button></div>
    </Form>
  </section>
}

function ExperimentDetail({ experiment, onBack, onRefresh, onTraffic, onStop, onFinish, loading, operating, canOperate }) {
  const [bTraffic, setBTraffic] = useState(experiment.b_traffic)
  useEffect(() => setBTraffic(experiment.b_traffic), [experiment.b_traffic])
  const isRunning = experiment.status === 'running'
  const canChange = isRunning && canOperate
  const submitTraffic = async () => {
    try { await onTraffic({ a_traffic: 100 - bTraffic, b_traffic: bTraffic }) } catch { /* parent reports the API error */ }
  }
  return <section className="ab-page">
    <div className="ab-page-header"><div><Button type="link" icon={<ArrowLeftOutlined />} onClick={onBack}>A/B 实验</Button><div className="ab-detail-title"><Typography.Title level={3}>{experiment.name}</Typography.Title>{statusTag(experiment.status)}</div><Typography.Text type="secondary">{experiment.id} · {experiment.environment?.toUpperCase()} · {experiment.namespace}</Typography.Text></div><Space><Button icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>刷新</Button>{isRunning && <Button danger icon={<StopOutlined />} onClick={onStop} loading={operating} disabled={!canOperate} title={canOperate ? '停止实验' : '当前账号没有操作 A/B 实验的权限'}>停止实验</Button>}</Space></div>
	    <div className="ab-detail-meta"><span>分组方式 <strong>{experiment.assignment === 'user_id' ? `按 JSON 字段固定分组 · ${experiment.routing_rule?.path || '$.user_id'}` : '按比例分流'}</strong></span><span>发布方式 <strong>{experiment.strategy || 'rolling'}</strong></span><span>开始时间 <strong>{new Date(experiment.started_at).toLocaleString('zh-CN')}</strong></span></div>
    <div className="ab-version-grid"><VersionBlock label="A 稳定版本" version={experiment.a_version} traffic={experiment.a_traffic} tone="stable" /><VersionBlock label="B 实验版本" version={experiment.b_version} traffic={experiment.b_traffic} tone="candidate" /></div>
    <Card className="ab-traffic-card" variant="borderless"><div className="ab-card-head"><div><Typography.Title level={5}>流量分配</Typography.Title><Typography.Text type="secondary">B 版本从 1% 开始，可在实验运行中调整</Typography.Text></div><Button type="primary" disabled={!canChange || bTraffic === experiment.b_traffic} loading={operating} onClick={submitTraffic} title={canOperate ? '保存流量' : '当前账号没有操作 A/B 实验的权限'}>保存流量</Button></div><Slider disabled={!canChange} min={1} max={100} value={bTraffic} onChange={setBTraffic} marks={{ 1: 'B 1%', 50: 'B 50%', 100: 'B 100%' }} /><div className="ab-traffic-preview"><span>A 版本 <strong>{100 - bTraffic}%</strong></span><span>B 版本 <strong>{bTraffic}%</strong></span></div></Card>
    <div className="ab-stats-grid-wrap"><StatsBlock label="A 版本指标" stats={experiment.a_stats} /><StatsBlock label="B 版本指标" stats={experiment.b_stats} /></div>
    <div className="ab-pods-grid"><Card variant="borderless"><div className="ab-card-head"><Typography.Title level={5}>A 版本 Pod</Typography.Title><span>{experiment.a_pods?.length || 0} 个</span></div><PodTable pods={experiment.a_pods} emptyText="没有 A 版本 Pod" /></Card><Card variant="borderless"><div className="ab-card-head"><Typography.Title level={5}>B 版本 Pod</Typography.Title><span>{experiment.b_pods?.length || 0} 个</span></div><PodTable pods={experiment.b_pods} emptyText="没有 B 版本 Pod" /></Card></div>
    <Card variant="borderless" className="ab-events-card"><div className="ab-card-head"><Typography.Title level={5}>实验事件</Typography.Title><span>{experiment.events?.length || 0} 条</span></div><div className="ab-event-list">{experiment.events?.length ? experiment.events.map((event) => <div className="ab-event-row" key={event.id}><span className="ab-event-dot" /><div><strong>{event.message}</strong><small>{new Date(event.created_at).toLocaleString('zh-CN')}</small></div></div>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无事件" />}</div></Card>
    {isRunning && <div className="ab-finish-actions"><Typography.Text type="secondary">实验结束后选择结果：</Typography.Text><Button icon={<PauseCircleOutlined />} onClick={() => onFinish('keep_a')} loading={operating} disabled={!canOperate} title={canOperate ? '保留 A 版本' : '当前账号没有操作 A/B 实验的权限'}>保留 A 版本</Button><Button type="primary" icon={<CheckCircleOutlined />} onClick={() => onFinish('promote_b')} loading={operating} disabled={!canOperate} title={canOperate ? '切换到 B 版本' : '当前账号没有操作 A/B 实验的权限'}>切换到 B 版本</Button></div>}
  </section>
}

export default function ABExperiment({ project, targets = [], releases = [], experiments = [], loading = false, onRefresh, onCreate, onUpdateTraffic, onStop, onFinish, canCreate = true, canOperate = true }) {
  const [page, setPage] = useState('list')
  const [selected, setSelected] = useState(null)
  const [submitting, setSubmitting] = useState(false)
  const [operating, setOperating] = useState(false)
  const openDetail = (item) => { setSelected(item); setPage('detail') }
  const refreshDetail = async () => {
    const nextItems = await onRefresh?.()
    const next = nextItems?.find((item) => item.id === selected?.id) || experiments.find((item) => item.id === selected?.id)
    if (next) setSelected(next)
  }
  const submitCreate = async (values) => {
    setSubmitting(true)
    try { const created = await onCreate?.(values); if (created?.id) { setSelected(created); setPage('detail') } } finally { setSubmitting(false) }
  }
  const operate = async (action) => {
    setOperating(true)
    try { const updated = await action(); if (updated?.id) setSelected(updated) } finally { setOperating(false) }
  }
  if (page === 'create') return <CreateExperiment targets={targets} releases={releases} onBack={() => setPage('list')} onSubmit={submitCreate} submitting={submitting} canCreate={canCreate} />
  if (page === 'detail' && selected) return <ExperimentDetail experiment={selected} onBack={() => setPage('list')} onRefresh={refreshDetail} loading={loading} operating={operating} canOperate={canOperate} onTraffic={(payload) => operate(() => onUpdateTraffic?.(selected.id, payload))} onStop={() => operate(() => onStop?.(selected.id))} onFinish={(result) => operate(() => onFinish?.(selected.id, result))} />
  return <ExperimentList project={project} experiments={experiments} onOpen={openDetail} onCreate={() => setPage('create')} onRefresh={onRefresh} loading={loading} canCreate={canCreate} />
}
