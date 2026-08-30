import { Button, Form, Input, Modal, Tooltip, Typography } from 'antd'
import { EnvironmentOutlined, QuestionCircleOutlined } from '@ant-design/icons'
import { useEffect } from 'react'

const DEFAULT_VALUES = {
  repository_url: '',
  default_branch: 'main',
  description: '',
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
  }
}

function cleanPayload(values) {
  return {
    repository_url: values.repository_url.trim(),
    default_branch: values.default_branch.trim(),
    description: values.description?.trim() || '',
  }
}

export default function ProjectSettings({ project, open, loading = false, onCancel, onSubmit, onGoTargets }) {
  const [form] = Form.useForm()

  useEffect(() => {
    if (open) {
      form.resetFields()
      form.setFieldsValue(projectValues(project))
    }
  }, [form, open, project])

  const handleFinish = (formValues) => onSubmit?.(cleanPayload(formValues))

  return (
    <Modal
      title="项目基本设置"
      open={open}
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={loading}
      okText="保存设置"
      cancelText="取消"
      destroyOnClose
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

      <Form
        form={form}
        layout="vertical"
        onFinish={handleFinish}
        initialValues={DEFAULT_VALUES}
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
          label={(
            <span className="project-settings-label">
              默认分支
              <Tooltip title="它是发布完成后要合入的主线。选择 commit 后，TTP 会创建批次分支，按环境验证，生产成功后再合入这里。">
                <QuestionCircleOutlined aria-label="默认分支说明" />
              </Tooltip>
            </span>
          )}
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
      </Form>
    </Modal>
  )
}
