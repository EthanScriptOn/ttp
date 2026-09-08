import { Alert, Col, Form, Input, Modal, Row, Select } from 'antd'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect } from 'react'

export default function ProjectForm({ open, onCancel, onSubmit, loading, clusters = [] }) {
  const [form] = Form.useForm()

  useEffect(() => {
    if (!open) return
    form.resetFields()
    form.setFieldsValue({ default_branch: 'main' })
  }, [open, form])

  return (
    <Modal title="创建项目" open={open} onCancel={onCancel} onOk={() => form.submit()} confirmLoading={loading} okButtonProps={{ disabled: clusters.length === 0 }} okText="创建项目" cancelText="取消" destroyOnHidden width={560}>
      {!clusters.length && <Alert type="warning" showIcon message="请先在集群管理中登记一个可用集群" />}
      <Form form={form} layout="vertical" onFinish={onSubmit} initialValues={{ default_branch: 'main' }} className="project-form">
        <Row gutter={[14, 0]}>
          <Col xs={24} sm={11}><Form.Item label="项目名称" name="name" rules={[{ required: true, message: '请输入项目名称' }]}><Input placeholder="例如：订单服务" /></Form.Item></Col>
          <Col xs={24} sm={13}><Form.Item label="默认分支" name="default_branch"><Input placeholder="main，不填则使用 main" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="代码仓库地址" name="repository_url" rules={[{ required: true, type: 'url', message: '请输入有效的仓库地址' }]}><Input placeholder="https://github.com/组织/仓库" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="镜像仓库地址" name="image_repository" rules={[{ validator: (_, value) => { if (!value) return Promise.resolve(); if (/\s|@/.test(value) || value.includes('://') || !value.includes('/') || !/^[^/]+\.[^/]+(:\d+)?\//.test(value)) return Promise.reject(new Error('格式：registry.example.com/命名空间/仓库，不带 tag')); return Promise.resolve() } }]}><Input placeholder="registry.example.com/team/app（可选，留空使用平台默认）" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="部署集群" name="cluster_id" rules={[{ required: true, message: '请选择部署集群' }]}><Select showSearch optionFilterProp="label" placeholder="选择项目运行的 Kubernetes 集群" options={clusters.map((cluster) => ({ value: cluster.id, label: `${cluster.name} · ${cluster.id}` }))} /></Form.Item></Col>
        </Row>
      </Form>
      <div className="project-create-git-hint">
        <SafetyCertificateOutlined />
        <div>
          <strong>仓库授权说明</strong>
          <span>创建项目后，在项目设置中配置这个仓库自己的机器人账号。</span>
          <span>GitHub 至少需要 Write，GitLab 至少需要 Developer。</span>
        </div>
      </div>
    </Modal>
  )
}
