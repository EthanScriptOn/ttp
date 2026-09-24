import {
  App as AntApp,
  Avatar,
  Button,
  Card,
  Col,
  ConfigProvider,
  Dropdown,
  Empty,
  Input,
  Layout,
  Menu,
  Pagination,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  AppstoreOutlined,
  CheckOutlined,
  ClusterOutlined,
  CloudUploadOutlined,
  CodeOutlined,
  DashboardOutlined,
  DeploymentUnitOutlined,
  DownOutlined,
  EnvironmentOutlined,
  ExperimentOutlined,
  FileTextOutlined,
  LogoutOutlined,
  PlusOutlined,
  ReloadOutlined,
  RocketOutlined,
  SearchOutlined,
  ClearOutlined,
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import LoginPage from './components/LoginPage'
import SpacePicker from './components/SpacePicker'
import ProjectForm from './components/ProjectForm'
import PodDrawer from './components/PodDrawer'
import PodTerminal from './components/PodTerminal'
import InfrastructureConnections from './components/InfrastructureConnections'
import ProjectSettings from './components/ProjectSettings'
import MonitorDashboard from './components/MonitorDashboard'
import ReleaseFlow from './components/ReleaseFlow'
import DeploymentConfigEditor from './components/DeploymentConfigEditor'
import SpaceSettingsPage from './components/SpaceSettingsPage'
import SpaceMembersPage from './components/SpaceMembersPage'
import ClusterMonitorOverview from './components/ClusterMonitorOverview'
import DeploymentTargets, { DeploymentTargetSelect } from './components/DeploymentTargets'
import ABExperiment from './components/ABExperiment'
import BrandLogo from './components/BrandLogo'
import {
  createDeploymentTarget,
  createProject,
  createSpace,
  createRelease,
  cancelRelease,
  getBranches,
  getCommits,
  getCurrentUser,
  getClusters,
  getImageRegistryConnections,
  getDeploymentTargets,
  getGitAccess,
  getProjectGitCredential,
  getProjectAccess,
  saveProjectGitCredential,
  deleteProjectGitCredential,
  getAuditLogs,
  getABExperiments,
  getMetrics,
  getProjects,
  getReleasePage,
  getReleaseFlow,
  getReleases,
  getReleaseTargetLogs,
  getSpaces,
  getPods,
  login,
  normalizeRelease,
  publishRelease,
  republishRelease,
  removeRelease,
  retryReleaseTarget,
  updateReleaseTraffic as updateReleaseTrafficApi,
  createABExperiment,
  updateABExperimentTraffic,
  stopABExperiment,
  finishABExperiment,
  selectSpace,
  updateDeploymentTarget,
  updateProject,
  deleteDeploymentTarget,
} from './services/api'
import { hasPermission, PERMISSIONS } from './services/permissions'

const { Header, Sider, Content } = Layout
const RELEASE_PAGE_SIZE = 10

function targetEnvironmentKey(target) {
  const value = `${target?.environment_stage || ''} ${target?.stage || ''} ${target?.environment || ''} ${target?.name || ''}`.toLowerCase()
  return ['dev', 'uat', 'pre', 'prod'].find((stage) => value.includes(stage)) || ''
}

const APP_THEME = {
  token: {
    colorPrimary: '#07a957',
    colorPrimaryHover: '#078f4a',
    colorPrimaryActive: '#06773e',
    colorInfo: '#078f4a',
    colorSuccess: '#2f9d6b',
    colorLink: '#078f4a',
    colorBgLayout: '#f7faf8',
    colorBorder: '#dcebe3',
    colorText: '#253b31',
    colorTextSecondary: '#62796e',
    borderRadius: 6,
    borderRadiusLG: 8,
  },
  components: {
    Menu: {
      itemBg: 'transparent',
      itemColor: '#5f756b',
      itemHoverColor: '#176e50',
      itemHoverBg: '#e8f7ef',
      itemSelectedColor: '#176e50',
      itemSelectedBg: '#daf3e4',
    },
    Progress: { defaultColor: '#2f9d6b' },
    Switch: { colorPrimary: '#2f9d6b', colorPrimaryHover: '#48b77f' },
  },
}

function App() {
  const [auth, setAuth] = useState(() => {
    const token = localStorage.getItem('cicd_token')
    return token ? { token, user: null } : null
  })
  const [spaces, setSpaces] = useState([])
  const [spaceId, setSpaceId] = useState(() => localStorage.getItem('cicd_space_id') || '')
  const [screen, setScreen] = useState(() => localStorage.getItem('cicd_token') ? 'spaces' : 'login')
  const [selectedProject, setSelectedProject] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!auth) return
    let active = true
    const loadSession = async () => {
      try {
        const result = await getCurrentUser()
        if (!active) return
        setAuth((old) => ({ ...old, user: result.user || old.user }))
        setSpaces(result.spaces || [])
        if (result.current_space_id) setSpaceId(result.current_space_id)
      } catch (sessionError) {
        if (!active) return
        if (sessionError.status === 401) {
          localStorage.removeItem('cicd_token')
          localStorage.removeItem('cicd_space_id')
          setAuth(null)
          setSpaces([])
          setScreen('login')
          setSelectedProject(null)
          return
        }
        try {
          const items = await getSpaces()
          if (active) setSpaces(items || [])
        } catch (spaceError) {
          if (active) message.error(spaceError.message || '加载空间失败')
        }
      }
    }
    loadSession()
    return () => { active = false }
  }, [auth?.token])

  const handleLogin = async (username, password) => {
    setLoading(true)
    setError('')
    try {
      const result = await login(username, password)
      localStorage.setItem('cicd_token', result.token)
      setAuth({ token: result.token, user: result.user })
      setSpaces(result.spaces || [])
      setSpaceId(result.current_space_id || '')
      setScreen('spaces')
    } catch (loginError) { setError(loginError.message || '登录失败') } finally { setLoading(false) }
  }

  const handleSpace = async (nextId) => {
    setLoading(true)
    try {
      const result = await selectSpace(nextId)
      if (result?.token) localStorage.setItem('cicd_token', result.token)
      localStorage.setItem('cicd_space_id', nextId)
      if (result?.current_space) {
        setSpaces((old) => old.map((space) => space.id === nextId ? { ...space, ...result.current_space } : space))
      }
      setSpaceId(nextId)
      setSelectedProject(null)
      setScreen('console')
    } catch (spaceError) { message.error(spaceError.message || '进入空间失败') } finally { setLoading(false) }
  }

  const handleCreateSpace = async (values) => {
    setLoading(true)
    try {
      const next = await createSpace(values)
      setSpaces((old) => [...old, next])
      await handleSpace(next.id)
    } catch (spaceError) {
      message.error(spaceError.message || '创建空间失败')
    } finally { setLoading(false) }
  }

  const logout = () => {
    localStorage.removeItem('cicd_token')
    localStorage.removeItem('cicd_space_id')
    setAuth(null); setSpaces([]); setScreen('login'); setSelectedProject(null)
  }

  const content = !auth || screen === 'login'
    ? <LoginPage onLogin={handleLogin} loading={loading} error={error} />
    : screen === 'spaces'
      ? <SpacePicker spaces={spaces} currentId={spaceId} onSelect={handleSpace} onCreate={handleCreateSpace} loading={loading} />
      : <AntApp><ConsoleLayout spaces={spaces} spaceId={spaceId} onSpaceChange={handleSpace} onOpenSpaces={() => { setSelectedProject(null); setScreen('spaces') }} switchingSpace={loading} user={auth.user} onLogout={logout} selectedProject={selectedProject} setSelectedProject={setSelectedProject} onSpaceUpdated={(space) => setSpaces((old) => old.map((item) => item.id === space.id ? { ...item, ...space } : item))} /></AntApp>

  return <ConfigProvider theme={APP_THEME}>{content}</ConfigProvider>
}

