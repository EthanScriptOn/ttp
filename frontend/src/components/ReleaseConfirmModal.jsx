import { Button, Modal, Space, Typography } from 'antd'
import { ArrowRightOutlined, CheckCircleOutlined, CloudUploadOutlined } from '@ant-design/icons'
import ReleasePreparation from './ReleasePreparation'

function shortSHA(commit) {
  const sha = typeof commit === 'string' ? commit : commit?.sha || commit?.id || ''
  return commit?.short_sha || commit?.shortSha || sha.slice(0, 7)
}

function environmentCode(target) {
  const value = String(target?.environment || '').trim()
  return value ? value.toUpperCase() : String(target?.name || '环境').trim()
}

function environmentLabel(target) {
  const labels = { dev: '开发', uat: '测试', pre: '预发布', prod: '生产' }
  const value = String(target?.environment || '').trim().toLowerCase()
  return labels[value] || String(target?.name || '').replace(/环境$/, '')
}

export default function ReleaseConfirmModal({
  open,
  projectName,
  branch,
  commits = [],
  releaseRouteTargets = [],
  batch,
  baseBranch,
  preparation,
  preparationStatus,
  preparationLoading,
  onPrepare,
  onResolve,
  strategy,
  onStrategyChange,
  trafficCandidate,
  onCandidateChange,
  onCancel,
  onSaveDraft,
  onPublish,
  releaseSaving,
  canPublish,
  canCreateDraft = true,
  isExistingRelease = false,
}) {
  const firstSHA = shortSHA(commits[0])
  const batchBranch = String(batch?.branch || batch?.id || '').trim()
  const strategyOptions = [
    { value: 'rolling', label: '滚动发布', description: '逐步替换旧 Pod' },
    { value: 'canary', label: '灰度发布', description: '先让少量流量访问新版本' },
    { value: 'blue_green', label: '蓝绿发布', description: '新旧两套版本再切换' },
  ]

  return (
    <Modal
      title="发布版本"
      open={open}
      onCancel={onCancel}
      footer={(
        <Space>
          {!isExistingRelease && canCreateDraft && <Button onClick={onSaveDraft} loading={releaseSaving}>保存草稿</Button>}
          <Button type="primary" icon={<CloudUploadOutlined />} onClick={onPublish} loading={releaseSaving} disabled={!canPublish || releaseSaving}>
            开始发布
          </Button>
        </Space>
      )}
      className="release-confirm-modal"
      width={720}
      destroyOnClose
    >
      <div className="release-confirm">
        <section className="release-confirm-section">
          <div className="release-confirm-section-heading">
            <Typography.Text strong>发布关系</Typography.Text>
            <Typography.Text type="secondary">代码进入当前批次</Typography.Text>
          </div>
          <div className="release-confirm-version">
            <div className="release-confirm-version-main">
              <strong title={projectName}>{projectName}</strong>
              <div className="release-confirm-code-line">
                <span className="release-confirm-code-field is-source">
                  <small>发布分支</small>
                  <Typography.Text code title={branch}>{branch || '未选择分支'}</Typography.Text>
                </span>
                <ArrowRightOutlined className="release-confirm-code-arrow" aria-hidden="true" />
                <span className="release-confirm-code-field is-batch">
                  <small>目标批次</small>
                  <Typography.Text code title={batchBranch || undefined}>{batchBranch || '自动创建'}</Typography.Text>
                </span>
              </div>
              <div className="release-confirm-commit-line">
                <span className="release-confirm-code-field">
                  <small>Commit</small>
                  <Typography.Text code title={firstSHA || undefined}>{firstSHA || '未选择版本'}</Typography.Text>
                </span>
                {commits.length > 1 && <span className="release-confirm-code-count">+{commits.length - 1} 个</span>}
              </div>
            </div>
            <Typography.Text className="release-confirm-version-message" ellipsis={{ tooltip: commits[0]?.message || undefined }}>
              {commits[0]?.message || '未填写提交说明'}
            </Typography.Text>
          </div>
        </section>

        <section className="release-confirm-section release-confirm-route-section">
          <div className="release-confirm-section-heading">
            <Typography.Text strong>发布环境</Typography.Text>
            <Typography.Text type="secondary">按顺序发布</Typography.Text>
          </div>
          <div className={`release-route release-route-count-${Math.min(Math.max(releaseRouteTargets.length, 1), 4)}`} aria-label="发布环境路线">
            {releaseRouteTargets.length ? releaseRouteTargets.map((target, index) => (
              <span className="release-route-step-wrap" key={target.id || `${target.name}-${index}`}>
                <span className="release-route-step">
                  <strong>{environmentCode(target)}</strong>
                  <small>{environmentLabel(target)}</small>
                </span>
                {index < releaseRouteTargets.length - 1 && <ArrowRightOutlined className="release-route-arrow" aria-hidden="true" />}
              </span>
            )) : <Typography.Text type="secondary">未选择环境</Typography.Text>}
          </div>
        </section>

        <section className="release-confirm-check-section">
          <ReleasePreparation
            sourceBranch={branch}
            baseBranch={baseBranch}
            status={preparationStatus}
            loading={preparationLoading}
            preparation={preparation}
            onPrepare={onPrepare}
            onResolve={onResolve}
          />
        </section>

        <section className="release-confirm-strategy">
          <div className="release-confirm-section-heading">
            <Typography.Text strong>发布方式</Typography.Text>
            <Typography.Text type="secondary">选择这次怎么替换 Pod</Typography.Text>
          </div>
          <div className="release-strategy-options">
            {strategyOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                className={`release-strategy-option ${strategy === option.value ? 'is-active' : ''}`}
                onClick={() => onStrategyChange(option.value)}
                aria-pressed={strategy === option.value}
              >
                <span className="release-strategy-option-mark">{strategy === option.value && <CheckCircleOutlined />}</span>
                <span>
                  <strong>{option.label}</strong>
                  <small>{option.description}</small>
                </span>
              </button>
            ))}
          </div>
          <div className={`release-confirm-traffic ${strategy === 'rolling' ? 'is-disabled' : ''}`}>
            <span>新版本流量</span>
            <input type="range" min="1" max="99" value={strategy === 'rolling' ? 100 : trafficCandidate} disabled={strategy === 'rolling'} onChange={(event) => onCandidateChange(Number(event.target.value))} />
            <strong>{strategy === 'rolling' ? 100 : trafficCandidate}%</strong>
          </div>
        </section>
      </div>
    </Modal>
  )
}
