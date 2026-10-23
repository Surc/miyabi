import { beforeEach, expect, onTestFinished, test, vi } from 'vitest'
import { focusManager, QueryClient, QueryObserver } from '@tanstack/react-query'
import { EMBY_PLAYBACK_TTL, embyPlaybackOptions } from '@/api/emby-playback'

vi.hoisted(() => vi.stubGlobal('window', {}))
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('window', {
    location: { origin: 'http://localhost' },
    addEventListener: vi.fn(),
    removeEventListener: vi.fn()
  })
  focusManager.setFocused(true)
})

function observePlayback(code = 'ABC-123') {
  // The app disables global focus refetching; this extension must override it.
  const client = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false } } })
  client.mount()
  const observer = new QueryObserver(client, embyPlaybackOptions(code))
  const stop = observer.subscribe(() => {})
  onTestFinished(() => {
    stop()
    client.unmount()
    client.clear()
    focusManager.setFocused(undefined)
  })
  return { observer, stop }
}

test('an open detail refreshes a changed target and disables the retained URL while fetching', async () => {
  let finish!: (response: Response) => void
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(Response.json({ status: 'ready', url: 'emby://play/old/item' }))
    .mockImplementationOnce(() => new Promise<Response>(resolve => (finish = resolve)))
  vi.stubGlobal('fetch', fetch)
  const { observer } = observePlayback()
  await vi.advanceTimersByTimeAsync(0)
  expect(observer.getCurrentResult().data?.url).toBe('emby://play/old/item')
  expect(fetch.mock.calls[0]![0]).toBe('/api/extensions/emby/playback?code=ABC-123')
  await vi.advanceTimersByTimeAsync(EMBY_PLAYBACK_TTL + 1)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(observer.getCurrentResult().isFetching).toBe(true)
  expect(observer.getCurrentResult().isStale).toBe(true)
  finish(Response.json({ status: 'ready', url: 'emby://play/new/item' }))
  await vi.advanceTimersByTimeAsync(0)
  expect(observer.getCurrentResult().data?.url).toBe('emby://play/new/item')
  expect(observer.getCurrentResult().isStale).toBe(false)
})

test('background tabs stop polling and returning to the detail rechecks even a fresh result', async () => {
  const fetch = vi
    .fn()
    .mockImplementation(() => Promise.resolve(Response.json({ status: 'not_found' })))
  vi.stubGlobal('fetch', fetch)
  observePlayback()
  await vi.advanceTimersByTimeAsync(0)
  focusManager.setFocused(false)
  await vi.advanceTimersByTimeAsync(2 * EMBY_PLAYBACK_TTL)
  expect(fetch).toHaveBeenCalledTimes(1)
  focusManager.setFocused(true)
  await vi.advanceTimersByTimeAsync(0)
  expect(fetch).toHaveBeenCalledTimes(2)
  focusManager.setFocused(false)
  focusManager.setFocused(true)
  await vi.advanceTimersByTimeAsync(0)
  expect(fetch).toHaveBeenCalledTimes(3)
})

test('a scan is detected automatically and a subsequent server failure remains an error', async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(Response.json({ status: 'not_found' }))
    .mockResolvedValueOnce(Response.json({ status: 'ready', url: 'emby://play/server/item' }))
    .mockResolvedValueOnce(Response.json({ error: 'upstream unavailable' }, { status: 502 }))
  vi.stubGlobal('fetch', fetch)
  const { observer } = observePlayback()
  await vi.advanceTimersByTimeAsync(0)
  expect(observer.getCurrentResult().data?.status).toBe('not_found')
  await vi.advanceTimersByTimeAsync(EMBY_PLAYBACK_TTL)
  expect(observer.getCurrentResult().data?.status).toBe('ready')
  await vi.advanceTimersByTimeAsync(EMBY_PLAYBACK_TTL)
  expect(observer.getCurrentResult().isError).toBe(true)
  expect(observer.getCurrentResult().error?.message).toBe('upstream unavailable')
  // Query keeps the last answer on error; the component must reject it.
  expect(observer.getCurrentResult().data?.url).toBe('emby://play/server/item')
})

test('closing the detail aborts its pending request and removes the refresh timer', async () => {
  const fetch = vi.fn(
    (_url: string, init: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError'))
        )
      })
  )
  vi.stubGlobal('fetch', fetch)
  const { stop } = observePlayback()
  const signal = fetch.mock.calls[0]![1].signal!
  expect(signal.aborted).toBe(false)
  stop()
  expect(signal.aborted).toBe(true)
  await vi.advanceTimersByTimeAsync(3 * EMBY_PLAYBACK_TTL)
  expect(fetch).toHaveBeenCalledTimes(1)
})

test('an empty movie code never starts background requests', async () => {
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  observePlayback(' ')
  await vi.advanceTimersByTimeAsync(2 * EMBY_PLAYBACK_TTL)
  expect(fetch).not.toHaveBeenCalled()
})
