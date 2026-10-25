import { renderToStaticMarkup } from 'react-dom/server'
import { expect, test, vi } from 'vitest'
import { MovieSynopsis } from '@/features/movie-detail/extensions/synopsis'
import { useMDCzSynopsis } from '@/api/mdcz'

const state = vi.hoisted(() => ({ nsfwMode: false }))
vi.mock('@/stores/settings', () => ({
  useSettingsStore: (select: (value: typeof state) => unknown) => select(state)
}))
vi.mock('@/api/mdcz', () => ({ useMDCzSynopsis: vi.fn() }))

test('saved introductions render as plain text without querying the bridge; privacy conceals them', () => {
  state.nsfwMode = false
  const html = renderToStaticMarkup(
    <MovieSynopsis code="TEST-001" plot={'<script>fixture</script>\n第二行'} />
  )
  expect(html).toContain('&lt;script&gt;')
  expect(html).not.toContain('<script>')
  expect(useMDCzSynopsis).not.toHaveBeenCalled()
  state.nsfwMode = true
  expect(renderToStaticMarkup(<MovieSynopsis code="TEST-001" plot="Private fixture" />)).toBe('')
  state.nsfwMode = false
})
