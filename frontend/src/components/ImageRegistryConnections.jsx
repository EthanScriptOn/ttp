import { Alert, Button, Card, Form, Input, Modal, Select, Space, Table, Tag, Typography, message } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import {
  createImageRegistryConnection,
  deleteImageRegistryConnection,
  getImageRegistryConnections,
  testImageRegistryConnection,
  updateImageRegistryConnection,
} from '../services/api'

const statusLabel = { active: '可用', invalid: '不可用', unverified: '未测试' }

export default function ImageRegistryConnections({ canRead = true, canManage = false }) {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(null)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState('')
  const [form] = Form.useForm()

  const load = async () => {
    if (!canRead) return
    setLoading(true)
    setError('')
    try {
      setItems(await getImageRegistryConnections())
    } catch (loadError) {
      setError(loadError.message || '镜像仓库连接加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [canRead])

  const startCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ auth_type: 'basic' })
    setOpen(true)
  }
  const startEdit = (item) => {
    setEditing(item)
    form.resetFields()
    form.setFieldsValue({ name: item.name, registry: item.registry, auth_type: item.auth_type, username: item.username, secret: '' })
    setOpen(true)
  }
  const submit = async (values) => {
    setSaving(true)
    try {
      const payload = { ...values, secret: values.secret || undefined }
      const next = editing ? await updateImageRegistryConnection(editing.id, payload) : await createImageRegistryConnection(payload)
      setItems((old) => editing ? old.map((item) => item.id === next.id ? next : item) : [...old, next])
      setOpen(false)
      message.success(editing ? '镜像仓库连接已更新' : '镜像仓库连接已添加')
    } catch (saveError) {
      message.error(saveError.message || '镜像仓库连接保存失败')
    } finally {
      setSaving(false)
    }
  }
  const test = async (item) => {
    setTesting(item.id)
    try {
      const next = await testImageRegistryConnection(item.id)
      setItems((old) => old.map((value) => value.id === next.id ? next : value))
      message.success('镜像仓库连接成功')
    } catch (testError) {
      message.error(testError.message || '镜像仓库连接测试失败')
      await load()
    } finally {
      setTesting('')
    }
  }
  const remove = (item) => {
    Modal.confirm({ title: `删除连接“${item.name}”？`, content: '已被项目使用的连接不能删除。', okText: '删除', okButtonProps: { danger: true }, cancelText: '取消', onOk: async () => {
      try {
        await deleteImageRegistryConnection(item.id)
        setItems((old) => old.filter((value) => value.id !== item.id))
        message.success('镜像仓库连接已删除')
      } catch (removeError) {
        message.error(removeError.message || '镜像仓库连接删除失败')
        throw removeError
      }
    } })
  }

  if (!canRead) return null
  const columns = [
    { title: '连接名称', dataIndex: 'name', key: 'name', render: (value) => <Typography.Text strong>{value}</Typography.Text> },
    { title: 'Registry', dataIndex: 'registry', key: 'registry', ellipsis: true },
    { title: '鉴权', dataIndex: 'auth_type', key: 'auth_type', render: (value) => value === 'token' ? 'Token' : '用户名 / 密码' },
    { title: '状态', dataIndex: 'status', key: 'status', render: (value) => <Tag color={value === 'active' ? 'green' : value === 'invalid' ? 'red' : 'default'}>{statusLabel[value] || value}</Tag> },
    ...(canManage ? [{ title: '操作', key: 'actions', width: 240, render: (_, item) => <Space size={4}><Button type="link" loading={testing === item.id} onClick={() => test(item)}>测试</Button><Button type="link" icon={<EditOutlined />} onClick={() => startEdit(item)}>编辑</Button><Button type="link" danger icon={<DeleteOutlined />} onClick={() => remove(item)}>删除</Button></Space> }] : []),
  ]

  return <Card className="space-settings-section" title={<span><SafetyCertificateOutlined /> 镜像仓库连接</span>} extra={<Space><Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>{canManage && <Button type="primary" icon={<PlusOutlined />} onClick={startCreate}>添加连接</Button>}</Space>}>
    <Typography.Paragraph type="secondary">项目只需选择连接；镜像仓库路径由平台按项目自动生成。用户名、密码或 Token 会加密保存在服务端，发布时临时用于推送和 Kubernetes 拉取。</Typography.Paragraph>
    {error && <Alert type="warning" showIcon message={error} />}
    <Table rowKey="id" loading={loading} columns={columns} dataSource={items} pagination={false} locale={{ emptyText: '当前空间还没有镜像仓库连接' }} />
    <Modal title={editing ? '编辑镜像仓库连接' : '添加镜像仓库连接'} open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} confirmLoading={saving} okText="保存" cancelText="取消" destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={submit} requiredMark>
        <Form.Item label="连接名称" name="name" rules={[{ required: true, message: '请输入连接名称' }]}><Input placeholder="例如：阿里云 ACR" /></Form.Item>
        <Form.Item label="Registry 地址" name="registry" extra="填写主机名，可带端口，不要填写 https:// 或路径。" rules={[{ required: true, message: '请输入 Registry 地址' }]}><Input placeholder="例如：registry.cn-hangzhou.aliyuncs.com" /></Form.Item>
        <Form.Item label="鉴权类型" name="auth_type" rules={[{ required: true }]}><Select options={[{ value: 'basic', label: '用户名 / 密码' }, { value: 'token', label: 'Token' }]} /></Form.Item>
        <Form.Item noStyle shouldUpdate={(prev, next) => prev.auth_type !== next.auth_type}>{({ getFieldValue }) => getFieldValue('auth_type') === 'basic' ? <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }]}><Input autoComplete="off" /></Form.Item> : <Form.Item label="用户名（可选）" name="username"><Input autoComplete="off" /></Form.Item>}</Form.Item>
        <Form.Item label="密码 / Token" name="secret" rules={[{ required: !editing, message: editing ? '留空表示保留原凭证' : '请输入密码或 Token' }]}><Input.Password autoComplete="new-password" placeholder={editing ? '留空表示保留原凭证' : '不会回显'} /></Form.Item>
      </Form>
    </Modal>
  </Card>
}
