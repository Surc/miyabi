import { renderToStaticMarkup } from 'react-dom/server'
import { expect, test, vi } from 'vitest'
import { usePublicationStatus } from '@/api/publication'
import { PublicationRecoveryStatus } from '@/features/tasks/publication-status'

vi.mock('@/api/publication', () => ({ usePublicationStatus: vi.fn() }))
function status(value: unknown) {
  vi.mocked(usePublicationStatus).mockReturnValue(value as ReturnType<typeof usePublicationStatus>)
  return renderToStaticMarkup(<PublicationRecoveryStatus />)
}
test('blocked publication remains visible even with no task record or known count', () => {
  const html = status({ data: { blocked: true, pending: 0 } })
  expect(html).toContain('导出写入已暂停')
  expect(html).toContain('无法确认发布恢复记录')
  expect(html).toContain('保留现有文件和备份')
  expect(html).not.toContain('自动删除')
})
test('clear recovery is quiet, failed reads never claim safety', () => {
  expect(status({ data: { blocked: false, pending: 0 } })).toBe('')
  expect(status({ isError: true, refetch: vi.fn() })).toContain('无法读取导出恢复状态')
  expect(status({ isPending: true })).toContain('正在读取')
})
