import { setImmediate } from 'node:timers/promises'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { expect, test } from 'vitest'
import {
  createVersionLoader,
  libraryVersionOptions,
  type ResourceVersion
} from '@/api/library-versions'

const version = (id: number, labels = ['中文']): ResourceVersion => ({
  id,
  labels,
  available: true,
  unmarked: false
})

test('version requests batch, deduplicate and skip canceled cards', async () => {
  const requests: number[][] = []
  const load = createVersionLoader(async ids => {
    requests.push(ids)
    return ids.map(id => version(id))
  })
  const controller = new AbortController()
  const canceled = expect(load(999, controller.signal)).rejects.toMatchObject({
    name: 'AbortError'
  })
  controller.abort()
  const active = Array.from({ length: 205 }, (_, i) => load(i + 1, new AbortController().signal))
  await Promise.all([...active, canceled])
  expect(requests.map(ids => ids.length)).toEqual([100, 100, 5])
  requests.length = 0
  await Promise.all([load(1, new AbortController().signal), load(1, new AbortController().signal)])
  expect(requests).toEqual([[1]])
})

test('missing and failed versions reject without inventing labels', async () => {
  const load = createVersionLoader(async () => [version(1)])
  const missing = expect(load(2, new AbortController().signal)).rejects.toThrow('响应不完整')
  expect(await load(1, new AbortController().signal)).toEqual(version(1))
  await missing
  const failed = createVersionLoader(async () => {
    throw new Error('offline')
  })
  await expect(failed(1, new AbortController().signal)).rejects.toThrow('offline')
})

test('switching library source discards an in-flight result for the same movie', async () => {
  const requests: Array<ReturnType<typeof Promise.withResolvers<ResourceVersion[]>>> = []
  const load = createVersionLoader(() => {
    const response = Promise.withResolvers<ResourceVersion[]>()
    requests.push(response)
    return response.promise
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const observer = new QueryObserver(client, libraryVersionOptions(load, 1, ['account', 'root']))
  const unsubscribe = observer.subscribe(() => {})
  try {
    await setImmediate()
    observer.setOptions(libraryVersionOptions(load, 1, ['other-account', 'other-root']))
    expect(observer.getCurrentResult().data).toBeUndefined()
    await setImmediate()
    requests[1]!.resolve([version(1, ['流出'])])
    await setImmediate()
    requests[0]!.resolve([version(1, ['中文破解'])])
    await setImmediate()
    expect(observer.getCurrentResult().data?.labels).toEqual(['流出'])
    const refresh = client.invalidateQueries({ queryKey: ['library'] })
    await setImmediate()
    requests[2]!.resolve([version(1, ['中文流出'])])
    await refresh
    expect(observer.getCurrentResult().data?.labels).toEqual(['中文流出'])
  } finally {
    unsubscribe()
    client.clear()
  }
})
