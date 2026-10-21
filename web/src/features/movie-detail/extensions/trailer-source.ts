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
