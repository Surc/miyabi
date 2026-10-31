import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'

import { apiGet, apiPut } from './client'

export const TOP_PAGE_SIZE = 48

export type TopBoard = { id: string; name: string; count: number }
export type TopInfo = {
  updated_at: string
  source_url: string
  annual_source_url: string
  boards: TopBoard[]
}
export type TopRanking = { board_id: string; name: string; rank: number }
export type TopMovie = {
  code: string
  title: string
  release_date: string
  cover: string
  rank: number
}
export type TopMovies = { board: TopBoard; total: number; items: TopMovie[] }

export type TopConfig = {
  show_navigation: boolean
  show_rankings: boolean
  default_board: string
}

export const topSettingsKey = ['settings', 'top'] as const

export function useTopConfig() {
  return useQuery({
    queryKey: topSettingsKey,
    queryFn: ({ signal }) => apiGet<TopConfig>('/api/settings/top', undefined, signal),
    staleTime: 15_000,
    refetchOnWindowFocus: true
  })
}

export function updateTopConfigOptions(client: QueryClient) {
  return {
    mutationFn: (config: TopConfig) => apiPut<TopConfig>('/api/settings/top', config),
    onMutate: () => client.cancelQueries({ queryKey: topSettingsKey }),
    onSuccess: (config: TopConfig) => client.setQueryData(topSettingsKey, config),
    onError: () => {
      void client.invalidateQueries({ queryKey: topSettingsKey })
    }
  }
}

export function useUpdateTopConfig() {
  return useMutation(updateTopConfigOptions(useQueryClient()))
}

export function useTopInfo() {
  return useQuery({
    queryKey: ['top', 'info'],
    queryFn: ({ signal }) => apiGet<TopInfo>('/api/top', undefined, signal),
    staleTime: Infinity
  })
}

export function useTopMovies(board: string, page: number) {
  return useQuery({
    queryKey: ['top', 'movies', board, page],
    queryFn: ({ signal }) =>
      apiGet<TopMovies>('/api/top/movies', { board, page, limit: TOP_PAGE_SIZE }, signal),
    staleTime: Infinity
  })
}

export function useMovieRankings(code: string) {
  const normalized = code.trim().toUpperCase()
  return useQuery({
    queryKey: ['top', 'rankings', normalized],
    queryFn: ({ signal }) =>
      apiGet<TopRanking[]>('/api/top/rankings', { code: normalized }, signal),
    enabled: normalized.length > 0,
    staleTime: Infinity
  })
}
