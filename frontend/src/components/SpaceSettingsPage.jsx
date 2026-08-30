import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
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
import {
  DeleteOutlined,
  LockOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import {
  createSpaceMember,
  getSpaceMembers,
  getSpacePermissions,
  getSpaceSettings,
  removeSpaceMember,
  updateSpaceMember,
  updateSpaceSettings,
} from '../services/api'
import {
  canManageMembers,
  hasPermission,
  localRoleDefinitions,
  normalizeRole,
  PERMISSIONS,
  ROLE_LABELS,
  ROLE_OPTIONS,
  roleDescription,
} from '../services/permissions'

const permissionNames = {
  'space:read': '查看空间',
  'space:update': '修改空间设置',
  'member:read': '查看成员',
  'member:manage': '管理成员和角色',
  'cluster:read': '查看集群和监控',
  'cluster:manage': '管理集群连接',
  'project:read': '查看项目',
  'project:create': '创建项目',
  'project:update': '修改项目配置',
  'release:read': '查看发布记录',
  'release:create': '创建发布草稿',
  'release:update': '修改发布草稿',
  'release:publish': '执行、取消和重复发布',
  'runtime:read': '查看 Pod、日志和指标',
  'runtime:config': '修改 Pod 运行配置',
  'audit:read': '查看操作记录',
}

const roleColors = { owner: 'gold', admin: 'green', developer: 'cyan', viewer: 'default' }

function formatDate(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function passwordRule(_, value) {
  if (!value || String(value).length < 8) return Promise.reject(new Error('密码至少需要 8 位'))
  if (String(value).length > 128) return Promise.reject(new Error('密码最多 128 位'))
  return Promise.resolve()
}

export default function SpaceSettingsPage({ role, user, onSpaceUpdated }) {
  const [settingsForm] = Form.useForm()
  const [memberForm] = Form.useForm()
  const [settings, setSettings] = useState(null)
  const [members, setMembers] = useState([])
  const [permissionData, setPermissionData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [savingSettings, setSavingSettings] = useState(false)
  const [memberModalOpen, setMemberModalOpen] = useState(false)
  const [memberSaving, setMemberSaving] = useState(false)
  const [changingMember, setChangingMember] = useState(0)
  const [removingMember, setRemovingMember] = useState(0)

  const currentRole = normalizeRole(settings?.role || role, 'viewer')
  const isSuperAdmin = Boolean(user?.is_super_admin)
  const canUpdateSpace = hasPermission(currentRole, PERMISSIONS.SPACE_UPDATE, isSuperAdmin)
  const canManage = canManageMembers(currentRole, isSuperAdmin)

  const load = async () => {
    setLoading(true)
    setError('')
    const results = await Promise.allSettled([
      getSpaceSettings(),
      getSpaceMembers(),
      getSpacePermissions(),
    ])
    const failures = []
    const [settingsResult, membersResult, permissionsResult] = results

    if (settingsResult.status === 'fulfilled') {
      setSettings(settingsResult.value)
      settingsForm.setFieldsValue({
        name: settingsResult.value?.space?.name || '',
        description: settingsResult.value?.space?.description || '',
      })
    } else failures.push(`空间资料：${settingsResult.reason?.message || '加载失败'}`)

    if (membersResult.status === 'fulfilled') setMembers(membersResult.value || [])
    else failures.push(`成员：${membersResult.reason?.message || '加载失败'}`)

    if (permissionsResult.status === 'fulfilled') setPermissionData(permissionsResult.value)
    else failures.push(`角色权限：${permissionsResult.reason?.message || '加载失败'}`)

    if (failures.length) setError(failures.join('；'))
    setLoading(false)
  }

  useEffect(() => {
    load()
  }, [])

  const roleDefinitions = useMemo(() => {
    const remote = permissionData?.roles || []
    return remote.length ? remote : localRoleDefinitions()
  }, [permissionData])

  const remotePermissionNames = useMemo(() => {
    const result = { ...permissionNames }
    for (const item of permissionData?.permissions || []) {
      if (item?.key && item?.name) result[item.key] = item.name
    }
    return result
  }, [permissionData])

  const saveSettings = async (values) => {
    setSavingSettings(true)
    try {
      const updated = await updateSpaceSettings({
        name: String(values.name || '').trim(),
        description: String(values.description || '').trim(),
      })
      setSettings((old) => ({ ...old, ...updated }))
      onSpaceUpdated?.(updated.space)
      message.success('空间设置已保存')
    } catch (saveError) {
      message.error(saveError.message || '空间设置保存失败')
    } finally {
      setSavingSettings(false)
    }
  }

  const openMemberModal = () => {
    memberForm.resetFields()
    memberForm.setFieldsValue({ role: 'developer' })
    setMemberModalOpen(true)
  }

  const addMember = async (values) => {
    setMemberSaving(true)
    try {
      await createSpaceMember({
        username: String(values.username || '').trim(),
        display_name: String(values.display_name || '').trim(),
        password: values.password,
        role: values.role,
      })
      setMemberModalOpen(false)
      memberForm.resetFields()
      message.success('成员已添加')
      const nextMembers = await getSpaceMembers()
      setMembers(nextMembers || [])
    } catch (addError) {
      message.error(addError.message || '添加成员失败')
    } finally {
      setMemberSaving(false)
    }
  }

  const changeRole = async (member, nextRole) => {
    if (!nextRole || nextRole === member.role) return
    setChangingMember(member.user_id)
    try {
      const updated = await updateSpaceMember(member.user_id, { role: nextRole })
      setMembers((old) => old.map((item) => item.user_id === member.user_id ? updated : item))
      message.success(`${member.username} 已调整为${ROLE_LABELS[nextRole] || nextRole}`)
    } catch (changeError) {
      message.error(changeError.message || '角色调整失败')
    } finally {
      setChangingMember(0)
    }
  }

  const removeMember = async (member) => {
    setRemovingMember(member.user_id)
    try {
      await removeSpaceMember(member.user_id)
      setMembers((old) => old.filter((item) => item.user_id !== member.user_id))
      message.success(`已移除成员 ${member.username}`)
    } catch (removeError) {
      message.error(removeError.message || '移除成员失败')
    } finally {
      setRemovingMember(0)
    }
  }

  const columns = [
    {
      title: '成员',
      key: 'member',
      render: (_, member) => (
        <div className="space-member-identity">
          <span className="space-member-avatar"><UserOutlined /></span>
          <span>
            <Typography.Text strong>{member.display_name || member.username}</Typography.Text>
            <Typography.Text type="secondary">{member.username}</Typography.Text>
          </span>
        </div>
      ),
    },
    {
      title: '角色',
      dataIndex: 'role',
      width: 190,
      render: (value, member) => (
        <Space size={6}>
          <Select
            size="small"
            value={normalizeRole(value)}
            options={ROLE_OPTIONS}
            loading={changingMember === member.user_id}
            disabled={!canManage || member.role === 'owner' || member.is_current_user || member.is_super_admin}
            onChange={(nextRole) => changeRole(member, nextRole)}
            className="space-member-role-select"
          />
          {member.is_current_user && <Tag color="green">当前用户</Tag>}
        </Space>
      ),
    },
    {
      title: '加入时间',
      dataIndex: 'joined_at',
      width: 170,
      render: formatDate,
    },
    {
      title: '操作',
      key: 'actions',
      width: 105,
      render: (_, member) => {
        const locked = !canManage || member.role === 'owner' || member.is_current_user || member.is_super_admin
        return locked ? <Typography.Text type="secondary">{member.role === 'owner' ? '空间所有者' : member.is_current_user ? '当前用户' : '不可操作'}</Typography.Text> : (
          <Popconfirm
            title={`确定移除 ${member.username}？`}
            description="移除后，对方将无法再访问当前空间。"
            okText="移除"
            cancelText="取消"
            okButtonProps={{ danger: true, loading: removingMember === member.user_id }}
            onConfirm={() => removeMember(member)}
          >
            <Button type="link" danger size="small" icon={<DeleteOutlined />} loading={removingMember === member.user_id}>移除</Button>
          </Popconfirm>
        )
      },
    },
  ]

  return (
    <div className="page-wrap space-settings-page">
      <div className="page-heading space-settings-heading">
        <div>
          <Typography.Text className="page-kicker">工作空间 · 管理</Typography.Text>
          <Typography.Title level={2}>空间设置</Typography.Title>
          <Typography.Paragraph type="secondary">管理空间资料、成员角色，以及每个角色能做什么。</Typography.Paragraph>
        </div>
        <Space>
          <Tag color={roleColors[currentRole] || 'default'} icon={<SafetyCertificateOutlined />}>{ROLE_LABELS[currentRole] || currentRole}</Tag>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        </Space>
      </div>

      {error && <Alert className="space-settings-alert" type="warning" showIcon message={error} />}

      {loading && !settings && !members.length ? <div className="space-settings-loading"><Spin /><Typography.Text type="secondary">加载空间设置...</Typography.Text></div> : (
        <div className="space-settings-grid">
          <section className="space-settings-section space-settings-profile">
            <div className="space-settings-section-head">
              <div><Typography.Title level={4}><SettingOutlined /> 空间资料</Typography.Title><Typography.Text type="secondary">这里的名称会显示在登录后的空间选择和顶部导航中。</Typography.Text></div>
              {!canUpdateSpace && <Tag>只读</Tag>}
            </div>
            {!canUpdateSpace && <Alert type="info" showIcon message="当前角色只能查看空间资料，不能修改设置。" />}
            <Form form={settingsForm} layout="vertical" onFinish={saveSettings} className="space-settings-form" requiredMark="optional">
              <Form.Item label="空间名称" name="name" rules={[{ required: true, message: '请输入空间名称' }, { max: 120, message: '空间名称最多 120 个字符' }]}>
                <Input disabled={!canUpdateSpace} placeholder="例如：研发空间" />
              </Form.Item>
              <Form.Item label="空间描述" name="description" rules={[{ max: 255, message: '空间描述最多 255 个字符' }]}>
                <Input.TextArea disabled={!canUpdateSpace} rows={4} showCount maxLength={255} placeholder="说明这个空间主要用于什么" />
              </Form.Item>
              {canUpdateSpace && <Button type="primary" icon={<SaveOutlined />} htmlType="submit" loading={savingSettings}>保存空间设置</Button>}
            </Form>
          </section>

          <section className="space-settings-section space-settings-members">
            <div className="space-settings-section-head">
              <div><Typography.Title level={4}><TeamOutlined /> 成员与权限</Typography.Title><Typography.Text type="secondary">成员加入空间后，能看到的项目和可执行的操作由角色决定。</Typography.Text></div>
              {canManage && <Button type="primary" icon={<PlusOutlined />} onClick={openMemberModal}>添加成员</Button>}
            </div>
            {!canManage && <Alert className="space-settings-readonly" type="info" showIcon message="你可以查看成员，但只有管理员或所有者可以添加、调整和移除成员。" />}
            <Table
              className="space-member-table"
              rowKey="user_id"
              columns={columns}
              dataSource={members}
              loading={loading}
              pagination={false}
              scroll={{ x: 680 }}
              locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前空间还没有成员" /> }}
            />
          </section>

          <section className="space-settings-section space-settings-roles">
            <div className="space-settings-section-head">
              <div><Typography.Title level={4}><SafetyCertificateOutlined /> 角色权限说明</Typography.Title><Typography.Text type="secondary">权限由服务端最终校验，页面上的按钮状态只是帮助你提前看懂可用操作。</Typography.Text></div>
            </div>
            <div className="space-role-grid">
              {roleDefinitions.map((definition) => {
                const definitionRole = normalizeRole(definition.key)
                const permissions = Array.isArray(definition.permissions) ? definition.permissions : []
                return <Card key={definitionRole} size="small" className={`space-role-card ${definitionRole === currentRole ? 'is-current' : ''}`}>
                  <div className="space-role-card-head"><div><Typography.Text strong>{ROLE_LABELS[definitionRole] || definition.name}</Typography.Text>{definitionRole === currentRole && <Tag color="green">当前角色</Tag>}</div><Tag color={roleColors[definitionRole] || 'default'}>{definitionRole}</Tag></div>
                  <Typography.Paragraph type="secondary">{definition.description || roleDescription(definitionRole)}</Typography.Paragraph>
                  <div className="space-role-permissions">{permissions.length ? permissions.map((permission) => <Tag key={permission}>{remotePermissionNames[permission] || permission}</Tag>) : <Typography.Text type="secondary">暂无权限说明</Typography.Text>}</div>
                </Card>
              })}
            </div>
          </section>
        </div>
      )}

      <Modal
        title={<span><UserOutlined /> 添加空间成员</span>}
        open={memberModalOpen}
        onCancel={() => setMemberModalOpen(false)}
        onOk={() => memberForm.submit()}
        okText="添加成员"
        cancelText="取消"
        confirmLoading={memberSaving}
        destroyOnClose
        width={520}
      >
        <Alert className="space-member-modal-alert" type="info" showIcon message="添加后，对方使用这个用户名和密码登录，再选择当前空间。" />
        <Form form={memberForm} layout="vertical" onFinish={addMember} requiredMark="optional" autoComplete="off">
          <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }, { pattern: /^[A-Za-z0-9._-]+$/, message: '只能使用字母、数字、点、下划线和短横线' }]}>
            <Input prefix={<UserOutlined />} placeholder="例如：zhangsan" />
          </Form.Item>
          <Form.Item label="显示名称" name="display_name" extra="不填写时使用用户名显示。">
            <Input placeholder="例如：张三" />
          </Form.Item>
          <Form.Item label="初始密码" name="password" rules={[{ required: true, message: '请输入初始密码' }, { validator: passwordRule }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="至少 8 位" />
          </Form.Item>
          <Form.Item label="空间角色" name="role" rules={[{ required: true, message: '请选择空间角色' }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
