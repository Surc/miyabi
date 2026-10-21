import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'
import { useJavLibraryRating, useMovieComments } from '@/api/movie-extras'
import {
  JavLibraryRating,
  CommentList,
  MovieComments
} from '@/features/movie-detail/extensions/social'
import { MovieHero } from '@/features/movie-detail/hero'
import type { DiscoverMovieDetail } from '@/api/discover'

vi.mock('@/api/movie-extras', () => ({ useJavLibraryRating: vi.fn(), useMovieComments: vi.fn() }))
beforeEach(() => {
  vi.mocked(useJavLibraryRating).mockReturnValue({ isPending: true } as ReturnType<
    typeof useJavLibraryRating
  >)
  vi.mocked(useMovieComments).mockReturnValue({ isPending: true } as ReturnType<
    typeof useMovieComments
  >)
})
test('JavLibrary is an independent ten-point rating beside saved FANZA data', () => {
  vi.mocked(useJavLibraryRating).mockReturnValue({
    data: { source: 'javlibrary', found: true, max: 10, score: 8.5 }
  } as ReturnType<typeof useJavLibraryRating>)
  const movie = {
    code: 'ABP-123',
    title: 'Saved film',
    cover: '',
    rating: 4.2,
    rating_max: 5,
    rating_source: 'fanza',
    duration: 0,
    release_date: '',
    actors: [],
    tags: []
  } as unknown as DiscoverMovieDetail
  const html = renderToStaticMarkup(
    <MovieHero
      movie={movie}
      libraryStatus="done"
      extraStats={<JavLibraryRating code={movie.code} />}
    />
  )
  expect(html).toContain('FANZA 评分')
  expect(html).toContain('4.2 / 5')
  expect(html).toContain('JavLibrary 评分')
  expect(html).toContain('8.5 / 10')
  expect(movie.rating).toBe(4.2)
})
test.each([
  { found: false, score: null, label: '未收录' },
  { found: true, score: null, label: '暂无评分' }
])('distinguishes missing film and unrated film: $label', ({ found, score, label }) => {
  vi.mocked(useJavLibraryRating).mockReturnValue({
    data: { source: 'javlibrary', found, max: 10, score }
  } as ReturnType<typeof useJavLibraryRating>)
  expect(renderToStaticMarkup(<JavLibraryRating code="ABP-123" />)).toContain(label)
})
test('rating errors remain retryable and distinct from unrated', () => {
  vi.mocked(useJavLibraryRating).mockReturnValue({ isError: true, isFetching: false } as ReturnType<
    typeof useJavLibraryRating
  >)
  const html = renderToStaticMarkup(<JavLibraryRating code="ABP-123" />)
  expect(html).toContain('暂不可用')
  expect(html).toContain('重试评分')
  expect(html).not.toContain('暂无评分')
})
test('only active source mounts, with a JavLibrary default for unlinked saved films', () => {
  vi.mocked(useMovieComments).mockClear()
  renderToStaticMarkup(<MovieComments javdbId="film" code="ABP-123" />)
  expect(useMovieComments).toHaveBeenCalledTimes(1)
  expect(useMovieComments).toHaveBeenCalledWith('javdb', 'film', 'ABP-123')
  vi.mocked(useMovieComments).mockClear()
  renderToStaticMarkup(<MovieComments javdbId="" code="ABP-123" />)
  expect(useMovieComments).toHaveBeenCalledTimes(1)
  expect(useMovieComments).toHaveBeenCalledWith('javlibrary', '', 'ABP-123')
})
test('comment pages deduplicate IDs, escape text and retain loaded comments after a next-page failure', () => {
  vi.mocked(useMovieComments).mockReturnValue({
    isPending: false,
    isError: true,
    isFetchNextPageError: true,
    data: {
      pages: [
        {
          items: [
            {
              id: 'one',
              author: '<Reader>',
              content: '<script>alert(1)</script>',
              created_at: '2026-10-21',
              rating: 4.5,
              rating_max: 5,
              likes: 2
            }
          ]
        },
        { items: [{ id: 'one', author: 'duplicate', content: 'duplicate' }] }
      ]
    }
  } as unknown as ReturnType<typeof useMovieComments>)
  const html = renderToStaticMarkup(<CommentList source="javdb" javdbId="film" code="ABP-123" />)
  expect(html).toContain('&lt;script&gt;')
  expect(html).not.toContain('<script>')
  expect(html).not.toContain('duplicate')
  expect(html).toContain('评论加载失败')
  expect(html).toContain('2026-10-21')
  expect(html).not.toContain('暂无评论')
})
test('empty comments and missing JavDB links have separate explanations', () => {
  vi.mocked(useMovieComments).mockReturnValue({
    isPending: false,
    data: { pages: [{ items: [] }] }
  } as unknown as ReturnType<typeof useMovieComments>)
  expect(
    renderToStaticMarkup(<CommentList source="javdb" javdbId="film" code="ABP-123" />)
  ).toContain('暂无评论')
  expect(renderToStaticMarkup(<CommentList source="javdb" javdbId="" code="ABP-123" />)).toContain(
    '尚未关联 JavDB'
  )
})
