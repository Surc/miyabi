import { queryOptions, useQuery } from '@tanstack/react-query'
import { apiGet } from './client'

export type EmbyPlaybackTarget = {
  status: 'ready' | 'disabled' | 'not_exported' | 'not_found'
  url?: string
}
export const embyPlaybackKeys = {
  all: ['emby-playback'] as const,
  movie: (code: string) => ['emby-playback', code] as const
}
export const EMBY_PLAYBACK_TTL = 30_000

// Only the open detail requests a target. The actual launch is a normal anchor
// click after resolution, retaining the browser's user gesture for app intents.
export function useEmbyPlayback(code: string) {
  return useQuery(embyPlaybackOptions(code))
}

export function embyPlaybackOptions(code: string) {
  return queryOptions({
    queryKey: embyPlaybackKeys.movie(code),
    queryFn: ({ signal }) =>
      apiGet<EmbyPlaybackTarget>('/api/extensions/emby/playback', { code }, signal),
    enabled: !!code.trim(),
    staleTime: EMBY_PLAYBACK_TTL,
    refetchInterval: EMBY_PLAYBACK_TTL,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: 'always',
    refetchOnReconnect: 'always',
    retry: false
  })
}

// Check the clock again on click: background tabs can delay stale timers.
export function isEmbyPlaybackFresh(updatedAt: number): boolean {
  const age = Date.now() - updatedAt
  return updatedAt > 0 && age >= 0 && age < EMBY_PLAYBACK_TTL
}

export function isEmbyPlayURL(value?: string): value is string {
  return /^emby:\/\/play\/[A-Za-z0-9_-]{1,200}\/[A-Za-z0-9_-]{1,200}$/.test(value ?? '')
}
