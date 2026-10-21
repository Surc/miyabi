import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { ApiError, apiGet } from './client'

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
}

const retry = (count: number, error: Error) =>
  count < 1 && (!(error instanceof ApiError) || error.status >= 500)

// Independent keys keep upstream catalogue metadata and saved ratings untouched.
export function useJavLibraryRating(code: string) {
  return useQuery({
    queryKey: ['movie-extras', 'javlibrary-rating', code],
    queryFn: ({ signal }) =>
      apiGet<ExternalRating>('/api/extensions/movie/javlibrary-rating', { code }, signal),
    enabled: !!code.trim(),
    staleTime: 10 * 60_000,
    retry
  })
}

export function useMovieComments(source: CommentSource, javdbId: string, code: string) {
  return useInfiniteQuery({
    queryKey: ['movie-extras', 'comments', source, source === 'javdb' ? javdbId : code],
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) =>
      apiGet<MovieCommentsPage>(
        '/api/extensions/movie/comments',
        {
          source,
          javdb_id: source === 'javdb' ? javdbId : undefined,
          code: source === 'javlibrary' ? code : undefined,
          page: pageParam
        },
        signal
      ),
    enabled: source === 'javdb' ? !!javdbId : !!code.trim(),
    getNextPageParam: page => (page.has_more && page.page < 1000 ? page.page + 1 : undefined),
    staleTime: 2 * 60_000,
    retry
  })
}
