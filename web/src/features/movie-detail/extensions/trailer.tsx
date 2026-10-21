import { PlayIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { PreviewImage } from '@/api/discover'
import { MediaImage } from '@/components/media-image'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { useSettingsStore } from '@/stores/settings'
import { MoviePreviews } from '../previews'
import { resolveTrailer, startNativeTrailer, type TrailerSource } from './trailer-source'

// The existing image viewer receives the same array and indices as upstream.
export function MovieMediaPreviews({
  images,
  video,
  cover
}: {
  images: PreviewImage[]
  video: string
  cover: string
}) {
  const source = resolveTrailer(video)
  const concealed = useSettingsStore(state => state.nsfwMode)
  return (
    <MoviePreviews
      images={images}
      leadingPreview={
        source ? (
          <MovieTrailer
            key={`${source.url}:${concealed}`}
            source={source}
            cover={cover}
            concealed={concealed}
          />
        ) : undefined
      }
    />
  )
}

function MovieTrailer({
  source,
  cover,
  concealed
}: {
  source: TrailerSource
  cover: string
  concealed: boolean
}) {
  const [open, setOpen] = useState(false)
  // Unmounting on privacy changes closes the dialog and releases media immediately.
  return (
    <>
      <button
        type="button"
        disabled={concealed}
        onClick={() => setOpen(true)}
        aria-label={concealed ? '关闭隐私模式后播放预告' : '播放预告'}
        className="group relative aspect-video cursor-pointer overflow-hidden rounded-xl disabled:cursor-default"
      >
        <MediaImage source={cover} className="object-cover" />
        <span className="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-black/40 text-white">
          <PlayIcon className="size-6" />
          预告
        </span>
      </button>
      {!concealed ? (
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent className="sm:max-w-4xl">
            <DialogTitle>影片预告</DialogTitle>
            <DialogDescription>
              视频使用浏览器网络播放；部分浏览器不支持 HLS 格式。
            </DialogDescription>
            {open ? <TrailerPlayback source={source} /> : null}
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  )
}

function TrailerPlayback({ source }: { source: TrailerSource }) {
  const [attempt, setAttempt] = useState(0)
  return (
    <div className="space-y-3">
      <NativeTrailer key={attempt} source={source} onRetry={() => setAttempt(attempt + 1)} />
    </div>
  )
}

function NativeTrailer({ source, onRetry }: { source: TrailerSource; onRetry: () => void }) {
  const media = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState('')
  const { url, hls } = source
  useEffect(() => {
    if (!media.current) return
    const cleanup = startNativeTrailer(media.current, { url, hls })
    if (!cleanup) setError('当前浏览器不支持此 HLS 预告格式。')
    return cleanup ?? undefined
  }, [url, hls])
  return (
    <>
      <video
        ref={media}
        controls
        playsInline
        preload="none"
        className="aspect-video w-full rounded-xl bg-black"
        onError={() => setError('预告加载失败，请检查浏览器网络或重试。')}
      />
      {error ? (
        <div role="alert" className="space-y-3 text-sm text-muted-foreground">
          <p>{error}</p>
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试预告
          </Button>{' '}
          <a href={url} target="_blank" rel="noopener noreferrer" className="underline">
            打开预告来源
          </a>
        </div>
      ) : null}
    </>
  )
}
