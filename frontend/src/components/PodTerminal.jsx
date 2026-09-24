import { CloseOutlined, CodeOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Drawer, Select, Tag, Typography } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import './PodTerminal.css'

const terminalSubprotocol = 'ttp-terminal.v1'

function socketURL(projectID, podName, targetID, container) {
  const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const path = `/api/projects/${encodeURIComponent(projectID)}/pods/${encodeURIComponent(podName)}/terminal`
  const params = new URLSearchParams()
  if (targetID) params.set('target_id', targetID)
  if (container) params.set('container', container)
  return `${scheme}//${window.location.host}${path}${params.toString() ? `?${params.toString()}` : ''}`
}

function terminalStatus(status) {
  switch (status) {
    case 'connected': return { color: 'green', label: '已连接' }
    case 'connecting': return { color: 'processing', label: '连接中' }
    case 'error': return { color: 'red', label: '连接失败' }
    default: return { color: 'default', label: '已断开' }
  }
}

export default function PodTerminal({ project, pod, target, targetId = '', open, onClose, canExecute = true }) {
  const hostRef = useRef(null)
  const socketRef = useRef(null)
  const [container, setContainer] = useState('')
  const [status, setStatus] = useState('disconnected')
  const [statusMessage, setStatusMessage] = useState('')
  const [retry, setRetry] = useState(0)
  const containers = useMemo(() => {
    const names = Object.keys(pod?.containers || {})
    return names.length ? names : pod?.container ? [pod.container] : []
  }, [pod?.containers, pod?.container])

  useEffect(() => {
    if (!open) return
    setContainer((current) => current && containers.includes(current) ? current : containers[0] || '')
    setRetry(0)
  }, [open, pod?.name, targetId, containers])

  useEffect(() => {
    if (!open || !pod?.name || !canExecute || !hostRef.current || (containers.length > 1 && !container)) return undefined
    let disposed = false
    const terminal = new Terminal({
      convertEol: false,
      cursorBlink: true,
      cursorStyle: 'bar',
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.35,
      scrollback: 5000,
      theme: {
        background: '#0b1110',
        foreground: '#d9e8e1',
        cursor: '#6ee7a3',
        cursorAccent: '#0b1110',
        selectionBackground: '#2c5f4d',
        black: '#16201d',
        brightBlack: '#667871',
        green: '#6ee7a3',
        brightGreen: '#98f5bd',
        cyan: '#73daca',
        brightCyan: '#9df4e4',
        white: '#d9e8e1',
        brightWhite: '#f4fff8',
      },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.loadAddon(new WebLinksAddon())
    terminal.open(hostRef.current)
    const fitTerminal = () => {
      if (disposed) return
      try { fit.fit() } catch { return }
      const socket = socketRef.current
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: 'resize', cols: terminal.cols, rows: terminal.rows }))
      }
    }
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(fitTerminal)
    observer?.observe(hostRef.current)
    fitTerminal()

    const token = localStorage.getItem('cicd_token') || ''
    if (!token) {
      setStatus('error')
      setStatusMessage('登录已失效，请重新登录')
      terminal.writeln('\x1b[31m登录已失效，请重新登录。\x1b[0m')
      return () => { observer?.disconnect(); terminal.dispose() }
    }
    setStatus('connecting')
    setStatusMessage('正在连接 Pod shell…')
    terminal.writeln(`\x1b[90m连接到 ${pod.name}${container ? ` · ${container}` : ''}…\x1b[0m`)
    const socket = new WebSocket(socketURL(project.id, pod.name, targetId, container), [terminalSubprotocol, `bearer.${token}`])
    socketRef.current = socket
    socket.onopen = () => {
      if (disposed) return
      setStatus('connected')
      setStatusMessage('交互式 shell 已连接')
      fitTerminal()
      terminal.focus()
    }
    socket.onmessage = (event) => {
      if (disposed) return
      if (typeof event.data !== 'string') return
      let message
      try { message = JSON.parse(event.data) } catch { terminal.write(event.data); return }
      if (message.type === 'output') terminal.write(message.data || '')
      if (message.type === 'error') {
        setStatus('error')
        setStatusMessage(message.message || '终端连接失败')
        terminal.write(`\r\n\x1b[31m${message.message || '终端连接失败'}\x1b[0m\r\n`)
      }
      if (message.type === 'exit') {
        setStatus('disconnected')
        setStatusMessage('shell 已退出')
        terminal.write('\r\n\x1b[90m会话已结束。\x1b[0m\r\n')
      }
    }
    socket.onerror = () => {
      if (disposed) return
      setStatus('error')
      setStatusMessage((current) => current || '无法连接到 Pod，请检查 Pod 状态和运行时配置')
    }
    socket.onclose = (event) => {
      if (disposed) return
      setStatus((current) => current === 'error' ? current : 'disconnected')
      setStatusMessage((current) => current || (event.reason || '终端连接已断开'))
    }
    const dataDisposable = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'input', data }))
    })
    const resizeDisposable = terminal.onResize(({ cols, rows }) => {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'resize', cols, rows }))
    })
    return () => {
      disposed = true
      observer?.disconnect()
      dataDisposable.dispose()
      resizeDisposable.dispose()
      if (socketRef.current === socket) socketRef.current = null
      if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) socket.close(1000, 'terminal closed')
      terminal.dispose()
    }
  }, [open, project?.id, pod?.name, targetId, container, containers.length, canExecute, retry])

  const state = terminalStatus(status)
  const targetContext = target ? `${target.cluster_id} / ${target.namespace}` : '当前部署环境'

  return <Drawer
    className="pod-terminal-drawer"
    title={<div className="pod-terminal-title"><CodeOutlined /><span>{pod?.name || 'Pod Terminal'}</span><Tag color="green">Terminal</Tag></div>}
    width={900}
    open={open}
    onClose={onClose}
    destroyOnHidden
    closeIcon={<CloseOutlined />}
    styles={{ body: { padding: 0 } }}
  >
    <div className="pod-terminal-shell">
      <div className="pod-terminal-toolbar">
        <div className="pod-terminal-meta">
          <Typography.Text className="pod-terminal-context">{targetContext}</Typography.Text>
          <span className="pod-terminal-dot">·</span>
          <Tag color={state.color}>{state.label}</Tag>
        </div>
        {containers.length > 1 && <Select size="small" value={container} onChange={setContainer} options={containers.map((item) => ({ value: item, label: item }))} aria-label="选择容器" />}
      </div>
      {!canExecute && <div className="pod-terminal-permission">当前账号没有进入 Pod 终端的权限</div>}
      <div className="pod-terminal-screen" ref={hostRef} role="log" aria-label="Pod 交互式终端" />
      <div className="pod-terminal-footer">
        <span className={`pod-terminal-status-dot ${status}`} aria-hidden="true" />
        <Typography.Text type="secondary">{statusMessage || '终端未连接'}</Typography.Text>
        {(status === 'error' || status === 'disconnected') && canExecute && <Button type="link" size="small" icon={<ReloadOutlined />} onClick={() => setRetry((value) => value + 1)}>重新连接</Button>}
      </div>
    </div>
  </Drawer>
}
