import { CodeOutlined, EnvironmentOutlined, FileTextOutlined, SettingOutlined } from '@ant-design/icons'
import { Alert, Button, Descriptions, Drawer, Input, Space, Spin, Tabs, Tag, message } from 'antd'
import { useEffect, useState } from 'react'
import { getPod, getPodLogs, updatePodConfig } from '../services/api'

const emptyPod = {}

export default function PodDrawer({ project, pod: podValue, target, targetId = '', open, onClose, canEdit = true }) {
  const pod = podValue || emptyPod
  const [detail, setDetail] = useState(null)
  const [logs, setLogs] = useState('')
  const [configText, setConfigText] = useState('')
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open || !pod) {
      setLoading(false)
      setLoadError('')
      return
    }
    let active = true
    setLoading(true)
    setLoadError('')

    const load = async () => {
      try {
        const [nextDetail, nextLogs] = await Promise.all([
          getPod(project.id, pod.name, targetId),
          getPodLogs(project.id, pod.name, pod.container, targetId),
        ])
        if (!active) return
        setDetail(nextDetail)
        setLogs(nextLogs || '')
        setConfigText(JSON.stringify({ config: nextDetail?.config || {}, environment: nextDetail?.environment || {} }, null, 2))
      } catch (error) {
        if (!active) return
        setDetail(null)
        setLogs('')
        setConfigText('')
        setLoadError(error?.message || 'Pod 详情和日志加载失败，请稍后重试')
      } finally {
        if (active) setLoading(false)
      }
    }

    load()
    return () => { active = false }
  }, [open, pod, project, targetId])

  const save = async () => {
    let parsed
    try {
      parsed = JSON.parse(configText)
    } catch {
      message.error('配置 JSON 格式错误，请检查后重试')
      return
    }

    setSaving(true)
    try {
      const next = await updatePodConfig(project.id, pod.name, parsed, targetId)
      setDetail((old) => ({ ...old, ...next }))
      message.success('Pod 配置已保存')
    } catch (error) {
      message.error(error?.message ? `Pod 配置保存失败：${error.message}` : 'Pod 配置保存失败，请稍后重试')
    } finally {
      setSaving(false)
    }
  }

  const targetContext = target ? `${target.name} · ${target.cluster_id} / ${target.namespace}` : `${detail?.cluster_id || pod?.cluster_id || '当前集群'} / ${detail?.namespace || pod?.namespace || '当前 namespace'}`

  return <Drawer title={<div className="pod-drawer-title"><span>{pod?.name}</span><Tag color="green">运行中</Tag></div>} width={700} open={open} onClose={onClose} destroyOnHidden>
    {loading ? <div className="drawer-loading"><Spin /></div> : loadError ? <Alert type="error" showIcon message="Pod 信息加载失败" description={loadError} /> : !detail ? <Alert type="warning" message="暂时拿不到 Pod 详情" /> : <Tabs defaultActiveKey="overview" items={[
      { key: 'overview', label: <span><CodeOutlined /> 概览</span>, children: <><div className="pod-drawer-context"><EnvironmentOutlined /> {targetContext}</div><Descriptions column={1} bordered size="small"><Descriptions.Item label="命名空间">{detail.namespace}</Descriptions.Item><Descriptions.Item label="Pod IP">{detail.pod_ip || '-'}</Descriptions.Item><Descriptions.Item label="所在节点">{detail.node_name || '-'}</Descriptions.Item><Descriptions.Item label="容器">{Object.keys(detail.containers || {}).join(', ') || pod.container}</Descriptions.Item><Descriptions.Item label="镜像">{Object.values(detail.containers || {})[0]?.image || '-'}</Descriptions.Item></Descriptions></> },
      { key: 'logs', label: <span><FileTextOutlined /> 日志</span>, children: <pre className="pod-logs">{logs || '暂无日志'}</pre> },
      { key: 'config', label: <span><SettingOutlined /> 配置</span>, children: <><Alert className="config-alert" type={canEdit ? 'info' : 'warning'} showIcon message={canEdit ? '修改后会写入运行态配置；容器是否需要重启由部署策略决定。' : '当前角色只能查看 Pod 配置，不能修改运行态参数。'} /><Input.TextArea value={configText} readOnly={!canEdit} onChange={(event) => setConfigText(event.target.value)} autoSize={{ minRows: 14, maxRows: 24 }} className="config-editor" /><Space className="config-actions">{canEdit && <Button type="primary" loading={saving} onClick={save}>保存配置</Button>}<Button disabled={!canEdit} onClick={() => setConfigText(JSON.stringify({ config: detail.config || {}, environment: detail.environment || {} }, null, 2))}>恢复</Button></Space></> },
    ]} />}
  </Drawer>
}
