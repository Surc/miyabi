import { QueryClient } from '@tanstack/react-query'
import { expect, onTestFinished, test, vi } from 'vitest'

import { topSettingsKey, updateTopConfigOptions, type TopConfig } from '@/api/top'

const previous: TopConfig = {
  show_navigation: true,
  show_rankings: true,
  default_board: 'all'
}

function fixture() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  client.setQueryData(topSettingsKey, previous)
  onTestFinished(() => client.clear())
  return {
    client,
    mutation: client.getMutationCache().build(client, updateTopConfigOptions(client))
  }
}

test('saving TOP preferences writes to the application and shares its response with all consumers', async () => {
  const { client, mutation } = fixture()
  const next: TopConfig = { show_navigation: false, show_rankings: false, default_board: '2022' }
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (url, request) => {
    expect(url).toBe('/api/settings/top')
    expect(request?.method).toBe('PUT')
    expect(JSON.parse(request?.body as string)).toEqual(next)
    return Response.json(next)
  })
  vi.stubGlobal('fetch', fetch)
  await mutation.execute(next)
  expect(fetch).toHaveBeenCalledOnce()
  expect(client.getQueryData(topSettingsKey)).toEqual(next)
})

test('a failed server save preserves the last saved values and reconciles the cache', async () => {
  const { client, mutation } = fixture()
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => Response.json({ error: 'Save failed' }, { status: 500 }))
  )
  await expect(mutation.execute({ ...previous, show_rankings: false })).rejects.toThrow(
    'Save failed'
  )
  expect(client.getQueryData(topSettingsKey)).toEqual(previous)
  expect(client.getQueryState(topSettingsKey)?.isInvalidated).toBe(true)
})

test('an earlier settings read cannot replace a successfully saved server configuration', async () => {
  const { client, mutation } = fixture()
  const response = Promise.withResolvers<TopConfig>()
  let signal: AbortSignal | undefined
  const read = client
    .fetchQuery({
      queryKey: topSettingsKey,
      queryFn: ({ signal: requestSignal }) => {
        signal = requestSignal
        return response.promise
      }
    })
    .catch(() => undefined)
  const next: TopConfig = { ...previous, default_board: '2022' }
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => Response.json(next))
  )
  await mutation.execute(next)
  response.resolve(previous)
  await read
  expect(signal?.aborted).toBe(true)
  expect(client.getQueryData(topSettingsKey)).toEqual(next)
})
