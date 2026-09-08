import { LockOutlined, LoginOutlined, UserOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Form, Input, Typography } from 'antd'
import BrandLogo from './BrandLogo'

export default function LoginPage({ onLogin, loading, error }) {
  const [form] = Form.useForm()

  const submit = async (values) => {
    await onLogin(values.username, values.password)
  }

  return (
    <main className="auth-shell">
      <div className="auth-visual">
        <BrandLogo className="auth-brand" />
        <div className="auth-visual-copy">
          <Typography.Title>从代码到 Pod，<br />只走一条清晰的路。</Typography.Title>
          <Typography.Paragraph>把仓库、发布和运行状态放在同一个空间里管理。</Typography.Paragraph>
        </div>
        <div className="auth-orbit orbit-one" />
        <div className="auth-orbit orbit-two" />
      </div>
      <div className="auth-panel">
        <Card variant="borderless" className="auth-card">
          <div className="mobile-brand"><BrandLogo tone="dark" /></div>
          <Typography.Title level={2}>欢迎回来</Typography.Title>
          <Typography.Paragraph type="secondary">登录你的发布空间</Typography.Paragraph>
          {error && <Alert showIcon type="error" message={error} className="form-alert" />}
          <Form form={form} layout="vertical" onFinish={submit}>
            <Form.Item label="用户名" name="username" rules={[{ required: true, message: '请输入用户名' }]}>
              <Input size="large" prefix={<UserOutlined />} placeholder="请输入用户名" autoComplete="username" />
            </Form.Item>
            <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
              <Input.Password size="large" prefix={<LockOutlined />} placeholder="请输入密码" autoComplete="current-password" />
            </Form.Item>
            <Button size="large" type="primary" htmlType="submit" block loading={loading} icon={<LoginOutlined />}>
              登录
            </Button>
          </Form>
        </Card>
      </div>
    </main>
  )
}
