import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut } from './client'
import { mdczKeys } from './mdcz'

export type SecretName =
  | 'proxy_url'
  | 'javdb_cookie'
  | 'javbus_cookie'
  | 'fantia_cookie'
  | 'deepl_key'
  | 'baidu_secret'
  | 'baidu_key'
  | 'llm_key'
export type ConfigurationValues = {
  sites: string[]
  direct_sites: string[]
  use_proxy: boolean
  timeout: number
  retries: number
  javdb_url: string
  javbus_url: string
  translation_engine: 'google' | 'deepl' | 'baidu' | 'openai'
  target_language: 'zh-CN' | 'zh-TW'
  baidu_service: 'general' | 'llm'
  baidu_app_id: string
  llm_base_url: string
  llm_model: string
  llm_api_format: 'responses' | 'chat-completions'
  llm_service_type: 'openai-compatible' | 'google' | 'deepseek'
}
export type Configuration = {
  configuration_version: number
  configuration_id: string
  values: ConfigurationValues
  secrets: Record<SecretName, boolean>
  available_sites: string[]
}
export type ConfigurationUpdate = Pick<
  Configuration,
  'configuration_id' | 'configuration_version'
> & {
  values: Partial<ConfigurationValues> & Partial<Record<SecretName, string>>
}
export const configurationKey = ['extensions', 'mdcz', 'configuration'] as const
export function useMDCzConfiguration(enabled: boolean) {
  return useQuery({
    queryKey: configurationKey,
    queryFn: ({ signal }) =>
      apiGet<Configuration>('/api/extensions/mdcz/configuration', undefined, signal),
    enabled,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false
  })
}
export function useUpdateMDCzConfiguration() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (input: ConfigurationUpdate) =>
      apiPut<Configuration>('/api/extensions/mdcz/configuration', input),
    onSuccess: async next => {
      // Cancel a read started before the save so it cannot restore old values.
      await client.cancelQueries({ queryKey: configurationKey })
      client.setQueryData(configurationKey, next)
      await client.cancelQueries({ queryKey: mdczKeys.synopsis })
      void client.resetQueries({ queryKey: mdczKeys.synopsis })
    }
  })
}
