import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { browseKeys, defaultBrowseSettings, useBrowseRating, type Cached } from '@/api/browse-cache'
import { discoverKeys } from '@/api/movie-detail-cache'
import type { ExternalRating } from '@/api/movie-extras'

const clients: QueryClient[] = []
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime('2026-10-27T16:00:00Z')
})
afterEach(() => {
  for (const client of clients.splice(0)) client.clear()
})

function setup(score: number | null, source: 'javdb' | 'javlibrary' = 'javdb') {
  const client = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } })
  clients.push(client)
  client.setQueryData(browseKeys.settings, defaultBrowseSettings)
  const key = [...browseKeys.all, 'rating', source, 'film']
  const cached: Cached<ExternalRating> = {
    data: { source, found: true, score, max: source === 'javdb' ? 5 : 10 },
    updated_at: new Date(Date.now() - 60_000).toISOString(),
    stale: false
  }
  client.setQueryData(key, cached)
  return { client, key }
}

function Rating({ source }: { source: 'javdb' | 'javlibrary' }) {
  const query = useBrowseRating('film', source)
  const rating = query.data?.data
  return <span>{`${rating?.source}:${rating?.score ?? 'missing'}:${query.isPending}`}</span>
}

function render(client: QueryClient, source: 'javdb' | 'javlibrary' = 'javdb') {
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <Rating source={source} />
    </QueryClientProvider>
  )
}

test('an existing empty card rating immediately follows a newly read detail score', () => {
  const { client } = setup(null)
  expect(render(client)).toContain('javdb:missing:false')
  client.setQueryData(discoverKeys.movie('film'), {
    rating: 4.6,
    rating_source: 'javdb',
    rating_max: 5
  })
  expect(render(client)).toContain('javdb:4.6:false')
  vi.advanceTimersByTime(1000)
  client.setQueryData(discoverKeys.movie('film'), {
    rating: 4.7,
    rating_source: 'javdb',
    rating_max: 5
  })
  expect(render(client)).toContain('javdb:4.7:false')
})

test('a known detail score initializes a card without a loading placeholder', () => {
  const { client, key } = setup(null)
  client.removeQueries({ queryKey: key, exact: true })
  client.setQueryData(discoverKeys.movie('film'), {
    rating: 4.6,
    rating_source: 'javdb',
    rating_max: 5
  })
  expect(render(client)).toContain('javdb:4.6:false')
  expect(client.getQueryData<Cached<ExternalRating>>(key)?.data.score).toBe(4.6)
})

test('newer rating responses are not replaced by an older detail', () => {
  const { client, key } = setup(4.8)
  client.setQueryData(discoverKeys.movie('film'), {
    rating: 4.2,
    rating_source: 'javdb',
    rating_max: 5
  })
  vi.advanceTimersByTime(1000)
  client.setQueryData<Cached<ExternalRating>>(key, previous => ({
    ...previous!,
    updated_at: new Date().toISOString()
  }))
  expect(render(client)).toContain('javdb:4.8:false')
})

test('an unrated detail does not erase a known rating or invent a zero score', () => {
  const { client, key } = setup(4.5)
  client.setQueryData(discoverKeys.movie('film'), { rating: 0, rating_source: 'javdb' })
  expect(render(client)).toContain('javdb:4.5:false')
  client.setQueryData<Cached<ExternalRating>>(key, previous => ({
    ...previous!,
    data: { ...previous!.data, score: null }
  }))
  expect(render(client)).toContain('javdb:missing:false')
})

test('JavLibrary never receives a JavDB detail score', () => {
  const { client } = setup(null, 'javlibrary')
  client.setQueryData(discoverKeys.movie('film'), {
    rating: 4.6,
    rating_source: 'javdb',
    rating_max: 5
  })
  expect(render(client, 'javlibrary')).toContain('javlibrary:missing:false')
})
