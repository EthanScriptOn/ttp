import { Alert, Button, Form, Input, Modal, Select, Space, Tag, Tooltip, Typography, message } from 'antd'
import { QuestionCircleOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'

const DEFAULT_VALUES = {
  repository_url: '',
  default_branch: 'main',
  description: '',
  registry_connection_id: '',
  auto_merge_target_id: '',
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
    registry_connection_id: project?.registry_connection_id || DEFAULT_VALUES.registry_connection_id,
    auto_merge_target_id: project?.auto_merge_target_id || DEFAULT_VALUES.auto_merge_target_id,
  }
}

function cleanPayload(values) {
  return {
    repository_url: values.repository_url.trim(),
    default_branch: values.default_branch.trim(),
    description: values.description?.trim() || '',
    registry_connection_id: values.registry_connection_id || '',
    auto_merge_target_id: values.auto_merge_target_id || '',
  }
}

export default function ProjectSettings({ project, open, loading = false, onCancel, onSubmit, gitCredential, gitCredentialLoading = false, onSaveGitCredential, onDeleteGitCredential, registryConnections = [], deploymentTargets = [], canManageGit = true }) {
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

  const handleFinish = (formValues) => {
    if (gitCredentialLoading) {
      message.warning('正在检查仓库机器人，请稍后再保存')
      return
    }
    if (gitCredential?.configured !== true) {
      message.warning('请先配置并验证仓库机器人')
      return
    }
    onSubmit?.(cleanPayload(formValues))
  }
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
      title="项目设置"
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

        <div className="project-settings-subheading">
          <span className="project-settings-label-with-help">
            <Typography.Text strong>仓库授权</Typography.Text>
            <Tooltip title="用于读取代码和执行发布。">
              <QuestionCircleOutlined aria-label="仓库授权说明" />
            </Tooltip>
          </span>
        </div>
        <div className="project-git-credential">
          <div className="project-git-credential-heading">
            <div><SafetyCertificateOutlined /><Typography.Text strong>仓库机器人</Typography.Text></div>
            {gitCredentialLoading ? <Tag>检查中</Tag> : gitCredential?.invalid ? <Tag color="red">授权已失效，请重新配置</Tag> : gitCredential?.configured ? <Tag color="green">已配置 · @{gitCredential.username}</Tag> : <Tag>未配置</Tag>}
          </div>
          <Typography.Text type={gitCredential?.invalid ? 'danger' : 'secondary'} className="project-git-credential-note">{gitCredential?.invalid ? '凭证不可用，请重新输入。Token 不会回显。' : 'Token 不会回显。'}</Typography.Text>
          {canManageGit ? <>
            <div className="project-git-credential-fields">
              <Select value={credentialForm.provider} onChange={(provider) => setCredentialForm((value) => ({ ...value, provider }))} options={[{ value: 'auto', label: '自动识别 Git 平台' }, { value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' }]} />
              <Input value={credentialForm.username} onChange={(event) => setCredentialForm((value) => ({ ...value, username: event.target.value }))} placeholder="机器人用户名" autoComplete="off" />
              <Input.Password value={credentialForm.token} onChange={(event) => setCredentialForm((value) => ({ ...value, token: event.target.value }))} placeholder={gitCredential?.configured ? '输入新 Token 以更新' : '访问 Token'} autoComplete="new-password" />
            </div>
            <Space className="project-git-credential-actions">
              <Button type="primary" loading={credentialSaving} onClick={handleCredentialSave}>检查并保存</Button>
              {gitCredential?.configured && <Button danger onClick={() => onDeleteGitCredential?.()}>移除授权</Button>}
            </Space>
          </> : <Alert type="info" showIcon message="当前项目角色只能查看 Git 连接状态，不能替换仓库机器人。" />}
        </div>

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

        <Form.Item
          label={(
            <span className="project-settings-label-with-help">
              发布成功后自动合并
              <Tooltip title="选择一个环境作为触发点，将本次发布分支合并到默认分支；留空表示任何环境发布成功后都不自动合并。仓库机器人需要具备合并权限。">
                <QuestionCircleOutlined aria-label="自动合并说明" />
              </Tooltip>
            </span>
          )}
          name="auto_merge_target_id"
        >
          <Select
            allowClear
            placeholder={deploymentTargets.length ? '不自动合并（可选择触发环境）' : '请先创建发布环境'}
            options={deploymentTargets.filter((target) => target.enabled !== false).map((target) => ({ value: target.id, label: target.name || target.environment_stage || target.environment }))}
          />
        </Form.Item>

        {!registryConnections.length && <Alert type="warning" showIcon message="当前空间还没有镜像仓库连接，请先到连接管理中添加并测试连接。" />}

        <Form.Item
          label={(
            <span className="project-settings-label-with-help">
              镜像仓库连接
              <Tooltip title="用于构建推送和集群拉取。">
                <QuestionCircleOutlined aria-label="镜像仓库连接说明" />
              </Tooltip>
            </span>
          )}
          name="registry_connection_id"
          rules={[{ required: true, message: '请选择镜像仓库连接' }]}
        >
          <Select showSearch optionFilterProp="label" placeholder={registryConnections.length ? '请选择镜像仓库连接' : '尚未配置镜像仓库连接'} options={registryConnections.map((connection) => ({ value: connection.id, label: `${connection.name} · ${connection.registry}` }))} />
        </Form.Item>

        <Form.Item label="项目说明" name="description">
          <Input allowClear placeholder="例如：订单服务的后端接口" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
