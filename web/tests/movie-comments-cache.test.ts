import { InfiniteQueryObserver, QueryClient } from '@tanstack/react-query'
import { expect, test, vi } from 'vitest'
import type { Cached } from '@/api/browse-cache'
import { movieCommentsOptions, type MovieCommentsPage } from '@/api/movie-extras'

function commentPage(page: number, content: string, stale = false): Cached<MovieCommentsPage> {
  return {
    data: {
      source: 'javlibrary',
      page,
      has_more: true,
      items: [{ id: String(page), author: '', content, created_at: '' }]
    },
    updated_at: '2026-11-01T00:00:00Z',
    stale
  }
}

test('a cached next page appears before refresh and remains available after verification fails', async () => {
  const first = commentPage(1, 'first page')
  const second = commentPage(2, 'cached second page', true)
  const refreshStarted = Promise.withResolvers<void>()
  const refresh = Promise.withResolvers<Response>()
  vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
  const fetch = vi.fn(async (path: string) => {
    const params = new URL(path, window.location.origin).searchParams
    if (params.get('page') === '1') return Response.json(first)
    if (!params.has('refresh')) return Response.json(second)
    refreshStarted.resolve()
    return refresh.promise
  })
  vi.stubGlobal('fetch', fetch)
  const client = new QueryClient()
  const options = movieCommentsOptions('javlibrary', '', ' abp-123 ')
  client.setQueryData(options.queryKey, { pages: [first.data], pageParams: [1] })
  const observer = new InfiniteQueryObserver(client, options)
  const stop = observer.subscribe(() => {})

  try {
    const pending = observer.fetchNextPage()
    await refreshStarted.promise
    expect(observer.getCurrentResult().data?.pages).toEqual([first.data, second.data])
    refresh.resolve(
      Response.json(
        { error: '需要验证', code: 'JAVLIBRARY_VERIFICATION_REQUIRED' },
        { status: 502 }
      )
    )
    const result = await pending
    expect(result.isFetchNextPageError).toBe(false)
    expect(result.data?.pageParams).toEqual([1, 2])
    expect(result.data?.pages[1]?.refreshError).toMatchObject({
      code: 'JAVLIBRARY_VERIFICATION_REQUIRED'
    })

    fetch.mockImplementationOnce(async () => Response.json(first))
    fetch.mockImplementationOnce(async () => Response.json(second))
    const refreshed = commentPage(2, 'updated second page')
    fetch.mockImplementationOnce(async () => Response.json(refreshed))
    expect((await observer.refetch()).data?.pages).toEqual([first.data, refreshed.data])
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      '/api/extensions/browse/comments?source=javlibrary&code=ABP-123&page=2',
      '/api/extensions/browse/comments?source=javlibrary&code=ABP-123&page=2&refresh=1',
      '/api/extensions/browse/comments?source=javlibrary&code=ABP-123&page=1',
      '/api/extensions/browse/comments?source=javlibrary&code=ABP-123&page=2',
      '/api/extensions/browse/comments?source=javlibrary&code=ABP-123&page=2&refresh=1'
    ])
  } finally {
    stop()
    client.clear()
  }
})
