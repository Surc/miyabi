import { useState } from 'react'
import { LoaderCircleIcon, PauseIcon, PlayIcon, StarIcon } from 'lucide-react'
import type { BrowseMoviesParams, DiscoverMovie } from '@/api/discover'
import { browseImage, useBrowseRating, resetBrowseSessions } from '@/api/browse-cache'
import { MovieCard, MovieGridLayout, MovieGridSkeleton } from '@/components/movie'
import { MovieStateBadge } from '@/components/movie/movie-badges'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { EmptyState } from '@/components/empty-state'
import { InlineError } from '@/components/error-state'
import { ListPagination } from '@/components/list-pagination'
import { MovieDetailTrigger } from '@/features/movie-detail/detail-trigger'
import { MovieSubscribeButton } from '@/features/discover/movie-subscribe-button'
import { useBrowsePreloader } from './use-preloader'
import { BrowseCacheSettingsButton } from './cache-settings'
import type { PreloadRow, PreloadState } from './preloader'

// This panel deliberately remains in the original list layout. Completed fills
// disappear; paused/failed fills retain their controls and the next batch shows it again.
export function BrowseProgress({
  state,
  pause,
  resume
}: {
  state: PreloadState
  pause: () => void
  resume: () => void
}) {
  if (state.phase === 'complete') return null
  return (
    <div role="status" className="space-y-3 rounded-xl border bg-muted/30 p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="font-semibold">
            {state.phase === 'paused'
              ? '预加载已暂停'
              : state.phase === 'error'
                ? '部分数据待确认'
                : '正在准备作品资料与磁力'}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">
            已准备 {state.ready} / {state.target} 部 · {Math.floor(state.ready / 20)} /{' '}
            {state.target / 20} 页
          </p>
        </div>
        <Button size="sm" variant="outline" onClick={state.phase === 'loading' ? pause : resume}>
          {state.phase === 'loading' ? <PauseIcon /> : <PlayIcon />}
          {state.phase === 'loading' ? '暂停' : state.phase === 'error' ? '重试' : '继续'}
        </Button>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          className="h-full bg-foreground transition-[width]"
          style={{ width: `${Math.min(100, (state.ready / state.target) * 100)}%` }}
        />
      </div>
      {state.error ? (
        <p className="text-sm text-destructive">{state.error}</p>
      ) : (
        <p className="text-xs text-muted-foreground">可先浏览，后台继续补齐；图片仅当前页加载。</p>
      )}
    </div>
  )
}
export function CachedBrowseResults({
  params = {},
  query,
  page,
  onPageChange,
  searching = false
}: {
  params?: BrowseMoviesParams
  query?: string
  page: number
  onPageChange: (page: number) => void
  searching?: boolean
}) {
  const [subtitlesOnly, setSubtitlesOnly] = useState(false)
  const { state, controller, settings } = useBrowsePreloader(params, query, page, subtitlesOnly)
  const rows = state.rows.slice((page - 1) * 20, page * 20)
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex gap-2">
          <Button
            size="sm"
            variant={subtitlesOnly ? 'outline' : 'default'}
            onClick={() => {
              setSubtitlesOnly(false)
              onPageChange(1)
            }}
          >
            全部
          </Button>
          <Button
            size="sm"
            variant={subtitlesOnly ? 'default' : 'outline'}
            onClick={() => {
              setSubtitlesOnly(true)
              onPageChange(1)
            }}
          >
            含中文字幕
          </Button>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={resetBrowseSessions}>
            刷新列表
          </Button>
          <BrowseCacheSettingsButton />
        </div>
      </div>
      {settings.isError ? (
        <InlineError onRetry={() => void settings.refetch()}>预加载设置读取失败</InlineError>
      ) : settings.data ? (
        <BrowseProgress
          state={state}
          pause={() => controller.pause()}
          resume={() => {
            void controller.resume()
          }}
        />
      ) : (
        <p role="status" className="text-sm text-muted-foreground">
          正在读取预加载设置…
        </p>
      )}
      {rows.length ? (
        <CurrentBrowsePage rows={state.rows} page={page} />
      ) : state.phase === 'loading' || settings.isPending ? (
        <MovieGridSkeleton count={20} />
      ) : (
        <EmptyState
          title={
            state.phase === 'complete'
              ? subtitlesOnly
                ? '已加载作品中暂无中文字幕资源'
                : searching
                  ? '没有搜索结果'
                  : '暂无内容'
              : '等待作品资料与磁力确认'
          }
        />
      )}
      <ListPagination
        page={page}
        totalPages={
          state.exhausted && state.phase === 'complete'
            ? Math.max(1, Math.ceil(state.rows.length / 20))
            : undefined
        }
        hasMore={state.rows.length > page * 20 || !state.exhausted}
        disabled={!settings.data || (rows.length === 0 && state.phase === 'loading')}
        onPageChange={onPageChange}
      />
      {state.exhausted && state.phase === 'complete' ? (
        <p className="text-center text-xs text-muted-foreground">
          已到来源末尾，共 {state.rows.length} 部符合条件的作品。
        </p>
      ) : null}
    </div>
  )
}
export function CurrentBrowsePage({ rows, page }: { rows: PreloadRow[]; page: number }) {
  return (
    <MovieGridLayout>
      {rows.slice((page - 1) * 20, page * 20).map(row => (
        <CachedMovieCard key={row.movie.id} row={row} />
      ))}
    </MovieGridLayout>
  )
}
function CachedMovieCard({ row }: { row: PreloadRow }) {
  const resource = row.resources
  const movie: DiscoverMovie = row.movie
  const count = resource?.data.magnet_count
  const ready = !!resource && resource.data.complete && !resource.stale
  const subscribable = ready && movie.release_status === 'upcoming' && count === 0
  return (
    <MovieDetailTrigger movie={{ id: movie.id }} className="block rounded-2xl outline-ring">
      <MovieCard
        movie={{ ...movie, cover: movie.cover ? browseImage(movie.id) : '' }}
        description={movie.release_date}
        state={<MovieStateBadge movie={movie} />}
        coverOverlay={
          subscribable ? (
            <div className="absolute top-2 right-2">
              <MovieSubscribeButton movie={movie} />
            </div>
          ) : undefined
        }
      >
        {resource?.data.has_subtitle ? (
          <Badge variant="outline" title={ready ? '磁力包含中文字幕标记' : '来自旧缓存，等待更新'}>
            中文字幕
          </Badge>
        ) : null}
        {resource?.data.has_cracked ? (
          <Badge variant="outline" title="磁力包含破解标记">
            破解
          </Badge>
        ) : null}
        {!ready ? (
          <Badge variant="outline">
            <LoaderCircleIcon className="size-3" />
            资源待确认
          </Badge>
        ) : (
          <Badge variant="outline">{count ?? 0} 条磁力</Badge>
        )}
        <div className="flex w-full flex-wrap gap-x-3 gap-y-1">
          <CardRating id={movie.id} source="javdb" />
          <CardRating id={movie.id} source="javlibrary" />
        </div>
      </MovieCard>
    </MovieDetailTrigger>
  )
}
function CardRating({ id, source }: { id: string; source: 'javdb' | 'javlibrary' }) {
  const query = useBrowseRating(id, source)
  const rating = query.data?.data
  const label = rating?.source === 'fanza' ? 'FANZA' : source === 'javdb' ? 'JavDB' : 'JavLibrary'
  return (
    <span
      className="inline-flex items-center gap-1 text-xs text-muted-foreground"
      title={
        query.data
          ? `${query.data.stale ? '旧缓存，等待更新 · ' : ''}${new Date(query.data.updated_at).toLocaleString()}`
          : query.isError
            ? '评分暂不可用，不影响预加载'
            : '正在读取评分'
      }
    >
      <StarIcon className="size-3 text-amber-500" />
      {label}
      <strong className="font-medium text-foreground">
        {rating?.found && rating.score !== null ? `${rating.score.toFixed(1)}/${rating.max}` : '—'}
      </strong>
    </span>
  )
}