function ConsoleLayout({ spaces, spaceId, onSpaceChange, onOpenSpaces, switchingSpace = false, user, onLogout, selectedProject, setSelectedProject, onSpaceUpdated }) {
  const [section, setSection] = useState('projects')
  const currentSpace = spaces.find((space) => space.id === spaceId) || spaces[0]
  const role = user?.is_super_admin ? 'admin' : currentSpace?.role || 'viewer'
  const can = (permission) => hasPermission(role, permission, user?.is_super_admin)
  const menuItems = [
    { key: 'projects', icon: <AppstoreOutlined />, label: '项目' },
    (can(PERMISSIONS.CLUSTER_READ) || can(PERMISSIONS.REGISTRY_READ)) && { key: 'infrastructure', icon: <ClusterOutlined />, label: '连接管理' },
    can(PERMISSIONS.CLUSTER_READ) && { key: 'cluster-monitor', icon: <DashboardOutlined />, label: '集群监控' },
    can(PERMISSIONS.AUDIT_READ) && { key: 'activity', icon: <DashboardOutlined />, label: '操作记录' },
    can(PERMISSIONS.SPACE_READ) && { key: 'settings', icon: <SettingOutlined />, label: '空间设置' },
    can(PERMISSIONS.MEMBER_READ) && { key: 'members', icon: <TeamOutlined />, label: '成员与权限' },
  ].filter(Boolean)
  const availableSections = new Set(menuItems.map((item) => item.key))
  useEffect(() => {
    if (!availableSections.has(section)) setSection('projects')
  }, [availableSections, section])
  const sectionTitle = selectedProject
    ? '项目详情'
    : ({ infrastructure: '连接管理', 'cluster-monitor': '集群监控', activity: '操作记录', settings: '空间设置', members: '成员与权限', projects: '项目' }[section] || '项目')
  const spaceMenuItems = [
    ...spaces.map((space) => ({
      key: space.id,
      icon: <TeamOutlined />,
      label: <span className="space-menu-label"><span className="space-menu-copy"><strong>{space.name}</strong><small>{space.description || '可访问的工作空间'}</small></span>{space.id === spaceId && <CheckOutlined className="space-menu-check" />}</span>,
    })),
    ...(spaces.length ? [{ type: 'divider' }] : []),
    { key: '__manage_spaces__', icon: <DeploymentUnitOutlined />, label: '管理空间' },
  ]
  const handleSpaceMenuClick = ({ key }) => {
    if (key === '__manage_spaces__') {
      onOpenSpaces()
      return
    }
    if (key !== spaceId) onSpaceChange(key)
  }
  return <Layout className="console-layout">
    <Sider width={238} className="console-sider" breakpoint="lg" collapsedWidth="0">
      <div className="console-logo"><BrandLogo compact /><span className="logo-beta">BETA</span></div>
      <Menu theme="light" mode="inline" selectedKeys={[selectedProject ? 'projects' : section]} onClick={({ key }) => { setSection(key); if (key !== 'projects') setSelectedProject(null) }} items={menuItems} className="console-menu" />
      <div className="sider-bottom"><div className="sider-status"><span className="status-dot" /> 服务正常</div></div>
    </Sider>
    <Layout>
      <Header className="console-header">
        <div className="header-context"><span className="header-section">{sectionTitle}</span></div>
        <div className="header-user"><Dropdown trigger={['click']} placement="bottomRight" menu={{ className: 'space-switcher-menu', items: spaceMenuItems, selectable: false, onClick: handleSpaceMenuClick }}><Button type="text" className="space-switcher" aria-label={`切换空间，当前空间 ${currentSpace?.name || '未选择'}`} loading={switchingSpace}><TeamOutlined /><span className="space-switcher-copy"><small>当前空间</small><strong>{currentSpace?.name || '未选择'}</strong></span><DownOutlined className="space-switcher-arrow" /></Button></Dropdown><Avatar size={32} className="user-avatar">{(user?.display_name || user?.username || '管')[0]}</Avatar><span className="user-name">{user?.display_name || user?.username || '管理员'}</span><Button type="text" aria-label="退出登录" icon={<LogoutOutlined />} onClick={onLogout} /></div>
      </Header>
      <Content className="console-content">
        {selectedProject ? <ProjectDetail key={selectedProject.id} project={selectedProject} spaceName={currentSpace?.name} userName={user?.display_name || user?.username} permissions={{ canUpdateProject: can(PERMISSIONS.PROJECT_UPDATE), canCreateRelease: can(PERMISSIONS.RELEASE_CREATE), canUpdateRelease: can(PERMISSIONS.RELEASE_UPDATE), canPublishRelease: can(PERMISSIONS.RELEASE_PUBLISH), canRuntimeRead: can(PERMISSIONS.RUNTIME_READ), canRuntimeTerminal: can(PERMISSIONS.RUNTIME_TERMINAL) }} onBack={() => setSelectedProject(null)} onOpenCluster={() => { setSelectedProject(null); setSection('cluster-monitor') }} /> : section === 'infrastructure' ? <InfrastructureConnections canReadClusters={can(PERMISSIONS.CLUSTER_READ)} canManageClusters={can(PERMISSIONS.CLUSTER_MANAGE)} canReadRegistry={can(PERMISSIONS.REGISTRY_READ)} canManageRegistry={can(PERMISSIONS.REGISTRY_MANAGE)} /> : section === 'cluster-monitor' ? <ClusterMonitorOverview canManageClusters={can(PERMISSIONS.CLUSTER_MANAGE)} onOpenConnections={() => setSection('infrastructure')} /> : section === 'activity' ? <ActivityPage /> : section === 'settings' ? <SpaceSettingsPage role={role} user={user} onSpaceUpdated={onSpaceUpdated} /> : section === 'members' ? <SpaceMembersPage role={role} user={user} /> : <ProjectsPage canCreateProject={can(PERMISSIONS.PROJECT_CREATE)} onOpen={setSelectedProject} />}
      </Content>
    </Layout>
  </Layout>
}

function ProjectsPage({ onOpen, canCreateProject = true }) {
  const [projects, setProjects] = useState([])
  const [targetsByProject, setTargetsByProject] = useState({})
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [createLoading, setCreateLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [clusters, setClusters] = useState([])
  const [registryConnections, setRegistryConnections] = useState([])
  const [keyword, setKeyword] = useState('')
  const [projectPage, setProjectPage] = useState(1)
  const [projectPageSize, setProjectPageSize] = useState(9)
  const load = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const [projectResult, clusterResult] = await Promise.allSettled([
        getProjects(),
        getClusters(),
      ])
      const failures = []
      const nextProjects = projectResult.status === 'fulfilled' ? (projectResult.value || []) : []
      const nextClusters = clusterResult.status === 'fulfilled' ? (clusterResult.value || []) : []
      if (projectResult.status === 'rejected') failures.push(`项目：${projectResult.reason?.message || '加载失败'}`)
      if (clusterResult.status === 'rejected') failures.push(`集群：${clusterResult.reason?.message || '加载失败'}`)
      setProjects(nextProjects)
      setClusters(nextClusters)
      const registryResult = await getImageRegistryConnections().catch(() => [])
      setRegistryConnections(registryResult || [])
      const targetResults = await Promise.allSettled(nextProjects.map((project) => getDeploymentTargets(project.id)))
      setTargetsByProject(Object.fromEntries(nextProjects.map((project, index) => [
        project.id,
        targetResults[index].status === 'fulfilled' ? targetResults[index].value : null,
      ])))
      if (failures.length) {
        setLoadError(failures.join('；'))
        message.warning(failures.join('；'))
      }
    } catch (projectError) {
      setLoadError(projectError.message || '加载项目失败')
      message.error(projectError.message || '加载项目失败')
    } finally { setLoading(false) }
  }
  useEffect(() => {
    load()
  }, [])
  const visible = projects.filter((project) => `${project.name} ${project.repository_url}`.toLowerCase().includes(keyword.toLowerCase()))
  const pageCount = Math.max(1, Math.ceil(visible.length / projectPageSize))
  const currentProjectPage = Math.min(projectPage, pageCount)
  const pagedProjects = visible.slice((currentProjectPage - 1) * projectPageSize, currentProjectPage * projectPageSize)
  useEffect(() => { setProjectPage(1) }, [keyword])
  const submit = async (values) => {
    setCreateLoading(true)
    try {
      const project = await createProject(values)
      setProjects((old) => [...old, project])
      setProjectPage(1)
      const projectTargets = await getDeploymentTargets(project.id).catch(() => null)
      setTargetsByProject((old) => ({ ...old, [project.id]: projectTargets }))
      setCreateOpen(false)
      message.success('项目创建成功，仓库机器人已验证')
      onOpen?.(project)
    } catch (projectError) {
      message.error(projectError.message || '创建项目失败')
    } finally { setCreateLoading(false) }
  }
  return <div className="page-wrap">
    <div className="page-heading"><div><Typography.Text className="page-kicker">工作空间 · 项目</Typography.Text><Typography.Title level={2}>项目</Typography.Title><Typography.Paragraph type="secondary">每个项目对应一个代码仓库，可以配置多个发布环境。</Typography.Paragraph></div><Space><Button icon={<ReloadOutlined />} onClick={load}>刷新</Button>{canCreateProject && <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>创建项目</Button>}</Space></div>
    <Row gutter={[16, 16]} className="summary-row"><Col xs={24} sm={8}><SummaryCard label="项目总数" value={projects.length} icon={<AppstoreOutlined />} tone="blue" /></Col><Col xs={24} sm={8}><SummaryCard label="运行中项目" value={projects.filter((p) => p.health === 'healthy' || p.pod_count > 0).length} icon={<RocketOutlined />} tone="green" /></Col><Col xs={24} sm={8}><SummaryCard label="本空间集群" value={new Set(projects.map((p) => p.cluster_id).filter(Boolean)).size} icon={<ClusterOutlined />} tone="orange" /></Col></Row>
    <div className="list-toolbar"><Typography.Title level={4}>全部项目 <span className="count-muted">{visible.length}</span></Typography.Title><input className="search-input" placeholder="搜索项目或仓库" value={keyword} onChange={(event) => setKeyword(event.target.value)} /></div>
    <div className="project-grid">{pagedProjects.map((project) => <ProjectCard key={project.id} project={project} targets={targetsByProject[project.id]} onClick={() => onOpen(project)} />)}</div>
    {!loading && visible.length > 0 && <div className="project-pagination"><span>共 {visible.length} 个项目</span>{visible.length > projectPageSize && <Pagination current={currentProjectPage} pageSize={projectPageSize} total={visible.length} showSizeChanger pageSizeOptions={[9, 18, 36]} showQuickJumper={visible.length > 36} onChange={(page, size) => { setProjectPage(page); setProjectPageSize(size) }} showTotal={(total, range) => `${range[0]}-${range[1]} / ${total}`} />}</div>}
    {!loading && !visible.length && <Card className="empty-panel"><Empty description={keyword ? '没有匹配的项目' : canCreateProject ? '还没有项目，先创建一个吧' : '当前空间还没有项目'}>{canCreateProject && <Button type="primary" onClick={() => setCreateOpen(true)}>创建项目</Button>}</Empty></Card>}
    {loading && <div className="loading-placeholder">加载项目中...</div>}
    {!loading && loadError && <div className="loading-placeholder">{loadError}</div>}
    {canCreateProject && <ProjectForm open={createOpen} onCancel={() => setCreateOpen(false)} onSubmit={submit} loading={createLoading} clusters={clusters} registryConnections={registryConnections} />}
  </div>
}

