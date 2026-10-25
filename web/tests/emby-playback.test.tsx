import { renderToStaticMarkup } from 'react-dom/server'
import { Children, isValidElement, type MouseEvent, type ReactElement, type ReactNode } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'
import { isEmbyPlayURL, useEmbyPlayback } from '@/api/emby-playback'
import { MovieEmbyPlayback } from '@/features/movie-detail/extensions/emby-playback'

vi.mock('@/api/emby-playback', async importOriginal => ({
  ...(await importOriginal<typeof import('@/api/emby-playback')>()),
  useEmbyPlayback: vi.fn()
}))
// SSR fixtures only provide the fields the component reads; query lifecycle is
// owned by React Query and is not simulated by this rendering test.
function mockTarget(
  state: Partial<{
    data: ReturnType<typeof useEmbyPlayback>['data']
    isPending: boolean
    isFetching: boolean
    isError: boolean
    isStale: boolean
    dataUpdatedAt: number
    error: Error
  }>
) {
  vi.mocked(useEmbyPlayback).mockReturnValue({
    data: { status: 'ready', url: 'emby://play/server-1/movie-2' },
    isPending: false,
    isFetching: false,
    isError: false,
    isStale: false,
    dataUpdatedAt: Date.now(),
    refetch: vi.fn(),
    ...state
  } as unknown as ReturnType<typeof useEmbyPlayback>)
}
beforeEach(() => {
  vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Linux; Android 13) Chrome' })
  mockTarget({})
})

test('the ready button launches the play intent synchronously as an anchor', () => {
  const html = renderToStaticMarkup(<MovieEmbyPlayback code="ABC-123" />)
  expect(useEmbyPlayback).toHaveBeenCalledWith('ABC-123')
  expect(html).toContain('href="emby://play/server-1/movie-2"')
  expect(html).not.toContain('emby://items/')
  expect(html).not.toContain('target="_blank"')
  expect(html).toContain('在 Emby App 中播放')
})

function findAnchor(
  node: ReactNode
): ReactElement<{ onClick: (event: MouseEvent) => void }> | undefined {
  if (!isValidElement<{ children?: ReactNode }>(node)) return
  if (node.type === 'a') return node as ReactElement<{ onClick: (event: MouseEvent) => void }>
  for (const child of Children.toArray(node.props.children)) {
    const found = findAnchor(child)
    if (found) return found
  }
}

test.each([29_999, 30_000])('the click checks freshness after a delayed render (%d ms)', age => {
  vi.useFakeTimers()
  mockTarget({})
  const query = vi.mocked(useEmbyPlayback).getMockImplementation()!('ABC-123')
  const android = MovieEmbyPlayback({ code: 'ABC-123' })
  // Render the Android child with the mocked hook, then call its real handler.
  const tree = (android.type as (props: { code: string }) => ReactNode)(android.props)
  const anchor = findAnchor(tree)!
  expect(anchor).toBeDefined()
  const preventDefault = vi.fn()
  vi.setSystemTime(Date.now() + age)
  anchor.props.onClick({ preventDefault } as unknown as MouseEvent)
  if (age === 30_000) {
    expect(preventDefault).toHaveBeenCalledOnce()
    expect(query.refetch).toHaveBeenCalledOnce()
  } else {
    expect(preventDefault).not.toHaveBeenCalled()
    expect(query.refetch).not.toHaveBeenCalled()
  }
})
test.each(['disabled', 'not_exported', 'not_found'] as const)(
  'unavailable target %s has no launch link and can be rechecked',
  status => {
    mockTarget({ data: { status } })
    const html = renderToStaticMarkup(<MovieEmbyPlayback code="ABC-123" />)
    expect(html).not.toContain('href=')
    expect(html).toContain('disabled')
    expect(html).toContain('重新检查')
  }
)
test('unsupported platforms never request a playback target', () => {
  vi.mocked(useEmbyPlayback).mockClear()
  vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' })
  const html = renderToStaticMarkup(<MovieEmbyPlayback code="ABC-123" />)
  expect(useEmbyPlayback).not.toHaveBeenCalled()
  expect(html).toContain('安卓 Emby App 3.4.26')
  expect(html).not.toContain('href=')
})
test('loading, failure and unsafe target states never become a playable link', () => {
  for (const state of [
    { isPending: true, isFetching: true, data: undefined },
    { isPending: false, isError: true, error: new Error('offline'), data: undefined },
    { isFetching: true },
    { isError: true, error: new Error('offline') },
    { isStale: true },
    { dataUpdatedAt: Date.now() - 30_000 },
    { isPending: false, data: { status: 'ready' as const, url: 'javascript:alert(1)' } }
  ]) {
    mockTarget(state)
    const html = renderToStaticMarkup(<MovieEmbyPlayback code="ABC-123" />)
    expect(html).not.toContain('href=')
    expect(html).toContain('disabled')
  }
})
test('only the official play URI shape is accepted', () => {
  expect(isEmbyPlayURL('emby://play/server/movie')).toBe(true)
  for (const value of [
    undefined,
    'emby://items/server/movie',
    'https://emby.invalid',
    'emby://play/server/movie?api_key=secret',
    'emby://play/server/../movie'
  ])
    expect(isEmbyPlayURL(value)).toBe(false)
})
