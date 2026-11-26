import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { RotateCwIcon, StarIcon } from 'lucide-react'
import { useBrowseRating } from '@/api/browse-cache'
import { ApiError, describeApiError } from '@/api/client'
import type { DiscoverMovie } from '@/api/discover'
import {
  requestJavLibraryVerification,
  useJavLibraryRating,
  useMovieComments,
  type CommentSource
} from '@/api/movie-extras'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type BrowserProblem = 'verification' | 'disconnected'

function javLibraryBrowserProblem(error: unknown): BrowserProblem | undefined {
  if (!(error instanceof ApiError)) return
  if (
    error.code === 'JAVLIBRARY_VERIFICATION_REQUIRED' ||
    (!error.code && error.message === 'JavLibrary 需要在已连接的浏览器中完成人工验证')
  )
    return 'verification'
  if (
    error.code === 'JAVLIBRARY_BROWSER_DISCONNECTED' ||
    (!error.code && error.message === '当前电脑的 JavLibrary 验证服务尚未连接 Miyabi')
  )
    return 'disconnected'
}

function JavLibraryVerification({
  problem,
  onRetry,
  retryOnFocus = false
}: {
  problem: BrowserProblem | undefined
  onRetry: (options?: { cancelRefetch: false }) => void
  retryOnFocus?: boolean
}) {
  const [guideOpen, setGuideOpen] = useState(false)
  const verification = useMutation({
    mutationFn: requestJavLibraryVerification,
    onSuccess: (_, action) => {
      if (action !== 'confirm') return
      setGuideOpen(false)
      onRetry()
    }
  })
  useEffect(() => {
    if (!retryOnFocus || !problem || guideOpen) return
    const retry = () => onRetry({ cancelRefetch: false })
    window.addEventListener('focus', retry)
    return () => window.removeEventListener('focus', retry)
  }, [guideOpen, onRetry, problem, retryOnFocus])

  if (!problem && !guideOpen) return null
  return (
    <Dialog
      open={guideOpen}
      onOpenChange={open => {
        setGuideOpen(open)
        if (open) verification.mutate('open')
      }}
    >
      <DialogTrigger
        render={
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="打开 JavLibrary 验证引导"
            title="打开 JavLibrary 验证引导"
            disabled={verification.isPending}
          />
        }
      >
        <RotateCwIcon />
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>JavLibrary 验证</DialogTitle>
          <DialogDescription>
            请在专用 Edge 中完成验证，再回来点「验证完成」。确认成功后会自动重试。
          </DialogDescription>
        </DialogHeader>
        {verification.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {describeApiError(verification.error)}
          </p>
        ) : null}
        <DialogFooter>
          <Button
            variant="outline"
            disabled={verification.isPending}
            onClick={() => verification.mutate('open')}
          >
            {verification.isPending && verification.variables === 'open'
              ? '正在打开专用 Edge…'
              : '打开专用 Edge'}
          </Button>
          <Button disabled={verification.isPending} onClick={() => verification.mutate('confirm')}>
            {verification.isPending && verification.variables === 'confirm'
              ? '正在确认…'
              : '验证完成'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function JavDBRating({
  movie
}: {
  movie: Pick<DiscoverMovie, 'id' | 'rating' | 'rating_source' | 'rating_max'>
}) {
  const query = useBrowseRating(movie.id, 'javdb', 'always')
  const rating = query.data?.data
  const savedScore = movie.rating_source === 'javdb' && movie.rating > 0 ? movie.rating : null
  const score = rating?.score ?? savedScore
  const max = rating?.score != null ? rating.max : movie.rating_max || 5
  return (
    <div className="flex flex-auto flex-col items-center gap-2 px-2 py-3 sm:px-3 sm:py-4">
      <dt className="flex items-center gap-1.5 text-xs whitespace-nowrap text-muted-foreground">
        <StarIcon className="size-4 shrink-0" />
        JavDB 评分
      </dt>
      <dd className="flex items-center gap-1 text-sm font-semibold whitespace-nowrap tabular-nums sm:text-base">
        {score !== null
          ? `${score.toFixed(1)} / ${max}`
          : query.isError
            ? '暂不可用'
            : query.isPending
              ? '加载中…'
              : '暂无评分'}
        {query.isError ? (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="重试 JavDB 评分"
            title="重试 JavDB 评分"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RotateCwIcon />
          </Button>
        ) : null}
      </dd>
    </div>
  )
}

export function JavLibraryRating({ code }: { code: string }) {
  const query = useJavLibraryRating(code)
  const { error, refetch, isError, isFetching } = query
  const problem = javLibraryBrowserProblem(error)
  let value = '加载中…'
  if (!code.trim()) value = '缺少番号'
  else if (query.data)
    value = !query.data.found
      ? '未收录'
      : query.data.score === null
        ? '暂无评分'
        : `${query.data.score.toFixed(1)} / ${query.data.max}`
  else if (isError) value = problem === 'verification' ? '需要验证' : '暂不可用'
  return (
    <div className="flex flex-auto flex-col items-center gap-2 px-2 py-3 sm:px-3 sm:py-4">
      <dt className="flex items-center gap-1.5 text-xs whitespace-nowrap text-muted-foreground">
        <StarIcon className="size-4 shrink-0" />
        JavLibrary 评分
      </dt>
      <dd className="flex items-center gap-1 text-sm font-semibold whitespace-nowrap tabular-nums sm:text-base">
        {value}
        <JavLibraryVerification problem={problem} onRetry={refetch} retryOnFocus />
        {isError && !problem ? (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="重试评分"
            title="重试评分"
            disabled={isFetching}
            onClick={() => void refetch()}
          >
            <RotateCwIcon />
          </Button>
        ) : null}
      </dd>
      {problem ? (
        <dd className="max-w-44 text-center text-xs text-muted-foreground">
          {problem === 'disconnected' ? '请先启动本机验证服务，再点刷新。' : '点刷新打开验证引导。'}
        </dd>
      ) : null}
    </div>
  )
}

export function MovieComments({ javdbId, code }: { javdbId: string; code: string }) {
  return (
    <section className="space-y-4">
      <h2 className="text-xl font-semibold tracking-normal">站点评论</h2>
      <Tabs
        key={`${javdbId}:${code}`}
        defaultValue={javdbId || !code.trim() ? 'javdb' : 'javlibrary'}
      >
        <TabsList aria-label="评论来源">
          <TabsTrigger value="javdb">JavDB</TabsTrigger>
          <TabsTrigger value="javlibrary">JavLibrary</TabsTrigger>
        </TabsList>
        <TabsContent value="javdb">
          <CommentList source="javdb" javdbId={javdbId} code={code} />
        </TabsContent>
        <TabsContent value="javlibrary">
          <CommentList source="javlibrary" javdbId={javdbId} code={code} />
        </TabsContent>
      </Tabs>
    </section>
  )
}

// Tabs mount only their active panel; pagination and failures are source-specific.
export function CommentList({
  source,
  javdbId,
  code
}: {
  source: CommentSource
  javdbId: string
  code: string
}) {
  const query = useMovieComments(source, javdbId, code)
  if (source === 'javdb' && !javdbId)
    return (
      <p className="text-sm text-muted-foreground">此影片尚未关联 JavDB，暂无法读取该站评论。</p>
    )
  if (source === 'javlibrary' && !code.trim())
    return <p className="text-sm text-muted-foreground">缺少番号，暂无法读取 JavLibrary 评论。</p>
  if (query.isPending)
    return (
      <p role="status" className="text-sm text-muted-foreground">
        正在加载评论…
      </p>
    )
  const seen = new Set<string>()
  const comments = (query.data?.pages.flatMap(page => page.items) ?? []).filter(item => {
    if (seen.has(item.id)) return false
    seen.add(item.id)
    return true
  })
  const refreshError = query.data?.pages.find(page => page.refreshError)?.refreshError
  const error = query.error ?? refreshError
  const problem = source === 'javlibrary' ? javLibraryBrowserProblem(error) : undefined
  const retry = () =>
    void (query.isFetchNextPageError
      ? query.fetchNextPage({ cancelRefetch: false })
      : query.refetch({ cancelRefetch: false }))
  return (
    <div className="space-y-4 pt-3">
      {comments.map(comment => (
        <article key={comment.id} className="space-y-2 rounded-xl border p-4">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <strong className="text-sm text-foreground">{comment.author || '匿名'}</strong>
            {comment.created_at ? <span>{comment.created_at}</span> : null}
            {comment.rating !== undefined ? (
              <span>
                评分 {comment.rating} / {comment.rating_max}
              </span>
            ) : null}
            {comment.likes !== undefined ? <span>赞 {comment.likes}</span> : null}
          </div>
          <p className="text-sm leading-7 wrap-break-word whitespace-pre-wrap">{comment.content}</p>
        </article>
      ))}
      {query.isError || refreshError ? (
        <div className="flex items-center gap-2">
          <InlineError onRetry={problem ? undefined : retry} retrying={query.isFetching}>
            {query.isError ? '评论加载失败。' : '评论更新失败，已保留缓存。'}
            {error instanceof ApiError ? describeApiError(error) : null}
          </InlineError>
          {source === 'javlibrary' ? (
            <JavLibraryVerification problem={problem} onRetry={retry} />
          ) : null}
        </div>
      ) : null}
      {!comments.length && !query.isError && !refreshError ? (
        <p className="text-sm text-muted-foreground">暂无评论</p>
      ) : null}
      {query.hasNextPage && !query.isFetchNextPageError ? (
        <Button
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
        >
          {query.isFetchingNextPage ? '加载中…' : '加载更多评论'}
        </Button>
      ) : null}
    </div>
  )
}
