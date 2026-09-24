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
  DiffOutlined,
  ExclamationCircleOutlined,
  FileAddOutlined,
  FileTextOutlined,
  FormatPainterOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SaveOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import { useEffect, useRef, useState } from 'react'
import {
  createDeploymentResource,
  createDeploymentResourceOverride,
  deleteDeploymentResource,
  deleteDeploymentResourceOverride,
  getDeploymentTargets,
  getDeploymentResources,
  updateDeploymentResource,
  updateDeploymentResourceOverride,
  validateDeploymentResource,
} from '../services/api'
import {
  detectManifestFormat,
  formatManifest,
  localValidateResourceFile,
} from '../services/deployment-config'
import { emptyDeploymentResource, RESOURCE_TEMPLATES } from '../services/deployment-resource-templates'
import DeploymentResourceDiffModal from './DeploymentResourceDiffModal'

const RESOURCE_BOUNDARY_HELP = <div className="deployment-resource-boundary">
  <div><strong>TTP 自动管理：</strong>每个空间/环境在目标集群中的 namespace、镜像拉取 Secret（配置镜像仓库凭证时）、发布标签，以及发布时注入的镜像和发布环境变量。</div>
  <div><strong>资源文件负责：</strong>Deployment、StatefulSet、Service、ConfigMap、Secret、PVC、Ingress、HPA 等应用资源；副本数、容器端口、探针/保活、PDB 等工作负载规格也在这里维护。</div>
  <div><strong>namespace 规则：</strong>模板会展示当前 DEV 环境的 namespace，但这是只读平台字段；保存时 TTP 会覆盖手动输入，发布到其他环境时再改写为目标环境 namespace。Namespace、PV、ClusterRole 等集群级资源不属于项目资源文件。</div>
</div>

const resourceKey = (file) => file?.override_id || file?.global_resource_id || file?.id || `new:${file?.path}`