function SummaryCard({ label, value, icon, tone }) { return <Card variant="borderless" className={`summary-card tone-${tone}`}><div className="summary-icon">{icon}</div><Statistic title={label} value={value} /></Card> }

function ProjectCard({ project, targets, onClick }) {
  const healthy = project.health === 'healthy' || project.pod_count > 0
  const targetCount = Array.isArray(targets) ? targets.length : project.deployment_target_count || 0
  return <Card hoverable className="project-card" onClick={onClick} variant="borderless">
    <div className="project-card-head"><div className="project-icon"><CodeOutlined /></div><div className="project-main"><Typography.Title level={4} ellipsis={{ tooltip: project.name }}>{project.name}</Typography.Title><Typography.Text type="secondary" ellipsis>{project.description || '暂无项目说明'}</Typography.Text></div></div>
    <div className="repo-line"><CodeOutlined /> <span title={project.repository_url}>{project.repository_url}</span></div>
    <div className="project-target-summary">
      <div className="project-target-summary-head"><span><EnvironmentOutlined /> 发布环境</span><strong>{targetCount}</strong></div>
      {Array.isArray(targets) && targets.length > 0 ? <div className="project-target-list project-target-list-simple">{targets.map((target) => <div className="project-target-name-only" key={target.id} title={target.name}>{target.name}</div>)}</div> : <div className="project-target-empty">{targets === undefined ? '正在加载环境摘要...' : targets === null ? '环境摘要暂不可用' : '暂无发布环境'}</div>}
    </div>
    <div className="project-card-meta"><span><span className={`health-dot ${healthy ? 'healthy' : 'muted'}`} />{healthy ? '项目运行正常' : '项目尚未发布'}</span><span>{project.healthy_pod_count ?? 0} / {project.pod_count ?? 0} 健康 Pod</span></div>
    <div className="project-card-footer"><span>最近发布：{project.last_release || '暂无'}</span><span className="open-project">进入项目 <span>→</span></span></div>
  </Card>
}

