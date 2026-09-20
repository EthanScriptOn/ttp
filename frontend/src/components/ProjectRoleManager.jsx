import { Alert, Button, Card, Checkbox, Form, Input, Modal, Popconfirm, Space, Tag, Typography, message } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import { createProjectRole, deleteProjectRole, getProjectRoles, updateProjectRole } from '../services/api'

const labels = {
  'project:view': '查看项目',
  'project:settings': '项目设置',
  'project:git:read': '读取代码仓库',
  'project:git:manage': '管理代码仓库连接',
  'project:deployment:read': '查看部署配置',
  'project:deployment:manage': '管理部署配置',
  'project:release:read': '查看发布',
  'project:release:create': '创建发布',
  'project:release:update': '修改发布草稿',
  'project:release:publish': '执行发布',
  'project:runtime:read': '查看运行态',
  'project:runtime:config': '修改运行配置',
  'project:runtime:terminal': '进入 Pod 终端',
  'project:members:read': '查看项目成员',
  'project:members:manage': '管理项目成员',
  'project:roles:manage': '管理项目角色授权',
}

export default function ProjectRoleManager({ canManage = false }) {
  const [roles, setRoles] = useState([])
  const [permissions, setPermissions] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm()

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const result = await getProjectRoles()
      setRoles(result.roles || [])
      setPermissions((result.permissions || []).map((item) => item.key ? item : ({ key: item, name: labels[item] || item })))
    } catch (loadError) {
      setError(loadError.message || '项目角色加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const customRoles = useMemo(() => roles.filter((role) => !role.is_system), [roles])
  const openCreate = () => {
    setEditing(null)
    form.resetFields()
    form.setFieldsValue({ permissions: permissions.map((item) => item.key).filter((key) => ['project:view', 'project:git:read', 'project:deployment:read', 'project:release:read', 'project:runtime:read'].includes(key)) })
    setOpen(true)
  }
  const openEdit = (role) => {
    setEditing(role)
    form.setFieldsValue({ name: role.name, description: role.description, permissions: role.permissions })
    setOpen(true)
  }
  const save = async (values) => {
    setSaving(true)
    try {
      const next = editing
        ? await updateProjectRole(editing.id, values)
        : await createProjectRole(values)
      setRoles((old) => editing ? old.map((role) => role.id === next.id ? next : role) : [...old, next])
      setOpen(false)
      message.success(editing ? '项目角色已更新' : '项目自定义角色已创建')
    } catch (saveError) {
      message.error(saveError.message || '项目角色保存失败')
    } finally {
      setSaving(false)
    }
  }
  const remove = async (role) => {
    try {
      await deleteProjectRole(role.id)
      setRoles((old) => old.filter((item) => item.id !== role.id))
      message.success('项目自定义角色已删除')
    } catch (removeError) {
      message.error(removeError.message || '项目角色删除失败')
    }
  }

  return <Card className="space-settings-section project-role-manager" title={<span><SafetyCertificateOutlined /> 项目角色与权限</span>} extra={canManage && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>创建自定义角色</Button>}>
    <Typography.Paragraph type="secondary">系统角色提供默认权限组合；自定义角色可以按项目需要绑定一组权限，再分配给项目成员。</Typography.Paragraph>
    {error && <Alert type="warning" showIcon message={error} />}
    <div className="project-role-manager-list">
      {roles.map((role) => <Card size="small" key={role.id || role.key} className="project-role-manager-item">
        <div className="project-role-manager-head">
          <div><Typography.Text strong>{role.name}</Typography.Text> {role.is_system ? <Tag color="blue">系统角色</Tag> : <Tag color="purple">自定义</Tag>}<Typography.Paragraph type="secondary">{role.description}</Typography.Paragraph></div>
          {!role.is_system && canManage && <Space><Button type="link" icon={<EditOutlined />} onClick={() => openEdit(role)}>编辑</Button><Popconfirm title="删除这个自定义角色？" description="已绑定成员时不能删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => remove(role)}><Button type="link" danger icon={<DeleteOutlined />}>删除</Button></Popconfirm></Space>}
        </div>
        <Space size={[4, 4]} wrap>{(role.permissions || []).map((permission) => <Tag key={permission}>{labels[permission] || permission}</Tag>)}</Space>
      </Card>)}
      {!loading && !roles.length && <Typography.Text type="secondary">暂无项目角色。</Typography.Text>}
    </div>
    <Modal title={editing ? '编辑项目自定义角色' : '创建项目自定义角色'} open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} okText="保存" cancelText="取消" confirmLoading={saving} destroyOnHidden width={680}>
      <Form form={form} layout="vertical" onFinish={save}>
        <Form.Item name="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]}><Input placeholder="例如：测试发布员" /></Form.Item>
        <Form.Item name="description" label="角色说明"><Input.TextArea rows={2} placeholder="说明这个角色适合谁使用" /></Form.Item>
        <Form.Item name="permissions" label="绑定权限" rules={[{ required: true, message: '至少选择一项权限' }]}>
          <Checkbox.Group style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 10 }}>
            {permissions.map((permission) => <Checkbox value={permission.key} key={permission.key}>{permission.name || labels[permission.key] || permission.key}</Checkbox>)}
          </Checkbox.Group>
        </Form.Item>
      </Form>
    </Modal>
  </Card>
}
