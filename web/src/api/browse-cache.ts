import { useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPut } from './client'
import type {
  BrowseMoviesParams,
  DiscoverMagnet,
  DiscoverMovie,
  DiscoverMovieDetail
} from './discover'
import type { ExternalRating } from './movie-extras'
import { discoverKeys, findCachedMovieCard } from './movie-detail-cache'
import { RequestSlots } from '@/features/discover/extensions/request-slots'

export type Cached<T> = { data: T; updated_at: string; stale: boolean; refresh_failed?: boolean }
export type ResourceSnapshot = {
  movie: DiscoverMovieDetail
  magnets: DiscoverMagnet[]
  complete: boolean
  has_subtitle: boolean
  has_cracked: boolean
}
export type ResourceStatus = Omit<ResourceSnapshot, 'movie' | 'magnets'> & { magnet_count: number }
export type BrowseSettings = {
  pages: number
  rating_hours: number
  magnet_minutes: number
  list_minutes: number
}
export type CacheStats = { movies: number; images: number; bytes: number }
export const defaultBrowseSettings: BrowseSettings = {
  pages: 6,
  rating_hours: 12,
  magnet_minutes: 30,
  list_minutes: 5
}
export const browseKeys = {
  all: ['browse-extension'] as const,
  settings: ['browse-extension', 'settings'] as const,
  stats: ['browse-extension', 'stats'] as const
}
const base = '/api/extensions/browse'
export const browseImage = (id: string) => `${base}/movies/${encodeURIComponent(id)}/image`
export function useBrowseSettings() {
  return useQuery({
    queryKey: browseKeys.settings,
    queryFn: ({ signal }) => apiGet<BrowseSettings>(`${base}/settings`, undefined, signal),
    staleTime: 60_000
  })
}
export function useBrowseCacheStats(enabled: boolean) {
  return useQuery({
    queryKey: browseKeys.stats,
    queryFn: ({ signal }) => apiGet<CacheStats>(`${base}/cache`, undefined, signal),
    enabled,
    staleTime: 0
  })
}
export const saveBrowseSettings = (settings: BrowseSettings) =>
  apiPut<BrowseSettings>(`${base}/settings`, settings)
export const clearBrowseCache = (scope: string) =>
  apiDelete<CacheStats>(`${base}/cache?scope=${encodeURIComponent(scope)}`)

const resetListeners = new Set<() => void>()
export function onBrowseReset(listener: () => void) {
  resetListeners.add(listener)
  return () => {
    resetListeners.delete(listener)
  }
}
export function resetBrowseSessions() {
  for (const listener of resetListeners) listener()
}

export async function readBrowsePage(
  params: BrowseMoviesParams,
  query: string | undefined,
  page: number,
  signal: AbortSignal,
  preview: (items: DiscoverMovie[]) => void
) {
  const values =
    query === undefined
      ? {
          zone: params.zone,
          entity_type: params.entityType,
          entity_id: params.entityID,
          main: params.main,
          tag_id: params.tagIds,
          year: params.year,
          month: params.month,
          sort: params.sort,
          order: params.order,
          page,
          limit: 20
        }
      : { q: query, page, limit: 20 }
  const path = `${base}/${query === undefined ? 'movies' : 'search'}`
  let result = await apiGet<Cached<DiscoverMovie[]>>(path, values, signal)
  if (result.stale) {
    preview(result.data)
    result = await apiGet<Cached<DiscoverMovie[]>>(path, { ...values, refresh: 1 }, signal)
  }
  if (result.stale) throw new Error('列表更新失败，已有缓存已保留，请重试')
  return result.data
}
export async function readBrowseResources(
  id: string,
  signal: AbortSignal,
  cached: (value: Cached<ResourceSnapshot>) => void
) {
  const path = `${base}/movies/${encodeURIComponent(id)}/resources`
  let result = await apiGet<Cached<ResourceSnapshot>>(path, undefined, signal)
  if (result.stale) {
    cached(result)
    result = await apiGet<Cached<ResourceSnapshot>>(path, { refresh: 1 }, signal)
  }
  return result
}

// Ratings are optional and only requested by mounted cards. JavLibrary gets a
// separate small queue so an unavailable rating site cannot block resource fill.
const librarySlots = new RequestSlots(2)
export function useBrowseRating(id: string, source: 'javdb' | 'javlibrary') {
  const client = useQueryClient()
  const settings = useBrowseSettings()
  const key = [...browseKeys.all, 'rating', source, id]
  return useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const request = async () => {
        if (signal.aborted) throw new DOMException('Canceled', 'AbortError')
        const path = `${base}/movies/${encodeURIComponent(id)}/ratings/${source}`
        let result = await apiGet<Cached<ExternalRating>>(path, undefined, signal)
        if (result.stale) {
          client.setQueryData(key, result)
          result = await apiGet<Cached<ExternalRating>>(path, { refresh: 1 }, signal)
        }
        return result
      }
      return source === 'javlibrary' ? librarySlots.run(signal, request) : request()
    },
    staleTime: 60_000,
    refetchInterval: Math.min((settings.data?.rating_hours ?? 12) * 3_600_000, 300_000),
    retry: false
  })
}
const snapshotAccess = new WeakMap<object, number>()
let snapshotSequence = 0
export function seedBrowseMagnets(
  client: ReturnType<typeof useQueryClient>,
  id: string,
  result: Cached<ResourceSnapshot>,
  pages = 6
) {
  if (!result.stale && result.data.complete) {
    const base = findCachedMovieCard(client, id)
    client.setQueryData(discoverKeys.magnets(id), result.data.magnets, {
      updatedAt: new Date(result.updated_at).getTime()
    })
    if (base)
      client.setQueryData(
        discoverKeys.movie(id),
        {
          ...base,
          ...result.data.movie,
          rating: base.rating,
          rating_source: base.rating_source,
          rating_max: base.rating_max,
          release_status: base.release_status,
          has_subtitle: result.data.has_subtitle,
          magnets_count: result.data.magnets.length
        },
        { updatedAt: new Date(result.updated_at).getTime() }
      )
    for (const key of [discoverKeys.movie(id), discoverKeys.magnets(id)]) {
      const query = client.getQueryCache().find({ queryKey: key, exact: true })
      if (query) snapshotAccess.set(query, ++snapshotSequence)
    }
    // Rank by use, not source timestamps: restored snapshots may share an old
    // update time. Only extension-owned idle entries participate in eviction.
    const idle = client
      .getQueryCache()
      .findAll({ queryKey: discoverKeys.all })
      .filter(query => snapshotAccess.has(query) && !query.getObserversCount())
      .sort((a, b) => (snapshotAccess.get(b) ?? 0) - (snapshotAccess.get(a) ?? 0))
    for (const query of idle.slice(pages * 20 * 4))
      client.removeQueries({ queryKey: query.queryKey, exact: true })
  }
}
