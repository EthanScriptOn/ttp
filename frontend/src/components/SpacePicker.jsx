import { ArrowRightOutlined, PlusOutlined, TeamOutlined } from '@ant-design/icons'
import { Button, Card, Empty, Modal, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { roleLabel } from '../services/permissions'
import BrandLogo from './BrandLogo'

export default function SpacePicker({ spaces, currentId, onSelect, onCreate, loading }) {
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm()

  const submit = async (values) => {
    await onCreate({ name: values.name, description: values.description })
    form.resetFields()
    setOpen(false)
  }

  return (
    <main className="space-shell">
      <div className="space-topbar"><BrandLogo /><span className="space-topbar-note">选择工作空间</span></div>
      <section className="space-content">
        <div className="section-kicker">你的工作空间</div>
        <Typography.Title level={1}>先选一个空间</Typography.Title>
        <Typography.Paragraph type="secondary" className="space-subtitle">空间决定你能看到哪些项目、集群和发布记录。</Typography.Paragraph>
        <div className={`space-grid ${spaces.length ? '' : 'is-empty'}`}>
          {spaces.map((space) => (
            <Card key={space.id} className={`space-card ${currentId === space.id ? 'is-current' : ''}`} hoverable onClick={() => onSelect(space.id)}>
              <div className="space-card-icon"><TeamOutlined /></div>
              <div className="space-card-body"><Typography.Title level={4}>{space.name}</Typography.Title><Typography.Paragraph type="secondary">{space.description || '暂无描述'}</Typography.Paragraph><span className="space-role">{roleLabel(space.role)}</span></div>
              <ArrowRightOutlined className="space-arrow" />
            </Card>
          ))}
          {!spaces.length && <Empty className="space-empty" description="还没有可用空间" />}
          <button className="space-create" type="button" onClick={() => setOpen(true)}><span className="space-create-icon"><PlusOutlined /></span><span><strong>创建新空间</strong><small>为团队建立独立的发布边界</small></span></button>
        </div>
      </section>
      <Modal title="创建工作空间" open={open} onCancel={() => setOpen(false)} footer={null} destroyOnHidden>
        <Form form={form} layout="vertical" onFinish={submit} className="modal-form">
          <Form.Item label="空间名称" name="name" rules={[{ required: true, message: '请输入空间名称' }]}><Input placeholder="例如：研发空间" /></Form.Item>
          <Form.Item label="描述" name="description"><Input.TextArea rows={3} placeholder="这个空间用来做什么？" /></Form.Item>
          <Button type="primary" htmlType="submit" loading={loading} block>创建并进入</Button>
        </Form>
      </Modal>
    </main>
  )
}
