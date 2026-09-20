import { Alert, Col, Form, Input, Modal, Row, Select, Typography } from 'antd'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect } from 'react'

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

const isValidBranch = (value) => Boolean(value) && !/\s/.test(value) && !value.startsWith('/') && !value.endsWith('/') && !value.startsWith('.') && !value.endsWith('.') && !value.includes('..') && !value.includes('@{')

export default function ProjectForm({ open, onCancel, onSubmit, loading, clusters = [], registryConnections = [] }) {
  const [form] = Form.useForm()

  useEffect(() => {
    if (!open) return
    form.resetFields()
    form.setFieldsValue({ default_branch: 'main' })
  }, [open, form])

  return (
    <Modal title="创建项目" open={open} onCancel={onCancel} onOk={() => form.submit()} confirmLoading={loading} okButtonProps={{ disabled: clusters.length === 0 || registryConnections.length === 0 }} okText="创建项目" cancelText="取消" destroyOnHidden width={560}>
      {!clusters.length && <Alert type="warning" showIcon message="请先在连接管理中登记一个可用 Kubernetes 集群" />}
      {!registryConnections.length && <Alert type="warning" showIcon message="请先在连接管理中添加并测试一个镜像仓库连接" />}
      <Form form={form} layout="vertical" onFinish={onSubmit} initialValues={{ default_branch: 'main' }} className="project-form">
        <Row gutter={[14, 0]}>
          <Col xs={24} sm={11}><Form.Item label="项目名称" name="name" rules={[{ required: true, message: '请输入项目名称' }]}><Input placeholder="例如：订单服务" /></Form.Item></Col>
          <Col xs={24} sm={11}><Form.Item label="默认分支" name="default_branch" rules={[{ required: true, message: '请输入默认分支' }, { validator: (_, value) => isValidBranch(value?.trim()) ? Promise.resolve() : Promise.reject(new Error('请输入有效的分支名称')) }]}><Input placeholder="例如：main 或 release" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="代码仓库地址" name="repository_url" rules={[{ required: true, message: '请输入代码仓库地址' }, { validator: (_, value) => isValidRepository(value?.trim()) ? Promise.resolve() : Promise.reject(new Error('请输入有效的 Git 仓库地址')) }]}><Input placeholder="例如：https://github.com/组织/仓库.git" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="项目说明" name="description"><Input allowClear placeholder="例如：订单服务的后端接口" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="镜像仓库连接" name="registry_connection_id" rules={[{ required: true, message: '请选择镜像仓库连接' }]} extra="发布会使用该连接推送镜像并为 Kubernetes 配置 imagePullSecret。镜像仓库路径由平台按项目自动生成，无需手填。"><Select showSearch optionFilterProp="label" placeholder={registryConnections.length ? '请选择镜像仓库连接' : '尚未配置镜像仓库连接'} options={registryConnections.map((connection) => ({ value: connection.id, label: `${connection.name} · ${connection.registry}` }))} /></Form.Item></Col>
          <Col xs={24}><Form.Item label="默认发布集群" name="cluster_id" rules={[{ required: true, message: '请选择默认发布集群' }]} extra="用于创建项目时的初始目标。项目创建后，请在“发布环境”中为开发、测试、生产分别绑定集群。"><Select showSearch optionFilterProp="label" placeholder="选择项目的初始 Kubernetes 集群" options={clusters.map((cluster) => ({ value: cluster.id, label: `${cluster.name} · ${cluster.id}` }))} /></Form.Item></Col>
          <Col xs={24}><Typography.Title level={5} style={{ margin: '8px 0 12px' }}>仓库机器人</Typography.Title></Col>
          <Col xs={24} sm={8}><Form.Item label="Git 平台" name="git_provider" rules={[{ required: true }]}><Select options={[{ value: 'auto', label: '自动识别' }, { value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' }]} /></Form.Item></Col>
          <Col xs={24} sm={8}><Form.Item label="机器人用户名" name="git_username" rules={[{ required: true, message: '请输入机器人用户名' }]}><Input autoComplete="off" placeholder="例如：ttp-bot" /></Form.Item></Col>
          <Col xs={24} sm={8}><Form.Item label="访问 Token" name="git_token" rules={[{ required: true, message: '请输入访问 Token' }]}><Input.Password autoComplete="new-password" placeholder="不会回显" /></Form.Item></Col>
        </Row>
      </Form>
      <div className="project-create-git-hint"><SafetyCertificateOutlined /><div><strong>仓库授权说明</strong><span>机器人用于读取仓库和执行发布操作。GitHub 至少需要 Write，GitLab 至少需要 Developer。</span></div></div>
    </Modal>
  )
}
