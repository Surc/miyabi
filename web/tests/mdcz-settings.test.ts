import { setImmediate } from 'node:timers/promises'
import { QueryClient } from '@tanstack/react-query'
import { expect, onTestFinished, test, vi } from 'vitest'
import { mdczKeys, updateMDCzOptions, type MDCzSettings } from '@/api/mdcz'
import { configurationKey } from '@/api/mdcz-configuration'

const previous: MDCzSettings = {
  enabled: true,
  managed: true,
  mode: 'managed',
  status: 'configured',
  generation: 'first',
  server_url: 'http://127.0.0.1:3840',
  has_api_key: true,
  translate: false,
  badges: true,
  position: 'bottomLeft'
}
function fixture() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(mdczKeys.settings, previous)
  onTestFinished(() => client.clear())
  const mutation = client.getMutationCache().build(client, updateMDCzOptions(client))
  return { client, mutation }
}

test('switching to native only saves Miyabi settings, without contacting bridge health or configuration', async () => {
  const { client, mutation } = fixture()
  const next = { ...previous, enabled: false, status: 'unavailable' as const }
  const fetch = vi.fn(async () => Response.json(next))
  vi.stubGlobal('fetch', fetch)
  client.setQueryData(configurationKey, { configuration_id: 'old' })
  await mutation.execute(next)
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(fetch).toHaveBeenCalledWith(
    '/api/extensions/mdcz/settings',
    expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify(next)
    })
  )
  expect(client.getQueryData(mdczKeys.settings)).toEqual(next)
  expect(client.getQueryData(configurationKey)).toBeUndefined()
})

test('an older settings read cannot revert a successful engine switch', async () => {
  const { client, mutation } = fixture()
  const response = Promise.withResolvers<MDCzSettings>()
  let signal: AbortSignal | undefined
  const read = client
    .fetchQuery({
      queryKey: mdczKeys.settings,
      queryFn: ({ signal: requestSignal }) => {
        signal = requestSignal
        return response.promise
      }
    })
    .catch(() => undefined)
  const next = { ...previous, enabled: false }
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => Response.json(next))
  )
  await mutation.execute(next)
  expect(signal?.aborted).toBe(true)
  response.resolve(previous)
  await read
  expect(client.getQueryData(mdczKeys.settings)).toEqual(next)
})

test('failed settings save keeps the persisted engine and advanced configuration cache', async () => {
  const { client, mutation } = fixture()
  const advanced = { configuration_id: 'preserve' }
  client.setQueryData(configurationKey, advanced)
  const response = Promise.withResolvers<Response>()
  vi.stubGlobal(
    'fetch',
    vi.fn(() => response.promise)
  )
  const pending = mutation.execute({ ...previous, enabled: false })
  const rejected = expect(pending).rejects.toMatchObject({ status: 409 })
  await setImmediate()
  expect(client.getQueryData(mdczKeys.settings)).toEqual(previous)
  response.resolve(Response.json({ error: 'configuration changed' }, { status: 409 }))
  await rejected
  expect(client.getQueryData(mdczKeys.settings)).toEqual(previous)
  expect(client.getQueryData(configurationKey)).toEqual(advanced)
})
