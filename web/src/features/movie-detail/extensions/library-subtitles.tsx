import { useState, type FormEvent } from 'react'
import { DownloadIcon, LoaderCircleIcon, PlusIcon, Trash2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { describeApiError } from '@/api/client'
import { type LibraryMovieVideo, useDeleteLibraryVideo } from '@/api/library'
import {
  useChangeLibrarySubtitles,
  useLibrarySubtitles,
  type LibrarySubtitleTrack,
  type LibrarySubtitleVideo
} from '@/api/library-subtitles'
import { InlineError } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { downloadLinkID, DownloadProtocol } from '../download-protocol'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'

const languages: Record<string, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁体中文',
  en: '英语',
  ja: '日语',
  ko: '韩语'
}
const sources: Record<string, string> = { manual: '手动添加', local: '本地文件', xunlei: '迅雷' }

export function LibrarySubtitles({
  movieId,
  videos,
  onMovieRemoved
}: {
  movieId: number
  videos?: LibraryMovieVideo[]
  onMovieRemoved?: () => void
}) {
  const query = useLibrarySubtitles(movieId)
  const change = useChangeLibrarySubtitles(movieId)
  const remove = useDeleteLibraryVideo(movieId)
  const [adding, setAdding] = useState<LibrarySubtitleVideo>()
  const [deleting, setDeleting] = useState<{ video: number; track: LibrarySubtitleTrack }>()
  const [removingVideo, setRemovingVideo] = useState<LibraryMovieVideo>()
  const [language, setLanguage] = useState('auto')
  const availableSubtitles = new Map(query.data?.map(video => [video.id, video]))
  const rows = videos
    ? videos.map(video => ({
        id: video.id,
        name: video.name,
        video,
        subtitles: availableSubtitles.get(video.id)
      }))
    : (query.data ?? []).map(subtitles => ({
        id: subtitles.id,
        name: subtitles.name,
        video: undefined,
        subtitles
      }))
  const heading = videos ? '视频与字幕' : '字幕'

  function add(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!adding) return
    const file = new FormData(event.currentTarget).get('subtitle')
    if (!(file instanceof File) || !file.size) return
    change.mutate(
      { type: 'add', video: adding.id, file, language: language === 'auto' ? '' : language },
      { onSuccess: () => setAdding(undefined) }
    )
  }

  return (
    <section aria-label={heading} className="space-y-4">
      <h2 className="text-xl font-semibold tracking-normal">{heading}</h2>
      {query.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          正在读取字幕…
        </p>
      ) : query.isError ? (
        <InlineError onRetry={() => void query.refetch()} retrying={query.isFetching}>
          {describeApiError(query.error)}
        </InlineError>
      ) : !rows.length ? (
        <p className="text-sm text-muted-foreground">当前导出目录中暂无可管理字幕的视频文件。</p>
      ) : null}
      {rows.length ? (
        <ul className="space-y-3">
          {rows.map(({ id, name, video, subtitles }) => {
            const tracks = subtitles?.tracks ?? []
            return (
              <li key={id} className="space-y-3 rounded-xl border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm font-medium">
                    <span className="break-all">{name}</span>
                    {video?.hard_subtitled || subtitles?.hard_subtitled ? (
                      <Badge variant="outline">文件名标记含字幕</Badge>
                    ) : null}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {subtitles?.can_download ? (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={change.isPending || remove.isPending}
                        onClick={() =>
                          change.mutate(
                            { type: 'download', video: id },
                            {
                              onSuccess: result => {
                                if (result?.downloaded)
                                  toast.success(`已补充 ${result.downloaded} 条字幕`)
                                else toast.info('未找到可补充的字幕')
                              }
                            }
                          )
                        }
                      >
                        {change.isPending &&
                        change.variables.type === 'download' &&
                        change.variables.video === id ? (
                          <>
                            <LoaderCircleIcon className="animate-spin" />
                            正在下载…
                          </>
                        ) : (
                          <>
                            <DownloadIcon />
                            补充下载
                          </>
                        )}
                      </Button>
                    ) : null}
                    {subtitles ? (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={change.isPending || remove.isPending}
                        onClick={() => {
                          change.reset()
                          setLanguage('auto')
                          setAdding(subtitles)
                        }}
                      >
                        <PlusIcon />
                        添加字幕
                      </Button>
                    ) : null}
                  </div>
                </div>
                {video ? (
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      {video.download_links?.length ? (
                        <>
                          <span>下载来源</span>
                          {video.download_links.map(link => (
                            <DownloadProtocol
                              key={link.key}
                              kind={link.key.startsWith('ed2k:') ? 'ed2k' : 'magnet'}
                              target={downloadLinkID(link.key)}
                            />
                          ))}
                        </>
                      ) : (
                        '来源未知'
                      )}
                    </div>
                    {video.can_delete ? (
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="text-destructive"
                        disabled={change.isPending || remove.isPending}
                        aria-label={`删除视频 ${video.name}`}
                        onClick={() => {
                          remove.reset()
                          setRemovingVideo(video)
                        }}
                      >
                        <Trash2Icon />
                        删除视频
                      </Button>
                    ) : null}
                  </div>
                ) : null}
                {tracks.length ? (
                  <ul className="divide-y divide-border">
                    {tracks.map(track => (
                      <li
                        key={track.id}
                        className="flex items-center justify-between gap-2 py-2 text-sm"
                      >
                        <div className="min-w-0">
                          <p className="break-all">{track.name}</p>
                          <p className="text-xs text-muted-foreground">
                            {[
                              track.language && (languages[track.language] ?? track.language),
                              track.format.toUpperCase(),
                              sources[track.source] ?? track.source
                            ]
                              .filter(Boolean)
                              .join(' · ')}
                          </p>
                        </div>
                        <Button
                          type="button"
                          size="icon-sm"
                          variant="ghost"
                          disabled={change.isPending || remove.isPending}
                          aria-label={`删除字幕 ${track.name}`}
                          onClick={() => {
                            change.reset()
                            setDeleting({ video: id, track })
                          }}
                        >
                          <Trash2Icon />
                        </Button>
                      </li>
                    ))}
                  </ul>
                ) : subtitles ? (
                  <p className="text-sm text-muted-foreground">暂无外挂字幕</p>
                ) : null}
              </li>
            )
          })}
        </ul>
      ) : null}
      {change.isError && change.variables.type === 'download' ? (
        <InlineError>{describeApiError(change.error)}</InlineError>
      ) : null}
      <Dialog
        open={!!removingVideo}
        onOpenChange={(open: boolean) => {
          if (!open && !remove.isPending) setRemovingVideo(undefined)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除视频</DialogTitle>
            <DialogDescription className="break-all">
              「{removingVideo?.name}」将移到 115 回收站。Miyabi 会清理对应的入库记录和来源关联。
              {videos?.length === 1 ? '如果这是最后一个视频，影片条目也会移除。' : null}
            </DialogDescription>
          </DialogHeader>
          {remove.isError ? <InlineError>{describeApiError(remove.error)}</InlineError> : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={remove.isPending}
              onClick={() => setRemovingVideo(undefined)}
            >
              取消
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => {
                if (removingVideo) {
                  remove.mutate(removingVideo.id, {
                    onSuccess: result => {
                      setRemovingVideo(undefined)
                      toast.success('已移到 115 回收站')
                      if (result.movie_removed) onMovieRemoved?.()
                    }
                  })
                }
              }}
            >
              {remove.isPending ? '正在移除…' : '移到回收站'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!adding}
        onOpenChange={open => {
          if (!open && !change.isPending) setAdding(undefined)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>添加字幕</DialogTitle>
            <DialogDescription>{adding?.name}</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={add}>
            <Input
              name="subtitle"
              type="file"
              accept=".srt,.ass,.ssa,.vtt"
              required
              aria-label="字幕文件"
              disabled={change.isPending}
            />
            <Select
              value={language}
              onValueChange={next => {
                if (next !== null) setLanguage(next)
              }}
              disabled={change.isPending}
            >
              <SelectTrigger aria-label="字幕语言">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">自动识别简繁中文</SelectItem>
                {Object.entries(languages).map(([id, label]) => (
                  <SelectItem key={id} value={id}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {change.isError ? <InlineError>{describeApiError(change.error)}</InlineError> : null}
            <DialogFooter>
              <Button type="submit" disabled={change.isPending}>
                {change.isPending ? '正在上传…' : '上传字幕'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!deleting}
        onOpenChange={open => {
          if (!open && !change.isPending) setDeleting(undefined)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除字幕</DialogTitle>
            <DialogDescription className="break-all">
              将删除字幕文件「{deleting?.track.name}」。
            </DialogDescription>
          </DialogHeader>
          {change.isError ? <InlineError>{describeApiError(change.error)}</InlineError> : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={change.isPending}
              onClick={() => setDeleting(undefined)}
            >
              取消
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={change.isPending}
              onClick={() => {
                if (deleting)
                  change.mutate(
                    { type: 'delete', video: deleting.video, track: deleting.track.id },
                    { onSuccess: () => setDeleting(undefined) }
                  )
              }}
            >
              {change.isPending ? '正在删除…' : '删除字幕'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