function ProjectDetail({ project: initialProject, spaceName, userName, permissions = {}, onBack, onOpenCluster }) {
  const [project, setProject] = useState(initialProject)
  const [projectAccess, setProjectAccess] = useState(null)
  const projectCan = (permission, fallback = false) => {
    if (!projectAccess) return fallback
    return projectAccess.is_space_admin || projectAccess.is_super_admin || projectAccess.permissions?.includes(permission) === true
  }
  const canUpdateProject = projectCan(PERMISSIONS.PROJECT_SETTINGS, permissions.canUpdateProject !== false)
  const canCreateRelease = projectCan(PERMISSIONS.PROJECT_RELEASE_CREATE, permissions.canCreateRelease !== false)
  const canUpdateRelease = projectCan(PERMISSIONS.PROJECT_RELEASE_UPDATE, permissions.canUpdateRelease !== false)
  const canPublishRelease = projectCan(PERMISSIONS.PROJECT_RELEASE_PUBLISH, permissions.canPublishRelease !== false)
  const canRuntimeRead = projectCan(PERMISSIONS.PROJECT_RUNTIME_READ, permissions.canRuntimeRead !== false)
  const canRuntimeTerminal = projectCan(PERMISSIONS.PROJECT_RUNTIME_TERMINAL, permissions.canRuntimeTerminal !== false)
  const canManageGit = projectCan(PERMISSIONS.PROJECT_GIT_MANAGE, false)
  const initialBranch = project.default_branch || 'main'
  const [branch, setBranch] = useState(initialBranch)
  const [branches, setBranches] = useState([])
  const [branchLoading, setBranchLoading] = useState(false)
  const [tab, setTab] = useState('release')
  const [commits, setCommits] = useState([])
  const [selected, setSelected] = useState([])
  const [releases, setReleases] = useState([])
  const [releaseFlow, setReleaseFlow] = useState(null)
  const [releasePage, setReleasePage] = useState(1)
  const [releaseTotal, setReleaseTotal] = useState(0)
  const [releaseSearch, setReleaseSearch] = useState('')
  const [experimentReleases, setExperimentReleases] = useState([])
  const [experiments, setExperiments] = useState([])
  const [experimentLoading, setExperimentLoading] = useState(false)
  const [pods, setPods] = useState([])
  const [metrics, setMetrics] = useState(null)
  const [targets, setTargets] = useState([])
  const [targetLoading, setTargetLoading] = useState(true)
  const [selectedTargetId, setSelectedTargetId] = useState('')
  const [releaseTargetIds, setReleaseTargetIds] = useState([])
  const [loading, setLoading] = useState(true)
  const [pod, setPod] = useState(null)
  const [terminalPod, setTerminalPod] = useState(null)
  const [monitorPod, setMonitorPod] = useState(null)
  const [podTarget, setPodTarget] = useState(null)
  const [releaseDetailId, setReleaseDetailId] = useState('')
  const [releaseDetailEnvironment, setReleaseDetailEnvironment] = useState('dev')
  const [releaseOpen, setReleaseOpen] = useState(false)
  const [strategy, setStrategy] = useState('rolling')
  const [candidate, setCandidate] = useState(1)
  const trafficCandidate = strategy === 'rolling' ? 0 : candidate
  const [releaseSaving, setReleaseSaving] = useState(false)
  const publishRequestRef = useRef(false)
  const [releaseTarget, setReleaseTarget] = useState(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [clusters, setClusters] = useState([])
  const [registryConnections, setRegistryConnections] = useState([])
  const [cancellingRelease, setCancellingRelease] = useState('')
  const [retryingTarget, setRetryingTarget] = useState('')
  const [gitCredential, setGitCredential] = useState(null)
  const [gitAccess, setGitAccess] = useState(null)
  const [gitAccessLoading, setGitAccessLoading] = useState(true)
  const [gitAccessError, setGitAccessError] = useState('')
  const gitAccessRequestRef = useRef(0)
  const runtimeRequestRef = useRef(0)

  const availableTargets = targets
  const selectedTarget = availableTargets.find((target) => target.id === selectedTargetId) || availableTargets[0]
  const releaseDetail = releases.find((item) => item.id === releaseDetailId) || null

  const loadGitAccess = async () => {
    const requestID = ++gitAccessRequestRef.current
    setGitAccessLoading(true)
    setGitAccessError('')
    setGitCredential(null)
    setGitAccess(null)
    const [credentialResult, accessResult] = await Promise.allSettled([
      getProjectGitCredential(project.id),
      getGitAccess(project.id),
    ])
    if (requestID !== gitAccessRequestRef.current) return
    if (credentialResult.status === 'fulfilled') setGitCredential(credentialResult.value)
    const failures = [credentialResult, accessResult]
      .filter((result) => result.status === 'rejected')
      .map((result) => result.reason?.message || '授权检查失败')
    if (failures.length) {
      setGitAccess(null)
      setGitAccessError(failures[0])
    } else {
      setGitAccess(accessResult.value)
    }
    setGitAccessLoading(false)
  }

  const loadProjectAccess = async () => {
    try {
      setProjectAccess(await getProjectAccess(project.id))
    } catch (error) {
      setProjectAccess(null)
      if (error?.code !== 'forbidden') message.warning(error.message || '项目权限加载失败')
    }
  }

  const loadRuntime = async (targetId = selectedTarget?.id) => {
    const requestID = ++runtimeRequestRef.current
    if (!targetId) {
      if (requestID === runtimeRequestRef.current) {
        setPods([])
        setMetrics(null)
      }
      return []
    }
    const runtimeResults = await Promise.allSettled([
      getPods(project.id, targetId),
      getMetrics(project.id, targetId),
    ])
    const [podResult, metricResult] = runtimeResults
    if (requestID === runtimeRequestRef.current && podResult.status === 'fulfilled') setPods(podResult.value || [])
    if (requestID === runtimeRequestRef.current && metricResult.status === 'fulfilled') {
      setMetrics((previous) => {
        const next = metricResult.value || null
        if (!next || !Array.isArray(next.series) || next.series.length === 0) return next
        const history = [...(Array.isArray(previous?.series) ? previous.series : []), ...next.series]
        const unique = new Map(history.map((point) => [String(point.timestamp), point]))
        return { ...next, series: [...unique.values()].slice(-120) }
      })
    }
    return runtimeResults
  }

  const loadReleaseTargetLogs = useCallback((releaseId, targetId) => getReleaseTargetLogs(project.id, releaseId, targetId), [project.id])

  const loadABExperiments = async () => {
    setExperimentLoading(true)
    try {
      const next = (await getABExperiments(project.id)) || []
      setExperiments(next)
      return next
    } finally {
      setExperimentLoading(false)
    }
  }

  const loadClusters = async () => {
    try {
      const nextClusters = (await getClusters()) || []
      setClusters(nextClusters)
      return nextClusters
    } catch (error) {
      message.warning(error.message || '集群列表暂时无法加载')
      return []
    }
  }

  const load = async (branchName = branch, pageNumber = releasePage, search = releaseSearch) => {
    setLoading(true)
    setTargetLoading(true)
    try {
      getImageRegistryConnections().then((items) => setRegistryConnections(items || [])).catch(() => {})
      const results = await Promise.allSettled([
        getBranches(project.id),
        getCommits(project.id, branchName),
        getReleasePage(project.id, pageNumber, RELEASE_PAGE_SIZE, search),
        getReleaseFlow(project.id),
        getDeploymentTargets(project.id),
        getABExperiments(project.id),
      ])
      const failures = []
      const [branchResult, commitResult, releaseResult, flowResult, targetResult, experimentResult] = results
      if (branchResult.status === 'fulfilled') setBranches(branchResult.value || [])
      else if (branchResult.reason?.code !== 'git_credential_invalid') failures.push(`分支：${branchResult.reason?.message || '加载失败'}`)
      if (commitResult.status === 'fulfilled') setCommits(commitResult.value || [])
      else if (commitResult.reason?.code !== 'git_credential_invalid') failures.push(`提交：${commitResult.reason?.message || '加载失败'}`)
      if (releaseResult.status === 'fulfilled') {
        setReleases(normalizeReleases(releaseResult.value.items || []))
        setReleaseTotal(releaseResult.value.total || 0)
        setReleasePage(releaseResult.value.page || pageNumber)
      }
      else failures.push(`发布记录：${releaseResult.reason?.message || '加载失败'}`)
      if (flowResult.status === 'fulfilled') setReleaseFlow(flowResult.value || null)
      else failures.push(`当前发布流程：${flowResult.reason?.message || '加载失败'}`)
      if (experimentResult.status === 'fulfilled') setExperiments(experimentResult.value || [])
      else failures.push(`A/B 实验：${experimentResult.reason?.message || '加载失败'}`)

      let nextTargetId = selectedTargetId || ''
      if (targetResult.status === 'fulfilled') {
        const nextTargets = targetResult.value || []
        setTargets(nextTargets)
        const targetExists = nextTargets.some((target) => target.id === nextTargetId && target.enabled)
        nextTargetId = targetExists ? nextTargetId : nextTargets.find((target) => target.enabled)?.id || ''
        if (nextTargetId !== selectedTargetId) setSelectedTargetId(nextTargetId)
      } else {
        failures.push(`发布环境：${targetResult.reason?.message || '加载失败'}`)
      }
      setTargetLoading(false)
      if (nextTargetId) {
        const [podResult, metricResult] = await loadRuntime(nextTargetId)
        if (podResult.status !== 'fulfilled') failures.push(`Pod：${podResult.reason?.message || '加载失败'}`)
        if (metricResult.status !== 'fulfilled') failures.push(`监控：${metricResult.reason?.message || '加载失败'}`)
      } else {
        setPods([])
        setMetrics(null)
      }
      if (failures.length) {
        const detail = failures.join('；')
        message.error(`部分数据加载失败：${detail}`)
      }
    } finally {
      setTargetLoading(false)
      setLoading(false)
    }
  }
  useEffect(() => {
    load(initialBranch, 1)
    loadProjectAccess()
    loadGitAccess()
    loadClusters()
  }, [project.id])

  useEffect(() => {
    if (tab !== 'experiments') return
    getReleases(project.id).then((items) => setExperimentReleases(normalizeReleases(items || []))).catch(() => {})
  }, [project.id, tab])

  const loadReleasePage = async (pageNumber, search = releaseSearch) => {
    setLoading(true)
    try {
      const [releaseResult, flowResult] = await Promise.allSettled([getReleasePage(project.id, pageNumber, RELEASE_PAGE_SIZE, search), getReleaseFlow(project.id)])
      if (releaseResult.status !== 'fulfilled') throw releaseResult.reason
      const result = releaseResult.value
      setReleases(normalizeReleases(result.items || []))
      setReleaseTotal(result.total || 0)
      setReleasePage(result.page || pageNumber)
      if (flowResult.status === 'fulfilled') setReleaseFlow(flowResult.value || null)
      else message.warning(`当前发布流程加载失败：${flowResult.reason?.message || '请稍后重试'}`)
      return result
    } catch (error) {
      message.error(error.message || '发布单加载失败')
      return null
    } finally {
      setLoading(false)
    }
  }

  const searchReleaseOrders = async (value) => {
    const search = `${value || ''}`.trim()
    setReleaseSearch(search)
    setReleasePage(1)
    return loadReleasePage(1, search)
  }

  const reloadTargets = async (preferredTargetId = selectedTargetId) => {
    setTargetLoading(true)
    try {
      const nextTargets = (await getDeploymentTargets(project.id)) || []
      setTargets(nextTargets)
      const nextId = nextTargets.some((target) => target.id === preferredTargetId && target.enabled)
        ? preferredTargetId
        : nextTargets.find((target) => target.enabled)?.id || ''
      setSelectedTargetId(nextId)
      return { targets: nextTargets, targetId: nextId }
    } finally {
      setTargetLoading(false)
    }
  }

  const changeTarget = async (nextTargetId) => {
    if (!nextTargetId || nextTargetId === selectedTargetId) return
    setSelectedTargetId(nextTargetId)
    setPod(null)
    setPodTarget(null)
    setMonitorPod(null)
    setLoading(true)
    try {
      const runtimeResults = await loadRuntime(nextTargetId)
      if (runtimeResults.some((result) => result.status !== 'fulfilled')) {
        message.warning('环境已切换，但运行态数据暂时无法加载')
      }
    } finally {
      setLoading(false)
    }
  }

  const openReleaseDetail = (release, environment = 'dev') => {
    if (!release?.id) return
    setReleaseDetailId(release.id)
    setReleaseDetailEnvironment(environment)
    const target = availableTargets.find((item) => item.id === environment || targetEnvironmentKey(item) === environment)
    if (target) {
      setPodTarget(target)
      if (target.id !== selectedTarget?.id) changeTarget(target.id)
    }
  }

  const changeReleaseDetailEnvironment = (environment) => {
    setReleaseDetailEnvironment(environment)
    const target = availableTargets.find((item) => item.id === environment || targetEnvironmentKey(item) === environment)
    if (target) changeTarget(target.id)
    else {
      setPod(null)
      setPodTarget(null)
      setPods([])
      setMetrics(null)
    }
  }

  const openDetailPod = (podValue, target) => {
    setPodTarget(target || selectedTarget)
    setPod(podValue)
  }

  const openPodLogs = (podValue, target) => openDetailPod(podValue, target)

  const openPodMonitor = (podValue, target) => {
    setPod(null)
    setTerminalPod(null)
    const nextTarget = target || selectedTarget
    setPodTarget(nextTarget)
    if (nextTarget?.id) setSelectedTargetId(nextTarget.id)
    setMonitorPod(podValue)
    setTab('monitor')
  }

  const openPodTerminal = (podValue, target) => {
    if (!canRuntimeTerminal) {
      message.warning('当前账号没有进入 Pod 终端的权限')
      return
    }
    setPod(null)
    setPodTarget(target || selectedTarget)
    setTerminalPod(podValue)
  }

  const createTarget = async (payload) => {
    const created = await createDeploymentTarget(project.id, payload)
    const result = await reloadTargets(selectedTargetId || created.id)
    if (!selectedTargetId) await loadRuntime(result.targetId)
  }

  const updateTarget = async (target, payload) => {
    const updated = await updateDeploymentTarget(project.id, target.id, payload)
    await reloadTargets(updated.id === selectedTargetId ? updated.id : selectedTargetId)
    if (updated.id === selectedTargetId) await loadRuntime(updated.id)
  }

  const deleteTarget = async (target) => {
    await deleteDeploymentTarget(project.id, target.id)
    const result = await reloadTargets(target.id === selectedTargetId ? '' : selectedTargetId)
    const nextId = result.targetId || result.targets[0]?.id
    if (nextId) {
      setSelectedTargetId(nextId)
      await loadRuntime(nextId)
    } else {
      setPods([])
      setMetrics(null)
    }
  }

  const canPublish = canPublishRelease && !gitAccessLoading && !gitAccessError && gitAccess?.usable === true

  const hasActiveReleases = releases.some((release) => ['queued', 'running'].includes(release.status))
  useEffect(() => {
    if (!hasActiveReleases) return undefined
    let active = true
    const poll = async () => {
      try {
        const next = await getReleasePage(project.id, releasePage, RELEASE_PAGE_SIZE, releaseSearch)
        if (!active) return
        setReleases(normalizeReleases(next.items || []))
        setReleaseTotal(next.total || 0)
        const flowResult = await Promise.allSettled([getReleaseFlow(project.id)])
        if (active && flowResult[0].status === 'fulfilled') setReleaseFlow(flowResult[0].value || null)
        // A rollout replaces Pods asynchronously. Refresh the selected
        // environment while the release is active so the table does not keep
        // pointing at a Pod that has already been removed by Kubernetes.
        if (selectedTargetId) await loadRuntime(selectedTargetId)
      } catch {
        // The next scheduled poll can recover from a temporary request failure.
      }
    }
    poll()
    const timer = window.setInterval(poll, 3000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [hasActiveReleases, project.id, releasePage, releaseSearch, selectedTargetId])

  const changeBranch = async (nextBranch) => {
    if (!nextBranch || nextBranch === branch) return
    setBranch(nextBranch)
    setSelected([])
    setBranchLoading(true)
    try {
      const nextCommits = (await getCommits(project.id, nextBranch)) || []
      setCommits(nextCommits)
      setSelected(nextCommits[0] ? [nextCommits[0]] : [])
    } catch (branchError) {
      message.error(branchError.message || '加载提交失败')
    } finally { setBranchLoading(false) }
  }

  const upsertRelease = (value) => {
    const next = normalizeRelease(value)
    setReleases((old) => {
      const exists = old.some((item) => item.id === next.id)
      if (!exists && releasePage !== 1) return old
      return [next, ...old.filter((item) => item.id !== next.id)].slice(0, RELEASE_PAGE_SIZE)
    })
    setExperimentReleases((old) => [next, ...old.filter((item) => item.id !== next.id)])
    return next
  }
  const upsertExperiment = (value) => {
    setExperiments((old) => [value, ...old.filter((item) => item.id !== value.id)])
    return value
  }
  const createExperiment = async (payload) => {
    if (!canCreateRelease) {
      message.warning('当前角色没有创建 A/B 实验的权限')
      return null
    }
    try {
      const created = upsertExperiment(await createABExperiment(project.id, payload))
      message.success(`A/B 实验 ${created.name} 已创建`)
      return created
    } catch (error) {
      message.error(error.message || '创建 A/B 实验失败')
      return null
    }
  }
  const updateExperimentTraffic = async (experimentId, payload) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有调整 A/B 实验的权限')
      return null
    }
    try {
      const updated = upsertExperiment(await updateABExperimentTraffic(project.id, experimentId, payload))
      message.success('实验流量已更新')
      return updated
    } catch (error) {
      message.error(error.message || '调整实验流量失败')
      return null
    }
  }
  const updateReleaseTraffic = async (release, targetId, payload) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有调整发布流量的权限')
      return null
    }
    if (!release?.id || !targetId) {
      message.warning('当前环境还没有可调整的发布目标')
      return null
    }
    try {
      const updated = upsertRelease(await updateReleaseTrafficApi(project.id, release.id, targetId, payload))
      message.success('发布流量已调整')
      return updated
    } catch (error) {
      message.error(error.message || '调整发布流量失败')
      return null
    }
  }
  const stopExperiment = async (experimentId) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有停止 A/B 实验的权限')
      return null
    }
    try {
      const updated = upsertExperiment(await stopABExperiment(project.id, experimentId))
      message.success('实验已停止，流量已切回 A 版本')
      return updated
    } catch (error) {
      message.error(error.message || '停止实验失败')
      return null
    }
  }
  const finishExperiment = async (experimentId, result) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有结束 A/B 实验的权限')
      return null
    }
    try {
      const updated = upsertExperiment(await finishABExperiment(project.id, experimentId, result))
      message.success(result === 'promote_b' ? '实验已结束，B 版本接管流量' : '实验已结束，保留 A 版本')
      return updated
    } catch (error) {
      message.error(error.message || '结束实验失败')
      return null
    }
  }
  const createReleaseOrder = async ({ branch: sourceBranch }) => {
    if (!canCreateRelease) {
      message.warning('当前角色没有创建发布单的权限')
      return null
    }
    const targetIds = availableTargets.filter((target) => target.enabled).map((target) => target.id)
    if (!targetIds.length) {
      message.warning('请先在“发布环境”中添加并启用 DEV 环境；部署配置资源文件不等于发布目标')
      setTab('targets')
      return null
    }
    try {
      const nextBranch = sourceBranch || branch
      const latestCommits = (await getCommits(project.id, nextBranch)) || []
      const latestCommit = latestCommits[0]
      if (!latestCommit?.sha) {
        message.warning('当前分支没有可发布版本')
        return null
      }
      setBranch(nextBranch)
      setCommits(latestCommits)
      setSelected([latestCommit])
      const created = await createRelease(project.id, {
        branch: nextBranch,
        commit_shas: [latestCommit.sha],
        target_ids: targetIds,
        strategy: 'rolling',
        stable_percent: 100,
        candidate_percent: 0,
        blue_percent: 0,
        green_percent: 0,
        publish: false,
      })
      const next = normalizeRelease(created)
      setReleaseSearch('')
      setReleasePage(1)
      setExperimentReleases((old) => [next, ...old.filter((item) => item.id !== next.id)])
      await loadReleasePage(1, '')
      message.success(`发布单 ${next.id} 已创建`)
      return next
    } catch (error) {
      message.error(error.message || '创建发布单失败')
      return null
    }
  }
  const removeReleaseOrder = async (item) => {
    if (!item?.id) return false
    const isDraft = item.status === 'draft'
    if (isDraft && !canUpdateRelease) {
      message.warning('当前角色没有移除发布单的权限')
      return false
    }
    if (!isDraft && (!canUpdateRelease || !canPublishRelease)) {
      message.warning('当前角色没有移除发布单的权限')
      return false
    }
    try {
      if (isDraft) {
        await removeRelease(project.id, item.id)
        const remainingTotal = Math.max(0, releaseTotal - 1)
        const lastPage = Math.max(1, Math.ceil(remainingTotal / RELEASE_PAGE_SIZE))
        await loadReleasePage(Math.min(releasePage, lastPage))
        setExperimentReleases((old) => old.filter((release) => release.id !== item.id))
        return { archived: true }
      }
      const result = await republishRelease(project.id, item.id)
      if (result.release) {
        upsertRelease(result.release)
        setReleasePage(1)
        await loadReleasePage(1)
      }
      return { republished: true, release: result.release }
    } catch (error) {
      message.error(error.message || '移除发布单失败')
      return null
    }
  }
  const publishEnvironment = async (release, environment, config) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有执行发布的权限')
      return null
    }
    if (!canPublish) {
      message.warning(gitAccess?.message || '请先在项目设置中完成仓库机器人授权')
      return null
    }
    if (publishRequestRef.current) {
      message.info('发布请求正在提交，请稍候')
      return null
    }
    publishRequestRef.current = true
    setReleaseSaving(true)
    const environmentName = environment?.label || '环境'
    const messageKey = `release-publish-${release.id}-${environment?.key || environment?.id || 'environment'}`
    message.open({ key: messageKey, type: 'loading', content: `正在提交${environmentName}发布请求，请稍候...`, duration: 0 })
    try {
      const updated = await publishRelease(project.id, release.id)
      const next = upsertRelease(updated)
      await loadRuntime(environment?.target?.id || environment?.id || selectedTarget?.id)
      message.success({ key: messageKey, content: `${environmentName}环境发布已提交`, duration: 3 })
      return next
    } catch (error) {
      message.error({ key: messageKey, content: error.message || `${environmentName}环境发布提交失败`, duration: 4 })
      return null
    } finally {
      publishRequestRef.current = false
      setReleaseSaving(false)
    }
  }
  const openNewRelease = (preferredBranch = branch) => {
    if (!canCreateRelease) {
      message.warning('当前角色没有创建发布的权限')
      return
    }
    if (preferredBranch && preferredBranch !== branch) setBranch(preferredBranch)
    setReleaseTarget(null)
    setSelected(commits[0] ? [commits[0]] : [])
    setReleaseTargetIds(availableTargets.filter((target) => target.enabled).map((target) => target.id))
    const firstReleaseTarget = availableTargets.find((target) => target.enabled) || availableTargets[0]
    setStrategy(firstReleaseTarget?.deploy_strategy || project.deploy_strategy || 'rolling')
    setReleaseOpen(true)
  }
  const doRelease = async (publish = true) => {
    if (publish && !canPublishRelease) {
      message.warning('当前角色没有执行发布的权限')
      return
    }
    if (!publish && !canCreateRelease) {
      message.warning('当前角色没有创建发布草稿的权限')
      return
    }
    if (!publish && releaseTarget) {
      message.info('这个发布已经是草稿，点击“开始发布”即可继续')
      return
    }
    if (!releaseTarget && !releaseTargetIds.length) { message.warning('请先在“发布环境”中添加并启用 DEV 环境'); setTab('targets'); return }
    if (publish && !canPublish) {
      message.warning(gitAccess?.message || '请先在项目设置中完成仓库机器人授权')
      return
    }
    setReleaseSaving(true)
    let created = null
    try {
      let release
      if (releaseTarget) {
        release = await publishRelease(project.id, releaseTarget.id)
      } else {
        const sourceBranch = branch
        const latestCommits = (await getCommits(project.id, sourceBranch)) || []
        const latestCommit = latestCommits[0]
        if (!latestCommit?.sha) {
          message.warning('当前分支没有可发布版本')
          return
        }
        setCommits(latestCommits)
        setSelected([latestCommit])
        const releaseBranch = sourceBranch
        const releaseSHAs = [latestCommit.sha]
        const traffic = strategy === 'rolling'
          ? { stable_percent: 100, candidate_percent: 0, blue_percent: 0, green_percent: 0 }
          : strategy === 'canary'
            ? { stable_percent: 100 - candidate, candidate_percent: candidate, blue_percent: 0, green_percent: 0 }
            : { stable_percent: 0, candidate_percent: 0, blue_percent: 100 - candidate, green_percent: candidate }
        created = await createRelease(project.id, {
          branch: releaseBranch,
          commit_shas: releaseSHAs,
          source_branch: sourceBranch,
          target_ids: releaseTargetIds,
          strategy,
          ...traffic,
          publish: false,
        })
        release = created
        if (publish) release = await publishRelease(project.id, created.id)
      }
      const next = upsertRelease(release)
      if (publish) await loadRuntime(selectedTarget?.id)
      setReleaseOpen(false)
      setReleaseTarget(null)
      setSelected([])
      if (publish) {
        message.success(created?.duplicate ? '发现相同版本，已再次开始发布' : '发布已开始，可以在发布记录里查看进度')
      } else {
        message.success('草稿已保存，可以稍后发布')
      }
      return next
    } catch (releaseError) {
      if (created) upsertRelease(created)
      message.error(releaseError.message || (publish ? '发布失败' : '保存草稿失败'))
    } finally { setReleaseSaving(false) }
  }
  const republish = async (item) => {
    const isDraft = item.status === 'draft'
    const nextBranch = item.branch || branch
    setReleaseTarget(isDraft ? item : null)
    setReleaseTargetIds(item.targets?.map((target) => target.id).filter(Boolean) || availableTargets.filter((target) => target.enabled).map((target) => target.id))
    if (nextBranch !== branch) {
      setBranch(nextBranch)
      setBranchLoading(true)
      try {
        const nextCommits = (await getCommits(project.id, nextBranch)) || []
        setCommits(nextCommits)
        setSelected(nextCommits[0] ? [nextCommits[0]] : [])
      } catch (error) {
        message.error(error.message || '加载分支版本失败')
        return
      } finally {
        setBranchLoading(false)
      }
    } else if (!isDraft) {
      setSelected(commits[0] ? [commits[0]] : [])
    } else {
      setSelected(item.commits || [])
    }
    setStrategy(item.strategy || item.plan?.strategy || 'rolling')
    const nextCandidate = item.strategy === 'blue_green' || item.plan?.strategy === 'blue_green'
      ? item.plan?.traffic?.green_percent
      : item.plan?.traffic?.candidate_percent
    if (Number.isFinite(nextCandidate) && nextCandidate > 0) setCandidate(nextCandidate)
    setReleaseOpen(true)
  }
  const stopRelease = async (item) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有取消发布的权限')
      return
    }
    setCancellingRelease(item.id)
    try {
      const updated = upsertRelease(await cancelRelease(project.id, item.id))
      message.success(updated.message || '发布已取消')
    } catch (error) {
      message.error(error.message || '取消发布失败')
    } finally {
      setCancellingRelease('')
    }
  }
  const retryTarget = async (item, target) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有执行发布的权限')
      return
    }
    const key = `${item.id}:${target.id}`
    setRetryingTarget(key)
    try {
      const updated = upsertRelease(await retryReleaseTarget(project.id, item.id, target.id))
      await loadRuntime(target.id)
      message.success(`已开始重试${target.name || '这个环境'}，其他已成功环境不会重复发布`)
      return updated
    } catch (error) {
      message.error(error.message || '环境重试失败')
    } finally {
      setRetryingTarget('')
    }
  }
  const openSettings = () => {
    setSettingsOpen(true)
  }
  const saveGitCredential = async (values) => {
    try {
      const result = await saveProjectGitCredential(project.id, values)
      setGitCredential(result.credential)
      setGitAccess(result.access)
      message.success('仓库机器人已验证并保存')
    } catch (error) {
      message.error(error.message || '仓库机器人保存失败')
      throw error
    }
  }
  const deleteGitCredential = async () => {
    try {
      await deleteProjectGitCredential(project.id)
      await loadGitAccess()
      message.success('仓库机器人授权已移除')
    } catch (error) {
      message.error(error.message || '移除仓库机器人失败')
    }
  }
  const saveSettings = async (values) => {
    if (gitAccessLoading) {
      message.warning('正在检查仓库机器人，请稍后再保存')
      return
    }
    if (gitCredential?.configured !== true) {
      message.warning('请先在项目设置中配置并验证仓库机器人')
      return
    }
    setSettingsLoading(true)
    try {
      const updated = await updateProject(project.id, values)
      const nextBranch = updated.default_branch || branch
      setProject(updated)
      setBranch(nextBranch)
      setSelected([])
      setSettingsOpen(false)
      await Promise.all([load(nextBranch), loadGitAccess()])
      message.success('项目基本设置已保存，下次发布会使用新的代码配置')
    } catch (error) {
      message.error(error.message || '项目设置保存失败')
    } finally { setSettingsLoading(false) }
  }
  return <div className="page-wrap detail-page">
    <div className="detail-shell">
      <button type="button" className="back-link" onClick={onBack}>← 返回项目列表</button>
      <div className="detail-heading"><div className="detail-project-title"><div className="project-icon large"><CodeOutlined /></div><div><Typography.Title level={2}>{project.name}</Typography.Title><Typography.Paragraph type="secondary"><CodeOutlined /> {project.repository_url} <span className="heading-separator">·</span> 分支 <strong>{branch}</strong></Typography.Paragraph></div></div><Space wrap>{canUpdateProject && <Button icon={<SettingOutlined />} onClick={openSettings}>项目设置</Button>}</Space></div>
      <div className="detail-tabs"><button className={tab === 'release' ? 'active' : ''} onClick={() => setTab('release')} type="button"><CloudUploadOutlined /> 发布</button><button className={tab === 'experiments' ? 'active' : ''} onClick={() => setTab('experiments')} type="button"><ExperimentOutlined /> A/B 实验</button><button className={tab === 'targets' ? 'active' : ''} onClick={() => setTab('targets')} type="button"><EnvironmentOutlined /> 发布环境</button><button className={tab === 'config' ? 'active' : ''} onClick={() => setTab('config')} type="button"><FileTextOutlined /> 部署配置</button><button className={tab === 'runtime' ? 'active' : ''} onClick={() => setTab('runtime')} type="button"><DeploymentUnitOutlined /> Pod 运行态</button><button className={tab === 'monitor' ? 'active' : ''} onClick={() => setTab('monitor')} type="button"><DashboardOutlined /> 监控</button></div>
    </div>
    {tab === 'release' && <ReleaseFlow
      embedded
      project={project}
      spaceName={spaceName}
      userName={userName}
      onBack={() => setTab('targets')}
      onOpenTargets={() => setTab('targets')}
      branch={branch}
      branches={branches}
      branchLoading={branchLoading}
      releases={releases}
      releaseFlow={releaseFlow}
      releasePage={releasePage}
      releasePageSize={RELEASE_PAGE_SIZE}
      releaseTotal={releaseTotal}
      releaseSearch={releaseSearch}
      loading={loading}
      onRefresh={() => load(branch, releasePage, releaseSearch)}
      onReleasePageChange={loadReleasePage}
      onReleaseSearch={searchReleaseOrders}
      onCreateRelease={createReleaseOrder}
      onPublishEnvironment={publishEnvironment}
      onUpdateTraffic={updateReleaseTraffic}
      publishing={releaseSaving}
      onRetryTarget={retryTarget}
      onLoadReleaseTargetLogs={loadReleaseTargetLogs}
      onEnvironmentChange={changeReleaseDetailEnvironment}
      onOpenPod={openDetailPod}
      onOpenPodLogs={openPodLogs}
      onOpenMonitor={openPodMonitor}
      onOpenTerminal={openPodTerminal}
      canOpenTerminal={canRuntimeTerminal}
      canOpenPodLogs={canRuntimeRead}
      pods={pods}
      retryingTarget={retryingTarget}
      canCreateRelease={canCreateRelease}
      canUpdateRelease={canUpdateRelease}
      canPublishRelease={canPublishRelease}
      onRemoveRelease={removeReleaseOrder}
      targets={availableTargets}
    />}
    {tab === 'experiments' && <ABExperiment
      project={project}
      targets={availableTargets}
      releases={experimentReleases}
      experiments={experiments}
      loading={experimentLoading}
      canCreate={canCreateRelease}
      canOperate={canPublishRelease}
      onRefresh={loadABExperiments}
      onCreate={createExperiment}
      onUpdateTraffic={updateExperimentTraffic}
      onStop={stopExperiment}
      onFinish={finishExperiment}
    />}
    {tab === 'targets' && <DeploymentTargets project={project} targets={targets} releases={releases} clusters={clusters} loading={targetLoading} canEdit={canUpdateProject} onReload={() => reloadTargets(selectedTargetId)} onCreate={createTarget} onUpdate={updateTarget} onDelete={deleteTarget} />}
    {tab === 'config' && <DeploymentConfigEditor project={project} readOnly={!canUpdateProject} />}
    {tab === 'runtime' && <RuntimeTab project={project} targets={availableTargets} clusters={clusters} selectedTargetId={selectedTarget?.id} onTargetChange={changeTarget} pods={pods} loading={loading} onRefresh={() => loadRuntime(selectedTarget?.id)} onOpenPod={(podValue) => openDetailPod(podValue, selectedTarget)} onOpenMonitor={(podValue) => openPodMonitor(podValue, selectedTarget)} onOpenTerminal={(podValue) => openPodTerminal(podValue, selectedTarget)} onOpenLogs={(podValue) => openPodLogs(podValue, selectedTarget)} canOpenTerminal={canRuntimeTerminal} canOpenLogs={canRuntimeRead} />}
    {tab === 'monitor' && <MonitorTab metrics={metrics} pods={pods} project={project} targets={availableTargets} selectedTargetId={selectedTarget?.id} onTargetChange={changeTarget} onRefresh={() => loadRuntime(selectedTarget?.id)} onOpenCluster={onOpenCluster} focusPod={monitorPod} onClearPod={() => setMonitorPod(null)} />}
    <ProjectSettings project={project} open={settingsOpen} loading={settingsLoading} onCancel={() => setSettingsOpen(false)} onSubmit={saveSettings} gitCredential={gitCredential} gitCredentialLoading={gitAccessLoading} onSaveGitCredential={saveGitCredential} onDeleteGitCredential={deleteGitCredential} registryConnections={registryConnections} deploymentTargets={availableTargets} canManageGit={canManageGit} />
    <PodDrawer project={project} pod={pod} targetId={(podTarget || selectedTarget)?.id} open={Boolean(pod)} onClose={() => setPod(null)} onPodNotFound={() => { setPod(null); void loadRuntime((podTarget || selectedTarget)?.id); message.info('Pod 已完成滚动更新，运行态列表已刷新') }} />
    <PodTerminal project={project} pod={terminalPod} target={podTarget || selectedTarget} targetId={(podTarget || selectedTarget)?.id} open={Boolean(terminalPod)} onClose={() => setTerminalPod(null)} canExecute={canRuntimeTerminal} />
  </div>
}

