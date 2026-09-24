import {
  Alert,
  Avatar,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Drawer,
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
  Tooltip,
  Typography,
  message,
} from 'antd'
import {
  DeleteOutlined,
  EditOutlined,
  LockOutlined,
  PlusOutlined,
  ReloadOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useEffect, useMemo, useState } from 'react'
import {
  createProjectMember,
  createProjectRole,
  createSpaceMember,
  deleteProjectRole,
  getProjectMembers,
  getProjectRoles,
  getProjects,
  getSpaceMembers,
  getSpacePermissions,
  removeProjectMember,
  removeSpaceMember,
  updateProjectMember,
  updateProjectRole,
  updateSpaceMember,
} from '../services/api'
import {
  canManageMembers,
  localRoleDefinitions,
  normalizeRole,
  ROLE_LABELS,
  ROLE_OPTIONS,
  roleDescription,
} from '../services/permissions'

const SPACE_PERMISSION_LABELS = {
  'space:read': '查看空间',
  'space:update': '修改空间设置',
  'member:read': '查看成员',
  'member:manage': '管理成员',
  'cluster:read': '查看集群',
  'cluster:manage': '管理集群',
  'project:read': '查看项目',
  'project:create': '创建项目',
  'project:update': '修改项目',
  'registry:read': '查看镜像仓库连接',
  'registry:manage': '管理镜像仓库连接',
  'release:read': '查看发布',
  'release:create': '创建发布',
  'release:update': '修改发布草稿',
  'release:publish': '执行发布',
  'runtime:read': '查看运行态',
  'runtime:config': '修改运行配置',
  'runtime:terminal': '进入 Pod 终端',
  'audit:read': '查看操作记录',
  'project:roles:manage': '管理项目角色',
}

const PROJECT_PERMISSION_LABELS = {
  'project:view': '查看项目',
  'project:settings': '管理项目设置',
  'project:git:read': '读取代码仓库',
  'project:git:manage': '管理代码仓库连接',
  'project:deployment:read': '查看部署配置',
  'project:deployment:manage': '管理部署配置',
  'project:release:read': '查看发布',
  'project:release:create': '创建发布',
  'project:release:update': '管理发布单',
  'project:release:publish': '执行发布',
  'project:runtime:read': '查看运行态',
  'project:runtime:config': '修改运行配置',
  'project:runtime:terminal': '进入 Pod 终端',
  'project:members:read': '查看项目成员',
  'project:members:manage': '管理项目成员',
}

const FALLBACK_PROJECT_ROLES = [
  {
    key: 'project_viewer',
    name: '项目只读',
    description: '查看项目、代码、部署和运行状态。',
    is_system: true,
    permissions: ['project:view', 'project:git:read', 'project:deployment:read', 'project:release:read', 'project:runtime:read'],
  },
  {
    key: 'project_developer',
    name: '项目开发者',
    description: '修改项目配置和部署资源，可以创建发布草稿。',
    is_system: true,
    permissions: ['project:view', 'project:settings', 'project:git:read', 'project:deployment:read', 'project:deployment:manage', 'project:release:read', 'project:release:create', 'project:release:update', 'project:runtime:read', 'project:runtime:config'],
  },
  {
    key: 'project_release_manager',
    name: '发布负责人',
    description: '负责发布流程和发布运行态，但不管理项目成员。',
    is_system: true,
    permissions: ['project:view', 'project:git:read', 'project:deployment:read', 'project:release:read', 'project:release:create', 'project:release:update', 'project:release:publish', 'project:runtime:read'],
  },
  {
    key: 'project_maintainer',
    name: '项目维护者',
    description: '拥有项目全部权限，包括项目成员授权。',
    is_system: true,
    permissions: ['project:view', 'project:settings', 'project:git:read', 'project:git:manage', 'project:deployment:read', 'project:deployment:manage', 'project:release:read', 'project:release:create', 'project:release:update', 'project:release:publish', 'project:runtime:read', 'project:runtime:config', 'project:runtime:terminal', 'project:members:read', 'project:members:manage'],
  },
]

const ROLE_COLORS = {
  owner: 'gold',
  admin: 'green',
  developer: 'cyan',
  viewer: 'default',
}

const SPACE_ROLE_OPTIONS = [
  { value: 'owner', label: ROLE_LABELS.owner, disabled: true },
  ...ROLE_OPTIONS,
]

const PERMISSION_RESOURCE_LABELS = {
  space: '空间',
  member: '成员与授权',
  cluster: '集群',
  project: '项目',
  registry: '镜像仓库',
  release: '发布',
  runtime: '运行态',
  audit: '操作记录',
  git: '代码仓库',
  deployment: '部署配置',
}

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

function avatarColor(member) {
  const value = String(member?.user_id || member?.username || '')
  const colors = ['#73a78d', '#77a7d7', '#998dd0', '#d4a45d', '#5b9d9a']
  return colors[Number(value.replace(/\D/g, '').slice(-2) || 0) % colors.length]
}

function initials(member) {
  const source = member?.display_name || member?.username || '?'
  return String(source).trim().slice(0, 1).toUpperCase()
}

function passwordRule(_, value) {
  if (!value || String(value).length < 8) return Promise.reject(new Error('密码至少需要 8 位'))
  if (String(value).length > 128) return Promise.reject(new Error('密码最多 128 位'))
  return Promise.resolve()
}

function roleReference(role) {
  return role?.is_system ? role.key : role?.id
}

function roleLabel(role) {
  if (!role) return '未授权'
  return role.name || ROLE_LABELS[role.key] || role.key
}

