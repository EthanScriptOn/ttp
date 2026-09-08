import {
  Alert,
  Button,
  Segmented,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  CheckCircleOutlined,
  CodeOutlined,
  CopyOutlined,
  FileTextOutlined,
  FormatPainterOutlined,
  ReloadOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { useEffect, useRef, useState } from 'react'
import {
  getDeploymentConfig,
  saveDeploymentConfig,
  validateDeploymentConfig,
} from '../services/api'
import {
  detectManifestFormat,
  formatManifest,
  localValidateManifest,
} from '../services/deployment-config'

export default function DeploymentConfigEditor({ project, readOnly = false }) {
  const [config, setConfig] = useState(null)
  const [text, setText] = useState('')
  const [format, setFormat] = useState('yaml')
  const [savedText, setSavedText] = useState('')
  const [savedFormat, setSavedFormat] = useState('yaml')
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [saving, setSaving] = useState(false)
  const [converting, setConverting] = useState(false)
  const [loadError, setLoadError] = useState('')
  const editorRef = useRef(null)
  const gutterRef = useRef(null)

  const dirty = text !== savedText || format !== savedFormat
  const lineCount = Math.max(1, text.split('\n').length)

  const load = async () => {
    setLoading(true)
    setLoadError('')
    try {
      const next = await getDeploymentConfig(project.id)
      const nextText = next.manifest || ''
      const nextFormat = next.format || (nextText ? detectManifestFormat(nextText) : 'yaml')
      setConfig(next)
      setText(nextText)
      setSavedText(nextText)
      setFormat(nextFormat)
      setSavedFormat(nextFormat)
    } catch (error) {
      setLoadError(error?.message || '部署配置加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [project.id])

  const updateText = (value) => {
    if (readOnly) return
    setText(value)
  }

  const check = async (showSuccess = true) => {
    const local = localValidateManifest(text, project.namespace)
    if (!local.ok) {
      message.error(local.message)
      return false
    }
    setChecking(true)
    try {
      const result = await validateDeploymentConfig(project.id, { manifest: text, format })
      if (showSuccess) message.success(`检查通过，共 ${result.resource_count || local.resources.length} 个资源`)
      return true
    } catch (error) {
      message.error(error?.message || '配置检查失败')
      return false
    } finally {
      setChecking(false)
    }
  }

  const save = async () => {
    if (readOnly) return
    if (!(await check(false))) return
    setSaving(true)
    try {
      const next = await saveDeploymentConfig(project.id, { manifest: text, format })
      setConfig(next)
      setText(next.manifest || text)
      setSavedText(next.manifest || text)
      setFormat(next.format || format)
      setSavedFormat(next.format || format)
      message.success('部署配置已保存，后续发布会使用这份配置')
    } catch (error) {
      message.error(error?.message || '部署配置保存失败')
    } finally {
      setSaving(false)
    }
  }

  const changeFormat = async (nextFormat) => {
    if (readOnly) return
    if (nextFormat === format) return
    setConverting(true)
    try {
      // The server remains the authoritative validator. Local conversion keeps
      // switching modes instant and also works while the backend is offline.
      const converted = formatManifest(text, nextFormat)
      setText(converted)
      setFormat(nextFormat)
    } catch (error) {
      message.error(error?.message || '当前内容无法转换为目标格式')
    } finally {
      setConverting(false)
    }
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      message.success('配置已复制')
    } catch {
      message.warning('浏览器不允许访问剪贴板，请手动复制')
    }
  }

  const syncScroll = (event) => {
    if (gutterRef.current) gutterRef.current.scrollTop = event.currentTarget.scrollTop
  }

  const handleKeyDown = (event) => {
    if (readOnly) return
    if (event.key !== 'Tab') return
    event.preventDefault()
    const target = event.currentTarget
    const start = target.selectionStart
    const end = target.selectionEnd
    const next = `${text.slice(0, start)}  ${text.slice(end)}`
    updateText(next)
    requestAnimationFrame(() => {
      target.selectionStart = start + 2
      target.selectionEnd = start + 2
    })
  }

  if (loading) return <div className="deployment-config-loading"><Spin /><Typography.Text type="secondary">加载部署配置...</Typography.Text></div>
  if (loadError) return <div className="deployment-config-error"><Alert type="error" showIcon message="部署配置加载失败" description={loadError} action={<Button icon={<ReloadOutlined />} onClick={load}>重试</Button>} /></div>

  return <section className="deployment-config-page">
    <div className="deployment-config-heading">
      <div>
        <Typography.Title level={3}>部署配置</Typography.Title>
      </div>
      <Space wrap>
        {readOnly && <Tag color="default">只读</Tag>}
        <Button icon={<ReloadOutlined />} onClick={load}>重新加载</Button>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={readOnly || !dirty} onClick={save}>保存配置</Button>
      </Space>
    </div>

    {readOnly && <Alert className="deployment-readonly-alert" type="info" showIcon message="当前角色只能查看部署配置，不能修改或保存。" />}

    <div className="deployment-config-context">
      <span><CodeOutlined /> 目标集群 <strong>{project.cluster_id || '未配置'}</strong></span>
      <span><FileTextOutlined /> 命名空间 <strong>{project.namespace || '未配置'}</strong></span>
      {config?.version > 0 && <Typography.Text type="secondary">当前版本 v{config.version}</Typography.Text>}
    </div>

    <div className="deployment-config-toolbar">
      <Segmented
        value={format}
        disabled={readOnly || converting}
        onChange={changeFormat}
        options={[{ label: 'YAML', value: 'yaml' }, { label: 'JSON', value: 'json' }]}
      />
      <Space wrap size={6}>
        <Button size="small" icon={<FormatPainterOutlined />} disabled={readOnly} onClick={() => { try { updateText(formatManifest(text, format)) } catch (error) { message.error(error?.message || '格式化失败') } }}>格式化</Button>
        <Button size="small" icon={<CheckCircleOutlined />} loading={checking} disabled={readOnly} onClick={check}>检查配置</Button>
        <Button size="small" icon={<CopyOutlined />} onClick={copy}>复制</Button>
      </Space>
    </div>

    <div className="deployment-config-layout">
      <div className="deployment-editor-tool">
        <div className="deployment-editor-head">
          <div><Typography.Text strong>资源文件</Typography.Text><Typography.Text type="secondary">多份资源用 <Typography.Text code>---</Typography.Text> 分隔</Typography.Text></div>
          <Typography.Text type="secondary">{lineCount} 行 · {Math.ceil(new Blob([text]).size / 1024)} KB</Typography.Text>
        </div>
        <div className="deployment-editor-body">
          <div className="deployment-editor-gutter" ref={gutterRef} aria-hidden="true">
            {Array.from({ length: lineCount }, (_, index) => <span key={index}>{index + 1}</span>)}
          </div>
          <textarea
            ref={editorRef}
            value={text}
            onChange={(event) => updateText(event.target.value)}
            onScroll={syncScroll}
            onKeyDown={handleKeyDown}
            spellCheck="false"
            readOnly={readOnly}
            className="deployment-editor-textarea"
            aria-label="Kubernetes 部署配置编辑器"
          />
        </div>
      </div>
    </div>
  </section>
}
