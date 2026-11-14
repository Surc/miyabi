import { useQuery } from '@tanstack/react-query'
import { apiGet } from './client'

export type PublicationStatus = { blocked: boolean; pending: number }
export function usePublicationStatus() {
  return useQuery({
    queryKey: ['extensions', 'publication', 'status'],
    queryFn: ({ signal }) =>
      apiGet<PublicationStatus>('/api/extensions/publication/status', undefined, signal),
    retry: false,
    // Writers outside task SSE can enter recovery protection after a healthy read.
    // Keep checking while this status is mounted, including after failed reads.
    refetchInterval: 10_000
  })
}
