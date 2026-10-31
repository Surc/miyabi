import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'

import { apiGet, apiPost, apiPut } from '@/api/client'

export type SubtitleSource = 'xunlei' | 'subtitlecat' | 'avsubtitles'
export type SubtitleLanguage = 'zh-CN' | 'zh-TW'

export type SubtitleConfig = {
  enabled: boolean
  sources: Record<SubtitleSource, boolean>
  language: SubtitleLanguage
}

export type SubtitleTestResult = {
  code: string
  status: 'downloaded' | 'not_found' | 'failed'
  candidates: number
  source?: string
  name?: string
  language?: SubtitleLanguage
  format?: string
  bytes?: number
  elapsed_ms: number
  errors?: string[]
}

export const subtitleSettingsKey = ['settings', 'subtitles'] as const

export function useSubtitleConfig() {
  return useQuery({
    queryKey: subtitleSettingsKey,
    queryFn: ({ signal }) => apiGet<SubtitleConfig>('/api/settings/subtitles', undefined, signal),
    staleTime: 15_000
  })
}

export function updateSubtitleConfigOptions(client: QueryClient) {
  return {
    mutationFn: (config: SubtitleConfig) =>
      apiPut<SubtitleConfig>('/api/settings/subtitles', config),
    onMutate: async (config: SubtitleConfig) => {
      await client.cancelQueries({ queryKey: subtitleSettingsKey })
      const previous = client.getQueryData<SubtitleConfig>(subtitleSettingsKey)
      client.setQueryData(subtitleSettingsKey, config)
      return { previous }
    },
    onSuccess: (config: SubtitleConfig) => client.setQueryData(subtitleSettingsKey, config),
    onError: (
      _error: unknown,
      _config: SubtitleConfig,
      context: { previous: SubtitleConfig | undefined } | undefined
    ) => {
      if (context?.previous) client.setQueryData(subtitleSettingsKey, context.previous)
      void client.invalidateQueries({ queryKey: subtitleSettingsKey })
    }
  }
}

export function useUpdateSubtitleConfig() {
  return useMutation(updateSubtitleConfigOptions(useQueryClient()))
}

export function useTestSubtitleDownload() {
  return useMutation({
    mutationFn: (request: { code: string; config: SubtitleConfig }) =>
      apiPost<SubtitleTestResult>('/api/settings/subtitles/test', request)
  })
}
