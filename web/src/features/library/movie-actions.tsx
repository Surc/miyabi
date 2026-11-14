import { Ellipsis, PencilIcon, RefreshCwIcon, SearchIcon } from 'lucide-react'
import { useRef, useState } from 'react'

import { describeApiError, imageURL } from '@/api/client'
import { useSearchMovies } from '@/api/discover'
import { useRescrapeLibraryMovie, type LibraryMovie } from '@/api/library'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { notifyTaskError, useNotifyScanTask } from '@/features/tasks/task-toast'

export function LibraryMovieActions({
  movie,
  disabled = false
}: {
  movie: LibraryMovie
  disabled?: boolean
}) {
  const [action, setAction] = useState<'code' | 'javdb' | null>(null)
  const [code, setCode] = useState(movie.code)
  const [javdbID, setJavDBID] = useState('')
  const trigger = useRef<HTMLButtonElement>(null)
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

  return (
    <Dialog
      open={action !== null}
      onOpenChange={open => {
        if (!open && !scrape.isPending) setAction(null)
      }}
    >
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button ref={trigger} type="button" variant="ghost" size="icon-sm" disabled={busy}>
            <Ellipsis />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="end"
          onCloseAutoFocus={event => {
            if (action) event.preventDefault()
          }}
        >
          <DropdownMenuItem
            disabled={busy}
            onSelect={() =>
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
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={busy}
            onSelect={() => {
              scrape.reset()
              setCode(movie.code)
              setAction('code')
            }}
          >
            <PencilIcon />
            纠正番号
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={busy}
            onSelect={() => {
              scrape.reset()
              setJavDBID('')
              setAction('javdb')
            }}
          >
            <SearchIcon />
            选择 JavDB 条目
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <DialogContent
        onCloseAutoFocus={event => {
          event.preventDefault()
          trigger.current?.focus()
        }}
      >
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
  )
}