function roleScope(role) {
  if (role?.scope === 'space') return '空间级'
  if (role?.scope === 'project') return '项目级'
  if (role?.is_system && !String(role.key || '').startsWith('project_')) return '空间级'
  return '项目级'
}

function rolePermissions(role, fallback = []) {
  return Array.isArray(role?.permissions) && role.permissions.length ? role.permissions : fallback
}

function projectRoleOptions(roles) {
  return roles.map((role) => ({
    value: roleReference(role),
    label: `${roleLabel(role)}${role.is_system ? '' : ' · 自定义'}`,
  }))
}

function rolePermissionCount(role) {
  return Array.isArray(role?.permissions) ? role.permissions.length : 0
}

function canUseRoleForProject(role) {
  return role?.key && (role.key.startsWith('project_') || !role.is_system)
}

function buildPermissionGroups(permissions, labels) {
  const grouped = new Map()
  permissions.forEach((permission) => {
    const key = permission?.key || permission
    if (!key) return
    const parts = String(key).split(':')
    const projectResource = {
      git: 'git',
      deployment: 'deployment',
      release: 'release',
      runtime: 'runtime',
      members: 'member',
      roles: 'member',
      view: 'project',
      settings: 'project',
    }
    const resource = parts[0] === 'project'
      ? projectResource[parts[1]] || 'project'
      : parts[0]
    const current = grouped.get(resource) || []
    current.push({
      key,
      name: permission?.name || labels[key] || key,
      description: permission?.description || '',
    })
    grouped.set(resource, current)
  })
  return Array.from(grouped.entries()).map(([resource, items]) => ({ resource, items }))
}

function normalizePermissionDefinitions(values, labels) {
  if (!Array.isArray(values) || !values.length) {
    return Object.entries(labels).map(([key, name]) => ({ key, name, description: '' }))
  }
  return values
    .map((permission) => {
      const key = permission?.key || permission
      if (!key) return null
      return {
        key: String(key),
        name: permission?.name || labels[key] || String(key),
        description: permission?.description || '',
      }
    })
    .filter(Boolean)
}