function RuntimeTab({ project, targets = [], clusters = [], selectedTargetId, onTargetChange, pods = [], loading, onRefresh, onOpenPod, onOpenMonitor, onOpenTerminal, onOpenLogs, canOpenTerminal, canOpenLogs = true }) {
  const target = targets.find((item) => item.id === selectedTargetId) || targets[0]
  const cluster = clusters.find((item) => item.id === target?.cluster_id)
  const clusterStatus = String(cluster?.status || '').toLowerCase()
  const runtimeStatus = clusterStatus === 'offline'
    ? { color: 'red', label: '集群离线' }
    : clusterStatus === 'draining'
      ? { color: 'orange', label: '集群维护中' }
      : target?.health === 'degraded'
        ? { color: 'orange', label: '运行异常' }
        : clusterStatus === 'active' && target?.health === 'healthy'
          ? { color: 'green', label: '集群正常' }
          : { color: 'default', label: '暂无运行数据' }
  const columns = [
    { title: 'Pod', dataIndex: 'name', render: (value, item) => <Button type="link" className="pod-link" onClick={() => onOpenPod(item)}>{value}</Button> },
    { title: '状态', dataIndex: 'phase', render: (value, item) => <Tag color={item.ready ? 'green' : 'orange'}>{item.ready ? '运行中' : value || '处理中'}</Tag> },
    { title: 'Pod IP', dataIndex: 'pod_ip' },
    { title: '节点', dataIndex: 'node_name' },
    { title: '重启次数', dataIndex: 'restarts', render: (value, item) => value ?? item.restart_count ?? 0 },
    { title: '操作', width: 285, render: (_, item) => <Space size={2}><Button type="link" icon={<DashboardOutlined />} onClick={() => onOpenMonitor?.(item)}>监控</Button><Button type="link" icon={<CodeOutlined />} disabled={!canOpenTerminal} title={canOpenTerminal ? '进入 Pod Terminal' : '当前账号没有进入 Pod 终端的权限'} onClick={() => onOpenTerminal?.(item)}>Terminal</Button><Button type="link" icon={<FileTextOutlined />} disabled={!canOpenLogs} title={canOpenLogs ? '查看 Pod 标准输出和标准错误日志' : '当前账号没有查看 Pod 日志的权限'} onClick={() => onOpenLogs?.(item)}>日志</Button><Button type="link" onClick={() => onOpenPod(item)}>详情</Button></Space> },
  ]
  return <section className="runtime-panel">
    <div className="panel-heading runtime-panel-heading">
      <div>
        <Typography.Title level={4}>Pod 运行态</Typography.Title>
        <Typography.Text type="secondary">{target ? `${target.cluster_id || '未配置集群'} / ${target.namespace || '未配置 namespace'}` : '尚未配置发布环境'}</Typography.Text>
      </div>
      <Space wrap className="runtime-panel-actions">
        {targets.length > 0 && <DeploymentTargetSelect className="runtime-target-select" targets={targets} value={target?.id} onChange={onTargetChange} disabled={loading} />}
        <Tag color={runtimeStatus.color}><span className="status-dot inline" /> {runtimeStatus.label}</Tag>
        <Button icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>刷新</Button>
      </Space>
    </div>
    <Table rowKey="name" loading={loading} columns={columns} dataSource={pods} pagination={false} locale={{ emptyText: '当前环境没有 Pod' }} />
  </section>
}

