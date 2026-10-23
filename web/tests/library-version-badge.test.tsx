import { renderToStaticMarkup } from 'react-dom/server'
import { expect, test, vi } from 'vitest'
import { useLibraryVersion } from '@/api/library-versions'
import { useMovieState } from '@/api/movie-states'
import {
  LibraryVersionBadge,
  OwnedVersionBadge,
  VersionLabels
} from '@/components/movie/library-version-badge'

vi.mock('@/api/library-versions', () => ({ useLibraryVersion: vi.fn() }))
vi.mock('@/api/movie-states', () => ({ useMovieState: vi.fn() }))

test('owned version labels sit at the lower left and preserve different file versions', () => {
  const value = { id: 1, available: true, labels: ['中文', '破解', '流出'], unmarked: true }
  const html = renderToStaticMarkup(<VersionLabels value={value} />)
  expect(html).toContain('bottom-2 left-2')
  expect(html).toContain('>中文<')
  expect(html).toContain('>破解<')
  expect(html).toContain('>流出<')
  expect(html).not.toContain('中文破解')
  expect(html).toContain('另有未标记文件')
  expect(renderToStaticMarkup(<VersionLabels value={{ ...value, available: false }} />)).toBe('')
  expect(renderToStaticMarkup(<VersionLabels value={{ ...value, labels: [] }} />)).toBe('')
})

test('refetching hides stale versions and failures do not claim a version', () => {
  const data = { id: 1, available: true, labels: ['中文破解'], unmarked: false }
  const mock = (state: object) =>
    vi.mocked(useLibraryVersion).mockReturnValue(state as ReturnType<typeof useLibraryVersion>)
  mock({ data, isFetching: true })
  const html = renderToStaticMarkup(<LibraryVersionBadge id={1} />)
  expect(html).toContain('版本加载中')
  expect(html).not.toContain('中文破解')
  mock({ data, isError: true })
  expect(renderToStaticMarkup(<LibraryVersionBadge id={1} />)).toBe('')
  mock({ data })
  expect(renderToStaticMarkup(<LibraryVersionBadge id={1} />)).toContain('中文破解')
})

test('discovery badges only query versions for a verified in-library movie', () => {
  const movie = { id: 'one', code: 'TEST-001' }
  vi.mocked(useLibraryVersion).mockReturnValue({} as ReturnType<typeof useLibraryVersion>)
  for (const state of [
    { state: 'not_in_library' as const, isPlaceholderData: true },
    { state: 'saving' as const, isPlaceholderData: false },
    { state: 'in_library' as const, library_id: 7, isPlaceholderData: false }
  ]) {
    vi.mocked(useMovieState).mockReturnValue(state)
    renderToStaticMarkup(<OwnedVersionBadge movie={movie} />)
    expect(useLibraryVersion).toHaveBeenLastCalledWith(state.state === 'in_library' ? 7 : undefined)
  }
})
