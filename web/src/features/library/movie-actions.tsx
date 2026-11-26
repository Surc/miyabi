import { CaptionsIcon, PencilIcon, PlayIcon, RefreshCwIcon, SearchIcon } from 'lucide-react'
import { useContext, useState } from 'react'
import { toast } from 'sonner'

import { MovieDetailDialogContext } from '@/features/movie-detail/dialog-context'

import { describeApiError, imageURL } from '@/api/client'
import { useSearchMovies } from '@/api/discover'
import { useRescrapeLibraryMovie, type LibraryMovie } from '@/api/library'
import { fetchMpvLink } from '@/api/play'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { ContextMenuContent, ContextMenuItem } from '@/components/ui/context-menu'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { SubtitleDialog } from '@/features/library/subtitle-dialog'
import { notifyTaskError, useNotifyScanTask } from '@/features/tasks/task-toast'

export function LibraryMovieActions({
  movie,
  disabled = false
}: {
  movie: Pick<LibraryMovie, 'id' | 'code'>
  disabled?: boolean
}) {
  const [action, setAction] = useState<'code' | 'javdb' | null>(null)
  const [code, setCode] = useState(movie.code)
  const [javdbID, setJavDBID] = useState('')
  const scrape = useRescrapeLibraryMovie(movie.id)
  const notifyScan = useNotifyScanTask()
  const busy = disabled || scrape.isPending
  const choosingJavDB = action === 'javdb'
  const search = useSearchMovies({
    query: choosingJavDB ? movie.code : '',
    limit: 100,
    sameCode: true
  })
  const candidates = search.data
  const canSubmit =
    !busy &&
    (choosingJavDB
      ? !search.isError && candidates?.some(item => item.id === javdbID)
      : code.trim() && code.trim() !== movie.code)

  const [managingSubtitles, setManagingSubtitles] = useState(false)
  const openMovie = useContext(MovieDetailDialogContext)
  const [launchingMpv, setLaunchingMpv] = useState(false)

  async function playWithMpv() {
    if (launchingMpv) return
    setLaunchingMpv(true)
    try {
      const { url } = await fetchMpvLink(movie.id)
      window.location.href = url
    } catch (error) {
      toast.error('无法唤起 MPV', { description: describeApiError(error) })
    } finally {
      setLaunchingMpv(false)
    }
  }

  return (
    <>
      <ContextMenuContent>
        <ContextMenuItem onClick={() => openMovie?.({ libraryId: movie.id })}>
          影片详情
        </ContextMenuItem>
        <ContextMenuItem
          disabled={busy}
          onClick={() =>
            scrape.mutate(undefined, {
              onSuccess: notifyScan,
              onError: error =>
                notifyTaskError(
                  `movie:${movie.id}:submit-error`,
                  '无法创建刮削任务',
                  describeApiError(error)
                )
            })
          }
        >
          <RefreshCwIcon />
          重新刮削
        </ContextMenuItem>
        <ContextMenuItem
          disabled={busy}
          onClick={() => {
            scrape.reset()
            setCode(movie.code)
            setAction('code')
          }}
        >
          <PencilIcon />
          纠正番号
        </ContextMenuItem>
        <ContextMenuItem
          disabled={busy}
          onClick={() => {
            scrape.reset()
            setJavDBID('')
            setAction('javdb')
          }}
        >
          <SearchIcon />
          选择 JavDB 条目
        </ContextMenuItem>
        <ContextMenuItem onClick={() => setManagingSubtitles(true)}>
          <CaptionsIcon />
          字幕下载
        </ContextMenuItem>
        <ContextMenuItem disabled={launchingMpv} onClick={() => void playWithMpv()}>
          <PlayIcon />
          使用 MPV 播放
        </ContextMenuItem>
      </ContextMenuContent>
      <SubtitleDialog movie={movie} open={managingSubtitles} onOpenChange={setManagingSubtitles} />
      <Dialog
        open={action !== null}
        onOpenChange={open => {
          if (!open && !scrape.isPending) setAction(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{choosingJavDB ? '选择 JavDB 条目' : '纠正番号'}</DialogTitle>
            <DialogDescription>
              {choosingJavDB
                ? `为 ${movie.code} 选择与源视频对应的条目。此次刮削使用该条目的资料和封面。`
                : `当前番号 ${movie.code}。保存后会重新刮削，并在后续同步与重建时沿用纠正结果。`}
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-6"
            onSubmit={event => {
              event.preventDefault()
              if (!canSubmit) return
              scrape.mutate(choosingJavDB ? { javdb_id: javdbID } : { code: code.trim() }, {
                onSuccess: task => {
                  setAction(null)
                  notifyScan(task)
                }
              })
            }}
          >
            {choosingJavDB ? (
              search.isPending ? (
                <p role="status" className="text-muted-foreground">
                  正在查找 JavDB 条目…
                </p>
              ) : search.isError ? (
                <InlineError onRetry={() => void search.refetch()} retrying={search.isFetching}>
                  {describeApiError(search.error)}
                </InlineError>
              ) : candidates?.length ? (
                <fieldset
                  aria-label="JavDB 条目"
                  className="max-h-[45vh] space-y-2 overflow-y-auto"
                  disabled={busy}
                >
                  {candidates.map(item => (
                    <label
                      key={item.id}
                      className="flex cursor-pointer items-center gap-3 rounded-xl border p-3"
                    >
                      <input
                        type="radio"
                        name="javdb-entry"
                        value={item.id}
                        checked={javdbID === item.id}
                        onChange={() => setJavDBID(item.id)}
                      />
                      {item.thumbnail ? (
                        <img
                          src={imageURL(item.thumbnail)}
                          alt=""
                          className="h-14 w-10 shrink-0 rounded object-cover"
                        />
                      ) : null}
                      <span className="min-w-0">
                        <span className="block wrap-break-word">{item.title || item.code}</span>
                        <span className="block text-xs text-muted-foreground">
                          {[item.release_date, item.duration ? `${item.duration} 分钟` : '']
                            .filter(Boolean)
                            .join(' · ')}
                        </span>
                      </span>
                    </label>
                  ))}
                </fieldset>
              ) : (
                <p className="text-muted-foreground">JavDB 搜索未返回同番号条目。</p>
              )
            ) : (
              <Input
                value={code}
                onChange={event => setCode(event.target.value)}
                maxLength={120}
                disabled={scrape.isPending}
                autoFocus
              />
            )}
            {scrape.error ? <InlineError>{describeApiError(scrape.error)}</InlineError> : null}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={scrape.isPending}
                onClick={() => setAction(null)}
              >
                取消
              </Button>
              <Button type="submit" disabled={!canSubmit}>
                {scrape.isPending ? '提交中…' : '保存并重新刮削'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}
