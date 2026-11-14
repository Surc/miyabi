import { test } from 'node:test'
import assert from 'node:assert/strict'
import { subtitleStatus } from '../src/features/tasks/subtitle-status.ts'

test('subtitle failure never says the already-published movie failed', () => {
  assert.equal(subtitleStatus({ status: 'failed' }), '影片已入库，字幕等待重试')
})
test('delayed automatic retry remains distinct from a manual retry', () => {
  assert.equal(
    subtitleStatus({ status: 'queued', retry_at: '2026-11-14T07:00:00Z' }),
    '字幕暂时失败，等待自动重试'
  )
})
test('pause takes precedence over an active status', () => {
  assert.equal(subtitleStatus({ status: 'running', paused: true }), '字幕任务已暂停')
})
test('queued running and complete follow-ups have distinct status labels', () => {
  assert.equal(subtitleStatus({ status: 'queued' }), '字幕已排队')
  assert.equal(subtitleStatus({ status: 'running' }), '正在处理字幕')
  assert.equal(subtitleStatus({ status: 'done' }), '字幕处理完成')
})
