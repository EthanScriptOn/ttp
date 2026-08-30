import {
  App as AntApp,
  Avatar,
  Badge,
  Button,
  Card,
  Checkbox,
  Col,
  ConfigProvider,
  Dropdown,
  Empty,
  Layout,
  List,
  Menu,
  Modal,
  Progress,
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
  ArrowRightOutlined,
  BranchesOutlined,
  CheckOutlined,
  ClusterOutlined,
  CloudUploadOutlined,
  CodeOutlined,
  DashboardOutlined,
  DeploymentUnitOutlined,
  DownOutlined,
  EnvironmentOutlined,
  FileTextOutlined,
  HistoryOutlined,
  LogoutOutlined,
  PlusOutlined,
  ReloadOutlined,
  RocketOutlined,
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { useEffect, useRef, useState } from 'react'
import LoginPage from './components/LoginPage'
import SpacePicker from './components/SpacePicker'
import ProjectForm from './components/ProjectForm'
import PodDrawer from './components/PodDrawer'
import ClusterOverview from './components/ClusterOverview'
import ProjectSettings from './components/ProjectSettings'
import ReleaseProgress from './components/ReleaseProgress'
import MonitorDashboard from './components/MonitorDashboard'
import CommitSelector from './components/CommitSelector'
import ReleaseConfirmModal from './components/ReleaseConfirmModal'
import DeploymentConfigEditor from './components/DeploymentConfigEditor'
import SpaceSettingsPage from './components/SpaceSettingsPage'
import DeploymentTargets, { DeploymentTargetSelect } from './components/DeploymentTargets'
import ReleaseTargetSummary from './components/ReleaseTargetSummary'
import ReleaseBatchPanel from './components/ReleaseBatchPanel'
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
  getGitServiceAccount,
  getTags,
  getAuditLogs,
  getProjects,
  getReleases,
  getReleaseBatches,
  getSpaces,
  getPods,
  login,
  normalizePreparation,
  normalizeBatch,
  normalizeRelease,
  removeCommit,
  prepareRelease,
  publishRelease,
  mergeReleaseToMain,
  closeReleaseBatch,
  publishReleaseTarget,
  retryReleaseTarget,
  selectSpace,
  updateDeploymentTarget,
  updateProject,
  deleteDeploymentTarget,
} from './services/api'
import { hasPermission, PERMISSIONS } from './services/permissions'

const { Header, Sider, Content } = Layout

const APP_THEME = {
  token: {
    colorPrimary: '#167c72',
    colorPrimaryHover: '#0f655d',
    colorPrimaryActive: '#0b514b',
    colorInfo: '#167c72',
    colorSuccess: '#2f8f68',
    colorLink: '#0f655d',
    colorBgLayout: '#f5f7f6',
    colorBorder: '#dee6e3',
    colorText: '#1f2a2e',
    colorTextSecondary: '#657278',
    borderRadius: 6,
    borderRadiusLG: 8,
  },
  components: {
    Menu: {
      darkItemBg: 'transparent',
      darkItemColor: '#b8c5c2',
      darkItemHoverColor: '#ffffff',
      darkItemHoverBg: 'rgba(255, 255, 255, .08)',
      darkItemSelectedColor: '#ffffff',
      darkItemSelectedBg: '#304247',
    },
    Progress: { defaultColor: '#2f8f68' },
    Switch: { colorPrimary: '#2f8f68', colorPrimaryHover: '#4aa57f' },
  },
}

