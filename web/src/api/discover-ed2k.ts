import { useInfiniteQuery } from '@tanstack/react-query'

import { apiGet } from './client'

type ED2KResource = {
  id: string
  name: string
  size: number
  uri: string
  created_at: string
}

type ED2KPage = {
  items: ED2KResource[]
  page: number
  has_more: boolean
}

export function useDiscoverED2K(movieID: string) {
  return useInfiniteQuery({
    queryKey: ['discover', 'movie', movieID, 'ed2k'],
    initialPageParam: 1,
    queryFn: ({ pageParam, signal }) =>
      apiGet<ED2KPage>(
        `/api/discover/movies/${encodeURIComponent(movieID)}/ed2k`,
        { page: pageParam },
        signal
      ),
    getNextPageParam: page => (page.has_more && page.page < 1000 ? page.page + 1 : undefined),
    staleTime: 2 * 60_000,
    retry: false
  })
}
