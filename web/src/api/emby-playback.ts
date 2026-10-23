import { useQuery } from '@tanstack/react-query'
import { apiGet } from './client'

export type EmbyPlaybackTarget = {
  status: 'ready' | 'disabled' | 'not_exported' | 'not_found'
  url?: string
}
export const embyPlaybackKeys = {
  all: ['emby-playback'] as const,
  movie: (code: string) => ['emby-playback', code] as const
}

// Only the open detail requests a target. The actual launch is a normal anchor
// click after resolution, retaining the browser's user gesture for app intents.
export function useEmbyPlayback(code: string) {
  return useQuery({
    queryKey: embyPlaybackKeys.movie(code),
    queryFn: ({ signal }) =>
      apiGet<EmbyPlaybackTarget>('/api/extensions/emby/playback', { code }, signal),
    enabled: !!code.trim(),
    staleTime: 30_000,
    retry: false
  })
}

export function isEmbyPlayURL(value?: string): value is string {
  return /^emby:\/\/play\/[A-Za-z0-9_-]{1,200}\/[A-Za-z0-9_-]{1,200}$/.test(value ?? '')
}
