import { CalendarIcon, ClockIcon, StarIcon, type LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import type { DiscoverMovie, DiscoverMovieDetail } from '@/api/discover'
import type { LibraryMovieDetail } from '@/api/library'
import { MovieResourceBadges, MovieStateBadge } from '@/components/movie/movie-badges'
import { MovieCover } from '@/components/movie/movie-cover'
import { Badge } from '@/components/ui/badge'
import { MovieMetadata } from './metadata'
import { movieDisplayTitle } from '@/components/movie/display-title'

export function MovieHero({
  movie,
  libraryStatus,
  extraStats,
  actions
}: {
  movie: DiscoverMovieDetail | LibraryMovieDetail
  libraryStatus?: LibraryMovieDetail['scrape_status']
  extraStats?: ReactNode
  actions?: ReactNode
}) {
  return (
    <section className="grid items-start gap-6 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] lg:gap-8">
      <div className="aspect-3/2 overflow-hidden rounded-2xl bg-muted ring-1 ring-foreground/10">
        <MovieCover source={movie.cover} loading="eager" />
      </div>

      <div className="min-w-0 space-y-5 py-1">
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className="tabular-nums">
              {movie.code}
            </Badge>
            {libraryStatus ? (
              <Badge variant={libraryStatus === 'done' ? 'library' : 'secondary'}>
                {{ done: '已入库', pending: '待刮削', failed: '刮削失败' }[libraryStatus]}
              </Badge>
            ) : (
              <MovieStateBadge movie={movie} hideViewed />
            )}
          </div>
          <h1 className="text-xl leading-snug font-bold tracking-normal sm:text-2xl">
            {movieDisplayTitle(movie.code, movie.title)}
          </h1>
          {movie.origin_title && movie.origin_title !== movie.title ? (
            <p className="text-sm leading-6 text-muted-foreground">{movie.origin_title}</p>
          ) : null}
        </div>

        {actions}
        <MovieStats movie={movie} extraStats={extraStats} />

        {!libraryStatus && 'release_status' in movie ? (
          <div className="flex flex-wrap gap-2">
            <MovieResourceBadges movie={movie} />
          </div>
        ) : null}

        <MovieMetadata movie={movie} />
      </div>
    </section>
  )
}

function MovieStats({
  movie,
  extraStats
}: {
  movie: Pick<
    DiscoverMovie,
    'release_date' | 'duration' | 'rating' | 'rating_source' | 'rating_max'
  >
  extraStats?: ReactNode
}) {
  const stats: Array<{ label: string; value: string; icon: LucideIcon }> = []
  if (movie.release_date) {
    stats.push({ label: '发行日期', value: movie.release_date, icon: CalendarIcon })
  }
  if (movie.duration > 0) {
    stats.push({ label: '片长', value: `${movie.duration} 分钟`, icon: ClockIcon })
  }
  if (movie.rating > 0) {
    const source = movie.rating_source === 'javdb' ? 'JavDB' : movie.rating_source?.toUpperCase()
    stats.push({
      label: source ? `${source} 评分` : '评分',
      value: movie.rating_max
        ? `${movie.rating.toFixed(1)} / ${movie.rating_max}`
        : movie.rating.toFixed(1),
      icon: StarIcon
    })
  }
  if (stats.length === 0 && !extraStats) return null

  return (
    <dl className="flex divide-x divide-border/60 overflow-x-auto rounded-2xl bg-muted py-1 ring-1 ring-border dark:bg-card/60 dark:ring-0">
      {stats.map(stat => (
        <div
          key={stat.label}
          className="flex flex-auto flex-col items-center gap-2 px-2 py-3 sm:px-3 sm:py-4"
        >
          <dt className="flex items-center gap-1.5 text-xs whitespace-nowrap text-muted-foreground">
            <stat.icon className="size-4 shrink-0" />
            {stat.label}
          </dt>
          <dd className="text-sm font-semibold whitespace-nowrap tabular-nums sm:text-base">
            {stat.value}
          </dd>
        </div>
      ))}
      {extraStats}
    </dl>
  )
}
