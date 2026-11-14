import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, expect, test, vi } from 'vitest'
import { ApiError } from '@/api/client'
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
  { name: ' 字幕汉化 ', hasSubtitle: false, visible: true },
  { name: '其他标签', hasSubtitle: true, visible: true },
  { name: '其他标签', hasSubtitle: false, visible: false }
])(
  'catalogue detail subtitle badge for $name (flag=$hasSubtitle)',
  ({ name, hasSubtitle, visible }) => {
    const movie: DiscoverMovieDetail = {
      id: 'movie',
      code: 'ABP-123',
      title: 'Title',
      origin_title: '',
      cover: '',
      thumbnail: '',
      rating: 0,
      duration: 0,
      release_date: '',
      release_status: 'released',
      actors: [],
      tags: [{ id: '', name, name_zht: '', category_id: '' }],
      has_subtitle: hasSubtitle,
      has_preview: false,
      preview_images: [],
      preview_video: '',
      magnets_count: 0,
      zone: 'censored',
      actor_movies: [],
      related_movies: []
    }
    const html = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <MovieHero movie={movie} />
      </QueryClientProvider>
    )
    expect(html.match(/>含字幕<\/span>/g) ?? []).toHaveLength(visible ? 1 : 0)
  }
)
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
  expect(html).not.toContain('http://127.0.0.1:18082/')
})
test.each([
  [new ApiError('验证', 502, 'JAVLIBRARY_VERIFICATION_REQUIRED'), '需要验证'],
  [new ApiError('JavLibrary 需要在已连接的浏览器中完成人工验证', 502), '需要验证'],
  [new ApiError('断开', 502, 'JAVLIBRARY_BROWSER_DISCONNECTED'), '请先启动本机验证服务']
])('rating recovery opens the local verification page for %s', (error, label) => {
  vi.mocked(useJavLibraryRating).mockReturnValue({ isError: true, error } as ReturnType<
    typeof useJavLibraryRating
  >)
  const html = renderToStaticMarkup(<JavLibraryRating code="ABP-123" />)
  expect(html).toContain(label)
  expect(html).toContain('href="http://127.0.0.1:18082/" target="_blank" rel="noopener noreferrer"')
  expect(html).toContain('aria-label="打开 JavLibrary 验证页"')
})
test.each(['film', ''])('JavLibrary comments stay disabled for JavDB ID "%s"', javdbId => {
  vi.mocked(useMovieComments).mockClear()
  const html = renderToStaticMarkup(<MovieComments javdbId={javdbId} code="ABP-123" />)
  expect(useMovieComments).toHaveBeenCalledTimes(1)
  expect(useMovieComments).toHaveBeenCalledWith('javdb', javdbId, 'ABP-123')
  expect(html).toContain('JavDB')
  expect(html).not.toContain('JavLibrary')
  if (!javdbId) expect(html).toContain('尚未关联 JavDB')
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
