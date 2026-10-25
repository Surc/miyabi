import { expect, test } from 'vitest'
import { BrowsePreloader } from '@/features/discover/extensions/preloader'
import { RequestSlots } from '@/features/discover/extensions/request-slots'
import type { DiscoverMovie, DiscoverMovieDetail } from '@/api/discover'
import type { Cached, ResourceSnapshot } from '@/api/browse-cache'

function movie(id: number): DiscoverMovie {
  return {
    id: String(id),
    code: `DEMO-${id}`,
    title: 'Title',
    cover: 'https://example.com/image.jpg',
    release_status: 'released'
  } as DiscoverMovie
}
function resources(id: string, subtitle = true): Cached<ResourceSnapshot> {
  return {
    data: {
      movie: movie(Number(id)) as DiscoverMovieDetail,
      magnets: [],
      complete: true,
      has_subtitle: subtitle,
      has_cracked: false
    },
    updated_at: '2026-10-21T00:00:00Z',
    stale: false
  }
}
test('fills six FILTERED pages by reading beyond six source pages, with at most two resource requests', async () => {
  let pages = 0,
    active = 0,
    maximum = 0
  const loader = new BrowsePreloader(
    {
      page: async page => {
        pages++
        return Array.from({ length: 20 }, (_, index) => movie((page - 1) * 20 + index + 1))
      },
      resources: async id => {
        active++
        maximum = Math.max(maximum, active)
        await Promise.resolve()
        active--
        return resources(id, Number(id) % 4 === 0)
      }
    },
    true
  )
  await loader.start()
  const state = loader.getSnapshot()
  expect(state.phase).toBe('complete')
  expect(state.ready).toBe(120)
  expect(state.rows).toHaveLength(120)
  expect(pages).toBe(24)
  expect(maximum).toBe(2)
  expect(state.rows.slice(0, 3).map(item => item.movie.id)).toEqual(['4', '8', '12'])
})
test('configurable targets refill only when entering the next display batch', async () => {
  const read: number[] = []
  const loader = new BrowsePreloader(
    {
      page: async page => {
        read.push(page)
        return Array.from({ length: 20 }, (_, i) => movie((page - 1) * 20 + i + 1))
      },
      resources: async id => resources(id)
    },
    false
  )
  loader.setTarget(1, 2)
  await loader.start()
  expect(read).toEqual([1, 2])
  loader.setTarget(2, 2)
  await loader.start()
  expect(read).toEqual([1, 2])
  loader.setTarget(3, 2)
  await loader.start()
  expect(read).toEqual([1, 2, 3, 4])
  expect(loader.getSnapshot().ready).toBe(80)
})
test('a short nonempty source page is not EOF; only a successful empty page ends the scan', async () => {
  const read: number[] = []
  const loader = new BrowsePreloader(
    {
      page: async page => {
        read.push(page)
        return page <= 3 ? Array.from({ length: 5 }, (_, i) => movie(page * 10 + i)) : []
      },
      resources: async id => resources(id)
    },
    false
  )
  loader.setTarget(1, 1)
  await loader.start()
  expect(read).toEqual([1, 2, 3, 4])
  expect(loader.getSnapshot()).toMatchObject({ phase: 'complete', ready: 15, exhausted: true })
})
test('partial, stale and failed resources stay retryable without advancing or losing works', async () => {
  for (const mode of ['partial', 'stale', 'error']) {
    let fail = true,
      pages = 0
    const loader = new BrowsePreloader(
      {
        page: async () => {
          pages++
          return Array.from({ length: 20 }, (_, i) => movie(i + 1))
        },
        resources: async id => {
          const item = resources(id)
          if (id === '1' && fail) {
            if (mode === 'error') throw new Error('unavailable')
            item.stale = true
            item.data.complete = mode !== 'partial'
          }
          return item
        }
      },
      true
    )
    loader.setTarget(1, 1)
    await loader.start()
    expect(loader.getSnapshot()).toMatchObject({ phase: 'error', ready: 19, exhausted: false })
    fail = false
    await loader.resume()
    expect(loader.getSnapshot()).toMatchObject({ phase: 'complete', ready: 20 })
    expect(pages).toBe(1)
    expect(new Set(loader.getSnapshot().rows.map(row => row.movie.id)).size).toBe(20)
  }
})
test('a repeated upstream page reports pagination failure rather than looping forever', async () => {
  const loader = new BrowsePreloader(
    { page: async () => [movie(1)], resources: async id => resources(id) },
    false
  )
  await loader.start()
  expect(loader.getSnapshot()).toMatchObject({ phase: 'error', exhausted: false, ready: 1 })
  expect(loader.getSnapshot().error).toContain('重复页')
})
test('negative subtitle results are rechecked after TTL even when the source is exhausted', async () => {
  let subtitled = false
  let reads = 0
  const loader = new BrowsePreloader(
    {
      page: async page => (page === 1 ? [movie(1), movie(2)] : []),
      resources: async id => {
        reads++
        return resources(id, subtitled)
      }
    },
    true
  )
  await loader.start()
  expect(loader.getSnapshot()).toMatchObject({ exhausted: true, ready: 0, rows: [] })
  subtitled = true
  expect(loader.expireResources(1000, Date.parse('2026-10-21T00:00:02Z'))).toBe(true)
  await loader.start()
  expect(reads).toBe(4)
  expect(loader.getSnapshot().rows.map(row => row.movie.id)).toEqual(['1', '2'])
  expect(loader.getSnapshot().ready).toBe(2)
})
test('pause cancels active work and changing the target does not silently resume it', async () => {
  let waiting = true
  const loader = new BrowsePreloader(
    {
      page: async () => Array.from({ length: 20 }, (_, i) => movie(i + 1)),
      resources: async (id, signal) => {
        if (waiting)
          await new Promise<void>((_, reject) =>
            signal.addEventListener(
              'abort',
              () => reject(new DOMException('Canceled', 'AbortError')),
              { once: true }
            )
          )
        return resources(id)
      }
    },
    false
  )
  loader.setTarget(1, 1)
  const running = loader.start()
  await Promise.resolve()
  loader.pause()
  await running
  loader.setTarget(1, 1)
  expect(loader.getSnapshot().phase).toBe('paused')
  waiting = false
  await loader.resume()
  expect(loader.getSnapshot().phase).toBe('complete')
})
test('StrictMode effect reacquisition does not cancel the mounted preloader', async () => {
  const loader = new BrowsePreloader(
    { page: async () => [], resources: async id => resources(id) },
    false
  )
  const release = loader.attach()
  release()
  const releaseAgain = loader.attach()
  loader.setTarget(1, 1)
  await Promise.resolve()
  await loader.start()
  expect(loader.getSnapshot().phase).toBe('complete')
  releaseAgain()
})
test('rating slots enforce the limit and remove canceled queued requests', async () => {
  const slots = new RequestSlots(1)
  let finish!: () => void
  const order: number[] = []
  const first = slots.run(new AbortController().signal, async () => {
    order.push(1)
    await new Promise<void>(resolve => {
      finish = resolve
    })
  })
  const abort = new AbortController()
  const canceled = slots
    .run(abort.signal, async () => {
      order.push(2)
    })
    .catch(error => error.name)
  const third = slots.run(new AbortController().signal, async () => {
    order.push(3)
  })
  await Promise.resolve()
  abort.abort()
  finish()
  await Promise.all([first, third])
  expect(await canceled).toBe('AbortError')
  expect(order).toEqual([1, 3])
  const pool = new RequestSlots(2)
  let active = 0,
    max = 0
  await Promise.all(
    Array.from({ length: 30 }, () =>
      pool.run(new AbortController().signal, async () => {
        active++
        max = Math.max(max, active)
        await Promise.resolve()
        active--
      })
    )
  )
  expect(max).toBe(2)
})
test('expired resources keep last-known rows but must be revalidated before counting ready', async () => {
  let calls = 0
  const loader = new BrowsePreloader(
    {
      page: async () => Array.from({ length: 20 }, (_, i) => movie(i + 1)),
      resources: async id => {
        calls++
        const result = resources(id)
        result.updated_at = '2026-10-21T01:00:00Z'
        return result
      }
    },
    false
  )
  loader.setTarget(1, 1)
  await loader.start()
  expect(loader.expireResources(30 * 60_000, Date.parse('2026-10-21T01:29:00Z'))).toBe(false)
  expect(loader.expireResources(30 * 60_000, Date.parse('2026-10-21T01:31:00Z'))).toBe(true)
  expect(loader.getSnapshot().rows).toHaveLength(20)
  expect(loader.getSnapshot().ready).toBe(0)
  await loader.start()
  expect(calls).toBe(40)
  expect(loader.getSnapshot().phase).toBe('complete')
})
