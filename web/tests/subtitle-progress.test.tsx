import { Children, isValidElement, type ReactElement, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'
import { useRetryTask, type SubtitleTask } from '@/api/tasks'
import { SubtitleProgressView } from '@/features/tasks/subtitle-progress'

vi.mock('@/api/tasks', () => ({ useRetryTask: vi.fn() }))
const task: SubtitleTask = {
  id: 84,
  type: 'subtitle',
  movie_id: 12,
  code: 'TEST-012',
  status: 'failed',
  progress: 0,
  can_retry: true,
  error: 'subtitle source unavailable',
  created_at: '2026-11-14T07:00:00Z',
  updated_at: '2026-11-14T07:00:00Z'
}
const retry = { mutate: vi.fn(), isPending: false }
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(useRetryTask).mockReturnValue(retry as unknown as ReturnType<typeof useRetryTask>)
})

test('subtitle failure keeps movie publication successful and exposes its independent error and retry', () => {
  const html = renderToStaticMarkup(<SubtitleProgressView task={task} />)
  expect(html).toContain('TEST-012')
  expect(html).toContain('影片已入库，字幕等待重试')
  expect(html).toContain('subtitle source unavailable')
  expect(html).toContain('重试字幕')
  expect(html).not.toContain('重新刮削')
})

test('retry invokes the subtitle task id rather than movie id or scan retry', () => {
  const tree = SubtitleProgressView({ task })
  function clicks(node: ReactNode): (() => void)[] {
    if (!isValidElement(node)) return []
    const element = node as ReactElement<{ children?: ReactNode; onClick?: () => void }>
    return [
      ...(element.props.onClick ? [element.props.onClick] : []),
      ...Children.toArray(element.props.children).flatMap(clicks)
    ]
  }
  const handlers = clicks(tree)
  expect(handlers).toHaveLength(1)
  handlers[0]!()
  expect(retry.mutate).toHaveBeenCalledExactlyOnceWith(84)
})

test('pending retry disables repeated clicks and backend retry permission is respected', () => {
  vi.mocked(useRetryTask).mockReturnValue({ ...retry, isPending: true } as unknown as ReturnType<
    typeof useRetryTask
  >)
  const pending = renderToStaticMarkup(<SubtitleProgressView task={task} />)
  expect(pending).toMatch(/<button[^>]*disabled=""/)
  expect(pending).toContain('提交中')
  const forbidden = renderToStaticMarkup(
    <SubtitleProgressView task={{ ...task, can_retry: false }} />
  )
  expect(forbidden).not.toContain('<button')
})

test('backend error text remains escaped, not executable markup', () => {
  const html = renderToStaticMarkup(
    <SubtitleProgressView task={{ ...task, error: '<script>alert(1)</script>' }} />
  )
  expect(html).toContain('&lt;script&gt;')
  expect(html).not.toContain('<script>')
})