function App() {
  const [auth, setAuth] = useState(() => {
    const token = localStorage.getItem('cicd_token')
    return token ? { token, user: { username: 'admin', display_name: '平台管理员', is_super_admin: true } } : null
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
          localStorage.removeItem('cicd_demo_mode')
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
    localStorage.removeItem('cicd_demo_mode')
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
    can(PERMISSIONS.MEMBER_READ) && { key: 'settings', icon: <SettingOutlined />, label: '空间设置' },
  ].filter(Boolean)
  const availableSections = new Set(menuItems.map((item) => item.key))
  useEffect(() => {
    if (!availableSections.has(section)) setSection('projects')
  }, [availableSections, section])
  const sectionTitle = selectedProject
    ? '项目'
    : ({ clusters: '集群管理', activity: '操作记录', settings: '空间设置', projects: '项目' }[section] || '项目')
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
      <Menu theme="dark" mode="inline" selectedKeys={[selectedProject ? 'projects' : section]} onClick={({ key }) => { setSection(key); if (key !== 'projects') setSelectedProject(null) }} items={menuItems} className="console-menu" />
      <div className="sider-bottom"><div className="sider-status"><span className="status-dot" /> 服务正常</div></div>
    </Sider>
    <Layout>
      <Header className="console-header">
        <div className="header-context"><span className="header-section">{sectionTitle}</span></div>
        <div className="header-user"><Dropdown trigger={['click']} placement="bottomRight" menu={{ className: 'space-switcher-menu', items: spaceMenuItems, selectable: false, onClick: handleSpaceMenuClick }}><Button type="text" className="space-switcher" aria-label={`切换空间，当前空间 ${currentSpace?.name || '未选择'}`} loading={switchingSpace}><TeamOutlined /><span className="space-switcher-copy"><small>当前空间</small><strong>{currentSpace?.name || '未选择'}</strong></span><DownOutlined className="space-switcher-arrow" /></Button></Dropdown><Avatar size={32} className="user-avatar">{(user?.display_name || user?.username || '管')[0]}</Avatar><span className="user-name">{user?.display_name || user?.username || '管理员'}</span><Button type="text" aria-label="退出登录" icon={<LogoutOutlined />} onClick={onLogout} /></div>
      </Header>
      <Content className="console-content">
        {selectedProject ? <ProjectDetail key={selectedProject.id} project={selectedProject} permissions={{ canUpdateProject: can(PERMISSIONS.PROJECT_UPDATE), canCreateRelease: can(PERMISSIONS.RELEASE_CREATE), canUpdateRelease: can(PERMISSIONS.RELEASE_UPDATE), canPublishRelease: can(PERMISSIONS.RELEASE_PUBLISH), canRuntimeConfig: can(PERMISSIONS.RUNTIME_CONFIG) }} onBack={() => setSelectedProject(null)} onOpenCluster={() => { setSelectedProject(null); setSection('clusters') }} /> : section === 'clusters' ? <ClusterOverview canManageClusters={can(PERMISSIONS.CLUSTER_MANAGE)} onOpenProjects={() => setSection('projects')} /> : section === 'activity' ? <ActivityPage /> : section === 'settings' ? <SpaceSettingsPage role={role} user={user} onSpaceUpdated={onSpaceUpdated} /> : <ProjectsPage canCreateProject={can(PERMISSIONS.PROJECT_CREATE)} onOpen={setSelectedProject} />}
      </Content>
    </Layout>
  </Layout>
}

function ProjectsPage({ onOpen, canCreateProject = true }) {
  const [projects, setProjects] = useState([])
  const [targetsByProject, setTargetsByProject] = useState({})
  const [gitServiceAccount, setGitServiceAccount] = useState(null)
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [createLoading, setCreateLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [clusters, setClusters] = useState([])
  const [keyword, setKeyword] = useState('')
  const [projectPage, setProjectPage] = useState(1)
  const projectPageSize = 10
  const load = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const [nextProjects, nextClusters, nextGitServiceAccount] = await Promise.all([
        getProjects(),
        getClusters(),
        getGitServiceAccount().catch(() => null),
      ])
      setProjects(nextProjects || [])
      setClusters(nextClusters || [])
      setGitServiceAccount(nextGitServiceAccount)
      const targetResults = await Promise.allSettled((nextProjects || []).map((project) => getDeploymentTargets(project.id)))
      setTargetsByProject(Object.fromEntries((nextProjects || []).map((project, index) => [
        project.id,
        targetResults[index].status === 'fulfilled' ? targetResults[index].value : null,
      ])))
    } catch (projectError) {
      setLoadError(projectError.message || '加载项目失败')
      message.error(projectError.message || '加载项目失败')
    } finally { setLoading(false) }
  }
  useEffect(() => {
    load()
  }, [])
  useEffect(() => {
    setProjectPage(1)
  }, [keyword])
  const visible = projects.filter((project) => `${project.name} ${project.repository_url}`.toLowerCase().includes(keyword.trim().toLowerCase()))
  const pageCount = Math.max(1, Math.ceil(visible.length / projectPageSize))
  const currentProjectPage = Math.min(projectPage, pageCount)
  const pagedProjects = visible.slice((currentProjectPage - 1) * projectPageSize, currentProjectPage * projectPageSize)
  const submit = async (values) => {
    setCreateLoading(true)
    try {
      const project = await createProject(values)
      setProjects((old) => [...old, project])
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
    <div className="list-toolbar"><div className="list-title"><Typography.Title level={4}>全部项目</Typography.Title><span className="count-muted">{keyword.trim() ? `${visible.length} / ${projects.length}` : projects.length}</span></div><input className="search-input" placeholder="搜索项目或仓库" value={keyword} onChange={(event) => setKeyword(event.target.value)} /></div>
    <div className="project-list" role="list" aria-label="项目列表">{pagedProjects.map((project) => <ProjectListRow key={project.id} project={project} targets={targetsByProject[project.id]} clusters={clusters} onClick={() => onOpen(project)} />)}</div>
    {!loading && visible.length > projectPageSize && <div className="project-list-footer"><Typography.Text type="secondary">显示 {(currentProjectPage - 1) * projectPageSize + 1}-{Math.min(currentProjectPage * projectPageSize, visible.length)} / 共 {visible.length} 个项目</Typography.Text><Pagination current={currentProjectPage} pageSize={projectPageSize} total={visible.length} showSizeChanger={false} onChange={setProjectPage} /></div>}
    {!loading && !visible.length && <Card className="empty-panel"><Empty description={keyword ? '没有匹配的项目' : canCreateProject ? '还没有项目，先创建一个吧' : '当前空间还没有项目'}>{canCreateProject && <Button type="primary" onClick={() => setCreateOpen(true)}>创建项目</Button>}</Empty></Card>}
    {loading && <div className="loading-placeholder">加载项目中...</div>}
    {!loading && loadError && <div className="loading-placeholder">{loadError}</div>}
    {canCreateProject && <ProjectForm open={createOpen} onCancel={() => setCreateOpen(false)} onSubmit={submit} loading={createLoading} gitServiceAccount={gitServiceAccount} clusters={clusters} />}
  </div>
}

function SummaryCard({ label, value, icon, tone }) { return <Card bordered={false} className={`summary-card tone-${tone}`}><div className="summary-icon">{icon}</div><Statistic title={label} value={value} /></Card> }

function ProjectListRow({ project, targets, clusters, onClick }) {
  const healthy = project.health === 'healthy' || project.pod_count > 0
  const targetCount = Array.isArray(targets) ? targets.length : project.deployment_target_count || 0
  const healthyPods = project.healthy_pod_count ?? 0
  const podCount = project.pod_count ?? 0
  const healthState = project.health === 'degraded' ? ['degraded', '部分异常'] : healthy ? ['healthy', '运行正常'] : ['muted', '尚未发布']
  const targetNames = Array.isArray(targets) && targets.length
    ? targets.map((target) => target.environment || target.name).filter(Boolean)
    : []
  const targetSummary = targetNames.length > 3 ? `${targetNames.slice(0, 3).join('、')} 等` : targetNames.join('、') || '暂无环境'
  return <button className="project-list-row" type="button" role="listitem" onClick={onClick}>
    <span className="project-list-icon"><CodeOutlined /></span>
    <span className="project-list-main">
      <span className="project-list-name"><strong title={project.name}>{project.name}</strong><Tag color={healthState[0] === 'degraded' ? 'orange' : healthState[0] === 'healthy' ? 'green' : 'default'}><span className={`health-dot ${healthState[0]}`} />{healthState[1]}</Tag></span>
      <span className="project-list-repo" title={project.repository_url}><CodeOutlined /> {project.repository_url}</span>
      {project.description && <span className="project-list-description" title={project.description}>{project.description}</span>}
    </span>
    <span className="project-list-stat"><small>环境</small><strong>{targetCount}</strong><span title={targetSummary}>{targetSummary}</span></span>
    <span className="project-list-stat"><small>健康 Pod</small><strong>{healthyPods} / {podCount}</strong><span>{podCount ? '运行中' : '未发布'}</span></span>
    <span className="project-list-stat project-list-release"><small>最近发布</small><strong>{project.last_release || '暂无'}</strong><span>{project.last_commit ? `commit ${project.last_commit}` : '等待首次发布'}</span></span>
    <ArrowRightOutlined className="project-list-arrow" />
  </button>
}

function ProjectDetail({ project: initialProject, permissions = {}, onBack, onOpenCluster }) {
  const [project, setProject] = useState(initialProject)
  const canUpdateProject = permissions.canUpdateProject !== false
  const canCreateRelease = permissions.canCreateRelease !== false
  const canUpdateRelease = permissions.canUpdateRelease !== false
  const canPublishRelease = permissions.canPublishRelease !== false
  const canRuntimeConfig = permissions.canRuntimeConfig !== false
  const initialBranch = project.default_branch || 'main'
  const [branch, setBranch] = useState(initialBranch)
  const [baseBranch, setBaseBranch] = useState(initialBranch)
  const [branches, setBranches] = useState([])
  const [branchLoading, setBranchLoading] = useState(false)
  const [tab, setTab] = useState('release')
  const [commits, setCommits] = useState([])
  const [tags, setTags] = useState([])
  const [selected, setSelected] = useState([])
  const [releases, setReleases] = useState([])
  const [batches, setBatches] = useState([])
  const [batchLoading, setBatchLoading] = useState(false)
  const [mergingRelease, setMergingRelease] = useState('')
  const [closingBatch, setClosingBatch] = useState('')
  const [pods, setPods] = useState([])
  const [runtimeError, setRuntimeError] = useState('')
  const [targets, setTargets] = useState([])
  const [targetLoading, setTargetLoading] = useState(true)
  const [selectedTargetId, setSelectedTargetId] = useState('')
  const [releaseTargetIds, setReleaseTargetIds] = useState([])
  const [loading, setLoading] = useState(true)
  const [pod, setPod] = useState(null)
  const [releaseOpen, setReleaseOpen] = useState(false)
  const [strategy, setStrategy] = useState('rolling')
  const [candidate, setCandidate] = useState(10)
  const trafficCandidate = strategy === 'rolling' ? 0 : candidate
  const [releaseSaving, setReleaseSaving] = useState(false)
  const [releaseTarget, setReleaseTarget] = useState(null)
  const [removingCommit, setRemovingCommit] = useState('')
  const [loadError, setLoadError] = useState('')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [clusters, setClusters] = useState([])
  const [cancellingRelease, setCancellingRelease] = useState('')
  const [retryingTarget, setRetryingTarget] = useState('')
  const [publishingTarget, setPublishingTarget] = useState('')
  const [preparation, setPreparation] = useState(null)
  const [preparationStatus, setPreparationStatus] = useState('')
  const [preparationLoading, setPreparationLoading] = useState(false)
  const [gitServiceAccount, setGitServiceAccount] = useState(null)
  const [gitAccess, setGitAccess] = useState(null)
  const [gitAccessLoading, setGitAccessLoading] = useState(true)
  const [gitAccessError, setGitAccessError] = useState('')
  const gitAccessRequestRef = useRef(0)
  const preparationRequestRef = useRef(0)

  const availableTargets = targets
  const selectedTarget = availableTargets.find((target) => target.id === selectedTargetId) || firstEnabledTarget(availableTargets)

  const loadGitAccess = async () => {
    const requestID = ++gitAccessRequestRef.current
    setGitAccessLoading(true)
    setGitAccessError('')
    setGitServiceAccount(null)
    setGitAccess(null)
    const [accountResult, accessResult] = await Promise.allSettled([
      getGitServiceAccount(),
      getGitAccess(project.id),
    ])
    if (requestID !== gitAccessRequestRef.current) return
    if (accountResult.status === 'fulfilled') setGitServiceAccount(accountResult.value)
    const failures = [accountResult, accessResult]
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
    const [podResult] = await Promise.allSettled([getPods(project.id, targetId)])
    if (podResult.status === 'fulfilled') {
      setPods(podResult.value || [])
      setRuntimeError('')
    } else {
      setPods([])
      setRuntimeError(podResult.reason?.message || 'Pod 加载失败')
    }
    return podResult
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
    setLoadError('')
    try {
      const results = await Promise.allSettled([
        getBranches(project.id),
        getCommits(project.id, branchName),
        getTags(project.id),
        getReleases(project.id),
        getReleaseBatches(project.id),
        getDeploymentTargets(project.id),
      ])
      const failures = []
      const [branchResult, commitResult, tagResult, releaseResult, batchResult, targetResult] = results
      if (branchResult.status === 'fulfilled') setBranches(branchResult.value || [])
      else failures.push(`分支：${branchResult.reason?.message || '加载失败'}`)
      if (commitResult.status === 'fulfilled') setCommits(commitResult.value || [])
      else failures.push(`提交：${commitResult.reason?.message || '加载失败'}`)
      if (tagResult.status === 'fulfilled') setTags(tagResult.value || [])
      else failures.push(`Tag：${tagResult.reason?.message || '加载失败'}`)
      if (releaseResult.status === 'fulfilled') setReleases(normalizeReleases(releaseResult.value || []))
      else failures.push(`发布记录：${releaseResult.reason?.message || '加载失败'}`)
      if (batchResult.status === 'fulfilled') setBatches((batchResult.value || []).map(normalizeBatch))
      else failures.push(`发布批次：${batchResult.reason?.message || '加载失败'}`)

      let nextTargetId = selectedTargetId || ''
      if (targetResult.status === 'fulfilled') {
        const nextTargets = targetResult.value || []
        setTargets(nextTargets)
        const targetExists = nextTargets.some((target) => target.id === nextTargetId && target.enabled)
        nextTargetId = targetExists ? nextTargetId : firstEnabledTarget(nextTargets)?.id || ''
        if (nextTargetId !== selectedTargetId) setSelectedTargetId(nextTargetId)
      } else {
        failures.push(`发布环境：${targetResult.reason?.message || '加载失败'}`)
      }
      setTargetLoading(false)
      const podResult = await loadRuntime(nextTargetId)
      if (podResult.status !== 'fulfilled') failures.push(`Pod：${podResult.reason?.message || '加载失败'}`)
      if (failures.length) {
        const detail = failures.join('；')
        setLoadError(detail)
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
        : firstEnabledTarget(nextTargets)?.id || ''
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
    setLoading(true)
    try {
      const podResult = await loadRuntime(nextTargetId)
      if (podResult.status !== 'fulfilled') {
        message.warning('环境已切换，但运行态数据暂时无法加载')
      }
    } finally {
      setLoading(false)
    }
  }

  const createTarget = async (payload) => {
    await createDeploymentTarget(project.id, payload)
    const result = await reloadTargets(selectedTargetId)
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
    }
  }

  const canPublish = canPublishRelease && !gitAccessLoading && !gitAccessError && gitAccess?.usable === true
  const canMergeMain = canPublishRelease && !gitAccessLoading && !gitAccessError && gitAccess?.can_merge === true

  const hasActiveReleases = releases.some((release) => ['queued', 'running'].includes(release.status))
  useEffect(() => {
    if (!hasActiveReleases) return undefined
    let active = true
    const poll = async () => {
      try {
        const [releaseResult, batchResult] = await Promise.allSettled([
          getReleases(project.id),
          getReleaseBatches(project.id),
        ])
        if (!active) return
        if (releaseResult.status === 'fulfilled') setReleases(normalizeReleases(releaseResult.value || []))
        if (batchResult.status === 'fulfilled') setBatches((batchResult.value || []).map(normalizeBatch))
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
    setBaseBranch(project.default_branch || 'main')
    setSelected([])
    setPreparation(null)
    setPreparationStatus('')
    setLoadError('')
    setBranchLoading(true)
    try {
      setCommits((await getCommits(project.id, nextBranch)) || [])
    } catch (branchError) {
      setLoadError(branchError.message || '加载提交失败')
      message.error(branchError.message || '加载提交失败')
    } finally { setBranchLoading(false) }
  }

  const selectedSHAs = selected.map((item) => item.sha).filter(Boolean)
  const upsertRelease = (value) => {
    const next = normalizeRelease(value)
    setReleases((old) => [next, ...old.filter((item) => item.id !== next.id)])
    return next
  }
  const openNewRelease = () => {
    if (!canCreateRelease) {
      message.warning('当前角色没有创建发布的权限')
      return
    }
    setReleaseTarget(null)
    setReleaseTargetIds(orderedReleaseTargets(availableTargets).map((target) => target.id).filter(Boolean))
    setBaseBranch(project.default_branch || 'main')
    setPreparation(null)
    setPreparationStatus('')
    const firstReleaseTarget = firstEnabledTarget(availableTargets)
    setStrategy(firstReleaseTarget?.deploy_strategy || project.deploy_strategy || 'rolling')
    setReleaseOpen(true)
  }
  const closeReleaseModal = () => {
    preparationRequestRef.current += 1
    setReleaseOpen(false)
    setReleaseTarget(null)
    setPreparationLoading(false)
  }
  const runPreparation = async (
    nextBaseBranch = baseBranch,
    { sourceBranch: sourceOverride, selectedSHA: shaOverride, silent = false } = {},
  ) => {
    if (!canCreateRelease) {
      if (!silent) message.warning('当前角色没有创建发布的权限')
      return null
    }
    const sourceBranch = sourceOverride || releaseTarget?.branch || branch
    const releaseCommits = releaseTarget?.commits?.length ? releaseTarget.commits : selected
    const sha = shaOverride || releaseCommits[0]?.sha || ''
    if (!sourceBranch || !nextBaseBranch || !sha) {
      if (!silent) message.warning('先选择至少一个 commit，再检查代码分支')
      return null
    }
    const requestID = ++preparationRequestRef.current
    setPreparationLoading(true)
    setPreparationStatus('loading')
    try {
      const result = normalizePreparation(await prepareRelease(project.id, { source_branch: sourceBranch, base_branch: nextBaseBranch, selected_sha: sha }))
      if (requestID !== preparationRequestRef.current) return null
      setPreparation(result)
      setPreparationStatus(result.status)
      return result
    } catch (error) {
      if (requestID !== preparationRequestRef.current) return null
      setPreparation(null)
      setPreparationStatus('unsupported')
      if (!silent) message.error(error.message || '发布前代码检查失败')
      return null
    } finally {
      if (requestID === preparationRequestRef.current) setPreparationLoading(false)
    }
  }
  useEffect(() => {
    if (!canCreateRelease) return undefined
    const sourceBranch = releaseTarget?.branch || branch
    const releaseCommits = releaseTarget?.commits?.length ? releaseTarget.commits : selected
    const selectedSHA = releaseCommits[0]?.sha || ''
    if (!sourceBranch || !baseBranch || !selectedSHA) return undefined
    const timer = window.setTimeout(() => {
      runPreparation(baseBranch, { sourceBranch, selectedSHA, silent: true })
    }, 180)
    return () => window.clearTimeout(timer)
  }, [releaseOpen, releaseTarget, selected, branch, baseBranch, canCreateRelease])
  const handlePreparationResolve = async (action) => {
    if (action?.action === 'open') {
      const hasConflicts = Array.isArray(preparation?.conflicts) && preparation.conflicts.length > 0
      if (hasConflicts) {
        setPreparationStatus('conflict')
        return
      }
      const sourceBranch = releaseTarget?.branch || branch
      const selectedSHA = (releaseTarget?.commits || selected)[0]?.sha || ''
      if (!sourceBranch || !baseBranch || !selectedSHA) {
        message.warning('先选择至少一个 commit，再处理代码合并')
        return
      }
      setPreparationLoading(true)
      setPreparationStatus('loading')
      try {
        const result = normalizePreparation(await prepareRelease(project.id, {
          action: 'merge',
          source_branch: sourceBranch,
          base_branch: baseBranch,
          selected_sha: selectedSHA,
          temporary_branch: preparation?.temp_branch || action.tempBranch,
        }))
        setPreparation(result)
        setPreparationStatus(result.status)
        if (result.status === 'unsupported' || (result.status === 'needs_merge' && !result.conflicts?.length)) {
          message.info('当前连接暂不支持在线合并，请先在 Git 仓库完成合并后重新检查')
        }
      } catch (error) {
        setPreparationStatus('unsupported')
        message.error(error.message || '打开代码合并页面失败')
      } finally {
        setPreparationLoading(false)
      }
      return
    }
    if (action?.action === 'continue') {
      const sourceBranch = releaseTarget?.branch || branch
      const selectedSHA = (releaseTarget?.commits || selected)[0]?.sha || ''
      if (!preparation?.temp_branch || !sourceBranch || !baseBranch || !selectedSHA) {
        message.warning('合并信息已失效，请重新执行检查')
        return
      }
      setPreparationLoading(true)
      setPreparationStatus('loading')
      try {
        const result = normalizePreparation(await prepareRelease(project.id, {
          action: 'resolve',
          source_branch: sourceBranch,
          base_branch: baseBranch,
          selected_sha: selectedSHA,
          temporary_branch: preparation.temp_branch,
          resolutions: action.files || [],
        }))
        setPreparation(result)
        setPreparationStatus(result.status)
        if (result.status === 'ready') message.success('冲突已解决，临时发布版本已准备好')
      } catch (error) {
        setPreparationStatus('conflict')
        message.error(error.message || '提交合并结果失败')
      } finally {
        setPreparationLoading(false)
      }
    }
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
    if (!releaseTarget && !selectedSHAs.length) { message.warning('先选择至少一个 commit'); return }
    if (!releaseTarget && !releaseTargetIds.length) { message.warning('至少选择一个发布环境'); return }
    if (publish && !canPublish) {
      message.warning(gitAccess?.message || '请先完成平台 Git 服务账号授权')
      return
    }
    const releaseCommits = releaseTarget?.commits?.length ? releaseTarget.commits : selected
    const sourceBranch = releaseTarget?.branch || branch
    const selectedSHA = releaseCommits[0]?.sha || ''
    let currentPreparation = preparation
    if (publish) {
      const preparationMatches = currentPreparation
        && currentPreparation.source_branch === sourceBranch
        && currentPreparation.base_branch === baseBranch
        && String(currentPreparation.selected_sha || '').toLowerCase() === String(selectedSHA).toLowerCase()
      if (!preparationMatches || currentPreparation.can_publish !== true) {
        currentPreparation = await runPreparation(baseBranch, { sourceBranch, selectedSHA })
        if (!currentPreparation || currentPreparation.can_publish !== true) {
          message.warning('代码检查未通过，暂时不能发布')
          return
        }
      }
    }
    setReleaseSaving(true)
    let created = null
    try {
      let release
      if (releaseTarget) {
        release = await publishRelease(project.id, releaseTarget.id)
      } else {
        const preparedBranch = currentPreparation?.release_branch || sourceBranch
        const usingPreparedVersion = Boolean(currentPreparation?.temp_branch && preparedBranch === currentPreparation.temp_branch && currentPreparation?.release_sha)
        const releaseBranch = usingPreparedVersion ? preparedBranch : sourceBranch
        const releaseSHAs = usingPreparedVersion ? [currentPreparation.release_sha] : releaseCommits.map((item) => item.sha).filter(Boolean)
        const traffic = strategy === 'rolling'
          ? { stable_percent: 100, candidate_percent: 0, blue_percent: 0, green_percent: 0 }
          : strategy === 'canary'
            ? { stable_percent: 100 - candidate, candidate_percent: candidate, blue_percent: 0, green_percent: 0 }
            : { stable_percent: 0, candidate_percent: 0, blue_percent: 100 - candidate, green_percent: candidate }
        created = await createRelease(project.id, {
          branch: releaseBranch,
          commit_shas: releaseSHAs,
          source_branch: sourceBranch,
          base_branch: baseBranch,
          temporary_branch: usingPreparedVersion ? currentPreparation.temp_branch : '',
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
  const choose = (checked, item) => setSelected((old) => {
    if (checked) return old.some((entry) => entry.sha === item.sha) ? old : [...old, item]
    return old.filter((entry) => entry.sha !== item.sha)
  })
  const chooseMany = (checked, items) => setSelected((old) => {
    const visibleSHAs = new Set(items.map((item) => item.sha))
    if (checked) {
      return [...old, ...items.filter((item) => !old.some((entry) => entry.sha === item.sha))]
    }
    return old.filter((item) => !visibleSHAs.has(item.sha))
  })
  const republish = (item) => {
    const items = item.commits?.length ? item.commits : commits.filter((commit) => item.short && (commit.short_sha === item.short || commit.sha?.startsWith(item.short)))
    setReleaseTarget(item)
    setReleaseTargetIds(orderedReleaseTargets(item.targets?.length ? item.targets : availableTargets).map((target) => target.id).filter(Boolean))
    setSelected(items)
    setBaseBranch(item.base_branch || project.default_branch || 'main')
    setPreparation(null)
    setPreparationStatus('')
    setStrategy(item.strategy || item.plan?.strategy || 'rolling')
    const nextCandidate = item.strategy === 'blue_green' || item.plan?.strategy === 'blue_green'
      ? item.plan?.traffic?.green_percent
      : item.plan?.traffic?.candidate_percent
    if (Number.isFinite(nextCandidate) && nextCandidate > 0) setCandidate(nextCandidate)
    setReleaseOpen(true)
  }
  const removeDraftCommit = async (item, commit) => {
    if (!canUpdateRelease) {
      message.warning('当前角色没有修改发布草稿的权限')
      return
    }
    const key = `${item.id}:${commit.sha}`
    setRemovingCommit(key)
    try {
      const updated = upsertRelease(await removeCommit(project.id, item.id, commit.sha))
      setReleaseTarget((old) => old?.id === updated.id ? updated : old)
      message.success('已从草稿移除这个 commit')
    } catch (error) { message.error(error.message || '移除失败') }
    finally { setRemovingCommit('') }
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
  const publishTarget = async (item, target) => {
    if (!canPublishRelease) {
      message.warning('当前角色没有执行发布的权限')
      return
    }
    if (!item?.id || !target?.id) return
    const key = `${item.id}:${target.id}`
    setPublishingTarget(key)
    try {
      const updated = upsertRelease(await publishReleaseTarget(project.id, item.id, target.id))
      message.success(`已开始发布到 ${target.name || target.environment || '下一个环境'}`)
      return updated
    } catch (error) {
      message.error(error.message || '推进环境发布失败')
    } finally {
      setPublishingTarget('')
    }
  }
  const refreshBatches = async () => {
    setBatchLoading(true)
    try {
      const next = await getReleaseBatches(project.id)
      setBatches((next || []).map(normalizeBatch))
      return next || []
    } catch (error) {
      message.error(error.message || '发布批次加载失败')
      return []
    } finally {
      setBatchLoading(false)
    }
  }
  const mergeMain = async (item) => {
    const releaseID = item?.id
    if (!releaseID || mergingRelease) return
    setMergingRelease(releaseID)
    try {
      const result = await mergeReleaseToMain(project.id, releaseID)
      if (result?.release) upsertRelease(result.release)
      if (result?.batch?.id) {
        const nextBatch = normalizeBatch(result.batch)
        setBatches((old) => [nextBatch, ...old.filter((batch) => batch.id !== nextBatch.id)])
      } else {
        await refreshBatches()
      }
      message.success('当前发布项已单独合入 main，批次中的其他发布项保持不变')
    } catch (error) {
      message.error(error.message || '合入 main 失败')
      if (error.status === 409 || error.code === 'conflict') {
        await Promise.all([load(branch), refreshBatches()])
      }
    } finally {
      setMergingRelease('')
    }
  }
  const closeBatch = async (batch) => {
    const batchID = batch?.id
    if (!batchID || closingBatch) return
    setClosingBatch(batchID)
    try {
      const updated = normalizeBatch(await closeReleaseBatch(project.id, batchID))
      setBatches((old) => [updated, ...old.filter((item) => item.id !== updated.id)])
      message.success(`批次 ${batchID} 已关闭，后续发布会创建新的批次`)
    } catch (error) {
      message.error(error.message || '关闭批次失败')
      if (error.status === 409 || error.code === 'conflict') await refreshBatches()
    } finally {
      setClosingBatch('')
    }
  }
  const openSettings = () => setSettingsOpen(true)
  const saveSettings = async (values) => {
    setSettingsLoading(true)
    try {
      const updated = await updateProject(project.id, values)
      const nextBranch = updated.default_branch || branch
      setProject(updated)
      setBranch(nextBranch)
      setBaseBranch(updated.default_branch || nextBranch)
      setSelected([])
      setPreparation(null)
      setPreparationStatus('')
      setSettingsOpen(false)
      await Promise.all([load(nextBranch), loadGitAccess()])
      message.success('项目基本设置已保存，下次发布会使用新的代码配置')
    } catch (error) {
      message.error(error.message || '项目设置保存失败')
    } finally { setSettingsLoading(false) }
  }
  const releaseRouteTargets = orderedReleaseTargets(releaseTarget?.targets?.length ? releaseTarget.targets : availableTargets.filter((target) => releaseTargetIds.includes(target.id)))
  const releaseBatchID = releaseTarget?.batch_id || releaseTarget?.batchId
  const releaseBatch = batches.find((item) => releaseBatchID && item.id === releaseBatchID)
    || batches.find((item) => String(item.status || '').toLowerCase() === 'open')
  return <div className="page-wrap detail-page">
    <button type="button" className="back-link" onClick={onBack}>← 项目列表</button>
    <div className="detail-heading"><div className="detail-project-title"><div className="project-icon large"><CodeOutlined /></div><div className="detail-project-copy"><div className="detail-project-name-row"><Typography.Title level={2}>{project.name}</Typography.Title><Tag className="detail-branch-tag">分支 {branch}</Tag></div><Typography.Text type="secondary" className="detail-repository" title={project.repository_url}><CodeOutlined /> {project.repository_url}</Typography.Text></div></div><Space wrap><Tag color="green">{selectedTarget?.name || '未选择环境'}</Tag>{canUpdateProject && <Button icon={<SettingOutlined />} onClick={openSettings}>设置</Button>}</Space></div>
    <div className="detail-tabs"><button className={tab === 'release' ? 'active' : ''} onClick={() => setTab('release')} type="button" aria-label="发布"><CloudUploadOutlined /> 发布</button><button className={tab === 'targets' ? 'active' : ''} onClick={() => setTab('targets')} type="button" aria-label="发布环境" title="发布环境"><EnvironmentOutlined /> 环境 <Badge count={availableTargets.length} size="small" /></button><button className={tab === 'config' ? 'active' : ''} onClick={() => setTab('config')} type="button" aria-label="部署配置" title="部署配置"><FileTextOutlined /> 配置</button><button className={tab === 'runtime' ? 'active' : ''} onClick={() => setTab('runtime')} type="button" aria-label="Pod 运行态" title="Pod 运行态"><DeploymentUnitOutlined /> Pod <Badge count={pods.length} size="small" /></button><button className={tab === 'monitor' ? 'active' : ''} onClick={() => setTab('monitor')} type="button" aria-label="监控"><DashboardOutlined /> 监控</button></div>
    {tab === 'targets' && <DeploymentTargets project={project} targets={targets} clusters={clusters} loading={targetLoading} canEdit={canUpdateProject} onReload={() => reloadTargets(selectedTargetId)} onCreate={createTarget} onUpdate={updateTarget} onDelete={deleteTarget} />}
    {tab === 'config' && <DeploymentConfigEditor project={project} readOnly={!canUpdateProject} />}
    {tab === 'release' && <ReleaseTab branch={branch} branches={branches} branchLoading={branchLoading} onBranchChange={changeBranch} commits={commits} tags={tags} selected={selected} choose={choose} chooseMany={chooseMany} releases={releases} batches={batches} batchLoading={batchLoading} loading={loading} loadError={loadError} onRefresh={() => load(branch)} onRefreshBatches={refreshBatches} onMergeMain={mergeMain} mergingRelease={mergingRelease} onCloseBatch={closeBatch} closingBatch={closingBatch} canMergeMain={canMergeMain} onPublish={openNewRelease} onRepublish={republish} onRemoveCommit={removeDraftCommit} removingCommit={removingCommit} onCancelRelease={stopRelease} cancellingRelease={cancellingRelease} onRetryTarget={retryTarget} retryingTarget={retryingTarget} onPublishTarget={publishTarget} publishingTarget={publishingTarget} canCreateRelease={canCreateRelease} canUpdateRelease={canUpdateRelease} canPublishRelease={canPublishRelease} />}
    {tab === 'runtime' && <RuntimeTab project={project} targets={availableTargets} selectedTargetId={selectedTarget?.id} onTargetChange={changeTarget} pods={pods} loading={loading} onRefresh={() => loadRuntime(selectedTarget?.id)} onOpenPod={setPod} />}
    {tab === 'monitor' && <MonitorTab pods={pods} project={project} targets={availableTargets} selectedTargetId={selectedTarget?.id} onTargetChange={changeTarget} onRefresh={() => loadRuntime(selectedTarget?.id)} onOpenPod={setPod} onOpenCluster={onOpenCluster} runtimeError={runtimeError} />}
    <ReleaseConfirmModal
      open={releaseOpen}
      projectName={project.name}
      branch={releaseTarget?.branch || branch}
      commits={releaseTarget?.commits?.length ? releaseTarget.commits : selected}
      releaseRouteTargets={releaseRouteTargets}
      batch={releaseBatch}
      baseBranch={baseBranch}
      branches={branches}
      preparation={preparation}
      preparationStatus={preparationStatus}
      preparationLoading={preparationLoading}
      onPrepare={(value) => runPreparation(value)}
      onResolve={handlePreparationResolve}
      strategy={strategy}
      onStrategyChange={setStrategy}
      trafficCandidate={trafficCandidate}
      onCandidateChange={setCandidate}
      onCancel={closeReleaseModal}
      onSaveDraft={() => doRelease(false)}
      onPublish={() => doRelease(true)}
      releaseSaving={releaseSaving}
      canPublish={canPublish}
      canCreateDraft={canCreateRelease}
      isExistingRelease={Boolean(releaseTarget)}
    />
    <ProjectSettings project={project} open={settingsOpen} loading={settingsLoading} onCancel={() => setSettingsOpen(false)} onSubmit={saveSettings} onGoTargets={() => { setSettingsOpen(false); setTab('targets') }} />
    <PodDrawer project={project} pod={pod} target={selectedTarget} targetId={selectedTarget?.id} open={Boolean(pod)} onClose={() => setPod(null)} canEdit={canRuntimeConfig} />
  </div>
}

function ReleaseTab({ branch, branches, branchLoading, onBranchChange, commits, tags, selected, choose, chooseMany, releases, batches, batchLoading, loading, loadError, onRefresh, onRefreshBatches, onMergeMain, mergingRelease, onCloseBatch, closingBatch, canMergeMain = false, onPublish, onRepublish, onRemoveCommit, removingCommit, onCancelRelease, cancellingRelease, onRetryTarget, retryingTarget, onPublishTarget, publishingTarget, canCreateRelease = true, canUpdateRelease = true, canPublishRelease = true }) {
  const [historyOpen, setHistoryOpen] = useState(false)
  const [batchOpen, setBatchOpen] = useState(false)
  const publishFromBatch = (target, context = {}) => {
    const release = context.release || context.item
    if (context.action === 'retry') return onRetryTarget?.(release, target)
    return onPublishTarget?.(release, target, context)
  }
  const openRelease = (item) => {
    setHistoryOpen(false)
    onRepublish(item)
  }

  return (
    <div className="release-page">
      <section className="release-main">
        <div className="panel-heading release-panel-heading">
          <Typography.Title level={4}>选择代码版本</Typography.Title>
          <Space wrap className="release-toolbar-actions">
            <Button icon={<HistoryOutlined />} onClick={() => setHistoryOpen(true)}>
              发布记录
            </Button>
            <Button icon={<BranchesOutlined />} onClick={() => setBatchOpen(true)}>
              发布批次
            </Button>
            <Button icon={<ReloadOutlined />} onClick={onRefresh}>刷新提交</Button>
          </Space>
        </div>

        {loadError && <div className="commit-load-error">{loadError}</div>}
        {!canCreateRelease && <div className="release-readonly-notice">当前角色只能查看，不能发起发布。</div>}
        <CommitSelector
          commits={commits}
          tags={tags}
          branch={branch}
          branches={branches}
          selected={selected}
          onChoose={choose}
          onChooseMany={chooseMany}
          loading={loading}
          branchLoading={branchLoading}
          onBranchChange={onBranchChange}
          readOnly={!canCreateRelease}
        />
        <div className="publish-bar">
          <span>已选择 <strong>{selected.length}</strong> 个 commit</span>
          {canCreateRelease
            ? <Space>
              <Button disabled={!selected.length} onClick={onPublish}>保存草稿</Button>
              <Button type="primary" icon={<CloudUploadOutlined />} disabled={!selected.length} onClick={onPublish}>发布选中版本</Button>
            </Space>
            : <Typography.Text type="secondary">只读模式</Typography.Text>}
        </div>
      </section>

      <Modal className="release-history-modal" title="发布记录" open={historyOpen} onCancel={() => setHistoryOpen(false)} footer={null} width={760} destroyOnClose>
        <div className="release-modal-toolbar"><Button icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>刷新</Button></div>
        <div className="release-list">
          {releases.length ? releases.map((item) => <ReleaseItem
            key={item.id}
            release={item}
            onRepublish={() => openRelease(item)}
            onRemoveCommit={(commit) => onRemoveCommit(item, commit)}
            removingCommit={removingCommit}
            onCancelRelease={() => onCancelRelease(item)}
            cancellingRelease={cancellingRelease}
            onRetryTarget={onRetryTarget}
            retryingTarget={retryingTarget}
            onPublishTarget={onPublishTarget}
            publishingTarget={publishingTarget}
            canCreateRelease={canCreateRelease}
            canUpdateRelease={canUpdateRelease}
            canPublishRelease={canPublishRelease}
          />) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有发布记录" />}
        </div>
      </Modal>

      <Modal className="release-batch-modal" title="发布批次" open={batchOpen} onCancel={() => setBatchOpen(false)} footer={null} width="min(1160px, calc(100vw - 32px))" destroyOnClose>
        <ReleaseBatchPanel
          batches={batches}
          releases={releases}
          loading={batchLoading}
          canPublish={canMergeMain}
          canPublishTarget={canPublishRelease}
          onRefresh={onRefreshBatches}
          onMergeMain={onMergeMain}
          mergingRelease={mergingRelease}
          onPublishTarget={publishFromBatch}
          publishingTarget={publishingTarget}
          onCloseBatch={onCloseBatch}
          closingBatch={closingBatch}
        />
      </Modal>
    </div>
  )
}

function ReleaseItem({ release, onRepublish, onRemoveCommit, removingCommit, onCancelRelease, cancellingRelease, onRetryTarget, retryingTarget, onPublishTarget, publishingTarget, canCreateRelease = true, canUpdateRelease = true, canPublishRelease = true }) {
  const statusMap = { succeeded: ['success', '发布成功'], running: ['processing', '发布中'], queued: ['warning', '排队中'], draft: ['default', '草稿'], failed: ['error', '发布失败'], cancelled: ['default', '已取消'], unknown: ['default', '未知状态'] }
  const [color, label] = statusMap[release.status] || statusMap.draft
  const canRepublish = canCreateRelease && canPublishRelease && ['draft', 'failed', 'succeeded', 'cancelled'].includes(release.status)
  const canCancel = canPublishRelease && ['queued', 'running'].includes(release.status)
  const targetActionsEnabled = canCreateRelease && canPublishRelease && ['running', 'failed'].includes(release.status)
  return <div className="release-item">
    <div className="release-item-top"><Tag color={color}>{label}</Tag><Typography.Text type="secondary">{release.created || formatDate(release.created_at)}</Typography.Text></div>
    <div className="release-commit"><Typography.Text code>{release.commits?.[0]?.short_sha || release.short || '-'}</Typography.Text><Typography.Text ellipsis>{release.commits?.[0]?.message || release.message || '版本发布'}</Typography.Text></div>
    <ReleaseProgress release={release} />
    <ReleaseTargetSummary targets={release.targets} release={release} canRetry={release.status === 'failed' && canPublishRelease} onRetryTarget={(target) => onRetryTarget?.(release, target)} retryingTarget={retryingTarget} onPublishTarget={(target, context) => onPublishTarget?.(release, target, context)} publishingTarget={publishingTarget} canPublishTarget={targetActionsEnabled} />
    {release.status === 'draft' && canUpdateRelease && release.commits?.length > 0 && <div className="draft-commits">{release.commits.map((commit) => <div key={commit.sha} className="draft-commit"><Typography.Text code>{commit.short_sha || commit.sha.slice(0, 7)}</Typography.Text><Button type="text" danger size="small" loading={removingCommit === `${release.id}:${commit.sha}`} onClick={() => onRemoveCommit(commit)}>移除</Button></div>)}</div>}
    {release.status === 'draft' && !canUpdateRelease && <Typography.Text type="secondary" className="release-readonly-copy">草稿内容仅可查看</Typography.Text>}
    <div className="release-item-bottom"><span>{strategyLabel(release.strategy || release.plan?.strategy)} · {release.commits?.length ?? 0} 个 commit{release.targets?.length ? ` · ${release.targets.length} 个环境` : ''}</span><Space size={4}>{canCancel && <Button type="link" danger size="small" loading={cancellingRelease === release.id} onClick={onCancelRelease}>取消发布</Button>}{canRepublish && <Button type="link" size="small" icon={<ReloadOutlined />} onClick={onRepublish}>{release.status === 'draft' ? '继续发布' : '再次发布'}</Button>}</Space></div>
  </div>
}

function RuntimeTab({ project, targets = [], selectedTargetId, onTargetChange, pods, loading, onRefresh, onOpenPod }) {
  const target = targets.find((item) => item.id === selectedTargetId) || firstEnabledTarget(targets)
  const columns = [
    { title: 'Pod', dataIndex: 'name', render: (value, item) => <Button type="link" className="pod-link" onClick={() => onOpenPod(item)}>{value}</Button> },
    { title: '状态', dataIndex: 'phase', render: (value, item) => <Tag color={item.ready ? 'green' : 'orange'}>{item.ready ? '运行中' : value || '处理中'}</Tag> },
    { title: 'Pod IP', dataIndex: 'pod_ip' },
    { title: '节点', dataIndex: 'node_name' },
    { title: '重启次数', dataIndex: 'restarts', render: (value, item) => value ?? item.restart_count ?? 0 },
    { title: '操作', width: 110, render: (_, item) => <Button type="link" onClick={() => onOpenPod(item)}>查看详情</Button> },
  ]
  return <section className="runtime-panel">
    <div className="panel-heading runtime-panel-heading">
      <div>
        <Typography.Title level={4}>Pod 运行态</Typography.Title>
        <Typography.Text type="secondary">{target?.cluster_id || project.cluster_id || '未配置集群'} / {target?.namespace || project.namespace || '未配置 namespace'}</Typography.Text>
      </div>
      <Space wrap className="runtime-panel-actions">
        {targets.length > 0 && <DeploymentTargetSelect className="runtime-target-select" targets={targets} value={target?.id} onChange={onTargetChange} disabled={loading} />}
        <Button icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>刷新</Button>
      </Space>
    </div>
    <Table rowKey="name" loading={loading} columns={columns} dataSource={pods} pagination={false} locale={{ emptyText: '当前环境没有 Pod' }} />
  </section>
}

function MonitorTab({ pods, project, targets = [], selectedTargetId, onTargetChange, onRefresh, onOpenPod, onOpenCluster, runtimeError }) {
  return <MonitorDashboard scope="project" pods={pods} project={project} targets={targets} selectedTargetId={selectedTargetId} onTargetChange={onTargetChange} onRefresh={onRefresh} onOpenPod={onOpenPod} onOpenCluster={onOpenCluster} error={runtimeError} />
}

function ActivityPage() {
  const [items, setItems] = useState([])
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

  return <div className="page-wrap">
    <div className="page-heading">
      <div>
        <Typography.Text className="page-kicker">工作空间 · 审计</Typography.Text>
        <Typography.Title level={2}>操作记录</Typography.Title>
        <Typography.Paragraph type="secondary">这里会记录项目创建、发布和运行态配置变更。</Typography.Paragraph>
      </div>
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新记录</Button>
    </div>
    {error && <div className="activity-error">{error}</div>}
    <Card bordered={false} className="activity-panel">
      {loading ? <div className="loading-placeholder">加载记录中...</div> : items.length ? <List
        dataSource={items}
        renderItem={(item) => <List.Item className="activity-item">
          <div className="activity-marker" />
          <div className="activity-content">
            <div className="activity-item-head"><Typography.Text strong>{item.action}</Typography.Text><Typography.Text type="secondary">{formatDate(item.created_at)}</Typography.Text></div>
            <div className="activity-target">{item.target || '系统'}</div>
            <Typography.Text type="secondary" className="activity-actor">操作人：{item.user_name || (item.user_id ? `用户 ${item.user_id}` : '系统')}</Typography.Text>
          </div>
        </List.Item>}
      /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无操作记录" />}
    </Card>
  </div>
}

function firstEnabledTarget(targets = []) {
  return [...(Array.isArray(targets) ? targets : [])]
    .sort((left, right) => Number(left?.sort_order || 1) - Number(right?.sort_order || 1) || String(left?.name || '').localeCompare(String(right?.name || ''), 'zh-CN'))
    .find((target) => target && target.enabled !== false)
}

function orderedReleaseTargets(targets = []) {
  return (Array.isArray(targets) ? targets : [])
    .filter((target) => target && target.enabled !== false)
    .sort((left, right) => Number(left?.sort_order || 1) - Number(right?.sort_order || 1) || String(left?.name || '').localeCompare(String(right?.name || ''), 'zh-CN'))
}

function normalizedTargetStatus(target) {
  const value = String(target?.status || target?.state || 'pending').trim().toLowerCase().replace(/[\s-]+/g, '_')
  return ({ success: 'succeeded', successful: 'succeeded', complete: 'succeeded', completed: 'succeeded', in_progress: 'running', processing: 'running', canceled: 'cancelled' })[value] || value
}

function nextReleaseTarget(release) {
  const targets = orderedReleaseTargets(release?.targets)
  const pendingIndex = targets.findIndex((target) => ['pending', 'waiting'].includes(normalizedTargetStatus(target)))
  if (pendingIndex < 0) return null
  if (targets.slice(0, pendingIndex).some((target) => normalizedTargetStatus(target) !== 'succeeded')) return null
  return targets[pendingIndex]
}

function normalizeReleases(items) { return items.map(normalizeRelease) }
function formatDate(value) { if (!value) return '-'; const date = new Date(value); if (Number.isNaN(date.getTime())) return value; return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function strategyLabel(value) { return ({ rolling: '滚动发布', canary: '灰度发布', blue_green: '蓝绿发布' })[value] || '滚动发布' }

export default App
