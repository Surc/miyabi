import { renderToStaticMarkup } from 'react-dom/server'
import { expect, test, vi } from 'vitest'
import { BrowseProgress, CurrentBrowsePage } from '@/features/discover/extensions/cached-results'
import { MovieDetailDialogContext } from '@/features/movie-detail/dialog-context'
import type { PreloadRow, PreloadState } from '@/features/discover/extensions/preloader'
import type { DiscoverMovieDetail } from '@/api/discover'

vi.mock('@/components/overflow-tooltip', () => ({
  OverflowTooltip: ({ children }: { children: React.ReactNode }) => children
}))
vi.mock('@/components/movie/movie-badges', () => ({ MovieStateBadge: () => null }))
vi.mock('@/stores/settings', () => ({
  useSettingsStore: (select: (state: { nsfwMode: boolean }) => boolean) =>
    select({ nsfwMode: false })
}))
vi.mock('@/api/browse-cache', async importOriginal => ({
  ...(await importOriginal<typeof import('@/api/browse-cache')>()),
  useBrowseRating: (_id: string, source: string) => ({
    data: {
      data: {
        source,
        found: true,
        score: source === 'javdb' ? 4.2 : 8.1,
        max: source === 'javdb' ? 5 : 10
      },
      stale: false,
      updated_at: '2026-10-21T00:00:00Z'
    }
  })
}))

test('completed fills disappear while paused, failed and running states retain controls', () => {
  const state: PreloadState = {
    rows: [],
    ready: 120,
    target: 120,
    phase: 'complete',
    exhausted: false
  }
  const render = (state: PreloadState) =>
    renderToStaticMarkup(<BrowseProgress state={state} pause={() => {}} resume={() => {}} />)
  expect(render(state)).toBe('')
  expect(render({ ...state, phase: 'paused' })).toContain('继续')
  expect(render({ ...state, phase: 'error' })).toContain('重试')
  expect(render({ ...state, phase: 'loading', ready: 20 })).toContain('暂停')
})
test('six cached pages mount images and rating UI only for the current twenty works', () => {
  const rows = Array.from({ length: 120 }, (_, index) => {
    const movie: DiscoverMovieDetail = {
      id: String(index + 1),
      code: `DEMO-${index + 1}`,
      title: 'Title',
      cover: 'https://example.com/cover.jpg',
      release_date: '2026-10-21',
      release_status: 'released',
      origin_title: '',
      duration: 0,
      rating: 4.2,
      thumbnail: '',
      preview_images: [],
      preview_video: '',
      magnets_count: 0,
      has_subtitle: false,
      has_preview: false,
      actors: [],
      tags: [],
      zone: 'censored',
      actor_movies: [],
      related_movies: []
    }
    return {
      movie,
      resources: {
        data: { magnet_count: 0, complete: true, has_subtitle: true, has_cracked: index === 20 },
        stale: false,
        updated_at: '2026-10-21T00:00:00Z'
      }
    } satisfies PreloadRow
  })
  const html = renderToStaticMarkup(
    <MovieDetailDialogContext.Provider value={() => {}}>
      <CurrentBrowsePage rows={rows} page={2} />
    </MovieDetailDialogContext.Provider>
  )
  expect(html).toContain('/movies/21/image')
  expect(html).toContain('/movies/40/image')
  expect(html).not.toContain('/movies/20/image')
  expect(html).not.toContain('/movies/41/image')
  expect(html).not.toContain('https://example.com/cover.jpg')
  expect(html.match(/JavDB/g)).toHaveLength(20)
  expect(html).toContain('4.2/5')
  expect(html).toContain('8.1/10')
  expect(html).toContain('中文字幕')
  expect(html).toContain('破解')
})
