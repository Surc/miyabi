import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  resolveTrailer,
  startNativeTrailer,
  startTrailer
} from '../src/features/movie-detail/extensions/trailer-source.ts'

test('only credential-free absolute HTTPS trailers are accepted', () => {
  for (const value of [
    '',
    '/video.mp4',
    'http://example.com/video.mp4',
    'javascript:alert(1)',
    'https://user:password@example.com/video.mp4'
  ])
    assert.equal(resolveTrailer(value), null)
  assert.equal(resolveTrailer('https://example.com/video.mp4').hls, false)
})
test('HLS detection uses the pathname and preserves signed query strings', () => {
  const url = 'https://example.com/PREVIEW.M3U8?token=abc%2Fdef&expires=123'
  assert.deepEqual(resolveTrailer(url), { url, hls: true })
  assert.equal(resolveTrailer('https://example.com/video.mp4?name=x.m3u8').hls, false)
})
function media(support = '', reject = false) {
  return Object.assign(new EventTarget(), {
    src: '',
    calls: [],
    canPlayType(type) {
      this.calls.push(type)
      return support
    },
    load() {
      this.calls.push('load')
    },
    play() {
      this.calls.push('play')
      return reject ? Promise.reject(new Error('blocked')) : Promise.resolve()
    },
    pause() {
      this.calls.push('pause')
    },
    removeAttribute(name) {
      this.calls.push(`remove:${name}`)
      this.src = ''
    }
  })
}
test('unsupported HLS never assigns a URL or starts network loading', () => {
  const m = media()
  assert.equal(startNativeTrailer(m, resolveTrailer('https://example.com/video.m3u8')), null)
  assert.equal(m.src, '')
  assert.ok(!m.calls.includes('load'))
})
test('supported native HLS loads the original URL', () => {
  const m = media('maybe')
  const stop = startNativeTrailer(m, resolveTrailer('https://example.com/video.m3u8?token=1'))
  assert.equal(m.src, 'https://example.com/video.m3u8?token=1')
  assert.equal(typeof stop, 'function')
})
test('ordinary video does not require HLS codec detection', () => {
  const m = media()
  startNativeTrailer(m, resolveTrailer('https://example.com/video.mp4'))
  assert.deepEqual(m.calls, ['load', 'play'])
})
test('closing, switching films and privacy unmount can release the media', () => {
  const m = media()
  const stop = startNativeTrailer(m, resolveTrailer('https://example.com/video.mp4'))
  stop()
  assert.equal(m.src, '')
  assert.deepEqual(m.calls.slice(-3), ['pause', 'remove:src', 'load'])
})
test('blocked autoplay is contained so native controls remain usable', async () => {
  const m = media('', true)
  assert.equal(
    typeof startNativeTrailer(m, resolveTrailer('https://example.com/video.mp4')),
    'function'
  )
  await Promise.resolve()
  assert.notEqual(m.src, '')
})

function hlsPlayer() {
  const calls = []
  const listeners = new Map()
  class Hls {
    static Events = { ERROR: 'error', MANIFEST_PARSED: 'manifestParsed' }
    static isSupported = () => true
    on(event, listener) {
      listeners.set(event, listener)
    }
    loadSource(url) {
      calls.push(url)
    }
    attachMedia(video) {
      calls.push(video)
    }
    destroy() {
      calls.push('destroy')
    }
  }
  return { calls, listeners, load: async () => ({ default: Hls }) }
}

test('MP4 playback reports native errors without loading the HLS player', () => {
  const video = media()
  const url = 'https://example.com/trailer.mp4'
  const errors = []
  const stop = startTrailer(video, resolveTrailer(url), reason => errors.push(reason), assert.fail)
  assert.equal(video.src, url)
  video.dispatchEvent(new Event('error'))
  assert.deepEqual(errors, ['load'])
  stop()
})

test('HLS plays through Media Source when native playback is unavailable', async () => {
  const url = 'https://example.com/trailer.m3u8?token=abc%2Fdef'
  const video = media()
  const player = hlsPlayer()
  const errors = []
  const stop = startTrailer(video, resolveTrailer(url), reason => errors.push(reason), player.load)
  await Promise.resolve()

  assert.equal(video.src, '')
  assert.deepEqual(player.calls, [url, video])
  player.listeners.get('manifestParsed')()
  assert.deepEqual(video.calls.slice(-1), ['play'])
  player.listeners.get('error')('error', { fatal: false })
  assert.deepEqual(errors, [])
  player.listeners.get('error')('error', { fatal: true })
  assert.deepEqual(errors, ['load'])

  stop()
  assert.equal(video.calls.at(-1), 'pause')
  assert.equal(player.calls.at(-1), 'destroy')
})

test('an HLS stream that fails natively is retried through Media Source', async () => {
  const video = media('maybe')
  const player = hlsPlayer()
  const url = 'https://example.com/trailer.m3u8'
  const stop = startTrailer(video, resolveTrailer(url), assert.fail, player.load)
  assert.equal(video.src, url)
  assert.deepEqual(player.calls, [])

  video.dispatchEvent(new Event('error'))
  await Promise.resolve()
  assert.equal(video.src, '')
  assert.deepEqual(player.calls, [url, video])
  stop()
})

test('closing while HLS support is loading never starts streaming', async () => {
  const video = media()
  const player = hlsPlayer()
  const stop = startTrailer(
    video,
    resolveTrailer('https://example.com/trailer.m3u8'),
    assert.fail,
    player.load
  )
  stop()
  await Promise.resolve()
  assert.deepEqual(player.calls, [])
})
