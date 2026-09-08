import { ClearOutlined, CodeOutlined, PlayCircleOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, Input, Select, Space, Tag, Typography, message } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { execPodCommand } from '../services/api'
import './PodTerminal.css'

function promptFor(podName, command = '') {
  return `app@${podName || 'pod'}:/app$${command ? ` ${command}` : ' '}`
}

export default function PodTerminal({ project, pod, target, targetId = '', open, onClose, canExecute = true }) {
  const [command, setCommand] = useState('')
  const [container, setContainer] = useState('')
  const [output, setOutput] = useState('')
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const containers = useMemo(() => {
    const names = Object.keys(pod?.containers || {})
    return names.length ? names : pod?.container ? [pod.container] : []
  }, [pod])

  useEffect(() => {
    if (!open) return
    setCommand('')
    setContainer(containers[0] || '')
    setError('')
    setOutput(`连接到 ${pod?.name || 'Pod'}\n${target ? `${target.name} · ${target.cluster_id} / ${target.namespace}\n` : ''}\n`)
  }, [open, pod, target, containers])

  const run = async () => {
    const value = command.trim()
    if (!value || running || !canExecute) return
    setError('')
    setOutput((old) => `${old}${promptFor(pod?.name, value)}\n`)
    setCommand('')
    setRunning(true)
    try {
      const result = await execPodCommand(project.id, pod.name, { command: value, container }, targetId)
      setOutput((old) => `${old}${result?.output || ''}${result?.output?.endsWith('\n') ? '' : '\n'}`)
    } catch (nextError) {
      const text = nextError?.message || '命令执行失败，请稍后重试'
      setError(text)
      setOutput((old) => `${old}${text}\n`)
      message.error(text)
    } finally {
      setRunning(false)
    }
  }

  const targetContext = target ? `${target.cluster_id} / ${target.namespace}` : '当前部署环境'

  return <Drawer className="pod-terminal-drawer" title={<div className="pod-terminal-title"><CodeOutlined /><span>{pod?.name || 'Pod Terminal'}</span><Tag color="green">Terminal</Tag></div>} width={780} open={open} onClose={onClose} destroyOnHidden>
    <div className="pod-terminal-context"><span>{targetContext}</span>{containers.length > 1 && <Select size="small" value={container} onChange={setContainer} options={containers.map((item) => ({ value: item, label: item }))} aria-label="选择容器" />}</div>
    {!canExecute && <Alert type="warning" showIcon message="当前账号没有进入 Pod 终端的权限" />}
    <div className="pod-terminal-screen" role="log" aria-live="polite"><pre>{output || `连接到 ${pod?.name || 'Pod'}\n`}</pre>{running && <span className="pod-terminal-cursor">▌</span>}</div>
    <div className="pod-terminal-input-row"><Typography.Text code>$</Typography.Text><Input value={command} onChange={(event) => setCommand(event.target.value)} onPressEnter={run} placeholder="输入命令，例如 ls -la、env、pwd" disabled={!canExecute || running} autoFocus /><Button type="primary" icon={<PlayCircleOutlined />} onClick={run} loading={running} disabled={!canExecute || !command.trim()}>执行</Button></div>
    <Space className="pod-terminal-actions"><Button icon={<ClearOutlined />} onClick={() => setOutput('')} disabled={!output}>清空输出</Button>{error && <Typography.Text type="danger">{error}</Typography.Text>}</Space>
  </Drawer>
}
