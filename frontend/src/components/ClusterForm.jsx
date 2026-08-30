import { Alert, Form, Input, Modal, Radio, Typography } from 'antd'
import { useEffect } from 'react'

const emptyValues = {
  name: '',
  api_endpoint: '',
  connection_mode: 'kubeconfig',
  kubeconfig_path: '',
  kube_context: '',
}

export default function ClusterForm({ open, cluster, onCancel, onSubmit, loading }) {
  const [form] = Form.useForm()
  const mode = Form.useWatch('connection_mode', form)

  useEffect(() => {
    if (!open) return
    form.resetFields()
    form.setFieldsValue(cluster ? {
      name: cluster.name || '',
      api_endpoint: cluster.api_endpoint || '',
      connection_mode: cluster.connection_mode || 'kubeconfig',
      kubeconfig_path: '',
      kube_context: cluster.kube_context || '',
    } : emptyValues)
  }, [cluster, form, open])

  return <Modal
    title={cluster ? '编辑集群配置' : '添加 Kubernetes 集群'}
    open={open}
    onCancel={onCancel}
    onOk={() => form.submit()}
    confirmLoading={loading}
    okText="保存并测试"
    cancelText="取消"
    destroyOnClose
    width={600}
  >
    <Form form={form} layout="vertical" onFinish={onSubmit} className="cluster-form">
      <Form.Item label="集群名称" name="name" rules={[{ required: true, message: '请输入集群名称' }]}>
        <Input placeholder="例如：测试集群" />
      </Form.Item>
      <Form.Item
        label="Kubernetes API 地址"
        name="api_endpoint"
        extra="用于确认目标集群；实际访问地址、证书和权限从 kubeconfig 读取。"
        rules={[{ type: 'url', message: '请输入完整地址，例如 https://k8s.example.com:6443' }]}
      >
        <Input placeholder="https://k8s.example.com:6443" />
      </Form.Item>
      <Form.Item label="连接方式" name="connection_mode" rules={[{ required: true }]}>
        <Radio.Group optionType="button" buttonStyle="solid" options={[
          { value: 'kubeconfig', label: '服务器上的 kubeconfig' },
          { value: 'in_cluster', label: '集群内身份' },
        ]} />
      </Form.Item>
      {mode === 'in_cluster' ? <Alert
        className="cluster-form-alert"
        type="info"
        showIcon
        message="使用控制台所在 Pod 的 ServiceAccount"
        description="适合把本平台部署在 Kubernetes 集群内；不需要填写 kubeconfig 路径。"
      /> : <>
        <Form.Item
          label="kubeconfig 文件路径"
          name="kubeconfig_path"
          extra={cluster ? '留空保持已保存的路径；路径必须存在于 CI/CD 后端服务器上。' : '路径必须存在于 CI/CD 后端服务器上，不是你当前电脑上的路径。'}
        >
          <Input placeholder="例如：/etc/cicd/kubeconfigs/test.config" />
        </Form.Item>
        <Form.Item label="Context（可选）" name="kube_context" extra="留空时使用 kubeconfig 的 current-context。">
          <Input placeholder="例如：test-cluster-admin@k8s" />
        </Form.Item>
      </>}
      {cluster && <Typography.Paragraph type="secondary" className="cluster-form-note">
        已保存的 kubeconfig 内容不会回显；留空路径不会覆盖已有配置。
      </Typography.Paragraph>}
    </Form>
  </Modal>
}
