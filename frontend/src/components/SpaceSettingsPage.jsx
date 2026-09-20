import {
  Alert,
  Button,
  Form,
  Input,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  ReloadOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { getSpaceSettings, updateSpaceSettings } from '../services/api'
import { hasPermission, normalizeRole, PERMISSIONS } from '../services/permissions'

export default function SpaceSettingsPage({ role, user, onSpaceUpdated }) {
  const [settingsForm] = Form.useForm()
  const [settings, setSettings] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [savingSettings, setSavingSettings] = useState(false)

  const currentRole = normalizeRole(settings?.role || role, 'viewer')
  const isSuperAdmin = Boolean(user?.is_super_admin)
  const canUpdateSpace = hasPermission(currentRole, PERMISSIONS.SPACE_UPDATE, isSuperAdmin)

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const result = await getSpaceSettings()
      setSettings(result)
      settingsForm.setFieldsValue({
        name: result?.space?.name || '',
        description: result?.space?.description || '',
      })
    } catch (loadError) {
      setError(loadError.message || '空间资料加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const saveSettings = async (values) => {
    setSavingSettings(true)
    try {
      const updated = await updateSpaceSettings({
        name: String(values.name || '').trim(),
        description: String(values.description || '').trim(),
      })
      setSettings((old) => ({ ...old, ...updated }))
      onSpaceUpdated?.(updated.space)
      message.success('空间设置已保存')
    } catch (saveError) {
      message.error(saveError.message || '空间设置保存失败')
    } finally {
      setSavingSettings(false)
    }
  }

  return (
    <div className="page-wrap space-settings-page">
      <div className="page-heading space-settings-heading">
        <div>
          <Typography.Title level={2}>空间设置</Typography.Title>
        </div>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        </Space>
      </div>

      {error && <Alert className="space-settings-alert" type="warning" showIcon message={error} />}

      {loading && !settings ? <div className="space-settings-loading"><Spin /><Typography.Text type="secondary">加载空间资料...</Typography.Text></div> : (
        <>
        <section className="space-settings-section space-settings-profile">
          {!canUpdateSpace && <div className="space-settings-section-head"><Tag>只读</Tag></div>}
          {!canUpdateSpace && <Alert type="info" showIcon message="当前角色只能查看空间资料，不能修改设置。" />}
          <Form form={settingsForm} layout="vertical" onFinish={saveSettings} className="space-settings-form" requiredMark={false}>
            <Form.Item label={<span>空间名称<span className="space-settings-required-mark">*</span></span>} name="name" rules={[{ required: true, message: '请输入空间名称' }, { max: 120, message: '空间名称最多 120 个字符' }]}>
              <Input disabled={!canUpdateSpace} placeholder="例如：研发空间" />
            </Form.Item>
            <Form.Item label="空间描述" name="description" rules={[{ max: 255, message: '空间描述最多 255 个字符' }]}>
              <Input.TextArea disabled={!canUpdateSpace} rows={4} showCount maxLength={255} placeholder="说明这个空间主要用于什么" />
            </Form.Item>
          </Form>
        </section>
        {canUpdateSpace && <div className="space-settings-actions">
            <Button type="primary" icon={<SaveOutlined />} onClick={() => settingsForm.submit()} loading={savingSettings}>保存空间设置</Button>
        </div>}
        </>
      )}
    </div>
  )
}
