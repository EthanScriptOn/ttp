import { ArrowRightOutlined, PlusOutlined, SearchOutlined, TeamOutlined } from '@ant-design/icons'
import { Button, Empty, Modal, Form, Input, Tag, Typography } from 'antd'
import { useState } from 'react'
import { roleLabel } from '../services/permissions'
import BrandLogo from './BrandLogo'

export default function SpacePicker({ spaces, currentId, onSelect, onCreate, loading }) {
  const [open, setOpen] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [form] = Form.useForm()

  const allSpaces = Array.isArray(spaces) ? spaces : []
  const normalizedKeyword = keyword.trim().toLowerCase()
  const visibleSpaces = allSpaces
    .filter((space) => {
      if (!normalizedKeyword) return true
      return [space.name, space.slug, space.id, space.description]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(normalizedKeyword))
    })
    .sort((left, right) => Number(String(right.id) === String(currentId)) - Number(String(left.id) === String(currentId)))

  const submit = async (values) => {
    await onCreate({ name: values.name, description: values.description })
    form.resetFields()
    setOpen(false)
  }

  return (
    <main className="space-shell">
      <div className="space-topbar"><BrandLogo /><span className="space-topbar-note">工作空间</span></div>
      <section className="space-content">
        <div className="space-heading">
          <div>
            <div className="section-kicker">工作空间 · {allSpaces.length}</div>
            <Typography.Title level={1}>选择空间</Typography.Title>
            <Typography.Paragraph type="secondary" className="space-subtitle">切换后，你将看到这个空间里的项目和发布记录。</Typography.Paragraph>
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>新建空间</Button>
        </div>
        <div className="space-toolbar">
          <Input
            allowClear
            value={keyword}
            prefix={<SearchOutlined />}
            placeholder="搜索空间名称、标识或描述"
            onChange={(event) => setKeyword(event.target.value)}
          />
          <Typography.Text type="secondary">{keyword.trim() ? `找到 ${visibleSpaces.length} 个` : `${allSpaces.length} 个空间`}</Typography.Text>
        </div>
        <div className="space-list" role="list" aria-label="可用空间">
          {visibleSpaces.map((space) => {
            const isCurrent = String(currentId) === String(space.id)
            const identifier = space.slug || space.key || space.id
            return <div key={space.id} role="listitem">
              <button className={`space-row ${isCurrent ? 'is-current' : ''}`} type="button" onClick={() => onSelect(space.id)}>
                <span className="space-row-icon"><TeamOutlined /></span>
                <span className="space-row-copy">
                  <span className="space-row-title"><strong>{space.name}</strong>{isCurrent && <Tag color="green">当前</Tag>}</span>
                  <span className="space-row-meta"><code title={identifier}>{identifier}</code><span className="space-row-separator">·</span><span title={space.description || '暂无描述'}>{space.description || '暂无描述'}</span></span>
                </span>
                <span className="space-row-action"><span>{roleLabel(space.role)}</span><ArrowRightOutlined /></span>
              </button>
            </div>
          })}
          {!visibleSpaces.length && <div className="space-list-empty"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={keyword.trim() ? '没有找到匹配的空间' : '还没有可用空间'} /></div>}
        </div>
      </section>
      <Modal title="创建工作空间" open={open} onCancel={() => setOpen(false)} footer={null} destroyOnClose>
        <Form form={form} layout="vertical" onFinish={submit} className="modal-form">
          <Form.Item label="空间名称" name="name" rules={[{ required: true, message: '请输入空间名称' }]}><Input placeholder="例如：研发空间" /></Form.Item>
          <Form.Item label="描述" name="description"><Input.TextArea rows={3} placeholder="这个空间用来做什么？" /></Form.Item>
          <Button type="primary" htmlType="submit" loading={loading} block>创建并进入</Button>
        </Form>
      </Modal>
    </main>
  )
}
