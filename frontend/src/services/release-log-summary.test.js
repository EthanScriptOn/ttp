import assert from 'node:assert/strict'
import test from 'node:test'
import { executionStageFromLogs, sourceForExecutionLog, summarizeExecutionLogs } from './release-log-summary.js'

function entry(id, source, line, level = 'INFO') {
  return { id, source, line, level, timestamp: `10:00:0${id}.000` }
}

test('shows source progress while git output is still streaming', () => {
  const summary = summarizeExecutionLogs([
    entry('1', 'git', 'branch=feature/login commit=abcdef1234567890'),
    entry('2', 'build', 'fetching source revision (attempt 1/4)'),
    entry('3', 'build', 'remote: Counting objects: 42% (118/281)'),
  ])

  assert.equal(summary.length, 1)
  assert.equal(summary[0].title, '读取代码')
  assert.equal(summary[0].state, 'active')
  assert.match(summary[0].detail, /42%/)
})

test('moves the active stage from source to build and registry', () => {
  const summary = summarizeExecutionLogs([
    entry('1', 'git', 'branch=main commit=abcdef1234567890'),
    entry('2', 'build', 'source revision ready'),
    entry('3', 'build', 'building and pushing image with BuildKit'),
    entry('4', 'build', '#1 [internal] load build definition from Dockerfile'),
    entry('5', 'build', '#5 pushing layers 0.1s done'),
  ])

  assert.deepEqual(summary.map((item) => item.title), ['读取代码', '构建镜像', '推送镜像'])
  assert.deepEqual(summary.map((item) => item.state), ['done', 'done', 'active'])
})

test('marks a completed Kubernetes rollout as finished', () => {
  const summary = summarizeExecutionLogs([
    entry('1', 'git', 'branch=main commit=abcdef1234567890'),
    entry('2', 'build', 'building and pushing image with BuildKit'),
    entry('3', 'build', 'image=registry.local/app@sha256:1234'),
    entry('4', 'k8s', 'apply Deployment/app namespace=dev'),
    entry('5', 'k8s', 'deployment/app successfully rolled out'),
    entry('6', 'ttp', 'release target=dev completed'),
  ])

  assert.deepEqual(summary.map((item) => item.title), ['读取代码', '构建镜像', '推送镜像', '更新 Kubernetes', '等待 Pod 就绪'])
  assert.ok(summary.every((item) => item.state === 'done'))
})

test('never leaves the overview empty for unclassified output', () => {
  const summary = summarizeExecutionLogs([entry('1', 'ttp', 'release preparation started')])
  assert.equal(summary.length, 1)
  assert.equal(summary[0].title, '准备发布')
  assert.equal(summary[0].state, 'active')
})

test('labels source checkout output as git instead of build', () => {
  assert.equal(sourceForExecutionLog('build', 'fetching source revision (attempt 1/4)'), 'git')
  assert.equal(sourceForExecutionLog('build', "fatal: unable to access 'https://github.com/example/repo.git/': Couldn't connect to server"), 'git')
  assert.equal(sourceForExecutionLog('build', '#1 [internal] load build definition from Dockerfile'), 'build')
  assert.equal(sourceForExecutionLog('build', '#5 pushing layers 0.1s done'), 'registry')
})

test('keeps the visible stage on source while Git objects are downloading', () => {
  const logs = [
    entry('1', 'k8s', 'manifest format=yaml version=5'),
    entry('2', 'git', 'branch=main commit=abcdef1234567890'),
    entry('3', 'git', 'Receiving objects: 1% (5/281), 7.21 MiB | 37.00 KiB/s'),
  ]
  assert.equal(executionStageFromLogs(logs), 'source')

  logs.push(entry('4', 'build', 'building and pushing image with BuildKit'))
  assert.equal(executionStageFromLogs(logs), 'build')
})

test('keeps a retried Git failure active after download resumes', () => {
  const summary = summarizeExecutionLogs([
    entry('1', 'git', 'fetching source revision (attempt 1/4)'),
    entry('2', 'git', "fatal: unable to access 'https://github.com/example/repo.git/': Couldn't connect to server", 'ERROR'),
    entry('3', 'git', 'git network attempt 1/4 failed, retrying in 3s'),
    entry('4', 'git', 'fetching source revision (attempt 2/4)'),
    entry('5', 'git', 'Receiving objects: 2% (6/281), 19.82 MiB | 39.00 KiB/s'),
  ])

  assert.equal(summary.length, 1)
  assert.equal(summary[0].title, '读取代码')
  assert.equal(summary[0].state, 'active')
})
