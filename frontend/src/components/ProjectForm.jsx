import { Alert, Col, Form, Input, Modal, Row, Select, Typography } from 'antd'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useEffect } from 'react'

function accountLabel(account) {
  const username = String(account?.username || '').trim()
  if (!username) return ''
  return username.startsWith('@') ? username : `@${username}`
}

export default function ProjectForm({ open, onCancel, onSubmit, loading, gitServiceAccount, clusters = [] }) {
  const [form] = Form.useForm()

  useEffect(() => {
    if (!open) return
    form.resetFields()
    form.setFieldsValue({ default_branch: 'main', cluster_id: clusters[0]?.id || '', namespace: 'default' })
  }, [clusters, open, form])

  return (
    <Modal title="创建项目" open={open} onCancel={onCancel} onOk={() => form.submit()} confirmLoading={loading} okText="创建项目" cancelText="取消" destroyOnClose width={560} okButtonProps={{ disabled: clusters.length === 0 }}>
      <Typography.Paragraph className="project-create-intro" type="secondary">
        填写项目、仓库和首个部署环境；其他环境可以创建后再添加。
      </Typography.Paragraph>
      <Form form={form} layout="vertical" onFinish={onSubmit} initialValues={{ default_branch: 'main' }} className="project-form">
        <Row gutter={[14, 0]}>
          <Col xs={24} sm={11}><Form.Item label="项目名称" name="name" rules={[{ required: true, message: '请输入项目名称' }]}><Input placeholder="例如：订单服务" /></Form.Item></Col>
          <Col xs={24} sm={13}><Form.Item label="默认分支（可选）" name="default_branch"><Input placeholder="main，不填则使用 main" /></Form.Item></Col>
          <Col xs={24}><Form.Item label="代码仓库地址" name="repository_url" rules={[{ required: true, type: 'url', message: '请输入有效的仓库地址' }]}><Input placeholder="https://github.com/组织/仓库" /></Form.Item></Col>
          <Col xs={24} sm={14}><Form.Item label="首个部署集群" name="cluster_id" rules={[{ required: true, message: '请选择部署集群' }]}><Select showSearch optionFilterProp="label" options={clusters.map((cluster) => ({ value: cluster.id, label: `${cluster.name || cluster.id} · ${cluster.id}`, disabled: ['offline', 'unavailable'].includes(cluster.status) }))} placeholder="选择已登记的集群" /></Form.Item></Col>
          <Col xs={24} sm={10}><Form.Item label="namespace" name="namespace" rules={[{ required: true, message: '请输入 namespace' }, { pattern: /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/, message: '只能使用小写字母、数字和短横线' }]}><Input placeholder="default" /></Form.Item></Col>
        </Row>
      </Form>
      {!clusters.length && <Alert type="warning" showIcon message="还没有可用集群" description="请先到“集群管理”登记一个 Kubernetes 集群，再创建项目。" />}
      <div className="project-create-git-hint">
        <SafetyCertificateOutlined />
        <div>
          <strong>仓库授权说明</strong>
          <span>发布时使用的是平台专用 Git 账号，不是你的 TTP 登录账号。</span>
          {gitServiceAccount?.username
            ? <span>当前要添加的账号：<strong>{accountLabel(gitServiceAccount)}</strong></span>
            : <span>创建项目后，发布页会显示具体要添加的账号。</span>}
          <span>请把上面显示的真实 Git 账号加入这个仓库，不是创建一个同名的 TTP 用户：GitHub 选择 Write（可写），GitLab 选择 Developer（开发者）。</span>
          <span>授权完成后回到发布页，点击“重新检查”即可。</span>
        </div>
      </div>
    </Modal>
  )
}
