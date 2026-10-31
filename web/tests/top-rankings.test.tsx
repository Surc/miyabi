import type { PropsWithChildren } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'

import { useMovieRankings, useTopConfig, useTopInfo, useTopMovies } from '@/api/top'
import { TooltipProvider } from '@/components/ui/tooltip'
import { MovieDetailDialogContext } from '@/features/movie-detail/dialog-context'
import { MovieTopRankings } from '@/features/movie-detail/extensions/top-rankings'
import { TopPage, TopResults } from '@/features/top/page'
import { validateTopSearch } from '@/features/top/search'

const { preferences, navigation } = vi.hoisted(() => ({
  preferences: { show_navigation: true, show_rankings: true, default_board: 'all' },
  navigation: { search: {} as { board?: string; page?: number }, navigate: vi.fn() }
}))

vi.mock('@/api/top', () => ({
  TOP_PAGE_SIZE: 48,
  useMovieRankings: vi.fn(),
  useTopMovies: vi.fn(),
  useTopInfo: vi.fn(),
  useTopConfig: vi.fn()
}))
vi.mock('@tanstack/react-router', () => ({
  getRouteApi: () => ({
    useSearch: () => navigation.search,
    useNavigate: () => navigation.navigate
  }),
  Link: ({
    to,
    search,
    children
  }: PropsWithChildren<{ to: string; search: Record<string, string | number> }>) => (
    <a
      href={`${to}?${new URLSearchParams(Object.entries(search).map(([key, value]) => [key, String(value)]))}`}
    >
      {children}
    </a>
  )
}))

beforeEach(() => {
  Object.assign(preferences, { show_navigation: true, show_rankings: true, default_board: 'all' })
  navigation.search = {}
  vi.mocked(useTopConfig, { partial: true }).mockReturnValue({ data: preferences })
  vi.mocked(useTopInfo, { partial: true }).mockReturnValue({ data: undefined })
  vi.mocked(useMovieRankings, { partial: true }).mockReturnValue({ data: undefined })
  vi.mocked(useTopMovies, { partial: true }).mockReturnValue({ data: undefined })
})

test('ranking labels show each board and link to the page containing the source rank', () => {
  vi.mocked(useMovieRankings, { partial: true }).mockReturnValue({
    data: [
      { board_id: 'all', name: 'JavDB TOP250', rank: 4 },
      { board_id: '2022', name: 'JavDB 2022年度 TOP250', rank: 144 }
    ]
  })
  const html = renderToStaticMarkup(<MovieTopRankings code="IPX-811" />)
  expect(useMovieRankings).toHaveBeenCalledWith('IPX-811')
  expect(html).toContain('No.4')
  expect(html).toContain('No.144')
  expect(html).toContain('JavDB 2022年度 TOP250')
  expect(html).toContain('/top?board=2022&amp;page=3')
  expect(html).toContain('/top?board=all&amp;page=1')
})

test.each([undefined, []])('missing ranking data leaves the movie detail usable', data => {
  vi.mocked(useMovieRankings, { partial: true }).mockReturnValue({ data, isError: true })
  expect(renderToStaticMarkup(<MovieTopRankings code="OTHER-123" />)).toBe('')
})

test('turning detail labels off hides cached ranks without requesting rankings', () => {
  preferences.show_rankings = false
  vi.mocked(useMovieRankings).mockClear()
  vi.mocked(useMovieRankings, { partial: true }).mockReturnValue({
    data: [{ board_id: 'all', name: 'JavDB TOP250', rank: 4 }]
  })
  expect(renderToStaticMarkup(<MovieTopRankings code="IPX-811" />)).toBe('')
  expect(useMovieRankings).not.toHaveBeenCalled()
})

test('the configured default applies on entry while explicit board links retain their target', () => {
  preferences.default_board = '2022'
  renderToStaticMarkup(<TopPage />)
  expect(useTopMovies).toHaveBeenLastCalledWith('2022', 1)

  navigation.search = validateTopSearch({ board: 'all', page: '2' })
  renderToStaticMarkup(<TopPage />)
  expect(useTopMovies).toHaveBeenLastCalledWith('all', 2)
})

test('the TOP page waits for saved server settings before requesting a default board', () => {
  vi.mocked(useTopConfig, { partial: true }).mockReturnValue({ data: undefined, isPending: true })
  vi.mocked(useTopMovies).mockClear()
  renderToStaticMarkup(<TopPage />)
  expect(useTopMovies).not.toHaveBeenCalled()
})

test('last ranking page retains original ranks and does not offer another page', () => {
  vi.mocked(useTopMovies, { partial: true }).mockReturnValue({
    isFetching: false,
    data: {
      board: { id: '2022', name: 'JavDB 2022年度 TOP250', count: 250 },
      total: 250,
      items: [
        { code: 'TEST-250', title: 'Ranked film', cover: '', release_date: '2022-01-01', rank: 250 }
      ]
    }
  })
  const html = renderToStaticMarkup(
    <TooltipProvider>
      <MovieDetailDialogContext value={vi.fn()}>
        <TopResults board="2022" page={6} onPageChange={vi.fn()} />
      </MovieDetailDialogContext>
    </TooltipProvider>
  )
  expect(useTopMovies).toHaveBeenCalledWith('2022', 6)
  expect(html).toContain('No.250')
  expect(html).toContain('TEST-250')
  expect(html).toContain('Ranked film')
  expect(html).toContain('role="button"')
  const next = [...html.matchAll(/<button\b[^>]*>[\s\S]*?<\/button>/g)].find(([button]) =>
    button.includes('下一页')
  )
  expect(next?.[0]).toContain('disabled=""')
})

test('board and page search values validate before reaching the API', () => {
  expect(validateTopSearch({})).toEqual({})
  expect(validateTopSearch({ board: 'all', page: '1' })).toEqual({ board: 'all' })
  expect(validateTopSearch({ board: '2022', page: '3' })).toEqual({ board: '2022', page: 3 })
  expect(validateTopSearch({ board: 'fc2' })).toEqual({ board: 'fc2' })
  for (const board of ['2007', '2026', 'unknown', [], 2022]) {
    expect(() => validateTopSearch({ board })).toThrow('榜单无效')
  }
  for (const page of [0, -1, 1.5, 'bad', 1_000_001]) {
    expect(() => validateTopSearch({ page })).toThrow('页码无效')
  }
})
