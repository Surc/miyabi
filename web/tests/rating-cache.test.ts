import { setImmediate } from 'node:timers/promises'
import { QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import {
  exportBrowseCache,
  importBrowseCache,
  readCachedRating,
  type Cached
} from '@/api/browse-cache'
import type { ExternalRating } from '@/api/movie-extras'

const snapshot = (score = 8.1, stale = true): Cached<ExternalRating> => ({
  data: { source: 'javlibrary', found: true, score, max: 10 },
  updated_at: '2026-10-30T00:00:00Z',
  stale
})

beforeEach(() => {
  vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
})
afterEach(() => vi.restoreAllMocks())

test('cached scores display even while both refresh slots are occupied', async () => {
  const pending = [Promise.withResolvers<Response>(), Promise.withResolvers<Response>()]
  const controller = new AbortController()
  const requests: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (path: string) => {
      requests.push(path)
      if (path.includes('cached_only=1'))
        return Response.json(path.startsWith('/cached') ? snapshot() : null)
      return pending[path.startsWith('/first') ? 0 : 1]!.promise
    })
  )
  const first = readCachedRating('/first', {}, new AbortController().signal, vi.fn())
  const second = readCachedRating('/second', {}, new AbortController().signal, vi.fn())
  await setImmediate()
  const preview = vi.fn()
  const cached = readCachedRating('/cached', {}, controller.signal, preview)
  const canceled = expect(cached).rejects.toMatchObject({ name: 'AbortError' })
  await setImmediate()
  expect(preview).toHaveBeenCalledWith(snapshot())
  expect(requests.filter(path => path.includes('refresh=1'))).toEqual([
    '/first?refresh=1',
    '/second?refresh=1'
  ])
  controller.abort()
  await canceled
  for (const request of pending) request.resolve(Response.json(snapshot(8.6, false)))
  await expect(first).resolves.toEqual(snapshot(8.6, false))
  await expect(second).resolves.toEqual(snapshot(8.6, false))
})

test('fresh persisted scores avoid upstream refreshes', async () => {
  const fetch = vi.fn(async (_path: string) => Response.json(snapshot(8.2, false)))
  vi.stubGlobal('fetch', fetch)
  const preview = vi.fn()
  await expect(
    readCachedRating('/rating', { code: 'ABP-123' }, new AbortController().signal, preview)
  ).resolves.toEqual(snapshot(8.2, false))
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(fetch.mock.calls[0]?.[0]).toBe('/rating?code=ABP-123&cached_only=1')
  expect(preview).not.toHaveBeenCalled()
})

test('failed background refresh keeps the displayed cached score and remains retryable', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const key = ['movie-extras', 'javlibrary-rating', 'ABP-123']
  let failed = true
  vi.stubGlobal(
    'fetch',
    vi.fn(async (path: string) =>
      Response.json(
        path.includes('cached_only=1')
          ? snapshot()
          : failed
            ? { ...snapshot(), refresh_failed: true }
            : snapshot(8.6, false)
      )
    )
  )
  const options = {
    queryKey: key,
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      readCachedRating('/rating', {}, signal, cached => client.setQueryData(key, cached))
  }
  try {
    await expect(client.fetchQuery(options)).rejects.toThrow('�ѱ����������')
    expect(client.getQueryData(key)).toEqual(snapshot())
    expect(client.getQueryState(key)?.status).toBe('error')
    failed = false
    await expect(client.fetchQuery(options)).resolves.toEqual(snapshot(8.6, false))
    expect(client.getQueryData(key)).toEqual(snapshot(8.6, false))
  } finally {
    client.clear()
  }
})

test('cache archives download as binary and upload unchanged with normal authentication', async () => {
  const archive = new File(['PK\u0003\u0004archive'], 'cache.zip', { type: 'application/zip' })
  const stats = { movies: 12, images: 3, bytes: 100 }
  const fetch = vi.fn(async (_path: string, init?: RequestInit) =>
    init?.method === 'POST' ? Response.json(stats) : new Response(archive)
  )
  vi.stubGlobal('fetch', fetch)
  const downloaded = await exportBrowseCache()
  expect(await downloaded.text()).toBe(await archive.text())
  await expect(importBrowseCache(archive)).resolves.toEqual(stats)
  expect(fetch.mock.calls[0]).toEqual([
    '/api/extensions/browse/cache/export',
    { credentials: 'same-origin' }
  ])
  expect(fetch.mock.calls[1]).toEqual([
    '/api/extensions/browse/cache/import',
    {
      credentials: 'same-origin',
      method: 'POST',
      headers: { 'Content-Type': 'application/zip' },
      body: archive
    }
  ])
})

test('archive errors preserve server explanations', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => Response.json({ error: '��Ч�Ļ����ļ�' }, { status: 400 }))
  )
  await expect(importBrowseCache(new File(['invalid'], 'bad.zip'))).rejects.toThrow(
    '��Ч�Ļ����ļ�'
  )
  await expect(exportBrowseCache()).rejects.toThrow('��Ч�Ļ����ļ�')
})
