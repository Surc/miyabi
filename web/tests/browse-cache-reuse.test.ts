import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { expect, test } from 'vitest'
import { seedBrowseMagnets, type Cached, type ResourceSnapshot } from '@/api/browse-cache'
import { discoverKeys } from '@/api/movie-detail-cache'
import type { DiscoverMovieDetail } from '@/api/discover'

function movie(id: string): DiscoverMovieDetail {
  return {
    id,
    code: `DEMO-${id}`,
    title: 'Title',
    cover: 'https://example.com/image.jpg',
    rating: 4.2,
    rating_source: 'javdb',
    rating_max: 5,
    release_status: 'released',
    origin_title: '',
    release_date: '2026-10-21',
    duration: 0,
    thumbnail: '',
    preview_images: [],
    preview_video: '',
    magnets_count: 0,
    has_subtitle: false,
    has_preview: false,
    actors: [],
    tags: [],
    zone: 'censored',
    actor_movies: [],
    related_movies: []
  }
}
test('preloaded snapshots reuse original detail keys and bound idle memory without evicting mounted details', () => {
  const client = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } })
  const observer = new QueryObserver(client, {
    queryKey: discoverKeys.movie('mounted'),
    initialData: movie('mounted'),
    enabled: false
  })
  const unsubscribe = observer.subscribe(() => {})
  const movies = Array.from({ length: 60 }, (_, index) => movie(String(index + 1)))
  client.setQueryData(discoverKeys.movies({ page: 1 }), movies)
  for (const item of movies) {
    const result: Cached<ResourceSnapshot> = {
      data: {
        movie: { ...item, rating: 0 },
        magnets: [
          {
            hash: item.id,
            name: 'Title',
            size: 1,
            has_subtitle: true,
            hd: true,
            files_count: 1,
            created_at: '',
            uri: 'magnet:test'
          }
        ],
        complete: true,
        has_subtitle: true,
        has_cracked: false
      },
      updated_at: '2026-10-21T00:00:00Z',
      stale: false
    }
    seedBrowseMagnets(client, item.id, result, 1)
  }
  const details = client
    .getQueryCache()
    .findAll({ queryKey: discoverKeys.all })
    .filter(query => query.queryKey[1] === 'movie')
  expect(details.filter(query => !query.getObserversCount()).length).toBeLessThanOrEqual(80)
  expect(client.getQueryData(discoverKeys.movie('mounted'))).toBeDefined()
  const cached = client.getQueryData<DiscoverMovieDetail>(discoverKeys.movie('60'))
  expect(cached?.rating).toBe(4.2)
  expect(cached?.has_subtitle).toBe(true)
  expect(client.getQueryData(discoverKeys.magnets('60'))).toHaveLength(1)
  unsubscribe()
  client.clear()
})
