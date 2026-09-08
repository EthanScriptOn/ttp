import {
  CheckCircleOutlined,
  DownOutlined,
  InfoCircleOutlined,
  LockOutlined,
  ReloadOutlined,
  RightOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { Button, Tag, Typography } from 'antd'
import { useState } from 'react'

function accountLabel(account) {
  const username = String(account?.username || '').trim()
  if (!username) return '尚未配置'
  return username.startsWith('@') ? username : `@${username}`
}

function providerLabel(value) {
  return ({ github: 'GitHub', gitlab: 'GitLab' })[value] || value || 'Git'
}

function permissionLabel(value) {
  return ({ Write: '可写', Developer: '开发者', Maintainer: '维护者', Guest: '只读' })[value] || value || '未检查'
}

function permissionDescription(provider, value) {
  const permission = String(value || '').trim()
  const normalizedProvider = String(provider || '').toLowerCase()
  if (normalizedProvider === 'github' && permission.toLowerCase() === 'write') return 'Write（可写）'
  if (normalizedProvider === 'gitlab' && permission.toLowerCase() === 'developer') return 'Developer（开发者）'
  return permission ? `${permission}（${permissionLabel(permission)}）` : '至少可写'
}

export default function GitAccessNotice({ account, access, loading = false, error = '', onRefresh, repositoryUrl = '' }) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const usable = access?.usable === true
  const accessAccount = access?.account?.username ? access.account : account
  const provider = access?.provider || accessAccount?.provider || 'unknown'
  const status = loading ? 'loading' : usable ? 'success' : 'warning'
  const Icon = loading ? ReloadOutlined : usable ? CheckCircleOutlined : WarningOutlined
  const title = loading
    ? '正在检查代码仓库'
    : usable
      ? '代码仓库已连接'
      : '还不能发布'
  const subtitle = loading
    ? '正在确认平台是否可以访问这个仓库'
    : usable
      ? '平台可以读取仓库并准备发布版本。'
      : '先完成一次仓库授权，平台才能读取代码并发布。'
  const targetRepository = access?.repository_url || repositoryUrl || '未读取到仓库地址'
  const requiredPermission = access?.required_permission || (provider === 'gitlab' ? 'Developer' : 'Write')
  const requiredPermissionText = permissionDescription(provider, requiredPermission)
  const requiredMergePermission = access?.required_merge_permission
  const requiredMergePermissionText = requiredMergePermission ? permissionDescription(provider, requiredMergePermission) : ''
  const needsSeparateMergePermission = requiredMergePermission && requiredMergePermission !== requiredPermission
  const hasAccount = Boolean(accessAccount?.username)
  const permissionInstruction = needsSeparateMergePermission
    ? `${requiredPermissionText}；合并受保护分支还需要 ${requiredMergePermissionText}`
    : requiredPermissionText
  const message = error
    || (usable
      ? '仓库已准备好，平台会使用上面配置的项目机器人访问这个仓库。'
      : hasAccount
        ? `简单说：在这个仓库的“成员/协作者”设置中，把账号 ${accountLabel(accessAccount)} 加入上面的目标仓库，权限选择 ${permissionInstruction}；完成后点击“重新检查”。`
        : '这个项目还没有配置仓库机器人，请在项目设置中填写机器人账号和 Token；配置后再点击“重新检查”。')
  const actualAccount = access?.authenticated_username
    ? `实际认证账号：${accountLabel({ username: access.authenticated_username })}`
    : '尚未完成身份认证'
  const steps = [
    { key: 'read', label: '查看代码', value: access?.can_read },
    { key: 'branch', label: '准备发布版本', value: access?.can_create_temporary_branch },
    { key: 'merge', label: '执行发布', value: access?.can_merge },
  ]
  const statusLabel = usable ? '已连接' : '需要授权'

  return (
    <div className={`git-access-notice is-${status}`}>
      <div className="git-access-head">
        <div className="git-access-title">
          <Icon />
          <div>
            <strong>{title}</strong>
            <Typography.Text type="secondary">{subtitle}</Typography.Text>
          </div>
        </div>
        <Button type="text" size="small" icon={<ReloadOutlined />} onClick={onRefresh} loading={loading}>重新检查</Button>
      </div>
      {!loading && <>
        <div className="git-access-summary">
          <div className="git-access-account">
            <LockOutlined />
            <div>
              <span>要添加的账号</span>
              <strong>{accountLabel(accessAccount)}</strong>
            </div>
          </div>
        <Tag color={usable ? 'success' : 'warning'}>{statusLabel}</Tag>
        </div>
        <div className="git-access-requirements">
          <div className="git-access-requirement">
            <span>添加到这个仓库</span>
            <strong title={targetRepository}>{targetRepository}</strong>
          </div>
          <div className="git-access-requirement">
            <span>需要的仓库权限</span>
            <strong>{requiredPermissionText}</strong>
            {needsSeparateMergePermission && <small>合并受保护分支：{requiredMergePermissionText}</small>}
          </div>
        </div>
        <div className="git-access-flow" aria-label="发布流程">
          {steps.map((item, index) => <div className="git-access-flow-step" key={item.key}>
            <span className={`git-access-flow-icon ${item.value === true ? 'is-passed' : 'is-missing'}`}>
              {item.value === true ? <CheckCircleOutlined /> : <WarningOutlined />}
            </span>
            <strong>{item.label}</strong>
            {index < steps.length - 1 && <RightOutlined className="git-access-flow-arrow" />}
          </div>)}
        </div>
        <div className="git-access-message"><InfoCircleOutlined /><span>{message}</span></div>
        <Button type="text" size="small" className="git-access-details-toggle" onClick={() => setDetailsOpen((open) => !open)} aria-expanded={detailsOpen}>
          {detailsOpen ? '收起详细信息' : '查看详细信息'} <DownOutlined className={detailsOpen ? 'is-open' : ''} />
        </Button>
        {detailsOpen && <div className="git-access-detail">
          <span>Git 平台：{providerLabel(provider)}</span>
          <span>{actualAccount}</span>
          <span>仓库权限：{permissionLabel(access?.permission)}</span>
          <span>最低要求：{permissionLabel(requiredPermission)}</span>
        </div>}
      </>}
      {loading && <div className="git-access-loading">请稍候，正在确认项目仓库机器人和仓库权限...</div>}
    </div>
  )
}
