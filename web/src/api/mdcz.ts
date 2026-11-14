import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiPut } from './client'

export type MDCzSettings = {
  enabled: boolean
  managed?: boolean
  mode?: 'managed' | 'external'
  status?: 'configured' | 'unconfigured' | 'unavailable'
  generation?: string
  web_port?: number
  server_url: string
  has_api_key: boolean
  translate: boolean
  badges: boolean
  position: 'topLeft' | 'topRight' | 'bottomLeft' | 'bottomRight'
}
export type MDCzUpdate = MDCzSettings & { api_key?: string }
export const mdczKeys = {
  settings: ['extensions', 'mdcz', 'settings'] as const,
  synopsis: ['extensions', 'mdcz', 'synopsis'] as const
}
export function useMDCzSettings() {
  return useQuery({
    queryKey: mdczKeys.settings,
    queryFn: ({ signal }) =>
      apiGet<MDCzSettings>('/api/extensions/mdcz/settings', undefined, signal)
  })
}
export function updateMDCzOptions(client: QueryClient) {
  return {
    mutationFn: (value: MDCzUpdate) => apiPut<MDCzSettings>('/api/extensions/mdcz/settings', value),
    onMutate: () => client.cancelQueries({ queryKey: mdczKeys.settings }),
    onSuccess: async (value: MDCzSettings) => {
      // A different bridge must not inherit the previous bridge's editable form.
      await client.cancelQueries({ queryKey: ['extensions', 'mdcz', 'configuration'] })
      client.removeQueries({ queryKey: ['extensions', 'mdcz', 'configuration'] })
      client.setQueryData(mdczKeys.settings, value)
      await client.cancelQueries({ queryKey: mdczKeys.synopsis })
      // Refresh open details without holding the settings form for a scrape.
      void client.resetQueries({ queryKey: mdczKeys.synopsis })
    }
  }
}
export function useUpdateMDCz() {
  return useMutation(updateMDCzOptions(useQueryClient()))
}
export function useTestMDCz() {
  return useMutation({
    mutationFn: (value: MDCzUpdate) =>
      apiPost<{ protocol: number; revision: string }>('/api/extensions/mdcz/test', value)
  })
}
export function useMDCzSynopsis(code: string) {
  return useQuery({
    queryKey: [...mdczKeys.synopsis, code],
    queryFn: ({ signal }) =>
      apiGet<{ enabled: boolean; plot: string }>('/api/extensions/mdcz/synopsis', { code }, signal),
    enabled: !!code.trim(),
    staleTime: 5 * 60_000,
    retry: false
  })
}
