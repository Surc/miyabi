import { StarIcon } from 'lucide-react'
import { useJavLibraryRating, useMovieComments, type CommentSource } from '@/api/movie-extras'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

export function JavLibraryRating({ code }: { code: string }) {
  const query = useJavLibraryRating(code)
  let value = '加载中…'
  if (!code.trim()) value = '缺少番号'
  else if (query.data)
    value = !query.data.found
      ? '未收录'
      : query.data.score === null
        ? '暂无评分'
        : `${query.data.score.toFixed(1)} / ${query.data.max}`
  else if (query.isError) value = '暂不可用'
  return (
    <div className="flex min-w-0 flex-1 flex-col items-center gap-2 p-3 sm:p-4">
      <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <StarIcon className="size-4" />
        JavLibrary 评分
      </dt>
      <dd className="text-sm font-semibold tabular-nums sm:text-base">{value}</dd>
      {query.isError ? (
        <Button
          size="sm"
          variant="ghost"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          重试评分
        </Button>
      ) : null}
    </div>
  )
}

export function MovieComments({ javdbId, code }: { javdbId: string; code: string }) {
  return (
    <section className="space-y-4">
      <h2 className="text-xl font-semibold tracking-normal">站点评论</h2>
      <Tabs key={`${javdbId}:${code}`} defaultValue={javdbId ? 'javdb' : 'javlibrary'}>
        <TabsList aria-label="评论来源">
          <TabsTrigger value="javdb">JavDB</TabsTrigger>
          <TabsTrigger value="javlibrary">JavLibrary</TabsTrigger>
        </TabsList>
        {(['javdb', 'javlibrary'] as const).map(source => (
          <TabsContent key={source} value={source}>
            <CommentList source={source} javdbId={javdbId} code={code} />
          </TabsContent>
        ))}
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
      {query.isError ? (
        <InlineError
          onRetry={() =>
            void (query.isFetchNextPageError ? query.fetchNextPage() : query.refetch())
          }
          retrying={query.isFetching}
        >
          评论加载失败，请重试。
        </InlineError>
      ) : null}
      {!comments.length && !query.isError ? (
        <p className="text-sm text-muted-foreground">暂无评论</p>
      ) : null}
      {query.hasNextPage && !query.isFetchNextPageError ? (
        <Button
          variant="outline"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage ? '加载中…' : '加载更多评论'}
        </Button>
      ) : null}
    </div>
  )
}