function MonitorTab({ metrics, pods, project, targets = [], selectedTargetId, onTargetChange, onRefresh, onOpenCluster, focusPod, onClearPod }) {
  return <MonitorDashboard scope="project" metrics={metrics} pods={pods} project={project} targets={targets} selectedTargetId={selectedTargetId} onTargetChange={onTargetChange} onRefresh={onRefresh} onOpenCluster={onOpenCluster} focusPod={focusPod} onClearPod={onClearPod} />
}

function ActivityPage() {
  const [items, setItems] = useState([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [keyword, setKeyword] = useState('')
  const [actor, setActor] = useState('')
  const [action, setAction] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setItems((await getAuditLogs(200)) || [])
    } catch (loadError) {
      setError(loadError.message || '操作记录加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const actorOptions = useMemo(() => {
    const values = new Map()
    items.forEach((item) => {
      const value = item.user_id ? String(item.user_id) : 'system'
      const label = item.user_name || (item.user_id ? `用户 ${item.user_id}` : '系统')
      if (!values.has(value)) values.set(value, label)
    })
    return [...values.entries()]
      .map(([value, label]) => ({ value, label }))
      .sort((left, right) => left.label.localeCompare(right.label, 'zh-CN'))
  }, [items])

  const actionOptions = useMemo(() => [...new Set(items.map((item) => item.action).filter(Boolean))]
    .sort((left, right) => left.localeCompare(right, 'zh-CN'))
    .map((value) => ({ value, label: value })), [items])

  const filteredItems = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase()
    return items.filter((item) => {
      const itemActor = item.user_id ? String(item.user_id) : 'system'
      if (actor && itemActor !== actor) return false
      if (action && item.action !== action) return false
      if (!normalizedKeyword) return true
      const searchable = [
        item.id,
        item.user_id,
        item.user_name,
        item.action,
        item.target,
        item.created_at,
        formatDate(item.created_at),
      ].filter(Boolean).join(' ').toLowerCase()
      return searchable.includes(normalizedKeyword)
    })
  }, [action, actor, items, keyword])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(filteredItems.length / pageSize))
    setPage((current) => Math.min(current, maxPage))
  }, [filteredItems.length, pageSize])

  useEffect(() => {
    setPage(1)
  }, [action, actor, keyword])

  const clearFilters = () => {
    setKeyword('')
    setActor('')
    setAction('')
  }

  const columns = [
    {
      title: '时间',
      dataIndex: 'created_at',
      width: 174,
      render: (value) => <Typography.Text type="secondary" className="activity-time">{formatDate(value, true)}</Typography.Text>,
    },
    {
      title: '操作人',
      dataIndex: 'user_name',
      width: 150,
      render: (value, item) => <div className="activity-operator"><span className="activity-operator-avatar">{(value || '系').slice(0, 1)}</span><span><strong>{value || (item.user_id ? `用户 ${item.user_id}` : '系统')}</strong>{item.user_id ? <small>ID {item.user_id}</small> : <small>系统操作</small>}</span></div>,
    },
    {
      title: '操作事项',
      dataIndex: 'action',
      width: 180,
      render: (value) => <Tag color="blue" className="activity-action-tag">{value || '系统操作'}</Tag>,
    },
    {
      title: '目标 / 关键字段',
      dataIndex: 'target',
      render: (value) => <Typography.Text className="activity-target-cell" title={value || '系统'}>{value || '系统'}</Typography.Text>,
    },
    {
      title: '记录编号',
      dataIndex: 'id',
      width: 112,
      render: (value) => <Typography.Text code>#{value || '-'}</Typography.Text>,
    },
  ]

  return <div className="page-wrap">
    <div className="page-heading activity-heading">
      <div>
        <Typography.Title level={2}>操作记录</Typography.Title>
        <Typography.Paragraph type="secondary">记录空间内的关键配置、发布和运行操作，可按操作人、事项或目标快速筛选。</Typography.Paragraph>
      </div>
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新记录</Button>
    </div>
    {error && <div className="activity-error">{error}</div>}
    <Card variant="borderless" className="activity-panel">
      <div className="activity-toolbar">
        <Input
          allowClear
          value={keyword}
          prefix={<SearchOutlined />}
          placeholder="搜索操作人、操作事项、目标或关键字段"
          onChange={(event) => setKeyword(event.target.value)}
          className="activity-keyword"
        />
        <Select
          allowClear
          value={actor || undefined}
          placeholder="操作人"
          options={actorOptions}
          onChange={(value) => setActor(value || '')}
          className="activity-filter"
        />
        <Select
          allowClear
          value={action || undefined}
          placeholder="操作事项"
          options={actionOptions}
          onChange={(value) => setAction(value || '')}
          className="activity-filter"
        />
        {(keyword || actor || action) && <Button type="link" icon={<ClearOutlined />} onClick={clearFilters}>清空筛选</Button>}
        <Typography.Text type="secondary" className="activity-result-count">共 {filteredItems.length} 条</Typography.Text>
      </div>
      <Table
        rowKey={(item) => item.id || `${item.created_at}-${item.action}-${item.target}`}
        columns={columns}
        dataSource={filteredItems}
        loading={loading}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={items.length ? '没有匹配的操作记录' : '暂无操作记录'} /> }}
        pagination={{
          current: page,
          pageSize,
          total: filteredItems.length,
          showSizeChanger: true,
          pageSizeOptions: ['10', '20', '50'],
          showTotal: (total, range) => `${range[0]}-${range[1]} / 共 ${total} 条记录`,
          onChange: (nextPage, nextPageSize) => {
            setPage(nextPage)
            setPageSize(nextPageSize)
          },
        }}
      />
    </Card>
  </div>
}

function normalizeReleases(items) { return items.map(normalizeRelease) }
function formatDate(value, withYear = false) { if (!value) return '-'; const date = new Date(value); if (Number.isNaN(date.getTime())) return value; return date.toLocaleString('zh-CN', withYear ? { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' } : { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function strategyLabel(value) { return ({ rolling: '滚动发布', canary: '灰度发布', blue_green: '蓝绿发布' })[value] || '滚动发布' }

export default App
