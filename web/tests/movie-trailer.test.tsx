import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'
import { MovieMediaPreviews } from '@/features/movie-detail/extensions/trailer'

const settings = vi.hoisted(() => ({ nsfwMode: false }))
vi.mock('@/stores/settings', () => ({
  useSettingsStore: (select: (state: typeof settings) => unknown) => select(settings)
}))
beforeEach(() => {
  settings.nsfwMode = false
})
const images = [
  { thumbnail: 'https://example.com/thumb.jpg', original: 'https://example.com/original.jpg' }
]
test('trailer tile precedes the original images and does not preload a video', () => {
  const html = renderToStaticMarkup(
    <MovieMediaPreviews images={images} video="https://example.com/preview.mp4" cover="" />
  )
  expect(html.indexOf('播放预告')).toBeLessThan(html.indexOf('original.jpg'))
  expect(html).toContain('预告与预览图')
  expect(html).not.toContain('<video')
  expect(html).not.toContain('src="https://example.com/preview.mp4"')
  expect(images).toHaveLength(1)
})
test('no trailer preserves the original image-only heading', () => {
  const html = renderToStaticMarkup(<MovieMediaPreviews images={images} video="" cover="" />)
  expect(html).toContain('预览图')
  expect(html).not.toContain('播放预告')
})
test('a valid trailer still renders when the film has no images', () => {
  const html = renderToStaticMarkup(
    <MovieMediaPreviews images={[]} video="https://example.com/preview.m3u8" cover="" />
  )
  expect(html).toContain('播放预告')
  expect(html).not.toContain('暂无预览图')
})
test('invalid and unsafe trailer sources cannot create a playable entry', () => {
  for (const video of ['javascript:alert(1)', 'http://example.com/video.mp4', '/video.mp4'])
    expect(
      renderToStaticMarkup(<MovieMediaPreviews images={[]} video={video} cover="" />)
    ).not.toContain('播放预告')
})
test('privacy mode disables the trailer entry and never renders video media', () => {
  settings.nsfwMode = true
  const html = renderToStaticMarkup(
    <MovieMediaPreviews
      images={images}
      video="https://example.com/video.mp4"
      cover="https://example.com/cover.jpg"
    />
  )
  expect(html).toContain('关闭隐私模式后播放预告')
  expect(html).toContain('disabled')
  expect(html).not.toContain('<video')
  expect(html).not.toContain('<img')
})
