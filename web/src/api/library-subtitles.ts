import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { apiDelete, apiGet, apiPost, apiUpload } from './client'
import { libraryKeys } from './library'

export type LibrarySubtitleTrack = {
  id: string
  name: string
  language?: string
  format: string
  source: string
}

export type LibrarySubtitleVideo = {
  id: number
  name: string
  can_download: boolean
  hard_subtitled: boolean
  tracks: LibrarySubtitleTrack[]
}

type SubtitleAction =
  | { type: 'download'; video: number }
  | { type: 'add'; video: number; file: File; language: string }
  | { type: 'delete'; video: number; track: string }

const subtitlesPath = (id: number) => `/api/library/movies/${id}/subtitles`
const subtitlesKey = (id: number) => [...libraryKeys.detail(id), 'subtitles'] as const

export function useLibrarySubtitles(id: number) {
  return useQuery({
    queryKey: subtitlesKey(id),
    queryFn: ({ signal }) => apiGet<LibrarySubtitleVideo[]>(subtitlesPath(id), undefined, signal)
  })
}

export function useChangeLibrarySubtitles(id: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (action: SubtitleAction) => {
      const path = `${subtitlesPath(id)}/${action.video}`
      switch (action.type) {
        case 'download':
          return apiPost<{ downloaded: number }>(`${path}/download`)
        case 'add': {
          const query = new URLSearchParams({ name: action.file.name, language: action.language })
          await apiUpload(`${path}?${query}`, action.file, 'application/octet-stream')
          return undefined
        }
        case 'delete':
          await apiDelete(`${path}/${action.track}`)
          return undefined
      }
    },
    onSettled: () => client.invalidateQueries({ queryKey: subtitlesKey(id) })
  })
}
