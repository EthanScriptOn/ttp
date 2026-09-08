import { Button, Form, Input, Modal, Select, Space, Tag, Typography, message } from 'antd'
import { EnvironmentOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'

const DEFAULT_VALUES = {
  repository_url: '',
  default_branch: 'main',
  description: '',
  image_repository: '',
}

const isValidRepository = (value) => {
  if (!value) return false
  if (/^git@[^\s:]+:[^\s]+$/i.test(value)) return true
  try {
    const url = new URL(value)
    return ['http:', 'https:', 'ssh:', 'git:'].includes(url.protocol) && Boolean(url.hostname)
  } catch {
    return false
  }
}

const isValidBranch = (value) => {
  if (!value || /\s/.test(value)) return false
  if (value.startsWith('/') || value.endsWith('/') || value.startsWith('.') || value.endsWith('.')) return false
  return !value.includes('..') && !value.includes('@{')
}

function projectValues(project) {
  return {
    repository_url: project?.repository_url || DEFAULT_VALUES.repository_url,
    default_branch: project?.default_branch || DEFAULT_VALUES.default_branch,
    description: project?.description || DEFAULT_VALUES.description,
    image_repository: project?.image_repository || DEFAULT_VALUES.image_repository,
  }
}

function cleanPayload(values) {
  return {
    repository_url: values.repository_url.trim(),
    default_branch: values.default_branch.trim(),
    description: values.description?.trim() || '',
    image_repository: values.image_repository?.trim() || '',
  }
}

export default function ProjectSettings({ project, open, loading = false, onCancel, onSubmit, onGoTargets, gitCredential, gitCredentialLoading = false, onSaveGitCredential, onDeleteGitCredential }) {
  const [form] = Form.useForm()
  const [credentialForm, setCredentialForm] = useState({ provider: 'auto', username: '', token: '' })
  const [credentialSaving, setCredentialSaving] = useState(false)
  useEffect(() => {
    if (open) {
      form.resetFields()
      form.setFieldsValue(projectValues(project))
      setCredentialForm({ provider: gitCredential?.provider || 'auto', username: gitCredential?.username || '', token: '' })
    }
  }, [form, open, project, gitCredential])

  const handleFinish = (formValues) => onSubmit?.(cleanPayload(formValues))
  const handleCredentialSave = async () => {
    if (!credentialForm.username.trim() || !credentialForm.token.trim()) {
      message.warning('请输入机器人账号和 Token')
      return
    }
    setCredentialSaving(true)
    try {
      await onSaveGitCredential?.({ ...credentialForm, username: credentialForm.username.trim(), token: credentialForm.token.trim() })
      setCredentialForm((value) => ({ ...value, token: '' }))
    } finally {
      setCredentialSaving(false)
    }
  }
  return (
    <Modal
      title="项目基本设置"
      open={open}
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={loading}
      okText="保存设置"
      cancelText="取消"
      destroyOnHidden
      width="min(680px, calc(100vw - 32px))"
      styles={{ body: { maxHeight: 'calc(100vh - 190px)', overflowY: 'auto', paddingInline: 2 } }}
    >
      <Typography.Paragraph type="secondary" style={{ marginBottom: 18 }}>
        这里只设置项目的基本信息和代码来源。一个项目可以有多个环境，集群、命名空间、实例数、容器端口和发布方式，请在“发布环境”中分别配置。
      </Typography.Paragraph>

      <div className="project-settings-route">
        <EnvironmentOutlined />
        <div>
          <Typography.Text strong>要配置部署环境？</Typography.Text>
          <Typography.Text type="secondary">每个环境可以使用不同的集群和发布策略。</Typography.Text>
        </div>
        <Button type="link" onClick={onGoTargets}>去配置环境</Button>
      </div>

      <div className="project-git-credential">
        <div className="project-git-credential-heading">
          <div><SafetyCertificateOutlined /><Typography.Text strong>仓库机器人</Typography.Text></div>
          {gitCredentialLoading ? <Tag>检查中</Tag> : gitCredential?.configured ? <Tag color="green">已配置 · @{gitCredential.username}</Tag> : <Tag>未配置</Tag>}
        </div>
        <Typography.Text type="secondary" className="project-git-credential-note">用于读取仓库和执行发布操作，Token 不会回显。</Typography.Text>
        <div className="project-git-credential-fields">
          <Select value={credentialForm.provider} onChange={(provider) => setCredentialForm((value) => ({ ...value, provider }))} options={[{ value: 'auto', label: '自动识别 Git 平台' }, { value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' }]} />
          <Input value={credentialForm.username} onChange={(event) => setCredentialForm((value) => ({ ...value, username: event.target.value }))} placeholder="机器人用户名" autoComplete="off" />
          <Input.Password value={credentialForm.token} onChange={(event) => setCredentialForm((value) => ({ ...value, token: event.target.value }))} placeholder={gitCredential?.configured ? '输入新 Token 以更新' : '访问 Token'} autoComplete="new-password" />
        </div>
        <Space className="project-git-credential-actions">
          <Button type="primary" loading={credentialSaving} onClick={handleCredentialSave}>检查并保存</Button>
          {gitCredential?.configured && <Button danger onClick={() => onDeleteGitCredential?.()}>移除授权</Button>}
        </Space>
      </div>

      <Form
        form={form}
        layout="vertical"
        onFinish={handleFinish}
        initialValues={DEFAULT_VALUES}
        requiredMark
        autoComplete="off"
        className="project-form"
      >
        <Form.Item
          label="代码仓库地址"
          name="repository_url"
          rules={[
            { required: true, message: '请输入代码仓库地址' },
            { validator: (_, value) => isValidRepository(value?.trim()) ? Promise.resolve() : Promise.reject(new Error('请输入有效的 Git 仓库地址')) },
          ]}
        >
          <Input allowClear placeholder="例如：https://git.example.com/team/order-service.git" />
        </Form.Item>

        <Form.Item
          label="默认分支"
          name="default_branch"
          rules={[
            { required: true, message: '请输入默认分支' },
            { validator: (_, value) => isValidBranch(value?.trim()) ? Promise.resolve() : Promise.reject(new Error('请输入有效的分支名称')) },
          ]}
        >
          <Input allowClear placeholder="例如：main 或 release" />
        </Form.Item>

        <Form.Item label="项目说明" name="description">
          <Input allowClear placeholder="例如：订单服务的后端接口" />
        </Form.Item>

        <Form.Item
          label="镜像仓库地址"
          name="image_repository"
          extra="发布产物的推送目标，格式 registry.example.com/命名空间/仓库（不带 tag）。留空使用平台默认仓库。"
          rules={[
            { validator: (_, value) => { const trimmed = value?.trim(); if (!trimmed) return Promise.resolve(); if (/\s|@/.test(trimmed) || trimmed.includes('://') || !trimmed.includes('/') || !/^[^/]+\.[^/]+(:\d+)?\//.test(trimmed)) return Promise.reject(new Error('请输入有效的镜像仓库地址，例如 registry.example.com/team/app')); return Promise.resolve() } },
          ]}
        >
          <Input allowClear placeholder="例如：registry.example.com/team/order-service" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
