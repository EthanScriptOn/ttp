import { Button, Modal, Tag, Typography } from 'antd'
import { useMemo } from 'react'
import { buildLineDiff } from '../services/deployment-resource-diff'

export default function DeploymentResourceDiffModal({ open, file, environmentName, onCancel }) {
  const diffRows = useMemo(() => buildLineDiff(file?.content, file?.global_content), [file?.content, file?.global_content])

  return <Modal
    open={open}
    width={960}
    centered
    destroyOnHidden
    className="deployment-merge-modal"
    title={<div className="deployment-merge-title">
      <span>与全局配置的差异</span>
      <Typography.Text type="secondary">{file?.path} · {environmentName}</Typography.Text>
    </div>}
    onCancel={onCancel}
    footer={<Button onClick={onCancel}>关闭</Button>}
  >
    <div className="deployment-merge-toolbar">
      <Tag color="green">{environmentName} 覆盖</Tag>
    </div>
    <div className="deployment-merge-diff">
      <div className="deployment-merge-diff-head">
        <strong><span className="deployment-merge-diff-legend removed">−</span>当前环境 · {environmentName}</strong>
        <strong><span className="deployment-merge-diff-legend added">+</span>最新全局</strong>
      </div>
      <div className="deployment-merge-diff-body">
        {diffRows.map((row, index) => <div key={`${index}-${row.type}`} className={`deployment-merge-diff-row ${row.type}`}>
          <div className={`deployment-merge-diff-cell environment ${row.environmentLine ? '' : 'empty'}`}>
            <span className="deployment-merge-diff-line-number">{row.environmentLine || ''}</span>
            <span className="deployment-merge-diff-marker">{row.type === 'environment' || row.type === 'changed' ? '−' : ''}</span>
            <code>{row.environment}</code>
          </div>
          <div className={`deployment-merge-diff-cell global ${row.globalLine ? '' : 'empty'}`}>
            <span className="deployment-merge-diff-line-number">{row.globalLine || ''}</span>
            <span className="deployment-merge-diff-marker">{row.type === 'global' || row.type === 'changed' ? '+' : ''}</span>
            <code>{row.global}</code>
          </div>
        </div>)}
      </div>
    </div>
  </Modal>
}
