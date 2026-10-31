import { getRouteApi } from '@tanstack/react-router'

import { TOP_PAGE_SIZE, useTopConfig, useTopInfo, useTopMovies, type TopMovie } from '@/api/top'
import { AppPage } from '@/components/app-page'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { ListPagination } from '@/components/list-pagination'
import { MovieCard, MovieGridLayout, MovieGridSkeleton } from '@/components/movie'
import { PageHeader } from '@/components/page-header'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { MovieDetailTrigger } from '@/features/movie-detail/detail-trigger'
import { TopRankBadge } from './rank-badge'
import { TOP_CATEGORIES } from './search'

const route = getRouteApi('/top')

export function TopPage() {
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const settings = useTopConfig()
  const board = search.board ?? settings.data?.default_board ?? 'all'
  const page = search.page ?? 1
  const info = useTopInfo()
  const annual = /^\d{4}$/.test(board)
  const setBoard = (next: string) => void navigate({ search: { board: next } })

  if (!settings.data) {
    return (
      <AppPage>
        <PageHeader title="TOP 榜单" description="JavDB TOP250 总榜、分类榜与年度榜" />
        {settings.isError ? (
          <ErrorState
            message="榜单设置加载失败"
            onRetry={() => void settings.refetch()}
            retrying={settings.isFetching}
          />
        ) : (
          <MovieGridSkeleton count={TOP_PAGE_SIZE} />
        )}
      </AppPage>
    )
  }

  return (
    <AppPage>
      <PageHeader title="TOP 榜单" description="JavDB TOP250 总榜、分类榜与年度榜" />
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <Tabs
          value={annual ? 'annual' : board}
          onValueChange={value =>
            setBoard(
              value === 'annual'
                ? (info.data?.boards.find(item => /^\d{4}$/.test(item.id))?.id ?? '2025')
                : value
            )
          }
        >
          <TabsList className="max-w-full">
            {TOP_CATEGORIES.map(item => (
              <TabsTrigger key={item.id} value={item.id}>
                {item.label}
              </TabsTrigger>
            ))}
            <TabsTrigger value="annual">年度</TabsTrigger>
          </TabsList>
        </Tabs>
        {annual ? (
          <Select value={board} onValueChange={setBoard}>
            <SelectTrigger className="w-full sm:w-44" aria-label="榜单年份">
              <SelectValue />
            </SelectTrigger>
            <SelectContent position="popper" align="start">
              <SelectGroup>
                {(info.data?.boards ?? [])
                  .filter(item => /^\d{4}$/.test(item.id))
                  .map(item => (
                    <SelectItem key={item.id} value={item.id}>
                      {item.id} 年度
                    </SelectItem>
                  ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : null}
      </div>
      {info.isError ? (
        <ErrorState
          message="榜单信息加载失败"
          onRetry={() => void info.refetch()}
          retrying={info.isFetching}
        />
      ) : (
        <>
          {info.data ? (
            <p className="text-xs leading-6 text-muted-foreground">
              数据更新于 {info.data.updated_at} ·{' '}
              <a
                href={info.data.source_url}
                target="_blank"
                rel="noreferrer"
                className="underline underline-offset-4"
              >
                总榜来源
              </a>
              {' · '}
              <a
                href={info.data.annual_source_url}
                target="_blank"
                rel="noreferrer"
                className="underline underline-offset-4"
              >
                分类与年度榜来源
              </a>
            </p>
          ) : null}
          <TopResults
            board={board}
            page={page}
            onPageChange={next =>
              void navigate({ search: { board, page: next > 1 ? next : undefined } })
            }
          />
        </>
      )}
    </AppPage>
  )
}

export function TopResults({
  board,
  page,
  onPageChange
}: {
  board: string
  page: number
  onPageChange: (page: number) => void
}) {
  const movies = useTopMovies(board, page)
  if (movies.isError) {
    return (
      <ErrorState
        message="榜单加载失败"
        onRetry={() => void movies.refetch()}
        retrying={movies.isFetching}
      />
    )
  }
  if (!movies.data) return <MovieGridSkeleton count={TOP_PAGE_SIZE} />
  const totalPages = Math.ceil(movies.data.total / TOP_PAGE_SIZE)
  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold">
        {movies.data.board.name}{' '}
        <span className="text-sm font-normal text-muted-foreground">· {movies.data.total} 部</span>
      </h2>
      {movies.data.items.length ? (
        <MovieGridLayout>
          {movies.data.items.map(movie => (
            <TopMovieCard key={movie.rank} movie={movie} />
          ))}
        </MovieGridLayout>
      ) : (
        <EmptyState title="这一页暂无影片" />
      )}
      <ListPagination
        page={page}
        totalPages={totalPages}
        hasMore={page < totalPages}
        disabled={movies.isFetching}
        onPageChange={onPageChange}
      />
    </div>
  )
}

export function TopMovieCard({ movie }: { movie: TopMovie }) {
  return (
    <MovieDetailTrigger movie={{ code: movie.code }} className="block rounded-2xl outline-ring">
      <MovieCard
        movie={movie}
        description={movie.release_date}
        coverOverlay={
          <div className="absolute top-2 right-2">
            <TopRankBadge rank={movie.rank} />
          </div>
        }
      />
    </MovieDetailTrigger>
  )
}
