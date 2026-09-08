import {
  App as AntApp,
  Avatar,
  Badge,
  Button,
  Card,
  Col,
  ConfigProvider,
  Dropdown,
  Empty,
  Layout,
  List,
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
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { useCallback, useEffect, useRef, useState } from 'react'
import LoginPage from './components/LoginPage'
import SpacePicker from './components/SpacePicker'
import ProjectForm from './components/ProjectForm'
import PodDrawer from './components/PodDrawer'
import PodTerminal from './components/PodTerminal'
import ClusterOverview from './components/ClusterOverview'
import ProjectSettings from './components/ProjectSettings'
import MonitorDashboard from './components/MonitorDashboard'
import ReleaseFlow from './components/ReleaseFlow'
import DeploymentConfigEditor from './components/DeploymentConfigEditor'
import SpaceSettingsPage from './components/SpaceSettingsPage'
import SpaceMembersPage from './components/SpaceMembersPage'
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
  getDeploymentTargets,
  getGitAccess,
  getProjectGitCredential,
  saveProjectGitCredential,
  deleteProjectGitCredential,
  getAuditLogs,
  getABExperiments,
  getMetrics,
  getProjects,
  getReleases,
  getReleaseTargetLogs,
  getSpaces,
  getPods,
  login,
  normalizeRelease,
  publishRelease,
  retryReleaseTarget,
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
    can(PERMISSIONS.CLUSTER_READ) && { key: 'clusters', icon: <ClusterOutlined />, label: '集群管理' },
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
    : ({ clusters: '集群管理', activity: '操作记录', settings: '空间设置', members: '成员与权限', projects: '项目' }[section] || '项目')
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
        {selectedProject ? <ProjectDetail key={selectedProject.id} project={selectedProject} spaceName={currentSpace?.name} userName={user?.display_name || user?.username} permissions={{ canUpdateProject: can(PERMISSIONS.PROJECT_UPDATE), canCreateRelease: can(PERMISSIONS.RELEASE_CREATE), canUpdateRelease: can(PERMISSIONS.RELEASE_UPDATE), canPublishRelease: can(PERMISSIONS.RELEASE_PUBLISH), canRuntimeConfig: can(PERMISSIONS.RUNTIME_CONFIG), canRuntimeTerminal: can(PERMISSIONS.RUNTIME_TERMINAL) }} onBack={() => setSelectedProject(null)} onOpenCluster={() => { setSelectedProject(null); setSection('clusters') }} /> : section === 'clusters' ? <ClusterOverview canManageClusters={can(PERMISSIONS.CLUSTER_MANAGE)} onOpenProjects={() => setSection('projects')} /> : section === 'activity' ? <ActivityPage /> : section === 'settings' ? <SpaceSettingsPage role={role} user={user} onSpaceUpdated={onSpaceUpdated} /> : section === 'members' ? <SpaceMembersPage role={role} user={user} /> : <ProjectsPage canCreateProject={can(PERMISSIONS.PROJECT_CREATE)} onOpen={setSelectedProject} />}
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
      message.success('项目创建成功，正在检查 Git 仓库权限')
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
    {canCreateProject && <ProjectForm open={createOpen} onCancel={() => setCreateOpen(false)} onSubmit={submit} loading={createLoading} clusters={clusters} />}
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
  const canUpdateProject = permissions.canUpdateProject !== false
  const canCreateRelease = permissions.canCreateRelease !== false
  const canPublishRelease = permissions.canPublishRelease !== false
  const canRuntimeConfig = permissions.canRuntimeConfig !== false
  const canRuntimeTerminal = permissions.canRuntimeTerminal !== false
  const initialBranch = project.default_branch || 'main'
  const [branch, setBranch] = useState(initialBranch)
  const [branches, setBranches] = useState([])
  const [branchLoading, setBranchLoading] = useState(false)
  const [tab, setTab] = useState('release')
  const [commits, setCommits] = useState([])
  const [selected, setSelected] = useState([])
  const [releases, setReleases] = useState([])
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
  const [releaseTarget, setReleaseTarget] = useState(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [clusters, setClusters] = useState([])
  const [cancellingRelease, setCancellingRelease] = useState('')
  const [retryingTarget, setRetryingTarget] = useState('')
  const [gitCredential, setGitCredential] = useState(null)
  const [gitAccess, setGitAccess] = useState(null)
  const [gitAccessLoading, setGitAccessLoading] = useState(true)
  const [gitAccessError, setGitAccessError] = useState('')
  const gitAccessRequestRef = useRef(0)

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

  const loadRuntime = async (targetId = selectedTarget?.id) => {
    if (!targetId) {
      setPods([])
      setMetrics(null)
      return []
    }
    const runtimeResults = await Promise.allSettled([
      getPods(project.id, targetId),
      getMetrics(project.id, targetId),
    ])
    const [podResult, metricResult] = runtimeResults
    if (podResult.status === 'fulfilled') setPods(podResult.value || [])
    if (metricResult.status === 'fulfilled') setMetrics(metricResult.value || null)
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

  const load = async (branchName = branch) => {
    setLoading(true)
    setTargetLoading(true)
    try {
      const results = await Promise.allSettled([
        getBranches(project.id),
        getCommits(project.id, branchName),
        getReleases(project.id),
        getDeploymentTargets(project.id),
        getABExperiments(project.id),
      ])
      const failures = []
      const [branchResult, commitResult, releaseResult, targetResult, experimentResult] = results
      if (branchResult.status === 'fulfilled') setBranches(branchResult.value || [])
      else failures.push(`分支：${branchResult.reason?.message || '加载失败'}`)
      if (commitResult.status === 'fulfilled') setCommits(commitResult.value || [])
      else failures.push(`提交：${commitResult.reason?.message || '加载失败'}`)
      if (releaseResult.status === 'fulfilled') setReleases(normalizeReleases(releaseResult.value || []))
      else failures.push(`发布记录：${releaseResult.reason?.message || '加载失败'}`)
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
    load(initialBranch)
    loadGitAccess()
    loadClusters()
  }, [project.id])

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
    const target = availableTargets.find((item) => targetEnvironmentKey(item) === environment)
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
        const next = await getReleases(project.id)
        if (active) setReleases(normalizeReleases(next || []))
      } catch {
        // The next scheduled poll can recover from a temporary request failure.
      }
    }
    const timer = window.setInterval(poll, 3000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [hasActiveReleases, project.id])

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
    setReleases((old) => [next, ...old.filter((item) => item.id !== next.id)])
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
      const targetIds = availableTargets.filter((target) => target.enabled).map((target) => target.id)
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
      const next = upsertRelease(created)
      message.success(`发布单 ${next.id} 已创建`)
      return next
    } catch (error) {
      message.error(error.message || '创建发布单失败')
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
    setReleaseSaving(true)
    try {
      const updated = await publishRelease(project.id, release.id)
      const next = upsertRelease(updated)
      message.success(`${environment?.label || '环境'} 环境发布已提交`)
      return next
    } finally {
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
    if (!releaseTarget && !releaseTargetIds.length) { message.warning('至少选择一个发布环境'); return }
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
      <div className="detail-tabs"><button className={tab === 'release' ? 'active' : ''} onClick={() => setTab('release')} type="button"><CloudUploadOutlined /> 发布</button><button className={tab === 'experiments' ? 'active' : ''} onClick={() => setTab('experiments')} type="button"><ExperimentOutlined /> A/B 实验 <Badge count={experiments.filter((item) => item.status === 'running').length || 0} size="small" /></button><button className={tab === 'targets' ? 'active' : ''} onClick={() => setTab('targets')} type="button"><EnvironmentOutlined /> 发布环境 <Badge count={availableTargets.length} size="small" /></button><button className={tab === 'config' ? 'active' : ''} onClick={() => setTab('config')} type="button"><FileTextOutlined /> 部署配置</button><button className={tab === 'runtime' ? 'active' : ''} onClick={() => setTab('runtime')} type="button"><DeploymentUnitOutlined /> Pod 运行态 <Badge count={pods.length} size="small" /></button><button className={tab === 'monitor' ? 'active' : ''} onClick={() => setTab('monitor')} type="button"><DashboardOutlined /> 监控</button></div>
    </div>
    {tab === 'release' && <ReleaseFlow
      embedded
      project={project}
      spaceName={spaceName}
      userName={userName}
      onBack={() => setTab('targets')}
      branch={branch}
      branches={branches}
      branchLoading={branchLoading}
      releases={releases}
      loading={loading}
      onRefresh={() => load(branch)}
      onCreateRelease={createReleaseOrder}
      onPublishEnvironment={publishEnvironment}
      onRetryTarget={retryTarget}
      onLoadReleaseTargetLogs={loadReleaseTargetLogs}
      onEnvironmentChange={changeReleaseDetailEnvironment}
      onOpenPod={openDetailPod}
      onOpenMonitor={openPodMonitor}
      onOpenTerminal={openPodTerminal}
      canOpenTerminal={canRuntimeTerminal}
      pods={pods}
      retryingTarget={retryingTarget}
      canCreateRelease={canCreateRelease}
      canPublishRelease={canPublishRelease}
      targets={availableTargets}
    />}
    {tab === 'experiments' && <ABExperiment
      project={project}
      targets={availableTargets}
      releases={releases}
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
    {tab === 'runtime' && <RuntimeTab project={project} targets={availableTargets} clusters={clusters} selectedTargetId={selectedTarget?.id} onTargetChange={changeTarget} pods={pods} loading={loading} onRefresh={() => loadRuntime(selectedTarget?.id)} onOpenPod={(podValue) => openDetailPod(podValue, selectedTarget)} onOpenMonitor={(podValue) => openPodMonitor(podValue, selectedTarget)} onOpenTerminal={(podValue) => openPodTerminal(podValue, selectedTarget)} canOpenTerminal={canRuntimeTerminal} />}
    {tab === 'monitor' && <MonitorTab metrics={metrics} pods={pods} project={project} targets={availableTargets} selectedTargetId={selectedTarget?.id} onTargetChange={changeTarget} onRefresh={() => loadRuntime(selectedTarget?.id)} onOpenCluster={onOpenCluster} focusPod={monitorPod} onClearPod={() => setMonitorPod(null)} />}
    <ProjectSettings project={project} open={settingsOpen} loading={settingsLoading} onCancel={() => setSettingsOpen(false)} onSubmit={saveSettings} onGoTargets={() => { setSettingsOpen(false); setTab('targets') }} gitCredential={gitCredential} gitCredentialLoading={gitAccessLoading} onSaveGitCredential={saveGitCredential} onDeleteGitCredential={deleteGitCredential} />
    <PodDrawer project={project} pod={pod} target={podTarget || selectedTarget} targetId={(podTarget || selectedTarget)?.id} open={Boolean(pod)} onClose={() => setPod(null)} canEdit={canRuntimeConfig} />
    <PodTerminal project={project} pod={terminalPod} target={podTarget || selectedTarget} targetId={(podTarget || selectedTarget)?.id} open={Boolean(terminalPod)} onClose={() => setTerminalPod(null)} canExecute={canRuntimeTerminal} />
  </div>
}

function RuntimeTab({ project, targets = [], clusters = [], selectedTargetId, onTargetChange, pods = [], loading, onRefresh, onOpenPod, onOpenMonitor, onOpenTerminal, canOpenTerminal }) {
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
    { title: '操作', width: 235, render: (_, item) => <Space size={2}><Button type="link" icon={<DashboardOutlined />} onClick={() => onOpenMonitor?.(item)}>监控</Button><Button type="link" icon={<CodeOutlined />} disabled={!canOpenTerminal} title={canOpenTerminal ? '进入 Pod Terminal' : '当前账号没有进入 Pod 终端的权限'} onClick={() => onOpenTerminal?.(item)}>Terminal</Button><Button type="link" onClick={() => onOpenPod(item)}>详情</Button></Space> },
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
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setItems((await getAuditLogs()) || [])
    } catch (loadError) {
      setError(loadError.message || '操作记录加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  useEffect(() => {
    const maxPage = Math.max(1, Math.ceil(items.length / pageSize))
    setPage((current) => Math.min(current, maxPage))
  }, [items.length, pageSize])

  const visibleItems = items.slice((page - 1) * pageSize, page * pageSize)

  return <div className="page-wrap">
    <div className="page-heading activity-heading">
      <div>
        <Typography.Title level={2}>操作记录</Typography.Title>
      </div>
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新记录</Button>
    </div>
    {error && <div className="activity-error">{error}</div>}
    <Card variant="borderless" className="activity-panel">
      {loading ? <div className="loading-placeholder">加载记录中...</div> : items.length ? <><List
        dataSource={visibleItems}
        renderItem={(item) => <List.Item className="activity-item">
          <div className="activity-marker" />
          <div className="activity-content">
            <div className="activity-item-head"><Typography.Text strong>{item.action}</Typography.Text><Typography.Text type="secondary">{formatDate(item.created_at)}</Typography.Text></div>
            <div className="activity-target">{item.target || '系统'}</div>
            <Typography.Text type="secondary" className="activity-actor">操作人：{item.user_name || (item.user_id ? `用户 ${item.user_id}` : '系统')}</Typography.Text>
          </div>
        </List.Item>}
      /><Pagination className="activity-pagination" current={page} pageSize={pageSize} total={items.length} showSizeChanger pageSizeOptions={['10', '20', '50']} showTotal={(total, range) => `${range[0]}-${range[1]} / 共 ${total} 条记录`} onChange={(nextPage, nextPageSize) => { setPage(nextPage); setPageSize(nextPageSize) }} /></> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无操作记录" />}
    </Card>
  </div>
}

function normalizeReleases(items) { return items.map(normalizeRelease) }
function formatDate(value) { if (!value) return '-'; const date = new Date(value); if (Number.isNaN(date.getTime())) return value; return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function strategyLabel(value) { return ({ rolling: '滚动发布', canary: '灰度发布', blue_green: '蓝绿发布' })[value] || '滚动发布' }

export default App
