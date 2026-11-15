import {
  infiniteQueryOptions,
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData
} from '@tanstack/react-query'
import { ApiError, apiGet, apiPost } from './client'
import { readCachedRating, type Cached } from './browse-cache'

export type CommentSource = 'javdb' | 'javlibrary'
export type ExternalRating = { source: string; found: boolean; score: number | null; max: number }
export type MovieComment = {
  id: string
  author: string
  content: string
  created_at: string
  rating?: number
  rating_max?: number
  likes?: number
}
export type MovieCommentsPage = {
  source: CommentSource
  page: number
  has_more: boolean
  items: MovieComment[]
  refreshError?: Error
}

const retry = (count: number, error: Error) =>
  count < 1 && (!(error instanceof ApiError) || error.status >= 500)

// Independent keys keep upstream catalogue metadata and saved ratings untouched.
export function useJavLibraryRating(code: string) {
  const client = useQueryClient()
  const normalized = code.trim().toUpperCase()
  const key = ['movie-extras', 'javlibrary-rating', normalized]
  return useQuery({
    queryKey: key,
    queryFn: ({ signal }) =>
      readCachedRating(
        '/api/extensions/browse/ratings/javlibrary',
        { code: normalized },
        signal,
        result => client.setQueryData(key, result)
      ),
    select: result => result.data,
    enabled: !!normalized,
    staleTime: 60_000,
    refetchInterval: 300_000,
    retry: false
  })
}

export function requestJavLibraryVerification(action: 'open' | 'confirm') {
  return apiPost<{ ok: true }>(`/api/extensions/movie/javlibrary-browser/${action}`)
}

export function useMovieComments(source: CommentSource, javdbId: string, code: string) {
  return useInfiniteQuery(movieCommentsOptions(source, javdbId, code))
}

export function movieCommentsOptions(source: CommentSource, javdbId: string, code: string) {
  const normalized = code.trim().toUpperCase()
  const identity = source === 'javdb' ? javdbId : normalized
  const key = ['movie-extras', 'comments', source, identity] as const
  const path = '/api/extensions/browse/comments'
  return infiniteQueryOptions({
    queryKey: key,
    initialPageParam: 1,
    queryFn: async ({ client, pageParam, signal }): Promise<MovieCommentsPage> => {
      const params = {
        source,
        javdb_id: source === 'javdb' ? identity : undefined,
        code: source === 'javlibrary' ? identity : undefined,
        page: pageParam
      }
      const previous = await apiGet<Cached<MovieCommentsPage>>(path, params, signal)
      if (!previous.stale) return previous.data

      signal.throwIfAborted()
      client.setQueryData<InfiniteData<MovieCommentsPage, number>>(key, current => {
        const pages = [...(current?.pages ?? [])]
        const pageParams = [...(current?.pageParams ?? [])]
        const index = pageParams.indexOf(pageParam)
        if (index === -1) {
          pages.push(previous.data)
          pageParams.push(pageParam)
        } else {
          pages[index] = previous.data
        }
        return { pages, pageParams }
      })

      try {
        const refreshed = await apiGet<Cached<MovieCommentsPage>>(
          path,
          { ...params, refresh: 1 },
          signal
        )
        if (!refreshed.stale) return refreshed.data
        return { ...previous.data, refreshError: new Error('评论更新失败') }
      } catch (error) {
        signal.throwIfAborted()
        return {
          ...previous.data,
          refreshError: error instanceof Error ? error : new Error('评论更新失败')
        }
      }
    },
    enabled: !!identity,
    getNextPageParam: page => (page.has_more && page.page < 1000 ? page.page + 1 : undefined),
    staleTime: query => (query.state.data?.pages.some(page => page.refreshError) ? 0 : 2 * 60_000),
    retry
  })
}
