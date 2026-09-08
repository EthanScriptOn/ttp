import {
  Button,
  Checkbox,
  DatePicker,
  Input,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import { ClearOutlined, SearchOutlined } from '@ant-design/icons'
import { useMemo, useState } from 'react'

function text(value) {
  return value === undefined || value === null ? '' : String(value)
}

function lower(value) {
  return text(value).trim().toLowerCase()
}

function unique(values) {
  return [...new Set(values.filter(Boolean))]
}

function tagsForCommit(commit, tags) {
  const sha = lower(commit.sha)
  const embedded = Array.isArray(commit.tags)
    ? commit.tags.map((item) => typeof item === 'object' ? item?.name : item)
    : []
  const matched = tags
    .filter((item) => lower(item.sha) === sha)
    .map((item) => item.name)
  return unique([...embedded, ...matched])
}

function commitTimestamp(value) {
  const timestamp = Date.parse(value || '')
  return Number.isFinite(timestamp) ? timestamp : null
}

export default function CommitSelector({
  commits = [],
  tags = [],
  selected = [],
  onChoose,
  onChooseMany,
  loading = false,
  branchLoading = false,
  readOnly = false,
}) {
  const [keyword, setKeyword] = useState('')
  const [author, setAuthor] = useState('')
  const [tag, setTag] = useState('')
  const [dateRange, setDateRange] = useState(null)

  const decoratedCommits = useMemo(
    () => commits.map((commit) => ({ ...commit, tags: tagsForCommit(commit, tags) })),
    [commits, tags],
  )

  const authorOptions = useMemo(() => unique(decoratedCommits.map((commit) => text(commit.author).trim()))
    .sort((left, right) => left.localeCompare(right, 'zh-CN'))
    .map((value) => ({ value, label: value })), [decoratedCommits])

  const tagOptions = useMemo(() => unique(decoratedCommits.flatMap((commit) => commit.tags))
    .sort((left, right) => left.localeCompare(right, 'zh-CN'))
    .map((value) => ({ value, label: value })), [decoratedCommits])

  const filteredCommits = useMemo(() => {
    const query = lower(keyword)
    const selectedAuthor = lower(author)
    const selectedTag = lower(tag)
    const start = dateRange?.[0]?.startOf?.('day')?.valueOf?.() ?? null
    const end = dateRange?.[1]?.endOf?.('day')?.valueOf?.() ?? null

    return decoratedCommits.filter((commit) => {
      const searchable = [commit.sha, commit.short_sha, commit.message, commit.author, ...commit.tags].map(lower).join(' ')
      if (query && !searchable.includes(query)) return false
      if (selectedAuthor && lower(commit.author) !== selectedAuthor) return false
      if (selectedTag && !commit.tags.some((item) => lower(item) === selectedTag)) return false

      const timestamp = commitTimestamp(commit.authored_at)
      if (start !== null && (timestamp === null || timestamp < start)) return false
      if (end !== null && (timestamp === null || timestamp > end)) return false
      return true
    })
  }, [author, dateRange, decoratedCommits, keyword, tag])

  const selectedSHAs = new Set(selected.map((item) => lower(item.sha)))
  const visibleSelectedCount = filteredCommits.filter((commit) => selectedSHAs.has(lower(commit.sha))).length
  const allVisibleSelected = filteredCommits.length > 0 && visibleSelectedCount === filteredCommits.length
  const hasFilters = Boolean(keyword.trim() || author || tag || dateRange?.length)

  const clearFilters = () => {
    setKeyword('')
    setAuthor('')
    setTag('')
    setDateRange(null)
  }

  const columns = [
    {
      title: '',
      width: 48,
      render: (_, item) => (
        <Checkbox
          checked={selectedSHAs.has(lower(item.sha))}
          disabled={readOnly}
          onChange={(event) => onChoose?.(event.target.checked, item)}
        />
      ),
    },
    {
      title: '提交',
      dataIndex: 'short_sha',
      width: 120,
      render: (value, item) => <Typography.Text code>{value || item.sha?.slice(0, 7) || '-'}</Typography.Text>,
    },
    {
      title: '描述',
      dataIndex: 'message',
      render: (value) => <Typography.Text ellipsis={{ tooltip: value }}>{value || '无提交说明'}</Typography.Text>,
    },
    {
      title: '提交人',
      dataIndex: 'author',
      width: 120,
      render: (value) => value || '-',
    },
    {
      title: 'Tag',
      dataIndex: 'tags',
      width: 180,
      render: (values = []) => values.length
        ? <Space size={[4, 4]} wrap>{values.map((value) => <Tag key={value} color="green">{value}</Tag>)}</Space>
        : <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '时间',
      dataIndex: 'authored_at',
      width: 180,
      render: (value) => formatDate(value),
    },
  ]

  return (
    <div className="commit-selector">
      <div className="commit-filter-bar">
        <div className="commit-filter-controls">
          <Input
            allowClear
            value={keyword}
            prefix={<SearchOutlined />}
            placeholder="搜索 SHA、描述、提交人或 Tag"
            onChange={(event) => setKeyword(event.target.value)}
          />
          <Select
            allowClear
            showSearch
            value={author || undefined}
            options={authorOptions}
            placeholder="提交人"
            onChange={(value) => setAuthor(value || '')}
          />
          <Select
            allowClear
            showSearch
            value={tag || undefined}
            options={tagOptions}
            placeholder="Tag"
            onChange={(value) => setTag(value || '')}
          />
          <DatePicker.RangePicker
            value={dateRange}
            format="YYYY-MM-DD"
            placeholder={['开始日期', '结束日期']}
            onChange={setDateRange}
          />
        </div>
        <div className="commit-filter-summary">
          <Space size={12} wrap>
            <Checkbox
              checked={allVisibleSelected}
              indeterminate={visibleSelectedCount > 0 && !allVisibleSelected}
              disabled={readOnly || !filteredCommits.length}
              onChange={(event) => onChooseMany?.(event.target.checked, filteredCommits)}
            >
              全选当前结果
            </Checkbox>
            <Typography.Text type="secondary">显示 {filteredCommits.length} / {commits.length} 条</Typography.Text>
            {hasFilters && <Button type="link" size="small" icon={<ClearOutlined />} onClick={clearFilters}>清空条件</Button>}
          </Space>
        </div>
      </div>
      <Table
        rowKey="sha"
        size="middle"
        loading={loading || branchLoading}
        columns={columns}
        dataSource={filteredCommits}
        pagination={{
          pageSize: 10,
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50],
          showTotal: (total) => `共 ${total} 条`,
        }}
        locale={{ emptyText: hasFilters ? '没有符合条件的提交' : '暂无提交记录' }}
      />
    </div>
  )
}

function formatDate(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}
