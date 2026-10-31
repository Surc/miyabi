import { setImmediate } from 'node:timers/promises'
import { QueryClient } from '@tanstack/react-query'
import { expect, onTestFinished, test, vi } from 'vitest'

import {
  subtitleSettingsKey,
  updateSubtitleConfigOptions,
  type SubtitleConfig
} from '@/api/subtitle'

const previous: SubtitleConfig = {
  enabled: true,
  sources: { xunlei: true, subtitlecat: true, avsubtitles: true },
  language: 'zh-CN'
}

function fixture() {
  const client = new QueryClient()
  client.setQueryData(subtitleSettingsKey, previous)
  onTestFinished(() => client.clear())
  const mutation = client.getMutationCache().build(client, updateSubtitleConfigOptions(client))
  return { client, mutation }
}

test('subtitle saves preserve explicit disabled flags and apply the persisted response', async () => {
  const { client, mutation } = fixture()
  const response = Promise.withResolvers<Response>()
  const fetch = vi.fn(() => response.promise)
  vi.stubGlobal('fetch', fetch)
  const next: SubtitleConfig = {
    enabled: false,
    sources: { xunlei: false, subtitlecat: false, avsubtitles: true },
    language: 'zh-TW'
  }
  const pending = mutation.execute(next)
  await setImmediate()
  expect(client.getQueryData(subtitleSettingsKey)).toEqual(next)
  expect(fetch).toHaveBeenCalledWith(
    '/api/settings/subtitles',
    expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify(next),
      credentials: 'same-origin'
    })
  )
  response.resolve(Response.json(next))
  await pending
  expect(client.getQueryData(subtitleSettingsKey)).toEqual(next)
})

test('failed subtitle saves restore the previous settings and invalidate the read cache', async () => {
  const { client, mutation } = fixture()
  const response = Promise.withResolvers<Response>()
  vi.stubGlobal(
    'fetch',
    vi.fn(() => response.promise)
  )
  const pending = mutation.execute({ ...previous, enabled: false })
  const rejected = expect(pending).rejects.toMatchObject({ status: 503 })
  await setImmediate()
  expect(client.getQueryData<SubtitleConfig>(subtitleSettingsKey)?.enabled).toBe(false)
  response.resolve(Response.json({ error: 'save failed' }, { status: 503 }))
  await rejected
  expect(client.getQueryData(subtitleSettingsKey)).toEqual(previous)
  expect(client.getQueryState(subtitleSettingsKey)?.isInvalidated).toBe(true)
})

test('a stale settings response cannot overwrite a subtitle change', async () => {
  const { client, mutation } = fixture()
  const readResponse = Promise.withResolvers<SubtitleConfig>()
  let signal: AbortSignal | undefined
  const read = client
    .fetchQuery({
      queryKey: subtitleSettingsKey,
      queryFn: ({ signal: requestSignal }) => {
        signal = requestSignal
        return readResponse.promise
      }
    })
    .catch(() => undefined)
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => Response.json({ ...previous, language: 'zh-TW' }))
  )
  const next: SubtitleConfig = { ...previous, language: 'zh-TW' }
  await mutation.execute(next)
  readResponse.resolve(previous)
  await read
  expect(signal?.aborted).toBe(true)
  expect(client.getQueryData(subtitleSettingsKey)).toEqual(next)
})
