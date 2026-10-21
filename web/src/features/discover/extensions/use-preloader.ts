import { useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { BrowseMoviesParams } from '@/api/discover'
import {
  onBrowseReset,
  readBrowsePage,
  readBrowseResources,
  seedBrowseMagnets,
  useBrowseSettings
} from '@/api/browse-cache'
import { BrowsePreloader } from './preloader'
import { discoverKeys } from '@/api/movie-detail-cache'

export function useBrowsePreloader(
  params: BrowseMoviesParams,
  query: string | undefined,
  page: number,
  subtitlesOnly: boolean
) {
  const client = useQueryClient()
  const settings = useBrowseSettings()
  const [generation, setGeneration] = useState(0)
  const key = JSON.stringify({ params: { ...params, page: undefined, limit: 20 }, generation })
  const controller = useMemo(() => {
    const { params: values }: { params: BrowseMoviesParams } = JSON.parse(key)
    return new BrowsePreloader(
      {
        page: async (page, signal, preview) => {
          const movies = await readBrowsePage(values, query, page, signal, preview)
          client.setQueryData(
            query === undefined
              ? discoverKeys.movies({ ...values, page, limit: 20 })
              : discoverKeys.search({ query, page, limit: 20 }),
            movies
          )
          return movies
        },
        resources: async (id, signal, cached) => {
          const data = await readBrowseResources(id, signal, cached)
          seedBrowseMagnets(
            client,
            id,
            data,
            client.getQueryData<{ pages: number }>(['browse-extension', 'settings'])?.pages ?? 6
          )
          return data
        }
      },
      subtitlesOnly
    )
  }, [key, query, subtitlesOnly, client])
  const state = useSyncExternalStore(
    controller.subscribe,
    controller.getSnapshot,
    controller.getSnapshot
  )
  useEffect(
    () =>
      onBrowseReset(() => {
        controller.pause()
        setGeneration(value => value + 1)
      }),
    [controller]
  )
  useEffect(() => controller.attach(), [controller])
  useEffect(() => {
    if (!settings.data) return
    controller.expireResources(settings.data.magnet_minutes * 60_000)
    controller.setTarget(page, settings.data.pages)
    const timer = setInterval(() => {
      if (
        document.visibilityState === 'visible' &&
        controller.expireResources(settings.data.magnet_minutes * 60_000)
      )
        controller.setTarget(page, settings.data.pages)
    }, 60_000)
    return () => clearInterval(timer)
  }, [controller, page, settings.data])
  return { state, controller, settings }
}
