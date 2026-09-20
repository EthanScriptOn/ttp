import {
  Alert,
  Button,
  Input,
  Modal,
  Select,
  Segmented,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd'
import {
  CheckCircleOutlined,
  CopyOutlined,
  DeleteOutlined,
  FileAddOutlined,
  FileTextOutlined,
  FormatPainterOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { useEffect, useRef, useState } from 'react'
import {
  createDeploymentResource,
  deleteDeploymentResource,
  getDeploymentTargets,
  getDeploymentResources,
  updateDeploymentResource,
  validateDeploymentResource,
} from '../services/api'
import {
  detectManifestFormat,
  formatManifest,
  localValidateResourceFile,
} from '../services/deployment-config'

const RESOURCE_TEMPLATES = [
  { value: 'deployment', label: 'Deployment', path: 'deployment.yaml', content: (namespace) => `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: ${namespace}
spec:
  replicas: 2
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
        - name: app
          image: your-image:latest
          ports:
            - containerPort: 8080
` },
  { value: 'service', label: 'Service', path: 'service.yaml', content: (namespace) => `apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: ${namespace}
spec:
  selector:
    app: app
  ports:
    - port: 80
      targetPort: 8080
` },
  { value: 'configmap', label: 'ConfigMap', path: 'configmap.yaml', content: (namespace) => `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-settings
  namespace: ${namespace}
data: {}
` },
  { value: 'secret', label: 'Secret', path: 'secret.yaml', content: (namespace) => `apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
  namespace: ${namespace}
type: Opaque
stringData: {}
` },
  { value: 'pvc', label: 'PersistentVolumeClaim', path: 'pvc.yaml', content: (namespace) => `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: app-data
  namespace: ${namespace}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
` },
  { value: 'ingress', label: 'Ingress', path: 'ingress.yaml', content: (namespace) => `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: app
  namespace: ${namespace}
spec:
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: app
                port:
                  number: 80
` },
  { value: 'hpa', label: 'HorizontalPodAutoscaler', path: 'hpa.yaml', content: (namespace) => `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: app
  namespace: ${namespace}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: app
  minReplicas: 1
  maxReplicas: 5
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
` },
  { value: 'statefulset', label: 'StatefulSet', path: 'statefulset.yaml', content: (namespace) => `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: app
  namespace: ${namespace}
spec:
  serviceName: app
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
        - name: app
          image: your-image:latest
` },
  { value: 'job', label: 'Job', path: 'job.yaml', content: (namespace) => `apiVersion: batch/v1
kind: Job
metadata:
  name: app-job
  namespace: ${namespace}
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: app
          image: your-image:latest
` },
  { value: 'cronjob', label: 'CronJob', path: 'cronjob.yaml', content: (namespace) => `apiVersion: batch/v1
kind: CronJob
metadata:
  name: app-cron
  namespace: ${namespace}
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: app
              image: your-image:latest
` },
]

const RESOURCE_BOUNDARY_HELP = <div className="deployment-resource-boundary">
  <div><strong>TTP 自动管理：</strong>每个空间/环境在目标集群中的 namespace、镜像拉取 Secret（配置镜像仓库凭证时）、发布标签，以及发布时注入的镜像和发布环境变量。</div>
  <div><strong>资源文件负责：</strong>Deployment、StatefulSet、Service、ConfigMap、Secret、PVC、Ingress、HPA 等应用资源；副本数、容器端口、探针/保活、PDB 等工作负载规格也在这里维护。</div>
  <div><strong>namespace 规则：</strong>模板会展示当前 DEV 环境的 namespace，但这是只读平台字段；保存时 TTP 会覆盖手动输入，发布到其他环境时再改写为目标环境 namespace。Namespace、PV、ClusterRole 等集群级资源不属于项目资源文件。</div>
</div>

const emptyResource = (namespace = '') => ({
  id: '', name: 'resource.yaml', path: 'resource.yaml', format: 'yaml',
  content: `apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example${namespace ? `\n  namespace: ${namespace}` : ''}\ndata: {}\n`,
  api_version: '', kind: '', resource_name: '', namespace: '', sort_order: 0,
  version: 0, release_supported: false,
})

export default function DeploymentConfigEditor({ project, readOnly = false }) {
  const [files, setFiles] = useState([])
  const [current, setCurrent] = useState(null)
  const [saved, setSaved] = useState(null)
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [converting, setConverting] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [defaultNamespace, setDefaultNamespace] = useState('')
  const gutterRef = useRef(null)

  const dirty = JSON.stringify(current || {}) !== JSON.stringify(saved || {})
  const lineCount = Math.max(1, String(current?.content || '').split('\n').length)

  const load = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const [next, targets] = await Promise.all([
        getDeploymentResources(project.id),
        getDeploymentTargets(project.id),
      ])
      const defaultTarget = [...targets].sort((a, b) => Number(a?.sort_order || 1) - Number(b?.sort_order || 1))[0]
      setDefaultNamespace(defaultTarget?.namespace || '')
      setFiles(next)
      setCurrent(next[0] || null)
      setSaved(next[0] || null)
    } catch (error) {
      setLoadError(error?.message || '资源文件加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [project.id])

  const selectFile = (file) => {
    if (dirty && !window.confirm('当前资源文件有未保存修改，确定切换吗？')) return
    setCurrent(file)
    setSaved(file)
  }

  const update = (patch) => {
    if (readOnly || !current) return
    setCurrent((value) => ({ ...value, ...patch }))
  }

  const addFile = (templateValue = '') => {
    if (readOnly) return
    if (dirty && !window.confirm('当前资源文件有未保存修改，确定新建吗？')) return
    const template = RESOURCE_TEMPLATES.find((item) => item.value === templateValue)
    const next = template
      ? { ...emptyResource(defaultNamespace), path: template.path, name: template.path, content: template.content(defaultNamespace || 'target-namespace') }
      : emptyResource(defaultNamespace)
    next.sort_order = files.length + 1
    setCurrent(next)
    setSaved(null)
  }

  const check = async (showSuccess = true) => {
    if (!current) {
      message.warning('请先新建或选择一个资源文件')
      return false
    }
    // Resource files are shared templates. TTP rewrites metadata.namespace to
    // the selected environment namespace immediately before publishing.
    const local = localValidateResourceFile(current.content, defaultNamespace)
    if (!local.ok) {
      message.error(local.message)
      return false
    }
    setChecking(true)
    try {
      const result = await validateDeploymentResource(project.id, current)
      if (showSuccess) message.success(`检查通过：${result.kind}/${result.resource_name}`)
      return true
    } catch (error) {
      message.error(error?.message || '资源文件检查失败')
      return false
    } finally {
      setChecking(false)
    }
  }

  const save = async () => {
    if (readOnly || !current || !(await check(false))) return
    setSaving(true)
    try {
      const result = current.id
        ? await updateDeploymentResource(project.id, current.id, current)
        : await createDeploymentResource(project.id, current)
      const nextFiles = current.id
        ? files.map((file) => file.id === result.id ? result : file)
        : [...files, result].sort((a, b) => (a.sort_order - b.sort_order) || a.path.localeCompare(b.path))
      setFiles(nextFiles)
      setCurrent(result)
      setSaved(result)
      message.success('资源文件已保存，发布时会逐文件应用')
    } catch (error) {
      message.error(error?.message || '资源文件保存失败')
    } finally {
      setSaving(false)
    }
  }

  const deleteResource = async (target) => {
    setDeleting(true)
    try {
      await deleteDeploymentResource(project.id, target.id)
      const nextFiles = files.filter((file) => file.id !== target.id)
      setFiles(nextFiles)
      setCurrent(nextFiles[0] || null)
      setSaved(nextFiles[0] || null)
      message.success('资源文件已删除')
    } catch (error) {
      message.error(error?.message || '资源文件删除失败')
    } finally {
      setDeleting(false)
    }
  }

  const remove = () => {
    if (readOnly || !current?.id) return
    const target = current
    Modal.confirm({
      title: '删除资源文件',
      icon: <DeleteOutlined />,
      width: 440,
      centered: true,
      className: 'deployment-resource-delete-confirm',
      content: <div className="deployment-delete-confirm-content">
        <div className="deployment-delete-confirm-file">
          <span className="deployment-delete-confirm-file-icon"><FileTextOutlined /></span>
          <div>
            <strong>{target.path}</strong>
            <span>{target.kind || 'Kubernetes 资源'}{target.resource_name ? ` · ${target.resource_name}` : ''}</span>
          </div>
        </div>
        <p>删除后，这个资源文件将从项目配置中移除。已经完成的历史发布不会受到影响。</p>
      </div>,
      okText: '确认删除',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: () => deleteResource(target),
    })
  }

  const changeFormat = (nextFormat) => {
    if (readOnly || !current || nextFormat === current.format) return
    setConverting(true)
    try {
      update({ content: formatManifest(current.content, nextFormat), format: nextFormat })
    } catch (error) {
      message.error(error?.message || '当前资源无法转换格式')
    } finally {
      setConverting(false)
    }
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(current?.content || '')
      message.success('资源文件已复制')
    } catch {
      message.warning('浏览器不允许访问剪贴板，请手动复制')
    }
  }

  const syncScroll = (event) => {
    if (gutterRef.current) gutterRef.current.scrollTop = event.currentTarget.scrollTop
  }

  const handleKeyDown = (event) => {
    if (readOnly || event.key !== 'Tab') return
    event.preventDefault()
    const target = event.currentTarget
    const start = target.selectionStart
    const end = target.selectionEnd
    update({ content: `${current.content.slice(0, start)}  ${current.content.slice(end)}` })
    requestAnimationFrame(() => { target.selectionStart = start + 2; target.selectionEnd = start + 2 })
  }

  if (loading) return <div className="deployment-config-loading"><Spin /><Typography.Text type="secondary">加载 Kubernetes 资源文件...</Typography.Text></div>
  if (loadError) return <div className="deployment-config-error"><Alert type="error" showIcon message="资源文件加载失败" description={loadError} action={<Button icon={<ReloadOutlined />} onClick={load}>重试</Button>} /></div>

  return <section className="deployment-config-page">
    <div className="deployment-config-heading">
      <div>
        <Typography.Title level={3} className="deployment-config-title">Kubernetes 资源文件
          <Tooltip overlayClassName="deployment-resource-boundary-tooltip" placement="right" title={RESOURCE_BOUNDARY_HELP}>
            <button type="button" className="deployment-resource-help" aria-label="查看 TTP 与资源文件边界说明">
              <QuestionCircleOutlined />
            </button>
          </Tooltip>
        </Typography.Title>
        <Typography.Text type="secondary">每个文件只包含一个可由 kubectl apply -f 应用的资源对象，发布时按列表逐项应用。</Typography.Text>
      </div>
      <Space wrap>
        {readOnly && <Tag color="default">只读</Tag>}
        <Button icon={<ReloadOutlined />} onClick={load}>重新加载</Button>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={readOnly || !current || !dirty} onClick={save}>保存文件</Button>
      </Space>
    </div>

    {readOnly && <Alert className="deployment-readonly-alert" type="info" showIcon message="当前角色只能查看资源文件，不能修改或保存。" />}

    <div className="deployment-config-layout deployment-resource-layout">
      <aside className="deployment-resource-list">
        <div className="deployment-editor-head deployment-resource-list-head">
          <Typography.Text strong className="deployment-resource-list-title">资源文件（{files.length}）</Typography.Text>
          <Space.Compact className="deployment-resource-list-actions">
            <Select size="small" allowClear placeholder="从模板新建" options={RESOURCE_TEMPLATES.map((item) => ({ value: item.value, label: item.label }))} disabled={readOnly} onChange={(value) => value && addFile(value)} />
            <Button size="small" icon={<FileAddOutlined />} disabled={readOnly} onClick={() => addFile()}>新建</Button>
          </Space.Compact>
        </div>
        {files.length === 0 && <Typography.Paragraph type="secondary" className="deployment-resource-empty">还没有资源文件，请新建一个 YAML 或 JSON 文件。</Typography.Paragraph>}
        {files.map((file) => <button type="button" key={file.id} className={`deployment-resource-item ${current?.id === file.id ? 'active' : ''}`} onClick={() => selectFile(file)}>
          <span className="deployment-resource-item-name">{file.path}</span>
          <span className="deployment-resource-item-meta">{file.kind || '未识别'} · {file.resource_name || file.name}</span>
        </button>)}
      </aside>

      <div className="deployment-editor-tool">
        {!current ? <div className="deployment-resource-placeholder"><Typography.Text type="secondary">请选择资源文件，或点击“新建”。</Typography.Text></div> : <>
          <div className="deployment-resource-platform-fields">
            <span>平台字段 · 目标环境 namespace</span>
            <Typography.Text code>{defaultNamespace || '保存环境后自动生成'}</Typography.Text>
            <Typography.Text type="secondary">只读，由 TTP 按环境生成</Typography.Text>
          </div>
          <div className="deployment-editor-head deployment-resource-editor-head">
            <Space wrap>
              <Input value={current.path} disabled={readOnly} onChange={(event) => update({ path: event.target.value, name: event.target.value.split('/').pop() })} placeholder="文件路径，例如 deployment.yaml" />
              {current.kind && <Tag color={current.release_supported ? 'green' : 'orange'}>{current.kind}{current.release_supported ? '' : '（当前发布器未支持）'}</Tag>}
            </Space>
            <Space size={6}>
              <Typography.Text type="secondary">{lineCount} 行 · {Math.ceil(new Blob([current.content || '']).size / 1024)} KB</Typography.Text>
              <Button danger size="small" icon={<DeleteOutlined />} loading={deleting} disabled={readOnly || !current.id} onClick={remove}>删除</Button>
            </Space>
          </div>
          <div className="deployment-config-toolbar">
            <Segmented value={current.format || detectManifestFormat(current.content)} disabled={readOnly || converting} onChange={changeFormat} options={[{ label: 'YAML', value: 'yaml' }, { label: 'JSON', value: 'json' }]} />
            <Space wrap size={6}>
              <Button size="small" icon={<FormatPainterOutlined />} disabled={readOnly} onClick={() => { try { update({ content: formatManifest(current.content, current.format) }) } catch (error) { message.error(error?.message || '格式化失败') } }}>格式化</Button>
              <Button size="small" icon={<CheckCircleOutlined />} loading={checking} onClick={() => check(true)}>检查文件</Button>
              <Button size="small" icon={<CopyOutlined />} onClick={copy}>复制</Button>
            </Space>
          </div>
          <div className="deployment-editor-body">
            <div className="deployment-editor-gutter" ref={gutterRef} aria-hidden="true">{Array.from({ length: lineCount }, (_, index) => <span key={index}>{index + 1}</span>)}</div>
            <textarea value={current.content} onChange={(event) => update({ content: event.target.value })} onScroll={syncScroll} onKeyDown={handleKeyDown} spellCheck="false" readOnly={readOnly} className="deployment-editor-textarea" aria-label="Kubernetes 资源文件编辑器" />
          </div>
        </>}
      </div>
    </div>
  </section>
}