export default function SpaceMembersPage({ role, user }) {
  const [memberForm] = Form.useForm()
  const [projectMemberForm] = Form.useForm()
  const [roleForm] = Form.useForm()
  const [members, setMembers] = useState([])
  const [spacePermissions, setSpacePermissions] = useState(null)
  const [projectRoles, setProjectRoles] = useState([])
  const [projects, setProjects] = useState([])
  const [projectMembers, setProjectMembers] = useState({})
  const [activeTab, setActiveTab] = useState('members')
  const [matrixScope, setMatrixScope] = useState('space')
  const [selectedPermissionRoleRef, setSelectedPermissionRoleRef] = useState('')
  const [memberKeyword, setMemberKeyword] = useState('')
  const [selectedProjectId, setSelectedProjectId] = useState('')
  const [loading, setLoading] = useState(true)
  const [projectMembersLoading, setProjectMembersLoading] = useState(false)
  const [projectMembersReady, setProjectMembersReady] = useState(false)
  const [error, setError] = useState('')
  const [projectRolesAvailable, setProjectRolesAvailable] = useState(true)
  const [memberModalOpen, setMemberModalOpen] = useState(false)
  const [projectMemberModalOpen, setProjectMemberModalOpen] = useState(false)
  const [roleModalOpen, setRoleModalOpen] = useState(false)
  const [memberSaving, setMemberSaving] = useState(false)
  const [projectMemberSaving, setProjectMemberSaving] = useState(false)
  const [roleSaving, setRoleSaving] = useState(false)
  const [changingMember, setChangingMember] = useState(0)
  const [changingProjectMember, setChangingProjectMember] = useState(0)
  const [removingMember, setRemovingMember] = useState(0)
  const [removingProjectMember, setRemovingProjectMember] = useState(0)
  const [editingRole, setEditingRole] = useState(null)
  const [drawer, setDrawer] = useState({ type: '', value: null })

  const currentRole = normalizeRole(role, 'viewer')
  const isSuperAdmin = Boolean(user?.is_super_admin)
  const canManage = canManageMembers(currentRole, isSuperAdmin)
  const selectedProject = projects.find((project) => project.id === selectedProjectId) || null
  const selectedProjectMembers = projectMembers[selectedProjectId] || []

  const loadProjectMembers = async (projectItems = projects) => {
    if (!projectItems.length) {
      setProjectMembers({})
      setProjectMembersReady(true)
      return
    }
    setProjectMembersLoading(true)
    const results = await Promise.allSettled(projectItems.map((project) => getProjectMembers(project.id)))
    const next = {}
    const failures = []
    results.forEach((result, index) => {
      const project = projectItems[index]
      if (result.status === 'fulfilled') next[project.id] = result.value || []
      else failures.push(`${project.name || project.id}：${result.reason?.message || '加载失败'}`)
    })
    setProjectMembers(next)
    setProjectMembersReady(failures.length === 0)
    if (failures.length && failures.length === projectItems.length) {
      const warning = '项目授权暂时无法加载，请确认当前账号拥有项目成员查看权限。'
      message.warning(warning)
    }
    setProjectMembersLoading(false)
  }

  const load = async () => {
    setLoading(true)
    setError('')
    const results = await Promise.allSettled([
      getSpaceMembers(),
      getSpacePermissions(),
      getProjectRoles(),
      getProjects(),
    ])
    const failures = []
    const nextMembers = results[0].status === 'fulfilled' ? results[0].value || [] : []
    const nextSpacePermissions = results[1].status === 'fulfilled' ? results[1].value : null
    const projectRolesResult = results[2]
    const nextProjectRoles = projectRolesResult.status === 'fulfilled' ? projectRolesResult.value?.roles || [] : FALLBACK_PROJECT_ROLES
    const nextProjects = results[3].status === 'fulfilled' ? results[3].value || [] : []
    if (results[0].status === 'rejected') failures.push(`成员：${results[0].reason?.message || '加载失败'}`)
    if (results[1].status === 'rejected') failures.push(`空间权限：${results[1].reason?.message || '加载失败'}`)
    setProjectRolesAvailable(projectRolesResult.status === 'fulfilled')
    if (projectRolesResult.status === 'rejected' && projectRolesResult.reason?.status !== 404) failures.push(`项目角色：${projectRolesResult.reason?.message || '加载失败'}`)
    if (results[3].status === 'rejected') failures.push(`项目：${results[3].reason?.message || '加载失败'}`)
    setMembers(nextMembers)
    setSpacePermissions(nextSpacePermissions)
    setProjectRoles(nextProjectRoles)
    setProjects(nextProjects)
    setSelectedProjectId((old) => old && nextProjects.some((project) => project.id === old) ? old : nextProjects[0]?.id || '')
    if (failures.length) setError(failures.join('；'))
    setLoading(false)
    await loadProjectMembers(nextProjects)
  }

  useEffect(() => {
    load()
  }, [])

  const spaceRoleDefinitions = useMemo(() => {
    const remote = spacePermissions?.roles || []
    return remote.length ? remote : localRoleDefinitions()
  }, [spacePermissions])

  const permissionNames = useMemo(() => {
    const result = { ...SPACE_PERMISSION_LABELS, ...PROJECT_PERMISSION_LABELS }
    for (const item of spacePermissions?.permissions || []) {
      if (item?.key && item?.name) result[item.key] = item.name
    }
    return result
  }, [spacePermissions])

  const availableProjectRoles = useMemo(() => projectRoles.filter(canUseRoleForProject), [projectRoles])

  const visibleMembers = useMemo(() => {
    const keyword = memberKeyword.trim().toLowerCase()
    if (!keyword) return members
    return members.filter((member) => `${member.display_name || ''} ${member.username || ''}`.toLowerCase().includes(keyword))
  }, [memberKeyword, members])

  const accessByUser = useMemo(() => {
    const result = new Map()
    projects.forEach((project) => {
      for (const member of projectMembers[project.id] || []) {
        const current = result.get(member.user_id) || []
        current.push({ project, member })
        result.set(member.user_id, current)
      }
    })
    return result
  }, [projectMembers, projects])

  const userAccess = (member) => {
    if (member.is_super_admin || member.role === 'owner' || member.role === 'admin') {
      return { count: projects.length, inherited: true, items: projects.map((project) => ({ project, member: null })) }
    }
    if (!projectMembersReady) return { count: null, inherited: false, unknown: true, items: [] }
    const items = accessByUser.get(member.user_id) || []
    return { count: items.length, inherited: false, items }
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
      await load()
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
      setProjectMembers((old) => Object.fromEntries(Object.entries(old).map(([projectId, items]) => [projectId, items.filter((item) => item.user_id !== member.user_id)])))
      message.success(`已移除成员 ${member.username}`)
    } catch (removeError) {
      message.error(removeError.message || '移除成员失败')
    } finally {
      setRemovingMember(0)
    }
  }

  const openProjectMemberModal = () => {
    projectMemberForm.resetFields()
    const firstRole = availableProjectRoles.find((item) => item.key === 'project_developer') || availableProjectRoles[0]
    projectMemberForm.setFieldsValue({ role_ref: roleReference(firstRole) })
    setProjectMemberModalOpen(true)
  }

  const refreshProjectMembers = async (projectId = selectedProjectId) => {
    if (!projectId) return
    try {
      const result = await getProjectMembers(projectId)
      setProjectMembers((old) => ({ ...old, [projectId]: result || [] }))
    } catch (loadError) {
      message.error(loadError.message || '项目成员加载失败')
    }
  }

  const addProjectMember = async (values) => {
    const selectedRole = availableProjectRoles.find((item) => roleReference(item) === values.role_ref)
    if (!selectedRole || !selectedProjectId) return
    setProjectMemberSaving(true)
    try {
      await createProjectMember(selectedProjectId, {
        user_id: values.user_id,
        role_id: selectedRole.is_system ? '' : selectedRole.id,
        role_key: selectedRole.is_system ? selectedRole.key : '',
      })
      setProjectMemberModalOpen(false)
      projectMemberForm.resetFields()
      message.success('项目成员已添加')
      await refreshProjectMembers()
    } catch (saveError) {
      message.error(saveError.message || '添加项目成员失败')
    } finally {
      setProjectMemberSaving(false)
    }
  }

  const changeProjectRole = async (member, roleRef) => {
    const selectedRole = availableProjectRoles.find((item) => roleReference(item) === roleRef)
    if (!selectedRole) return
    setChangingProjectMember(member.user_id)
    try {
      await updateProjectMember(selectedProjectId, member.user_id, {
        role_id: selectedRole.is_system ? '' : selectedRole.id,
        role_key: selectedRole.is_system ? selectedRole.key : '',
      })
      message.success(`${member.display_name || member.username} 的项目角色已更新`)
      await refreshProjectMembers()
    } catch (changeError) {
      message.error(changeError.message || '项目角色更新失败')
    } finally {
      setChangingProjectMember(0)
    }
  }

  const removeProjectMemberAccess = async (member) => {
    setRemovingProjectMember(member.user_id)
    try {
      await removeProjectMember(selectedProjectId, member.user_id)
      message.success('项目授权已移除')
      await refreshProjectMembers()
    } catch (removeError) {
      message.error(removeError.message || '移除项目授权失败')
    } finally {
      setRemovingProjectMember(0)
    }
  }

  const openRoleModal = (roleToEdit = null) => {
    setEditingRole(roleToEdit)
    roleForm.resetFields()
    roleForm.setFieldsValue(roleToEdit ? {
      name: roleToEdit.name,
      description: roleToEdit.description,
      permissions: roleToEdit.permissions,
    } : {
      permissions: ['project:view', 'project:git:read', 'project:deployment:read', 'project:release:read', 'project:runtime:read'],
    })
    setRoleModalOpen(true)
  }

  const saveRole = async (values) => {
    setRoleSaving(true)
    try {
      const next = editingRole
        ? await updateProjectRole(editingRole.id, values)
        : await createProjectRole(values)
      setProjectRoles((old) => editingRole ? old.map((item) => item.id === next.id ? next : item) : [...old, next])
      setRoleModalOpen(false)
      message.success(editingRole ? '自定义角色已更新' : '自定义角色已创建')
    } catch (saveError) {
      message.error(saveError.message || '角色保存失败')
    } finally {
      setRoleSaving(false)
    }
  }

  const removeRole = async (roleToRemove) => {
    try {
      await deleteProjectRole(roleToRemove.id)
      setProjectRoles((old) => old.filter((item) => item.id !== roleToRemove.id))
      message.success('自定义角色已删除')
    } catch (removeError) {
      message.error(removeError.message || '删除角色失败')
    }
  }

  const memberColumns = [
    {
      title: '成员',
      key: 'member',
      render: (_, member) => (
        <div className="members-rbac-person">
          <Avatar size={32} style={{ backgroundColor: avatarColor(member) }}>{initials(member)}</Avatar>
          <div>
            <Typography.Text strong>{member.display_name || member.username}</Typography.Text>
            <Typography.Text type="secondary">{member.username}{member.is_current_user ? ' · 你' : ''}</Typography.Text>
          </div>
        </div>
      ),
    },
    {
      title: '空间角色',
      dataIndex: 'role',
      width: 175,
      render: (value, member) => (
        <Space size={6} wrap>
          <Select
            size="small"
            value={normalizeRole(value)}
            options={SPACE_ROLE_OPTIONS}
            loading={changingMember === member.user_id}
            disabled={!canManage || member.role === 'owner' || member.is_current_user || member.is_super_admin}
            onChange={(nextRole) => changeRole(member, nextRole)}
            className="members-rbac-role-select"
          />
        </Space>
      ),
    },
    {
      title: '项目访问',
      key: 'projects',
      width: 145,
      render: (_, member) => {
        const access = userAccess(member)
        if (access.unknown) return <Tooltip title="当前账号没有查看全部项目成员的权限"><span className="members-rbac-project-count is-unknown"><strong>—</strong><span>需管理员查看</span></span></Tooltip>
        return <div className="members-rbac-project-count"><strong>{access.count}</strong><span>{access.inherited ? '个项目 · 管理员继承' : '个项目'}</span></div>
      },
    },
    {
      title: '项目角色摘要',
      key: 'project_roles',
      render: (_, member) => {
        const access = userAccess(member)
        if (access.unknown) return <Tag>项目授权需管理员查看</Tag>
        const roleCounts = new Map()
        access.items.forEach(({ member: projectMember }) => {
          const label = projectMember?.role_name || '项目维护者'
          roleCounts.set(label, (roleCounts.get(label) || 0) + 1)
        })
        if (access.inherited) return <Tag color="gold">全部项目 · 管理员继承</Tag>
        if (!roleCounts.size) return <Tag>尚未授权</Tag>
        return <Space size={[4, 4]} wrap>{Array.from(roleCounts.entries()).slice(0, 3).map(([label, count]) => <Tag color="purple" key={label}>{label}{count > 1 ? ` × ${count}` : ''}</Tag>)}</Space>
      },
    },
    {
      title: '状态',
      key: 'status',
      width: 100,
      render: (_, member) => <Tag color={member.invitation_status === 'pending' ? 'orange' : 'green'}>{member.invitation_status === 'pending' ? '待接受' : '正常'}</Tag>,
    },
    {
      title: '',
      key: 'actions',
      width: 150,
      render: (_, member) => {
        const locked = !canManage || member.role === 'owner' || member.is_current_user || member.is_super_admin
        return (
          <Space size={2}>
            <Button type="link" size="small" onClick={() => setDrawer({ type: 'member', value: member })}>查看</Button>
            {!locked && <Popconfirm
              title={`确定移除 ${member.username}？`}
              description="移除后，对方将无法再访问当前空间。"
              okText="移除"
              cancelText="取消"
              okButtonProps={{ danger: true, loading: removingMember === member.user_id }}
              onConfirm={() => removeMember(member)}
            >
              <Button type="link" danger size="small" icon={<DeleteOutlined />} loading={removingMember === member.user_id}>移除</Button>
            </Popconfirm>}
          </Space>
        )
      },
    },
  ]

  const projectMemberRows = useMemo(() => {
    const selected = new Set(selectedProjectMembers.map((member) => member.user_id))
    return members.filter((member) => !selected.has(member.user_id) && !member.is_super_admin && !['owner', 'admin'].includes(member.role))
  }, [members, selectedProjectMembers])

  const projectMemberColumns = [
    {
      title: '成员',
      key: 'member',
      render: (_, member) => (
        <div className="members-rbac-person">
          <Avatar size={30} style={{ backgroundColor: avatarColor(member) }}>{initials(member)}</Avatar>
          <div><Typography.Text strong>{member.display_name || member.username}</Typography.Text><Typography.Text type="secondary">{member.username}</Typography.Text></div>
        </div>
      ),
    },
    {
      title: '项目角色',
      key: 'role',
      width: 205,
      render: (_, member) => (
        <Select
          size="small"
          value={member.role_id || member.role_key}
          options={projectRoleOptions(availableProjectRoles)}
          disabled={!canManage || member.is_current_user || member.is_super_admin}
          loading={changingProjectMember === member.user_id}
          onChange={(value) => changeProjectRole(member, value)}
          className="members-rbac-project-role-select"
        />
      ),
    },
    {
      title: '授权来源',
      key: 'source',
      render: (_, member) => <Tag color={member.role_id ? 'purple' : 'default'}>{member.role_id ? '显式授权 · 自定义角色' : '显式授权'}</Tag>,
    },
    {
      title: '最近变更',
      dataIndex: 'updated_at',
      width: 160,
      render: formatDate,
    },
    {
      title: '',
      key: 'actions',
      width: 100,
      render: (_, member) => canManage && !member.is_current_user && !member.is_super_admin ? (
        <Popconfirm
          title={`移除 ${member.username} 的项目授权？`}
          description="移除后，对方将无法访问这个项目。"
          okText="移除"
          cancelText="取消"
          okButtonProps={{ danger: true, loading: removingProjectMember === member.user_id }}
          onConfirm={() => removeProjectMemberAccess(member)}
        >
          <Button type="link" danger size="small" loading={removingProjectMember === member.user_id}>移除</Button>
        </Popconfirm>
      ) : <Typography.Text type="secondary">-</Typography.Text>,
    },
  ]

  const permissionRoles = useMemo(() => matrixScope === 'space'
    ? spaceRoleDefinitions.map((item) => ({ ...item, is_system: true, scope: 'space' }))
    : availableProjectRoles.map((item) => ({ ...item, scope: 'project' })), [
    availableProjectRoles,
    matrixScope,
    spaceRoleDefinitions,
  ])
  const selectedPermissionRole = useMemo(() => {
    if (!permissionRoles.length) return null
    return permissionRoles.find((item) => roleReference(item) === selectedPermissionRoleRef) || permissionRoles[0]
  }, [permissionRoles, selectedPermissionRoleRef])
  const permissionDefinitions = useMemo(() => normalizePermissionDefinitions(
    matrixScope === 'space' ? spacePermissions?.permissions : spacePermissions?.project_permissions,
    matrixScope === 'space' ? SPACE_PERMISSION_LABELS : PROJECT_PERMISSION_LABELS,
  ), [matrixScope, spacePermissions])
  const permissionGroups = useMemo(
    () => buildPermissionGroups(permissionDefinitions, permissionNames),
    [permissionDefinitions, permissionNames],
  )
  const selectedPermissionKeys = useMemo(
    () => new Set(rolePermissions(selectedPermissionRole)),
    [selectedPermissionRole],
  )

  const openPermissionDetails = (roleItem, scope = roleItem?.scope || 'project') => {
    setMatrixScope(scope)
    setSelectedPermissionRoleRef(roleReference(roleItem))
    setActiveTab('matrix')
  }

  const renderMemberDrawer = () => {
    const member = drawer.value
    if (!member) return null
    const access = userAccess(member)
    return <Drawer
      title={<div className="members-rbac-drawer-heading"><Avatar size={34} style={{ backgroundColor: avatarColor(member) }}>{initials(member)}</Avatar><div><Typography.Title level={4}>{member.display_name || member.username}</Typography.Title><Typography.Text type="secondary">{member.username}</Typography.Text></div></div>}
      open
      width={460}
      onClose={() => setDrawer({ type: '', value: null })}
      destroyOnHidden
    >
      <Descriptions size="small" column={1} bordered>
        <Descriptions.Item label="空间角色"><Tag color={ROLE_COLORS[member.role] || 'default'}>{ROLE_LABELS[member.role] || member.role}</Tag></Descriptions.Item>
        <Descriptions.Item label="加入时间">{formatDate(member.joined_at)}</Descriptions.Item>
        <Descriptions.Item label="项目访问">{access.unknown ? '项目授权详情需空间管理员查看' : access.inherited ? '全部项目（空间管理员继承）' : `${access.count} 个项目`}</Descriptions.Item>
      </Descriptions>
      <Typography.Title level={5} className="members-rbac-drawer-section-title">项目授权</Typography.Title>
      {access.unknown ? <Alert type="info" showIcon message="当前账号没有查看全部项目成员的权限，请让空间管理员打开“项目授权”查看。" /> : access.items.length ? <div className="members-rbac-drawer-projects">{access.items.map(({ project, member: projectMember }) => <div className="members-rbac-drawer-project" key={project.id}><div><Typography.Text strong>{project.name}</Typography.Text><Typography.Text type="secondary">{project.description || project.id}</Typography.Text></div><Tag color={projectMember ? 'purple' : 'gold'}>{projectMember?.role_name || '空间管理员继承'}</Tag></div>)}</div> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未授权项目" />}
    </Drawer>
  }

  const renderRoleDrawer = () => {
    const roleToView = drawer.value
    if (!roleToView) return null
    const permissions = rolePermissions(roleToView)
    return <Drawer
      title={<div><Typography.Title level={4}>{roleLabel(roleToView)}</Typography.Title><Typography.Text type="secondary">{roleScope(roleToView)} · {roleToView.is_system ? '系统角色' : '自定义角色'}</Typography.Text></div>}
      open
      width={460}
      onClose={() => setDrawer({ type: '', value: null })}
      destroyOnHidden
      extra={!roleToView.is_system && canManage && <Space><Button type="link" icon={<EditOutlined />} onClick={() => { setDrawer({ type: '', value: null }); openRoleModal(roleToView) }}>编辑</Button><Popconfirm title="删除这个自定义角色？" description="已绑定成员时不能删除。" okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={() => { setDrawer({ type: '', value: null }); removeRole(roleToView) }}><Button type="link" danger icon={<DeleteOutlined />}>删除</Button></Popconfirm></Space>}
    >
      <Typography.Paragraph type="secondary">{roleToView.description || '暂无角色说明。'}</Typography.Paragraph>
      <Typography.Title level={5} className="members-rbac-drawer-section-title">已绑定权限（{permissions.length}）</Typography.Title>
      <Space size={[6, 6]} wrap>{permissions.map((permission) => <Tag color="green" key={permission}>{permissionNames[permission] || permission}</Tag>)}</Space>
    </Drawer>
  }

  return (
    <div className="page-wrap members-rbac-page">
      <div className="page-heading members-rbac-heading">
        <div>
          <Typography.Title level={2}>成员与权限</Typography.Title>
        </div>
      </div>

      {error && <Alert className="members-rbac-alert" type="warning" showIcon message={error} />}
      <div className="members-rbac-tabs" role="tablist" aria-label="成员与权限">
        {[
          ['members', '成员'],
          ['roles', '角色'],
          ['matrix', '权限'],
          ['grants', '项目授权'],
        ].map(([key, label]) => <button key={key} type="button" role="tab" aria-selected={activeTab === key} className={activeTab === key ? 'active' : ''} onClick={() => setActiveTab(key)}>{label}</button>)}
      </div>

      {loading && !members.length ? <div className="space-settings-loading"><Spin /><Typography.Text type="secondary">加载成员与权限...</Typography.Text></div> : (
        <>
          {activeTab === 'members' && <section className="members-rbac-panel">
            <div className="members-rbac-toolbar members-rbac-members-toolbar">
              <Input.Search allowClear value={memberKeyword} onChange={(event) => setMemberKeyword(event.target.value)} placeholder="搜索成员或邮箱" className="members-rbac-search" />
              <Space className="members-rbac-member-actions">
                <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
                {canManage && <Button type="primary" icon={<PlusOutlined />} onClick={openMemberModal}>邀请成员</Button>}
              </Space>
            </div>
            {!canManage && <Alert className="members-rbac-readonly" type="info" showIcon message="你可以查看成员与授权，但只有空间管理员或所有者可以调整空间成员和项目授权。" />}
            <Card className="members-rbac-table-card" size="small">
              <Table
                rowKey="user_id"
                columns={memberColumns}
                dataSource={visibleMembers}
                loading={loading || projectMembersLoading}
                pagination={false}
                locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={memberKeyword ? '没有匹配的成员' : '当前空间还没有成员'} /> }}
              />
            </Card>
          </section>}

          {activeTab === 'roles' && <section className="members-rbac-panel">
            <div className="members-rbac-toolbar">
              <div><Typography.Title level={4}>角色</Typography.Title></div>
              {canManage && <Tooltip title={projectRolesAvailable ? '创建一个可复用的项目级自定义角色' : '当前后端未提供项目角色管理接口，暂时只能使用系统角色'}><Button type="primary" icon={<PlusOutlined />} onClick={() => openRoleModal()} disabled={!projectRolesAvailable}>创建自定义角色</Button></Tooltip>}
            </div>
            <div className="members-rbac-role-grid members-rbac-role-grid-unified">
              {spaceRoleDefinitions.map((definition) => {
                const definitionRole = normalizeRole(definition.key)
                const unifiedRole = { ...definition, is_system: true, scope: 'space' }
                return <Card key={`space-${definitionRole}`} className={`members-rbac-role-card ${definitionRole === currentRole ? 'is-current' : ''}`} size="small" onClick={() => setDrawer({ type: 'role', value: unifiedRole })}>
                  <div className="members-rbac-role-head"><div className="members-rbac-role-title"><span className="members-rbac-role-icon">S</span><div><Typography.Text strong>{ROLE_LABELS[definitionRole] || definition.name}</Typography.Text><Typography.Text type="secondary">空间级</Typography.Text></div></div><Space size={4}>{definitionRole === currentRole && <Tag color="green">当前角色</Tag>}<Tag color="blue">系统角色</Tag></Space></div>
                  <Typography.Paragraph type="secondary">{definition.description || roleDescription(definitionRole)}</Typography.Paragraph>
                  <div className="members-rbac-role-meta"><span>{rolePermissionCount(definition)} 项权限</span><Button type="link" size="small" onClick={(event) => { event.stopPropagation(); openPermissionDetails(unifiedRole, 'space') }}>查看权限</Button></div>
                </Card>
              })}
              {projectRoles.map((roleItem) => {
                const unifiedRole = { ...roleItem, scope: 'project' }
                return <Card key={`project-${roleItem.id || roleItem.key}`} className="members-rbac-role-card" size="small" onClick={() => setDrawer({ type: 'role', value: unifiedRole })}>
                  <div className="members-rbac-role-head"><div className="members-rbac-role-title"><span className="members-rbac-role-icon project">P</span><div><Typography.Text strong>{roleLabel(roleItem)}</Typography.Text><Typography.Text type="secondary">项目级</Typography.Text></div></div><Tag color={roleItem.is_system ? 'blue' : 'purple'}>{roleItem.is_system ? '系统角色' : '自定义'}</Tag></div>
                  <Typography.Paragraph type="secondary">{roleItem.description || '暂无角色说明。'}</Typography.Paragraph>
                  <div className="members-rbac-role-meta"><span>{rolePermissionCount(roleItem)} 项权限</span><Space size={2}>{!roleItem.is_system && canManage && <Button type="link" size="small" icon={<EditOutlined />} onClick={(event) => { event.stopPropagation(); openRoleModal(roleItem) }}>编辑</Button>}<Button type="link" size="small" onClick={(event) => { event.stopPropagation(); openPermissionDetails(unifiedRole, 'project') }}>查看权限</Button></Space></div>
                </Card>
              })}
            </div>
          </section>}

          {activeTab === 'matrix' && <section className="members-rbac-panel">
            <Card className="members-rbac-permission-card" size="small">
              <div className="members-rbac-permission-head">
                <Typography.Title level={5}>权限</Typography.Title>
                <div className="members-rbac-scope-switch">
                  {[['space', '空间权限'], ['project', '项目权限']].map(([key, label]) => <button type="button" key={key} className={matrixScope === key ? 'active' : ''} onClick={() => { setMatrixScope(key); setSelectedPermissionRoleRef('') }}>{label}</button>)}
                </div>
              </div>
              <div className="members-rbac-permission-layout">
                <aside className="members-rbac-permission-role-panel">
                  <div className="members-rbac-permission-role-head">
                    <Typography.Text strong>角色</Typography.Text>
                    <Typography.Text type="secondary">{permissionRoles.length} 个</Typography.Text>
                  </div>
                  <div className="members-rbac-permission-role-list">
                    {permissionRoles.map((roleItem) => {
                      const roleRef = roleReference(roleItem)
                      const selected = roleRef === roleReference(selectedPermissionRole)
                      return <button
                        type="button"
                        key={roleRef}
                        className={`members-rbac-permission-role-option ${selected ? 'active' : ''}`}
                        onClick={() => setSelectedPermissionRoleRef(roleRef)}
                      >
                        <span className={`members-rbac-role-icon ${matrixScope === 'project' ? 'project' : ''}`}>{matrixScope === 'project' ? 'P' : 'S'}</span>
                        <span className="members-rbac-permission-role-copy">
                          <Typography.Text strong>{roleLabel(roleItem)}</Typography.Text>
                          <Typography.Text type="secondary">{roleItem.is_system ? '系统角色' : '自定义角色'} · {rolePermissionCount(roleItem)} 项权限</Typography.Text>
                        </span>
                      </button>
                    })}
                    {!permissionRoles.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无角色" />}
                  </div>
                </aside>
                <div className="members-rbac-permission-detail">
                  {selectedPermissionRole ? <>
                    <div className="members-rbac-permission-detail-head">
                      <div>
                        <Typography.Title level={4}>{roleLabel(selectedPermissionRole)}</Typography.Title>
                        <Space size={6} wrap>
                          <Tag color={selectedPermissionRole.is_system ? 'blue' : 'purple'}>{selectedPermissionRole.is_system ? '系统角色' : '自定义角色'}</Tag>
                          <Tag>{rolePermissionCount(selectedPermissionRole)} 项权限</Tag>
                        </Space>
                      </div>
                      {!selectedPermissionRole.is_system && canManage && <Button type="link" icon={<EditOutlined />} onClick={() => openRoleModal(selectedPermissionRole)}>编辑</Button>}
                    </div>
                    {selectedPermissionRole.description && <Typography.Paragraph type="secondary" className="members-rbac-permission-description">{selectedPermissionRole.description}</Typography.Paragraph>}
                    <div className="members-rbac-permission-groups">
                      {permissionGroups.map((group) => {
                        const grantedCount = group.items.filter((permission) => selectedPermissionKeys.has(permission.key)).length
                        return <section className="members-rbac-permission-group" key={group.resource}>
                          <div className="members-rbac-permission-group-head">
                            <Typography.Text strong>{PERMISSION_RESOURCE_LABELS[group.resource] || group.resource}</Typography.Text>
                            <Typography.Text type="secondary">{grantedCount}/{group.items.length}</Typography.Text>
                          </div>
                          <div className="members-rbac-permission-items">
                            {group.items.map((permission) => {
                              const enabled = selectedPermissionKeys.has(permission.key)
                              return <div className={`members-rbac-permission-item ${enabled ? 'is-enabled' : ''}`} key={permission.key}>
                                <span className={`members-rbac-permission-check ${enabled ? '' : 'is-off'}`}>{enabled ? '✓' : '—'}</span>
                                <span className="members-rbac-permission-copy">
                                  <Typography.Text>{permission.name}</Typography.Text>
                                  <Typography.Text type="secondary">{permission.key}</Typography.Text>
                                </span>
                              </div>
                            })}
                          </div>
                        </section>
                      })}
                    </div>
                  </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择一个角色查看权限" />}
                </div>
              </div>
            </Card>
          </section>}

          {activeTab === 'grants' && <section className="members-rbac-panel">
            <div className="members-rbac-toolbar">
              <div><Typography.Title level={4}>项目授权</Typography.Title></div>
              {canManage && <Button type="primary" icon={<PlusOutlined />} onClick={openProjectMemberModal} disabled={!selectedProjectId || !projectMemberRows.length || !availableProjectRoles.length}>添加项目成员</Button>}
            </div>
            <div className="members-rbac-grant-layout">
              <Card className="members-rbac-project-picker" size="small">
                <Typography.Title level={5}>选择项目</Typography.Title>
                {projects.map((project) => <button type="button" className={`members-rbac-project-option ${selectedProjectId === project.id ? 'active' : ''}`} key={project.id} onClick={() => setSelectedProjectId(project.id)}><span>{project.name}</span><span>{(projectMembers[project.id] || []).length} 人</span></button>)}
                {!projects.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无可访问项目" />}
              </Card>
              <Card className="members-rbac-grant-main" size="small">
                {selectedProject ? <>
                  <div className="members-rbac-grant-head"><div><Typography.Title level={5}>{selectedProject.name} · 项目成员</Typography.Title></div><Tag color="green">{selectedProject.id}</Tag></div>
                  <div className="members-rbac-grant-rule"><b>继承规则</b><span>空间所有者和管理员自动拥有所有项目权限；其他成员必须显式绑定项目角色。</span></div>
                  <Table
                    rowKey="user_id"
                    columns={projectMemberColumns}
                    dataSource={selectedProjectMembers}
                    loading={projectMembersLoading}
                    pagination={false}
                    locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这个项目还没有显式授权成员" /> }}
                  />
                </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择一个项目查看授权" />}
              </Card>
            </div>
          </section>}
        </>
      )}

      <Modal
        title={<span><UserOutlined /> 邀请空间成员</span>}
        open={memberModalOpen}
        onCancel={() => setMemberModalOpen(false)}
        onOk={() => memberForm.submit()}
        okText="发送邀请"
        cancelText="取消"
        confirmLoading={memberSaving}
        destroyOnHidden
        width={520}
      >
        <Alert className="members-rbac-modal-alert" type="info" showIcon message="添加后，对方使用这个用户名和密码登录，再选择当前空间。" />
        <Form form={memberForm} layout="vertical" onFinish={addMember} requiredMark autoComplete="off">
          <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }, { pattern: /^[A-Za-z0-9._-]+$/, message: '只能使用字母、数字、点、下划线和短横线' }]}><Input prefix={<UserOutlined />} placeholder="例如：zhangsan" /></Form.Item>
          <Form.Item label="显示名称" name="display_name" extra="不填写时使用用户名显示。"><Input placeholder="例如：张三" /></Form.Item>
          <Form.Item label="初始密码" name="password" rules={[{ required: true, message: '请输入初始密码' }, { validator: passwordRule }]}><Input.Password prefix={<LockOutlined />} placeholder="至少 8 位" /></Form.Item>
          <Form.Item label="空间角色" name="role" rules={[{ required: true, message: '请选择空间角色' }]}><Select options={ROLE_OPTIONS} /></Form.Item>
        </Form>
      </Modal>

      <Modal title={editingRole ? '编辑自定义项目角色' : '创建自定义项目角色'} open={roleModalOpen} onCancel={() => setRoleModalOpen(false)} onOk={() => roleForm.submit()} okText="保存" cancelText="取消" confirmLoading={roleSaving} destroyOnHidden width={700}>
        <Form form={roleForm} layout="vertical" onFinish={saveRole}>
          <Form.Item name="name" label="角色名称" rules={[{ required: true, message: '请输入角色名称' }]}><Input placeholder="例如：测试发布员" /></Form.Item>
          <Form.Item name="description" label="角色说明"><Input.TextArea rows={2} placeholder="说明这个角色适合谁使用" /></Form.Item>
          <Form.Item name="permissions" label="绑定权限" rules={[{ required: true, message: '至少选择一项权限' }]}>
            <Checkbox.Group className="members-rbac-permission-form">{Object.entries(PROJECT_PERMISSION_LABELS).map(([key, label]) => <Checkbox value={key} key={key}>{label}</Checkbox>)}</Checkbox.Group>
          </Form.Item>
        </Form>
      </Modal>

      <Modal title={`添加到 ${selectedProject?.name || '项目'}`} open={projectMemberModalOpen} onCancel={() => setProjectMemberModalOpen(false)} onOk={() => projectMemberForm.submit()} okText="添加授权" cancelText="取消" confirmLoading={projectMemberSaving} destroyOnHidden>
        <Form form={projectMemberForm} layout="vertical" onFinish={addProjectMember}>
          <Form.Item name="user_id" label="空间成员" rules={[{ required: true, message: '请选择空间成员' }]}><Select showSearch optionFilterProp="label" options={projectMemberRows.map((member) => ({ value: member.user_id, label: `${member.display_name || member.username}（${member.username}）` }))} placeholder="选择要加入项目的成员" /></Form.Item>
          <Form.Item name="role_ref" label="项目角色" rules={[{ required: true, message: '请选择项目角色' }]}><Select options={projectRoleOptions(availableProjectRoles)} placeholder="选择项目角色" /></Form.Item>
        </Form>
      </Modal>

      {drawer.type === 'member' && renderMemberDrawer()}
      {drawer.type === 'role' && renderRoleDrawer()}
    </div>
  )
}
