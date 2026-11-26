import type Hls from 'hls.js'

export type TrailerSource = { url: string; hls: boolean }
export function resolveTrailer(raw: string): TrailerSource | null {
  try {
    const url = new URL(raw)
    if (url.protocol !== 'https:' || !url.hostname || url.username || url.password) return null
    return { url: url.href, hls: /\.m3u8$/i.test(url.pathname) }
  } catch {
    return null
  }
}

type Media = Pick<
  HTMLVideoElement,
  'src' | 'canPlayType' | 'load' | 'play' | 'pause' | 'removeAttribute'
>
// Return null before touching src when HLS is unsupported; signed queries stay intact.
export function startNativeTrailer(media: Media, source: TrailerSource): (() => void) | null {
  if (
    source.hls &&
    !media.canPlayType('application/vnd.apple.mpegurl') &&
    !media.canPlayType('application/x-mpegURL')
  )
    return null
  media.src = source.url
  media.load()
  void media.play().catch(() => {}) // Native controls remain usable if autoplay is blocked.
  return () => {
    media.pause()
    media.removeAttribute('src')
    media.load()
  }
}

export function startTrailer(
  media: HTMLVideoElement,
  source: TrailerSource,
  onError: (reason: 'unsupported' | 'load') => void,
  loadHls = () => import('hls.js')
): () => void {
  let stopped = false
  let player: Hls | undefined
  let stopNative: (() => void) | null

  function startHls(unavailable: 'unsupported' | 'load') {
    void loadHls()
      .then(({ default: Hls }) => {
        if (stopped) return
        if (!Hls.isSupported()) {
          onError(unavailable)
          return
        }
        player = new Hls()
        player.on(Hls.Events.ERROR, (_, data) => {
          if (!stopped && data.fatal) onError('load')
        })
        player.on(Hls.Events.MANIFEST_PARSED, () => {
          if (!stopped) void media.play().catch(() => {})
        })
        player.loadSource(source.url)
        player.attachMedia(media)
      })
      .catch(() => {
        if (!stopped) onError('load')
      })
  }

  function onMediaError() {
    if (stopNative && source.hls) {
      const stop = stopNative
      stopNative = null
      stop()
      startHls('load')
    } else if (stopNative || player) {
      onError('load')
    }
  }

  media.addEventListener('error', onMediaError)
  stopNative = startNativeTrailer(media, source)
  if (!stopNative) startHls('unsupported')

  return () => {
    stopped = true
    media.removeEventListener('error', onMediaError)
    stopNative?.()
    if (player) {
      media.pause()
      player.destroy()
    }
  }
}