const scopeLabel = (file, environmentName) => {
  if (file?.global_changed) return { color: 'gold', text: '全局有更新' }
  if (file?.scope === 'inherited') return { color: 'default', text: '全局继承' }
  if (file?.scope === 'overridden') return { color: 'green', text: `${environmentName} 覆盖` }
  if (file?.scope === 'environment') return { color: 'cyan', text: `${environmentName} 专属` }
  return { color: 'blue', text: '全局' }
}

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
  const [targets, setTargets] = useState([])
  const [activeTargetID, setActiveTargetID] = useState('')
  const [defaultNamespace, setDefaultNamespace] = useState('')
  const [diffOpen, setDiffOpen] = useState(false)
  const gutterRef = useRef(null)

  const activeTarget = targets.find((target) => target.id === activeTargetID)
  const activeNamespace = activeTarget?.namespace || defaultNamespace
  const environmentName = activeTarget?.name || activeTarget?.environment || '当前环境'
  const globalView = !activeTargetID
  const dirty = JSON.stringify(current || {}) !== JSON.stringify(saved || {})
  const hasGlobalDifference = current?.scope === 'overridden' && current.content !== current.global_content
  const lineCount = Math.max(1, String(current?.content || '').split('\n').length)

  const load = async (targetID = activeTargetID, preferred = null) => {
    setLoading(true)
    setLoadError('')
    try {
      const [next, targets] = await Promise.all([
        getDeploymentResources(project.id, targetID),
        getDeploymentTargets(project.id),
      ])
      const defaultTarget = [...targets].sort((a, b) => Number(a?.sort_order || 1) - Number(b?.sort_order || 1))[0]
      const preferredFile = preferred && next.find((file) => (
        (preferred.override_id && file.override_id === preferred.override_id)
        || (preferred.global_resource_id && file.global_resource_id === preferred.global_resource_id)
        || (preferred.path && file.path === preferred.path)
      ))
      setDefaultNamespace(defaultTarget?.namespace || '')
      setTargets(targets)
      setFiles(next)
      setCurrent(preferredFile || next[0] || null)
      setSaved(preferredFile || next[0] || null)
    } catch (error) {
      setLoadError(error?.message || '资源文件加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    setActiveTargetID('')
    load('')
  }, [project.id])

  const confirmDiscard = (actionLabel, action) => {
    if (!dirty) {
      action()
      return
    }
    Modal.confirm({
      title: '未保存修改',
      icon: <ExclamationCircleOutlined />,
      content: `当前资源文件有未保存修改，${actionLabel}后这些修改将丢失。确定继续吗？`,
      okText: '继续',
      cancelText: '取消',
      centered: true,
      width: 420,
      className: 'deployment-resource-switch-confirm',
      onOk: action,
    })
  }

  const selectFile = (file) => {
    confirmDiscard('切换文件', () => {
      setCurrent(file)
      setSaved(file)
    })
  }

  const switchView = (targetID) => {
    if (targetID === activeTargetID) return
    confirmDiscard('切换配置范围', () => {
      setActiveTargetID(targetID)
      load(targetID)
    })
  }

  const update = (patch) => {
    if (readOnly || !current) return
    setCurrent((value) => ({ ...value, ...patch }))
  }

  const addFile = (templateValue = '') => {
    if (readOnly) return
    confirmDiscard('新建文件', () => {
      const template = RESOURCE_TEMPLATES.find((item) => item.value === templateValue)
      const next = template
        ? { ...emptyDeploymentResource(activeNamespace), path: template.path, name: template.path, content: template.content(activeNamespace || 'target-namespace') }
        : emptyDeploymentResource(activeNamespace)
      next.sort_order = files.length + 1
      next.scope = globalView ? 'global' : 'environment'
      next.target_id = activeTargetID
      setCurrent(next)
      setSaved(null)
    })
  }

  const check = async (showSuccess = true) => {
    if (!current) {
      message.warning('请先新建或选择一个资源文件')
      return false
    }
    const local = localValidateResourceFile(current.content, activeNamespace)
    if (!local.ok) {
      message.error(local.message)
      return false
    }
    setChecking(true)
    try {
      const result = await validateDeploymentResource(project.id, { ...current, target_id: activeTargetID })
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
    if (readOnly || !current) return
    if (!(await check(false))) return
    setSaving(true)
    try {
      let result
      if (globalView) {
        result = current.id
          ? await updateDeploymentResource(project.id, current.id, current)
          : await createDeploymentResource(project.id, current)
      } else {
        const payload = {
          ...current,
          target_id: activeTargetID,
          base_global_version: current.global_version || current.base_global_version,
          global_resource_id: current.scope === 'inherited'
            ? (current.global_resource_id || current.id)
            : current.global_resource_id,
        }
        result = current.override_id
          ? await updateDeploymentResourceOverride(project.id, current.override_id, payload)
          : await createDeploymentResourceOverride(project.id, payload)
      }
      await load(activeTargetID, {
        override_id: result.override_id,
        global_resource_id: result.global_resource_id,
        path: result.path,
      })
      message.success(globalView ? '全局资源文件已保存' : '当前环境配置已保存')
    } catch (error) {
      message.error(error?.message || '资源文件保存失败')
    } finally {
      setSaving(false)
    }
  }

  const deleteResource = async (target) => {
    setDeleting(true)
    try {
      if (globalView) {
        await deleteDeploymentResource(project.id, target.id)
        message.success('全局资源文件已删除')
      } else {
        await deleteDeploymentResourceOverride(project.id, target.override_id, activeTargetID)
        message.success(target.scope === 'overridden' ? '已恢复使用全局配置' : '环境专属文件已删除')
      }
      await load(activeTargetID)
    } catch (error) {
      message.error(error?.message || '资源文件删除失败')
    } finally {
      setDeleting(false)
    }
  }

  const remove = () => {
    if (readOnly || !current?.id || (!globalView && current.scope === 'inherited')) return
    const target = current
    const restoreGlobal = !globalView && target.scope === 'overridden'
    Modal.confirm({
      title: restoreGlobal ? '恢复使用全局配置' : '删除资源文件',
      icon: restoreGlobal ? <UndoOutlined /> : <DeleteOutlined />,
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
        <p>{restoreGlobal
          ? `将删除 ${environmentName} 的覆盖版本，之后自动跟随全局文件。`
          : globalView
            ? '删除全局文件后，各环境中基于它创建的覆盖也会删除。已经完成的历史发布不会受到影响。'
            : `删除后，这个文件只会从 ${environmentName} 中移除。`}</p>
      </div>,
      okText: restoreGlobal ? '恢复全局' : '确认删除',
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
  if (loadError) return <div className="deployment-config-error"><Alert type="error" showIcon message="资源文件加载失败" description={loadError} action={<Button icon={<ReloadOutlined />} onClick={() => load(activeTargetID)}>重试</Button>} /></div>

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
        <div className="deployment-resource-view-switch">
          <span>配置范围</span>
          <Segmented
            value={activeTargetID}
            disabled={loading}
            onChange={switchView}
            options={[
              { label: '全局配置', value: '' },
              ...targets.map((target) => ({ label: target.name || target.environment, value: target.id })),
            ]}
          />
        </div>
      </div>
      <Space wrap>
        {readOnly && <Tag color="default">只读</Tag>}
        <Button icon={<ReloadOutlined />} onClick={() => load(activeTargetID)}>重新加载</Button>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={readOnly || !current || !dirty} onClick={save}>保存文件</Button>
      </Space>
    </div>

    {readOnly && <Alert className="deployment-readonly-alert" type="info" showIcon message="当前角色只能查看资源文件，不能修改或保存。" />}

    <div className="deployment-config-layout deployment-resource-layout">
      <aside className="deployment-resource-list">
        <div className="deployment-editor-head deployment-resource-list-head">
          <Space.Compact className="deployment-resource-list-actions">
            <Select
              size="small"
              allowClear
              placeholder="从模板新建"
              options={RESOURCE_TEMPLATES.map((item) => ({ value: item.value, label: item.label }))}
              popupMatchSelectWidth={false}
              listHeight={360}
              styles={{ popup: { root: { minWidth: 220 } } }}
              disabled={readOnly}
              onChange={(value) => value && addFile(value)}
            />
            <Button size="small" icon={<FileAddOutlined />} disabled={readOnly} onClick={() => addFile()}>新建</Button>
          </Space.Compact>
        </div>
        {files.length === 0 && <Typography.Paragraph type="secondary" className="deployment-resource-empty">还没有资源文件，请新建一个 YAML 或 JSON 文件。</Typography.Paragraph>}
        {files.map((file) => {
          const status = scopeLabel(file, environmentName)
          return <button type="button" key={resourceKey(file)} className={`deployment-resource-item ${resourceKey(current) === resourceKey(file) ? 'active' : ''}`} onClick={() => selectFile(file)}>
          <span className="deployment-resource-item-title"><span className="deployment-resource-item-name">{file.path}</span><Tag color={status.color}>{status.text}</Tag></span>
          <span className="deployment-resource-item-meta">{file.kind || '未识别'} · {file.resource_name || file.name}</span>
        </button>})}
      </aside>

      <div className="deployment-editor-tool">
        {!current ? <div className="deployment-resource-placeholder"><Typography.Text type="secondary">请选择资源文件，或点击“新建”。</Typography.Text></div> : <>
          {current.global_changed && <Alert
            className="deployment-resource-update-alert"
            type="info"
            showIcon
            message="全局文件已更新"
            description={`${environmentName} 的环境覆盖继续生效，编辑和发布不受影响。`}
            action={<Button size="small" icon={<DiffOutlined />} onClick={() => setDiffOpen(true)}>查看差异</Button>}
          />}
          <div className="deployment-resource-platform-fields">
            <span>namespace</span>
            <Typography.Text code>{activeNamespace || '保存环境后自动生成'}</Typography.Text>
            <Typography.Text type="secondary">由 TTP 按环境生成，不可编辑</Typography.Text>
          </div>
          <div className="deployment-editor-head deployment-resource-editor-head">
            <Space wrap>
              <Input value={current.path} disabled={readOnly} onChange={(event) => update({ path: event.target.value, name: event.target.value.split('/').pop() })} placeholder="文件路径，例如 deployment.yaml" />
              {current.kind && <Tag color={current.release_supported ? 'green' : 'orange'}>{current.kind}{current.release_supported ? '' : '（当前发布器未支持）'}</Tag>}
            </Space>
            <Space size={6}>
              <Typography.Text type="secondary">{lineCount} 行 · {Math.ceil(new Blob([current.content || '']).size / 1024)} KB</Typography.Text>
              {!globalView && hasGlobalDifference && !current.global_changed && <Button
                size="small"
                icon={<DiffOutlined />}
                onClick={() => setDiffOpen(true)}
              >查看差异</Button>}
              {(globalView || current.scope !== 'inherited') && <Button
                danger={!(!globalView && current.scope === 'overridden')}
                size="small"
                icon={!globalView && current.scope === 'overridden' ? <UndoOutlined /> : <DeleteOutlined />}
                loading={deleting}
                disabled={readOnly || !current.id}
                onClick={remove}
              >{!globalView && current.scope === 'overridden' ? '恢复全局' : '删除'}</Button>}
            </Space>
          </div>
          <div className="deployment-config-toolbar">
            <Segmented value={current.format || detectManifestFormat(current.content)} disabled={readOnly || converting} onChange={changeFormat} options={[{ label: 'YAML', value: 'yaml' }, { label: 'JSON', value: 'json' }]} />
            <Space wrap size={6} className="deployment-config-toolbar-actions">
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
    <DeploymentResourceDiffModal
      open={diffOpen}
      file={current}
      environmentName={environmentName}
      onCancel={() => setDiffOpen(false)}
    />
  </section>
}
