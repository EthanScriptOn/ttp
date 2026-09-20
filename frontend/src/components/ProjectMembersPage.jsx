import {
  Alert,
  Avatar,
  Button,
  Card,
  Empty,
  Form,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import { DeleteOutlined, PlusOutlined, ReloadOutlined, SafetyCertificateOutlined, TeamOutlined, UserOutlined } from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import {
  createProjectMember,
  getProjectMembers,
  getProjectRoles,
  getSpaceMembers,
  removeProjectMember,
  updateProjectMember,
} from '../services/api'

const permissionLabels = {
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
  'project:roles:manage': '管理项目角色',
}

function roleOptions(roles) {
  return roles.map((role) => ({ value: role.id || role.key, label: `${role.name || role.key}${role.is_system ? '' : ' · 自定义'}`, role }))
}

export default function ProjectMembersPage({ project, canManage = false }) {
  const [members, setMembers] = useState([])
  const [roles, setRoles] = useState([])
  const [spaceMembers, setSpaceMembers] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [changing, setChanging] = useState(0)
  const [removing, setRemoving] = useState(0)
  const [form] = Form.useForm()

  const load = async () => {
    setLoading(true)
    setError('')
    const results = await Promise.allSettled([
      getProjectMembers(project.id),
      getProjectRoles(),
      getSpaceMembers(),
    ])
    const failures = []
    if (results[0].status === 'fulfilled') setMembers(results[0].value || [])
    else failures.push(`成员：${results[0].reason?.message || '加载失败'}`)
    if (results[1].status === 'fulfilled') setRoles(results[1].value?.roles || [])
    else failures.push(`角色：${results[1].reason?.message || '加载失败'}`)
    if (results[2].status === 'fulfilled') setSpaceMembers(results[2].value || [])
    else failures.push(`空间成员：${results[2].reason?.message || '加载失败'}`)
    if (failures.length) setError(failures.join('；'))
    setLoading(false)
  }

  useEffect(() => { load() }, [project.id])

  const availableMembers = useMemo(() => {
    const selected = new Set(members.map((item) => item.user_id))
    return spaceMembers.filter((item) => !selected.has(item.user_id) && !item.is_super_admin)
  }, [members, spaceMembers])

  const openCreate = () => {
    form.resetFields()
    const firstRole = roles.find((role) => role.key === 'project_developer') || roles[0]
    form.setFieldsValue({ role_ref: firstRole?.id || firstRole?.key })
    setOpen(true)
  }

  const save = async (values) => {
    const role = roles.find((item) => (item.id || item.key) === values.role_ref)
    if (!role) return
    setSaving(true)
    try {
      await createProjectMember(project.id, {
        user_id: values.user_id,
        role_id: role.is_system ? '' : role.id,
        role_key: role.is_system ? role.key : '',
      })
      setOpen(false)
      message.success('项目成员已添加')
      await load()
    } catch (saveError) {
      message.error(saveError.message || '添加项目成员失败')
    } finally {
      setSaving(false)
    }
  }

  const changeRole = async (member, reference) => {
    const role = roles.find((item) => (item.id || item.key) === reference)
    if (!role) return
    setChanging(member.user_id)
    try {
      const updated = await updateProjectMember(project.id, member.user_id, {
        role_id: role.is_system ? '' : role.id,
        role_key: role.is_system ? role.key : '',
      })
      setMembers((old) => old.map((item) => item.user_id === member.user_id ? updated : item))
      message.success(`${member.display_name || member.username} 的项目角色已更新`)
    } catch (changeError) {
      message.error(changeError.message || '项目角色更新失败')
    } finally {
      setChanging(0)
    }
  }

  const remove = async (member) => {
    setRemoving(member.user_id)
    try {
      await removeProjectMember(project.id, member.user_id)
      setMembers((old) => old.filter((item) => item.user_id !== member.user_id))
      message.success('项目成员已移除')
    } catch (removeError) {
      message.error(removeError.message || '移除项目成员失败')
    } finally {
      setRemoving(0)
    }
  }

  const columns = [
    {
      title: '成员',
      key: 'member',
      render: (_, member) => (
        <Space>
          <Avatar size="small" icon={<UserOutlined />} />
          <span>
            <Typography.Text strong>{member.display_name || member.username}</Typography.Text>
            <br />
            <Typography.Text type="secondary">{member.username}</Typography.Text>
          </span>
        </Space>
      ),
    },
    {
      title: '项目角色',
      key: 'role',
      width: 250,
      render: (_, member) => {
        const roleRef = member.role_id || member.role_key
        return <Space>
          <Select
            size="small"
            value={roleRef}
            options={roleOptions(roles)}
            disabled={!canManage || member.is_current_user || member.is_super_admin}
            loading={changing === member.user_id}
            onChange={(value) => changeRole(member, value)}
            style={{ minWidth: 190 }}
          />
          {member.is_current_user && <Tag color="green">当前用户</Tag>}
        </Space>
      },
    },
    {
      title: '权限',
      key: 'permissions',
      render: (_, member) => (
        <Space size={[4, 4]} wrap>
          {(member.permissions || []).slice(0, 4).map((permission) => <Tag key={permission}>{permissionLabels[permission] || permission}</Tag>)}
          {(member.permissions || []).length > 4 && <Tag>+{member.permissions.length - 4}</Tag>}
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      render: (_, member) => canManage && !member.is_current_user && !member.is_super_admin ? (
        <Popconfirm
          title={`移除 ${member.username}？`}
          description="移除后将无法访问该项目。"
          okText="移除"
          cancelText="取消"
          okButtonProps={{ danger: true, loading: removing === member.user_id }}
          onConfirm={() => remove(member)}
        >
          <Button type="link" danger icon={<DeleteOutlined />} loading={removing === member.user_id}>移除</Button>
        </Popconfirm>
      ) : <Typography.Text type="secondary">-</Typography.Text>,
    },
  ]

  return <div className="page-wrap project-members-page">
    <div className="page-heading">
      <div>
        <Typography.Text className="page-kicker">项目 · 访问控制</Typography.Text>
        <Typography.Title level={2}>成员权限</Typography.Title>
        <Typography.Paragraph type="secondary">只有被加入项目的空间成员才能访问项目。角色决定代码、部署、发布和运行态权限。</Typography.Paragraph>
      </div>
      <Space>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        {canManage && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate} disabled={!availableMembers.length}>添加成员</Button>}
      </Space>
    </div>
    {error && <Alert type="warning" showIcon message={error} />}
    {!canManage && <Alert type="info" showIcon message="你可以查看项目成员和角色，但只有项目维护者可以调整授权。" />}
    {loading && !members.length ? <div className="space-settings-loading"><Spin /><Typography.Text type="secondary">加载项目权限...</Typography.Text></div> : (
      <Card variant="borderless" className="project-members-card" title={<span><TeamOutlined /> 项目成员</span>}>
        <Table
          rowKey="user_id"
          columns={columns}
          dataSource={members}
          loading={loading}
          pagination={false}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有项目成员" /> }}
        />
      </Card>
    )}
    <Card variant="borderless" className="project-roles-card" title={<span><SafetyCertificateOutlined /> 可用项目角色</span>}>
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        {roles.map((role) => <div className="project-role-summary" key={role.id || role.key}>
          <div><Typography.Text strong>{role.name}</Typography.Text>{role.is_system ? <Tag color="blue">系统角色</Tag> : <Tag color="purple">自定义</Tag>}<Typography.Paragraph type="secondary">{role.description}</Typography.Paragraph></div>
          <Space size={[4, 4]} wrap>{(role.permissions || []).map((permission) => <Tag key={permission}>{permissionLabels[permission] || permission}</Tag>)}</Space>
        </div>)}
      </Space>
    </Card>
    <Modal title="添加项目成员" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} confirmLoading={saving} okText="添加" cancelText="取消" destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={save}>
        <Form.Item name="user_id" label="空间成员" rules={[{ required: true, message: '请选择空间成员' }]}>
          <Select showSearch optionFilterProp="label" options={availableMembers.map((member) => ({ value: member.user_id, label: `${member.display_name || member.username}（${member.username}）` }))} placeholder="选择要加入项目的成员" />
        </Form.Item>
        <Form.Item name="role_ref" label="项目角色" rules={[{ required: true, message: '请选择项目角色' }]}>
          <Select options={roleOptions(roles)} placeholder="选择项目角色" />
        </Form.Item>
      </Form>
    </Modal>
  </div>
}
