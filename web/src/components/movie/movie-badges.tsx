import { useIsMovieViewed } from '@/api/browse-history'
import type { DiscoverMovie } from '@/api/discover'
import type { MovieIdentity } from '@/api/movie-states'
import { useMovieState } from '@/api/movie-states'
import { Badge } from '@/components/ui/badge'
import { isSubtitleLocalizationTag } from '@/lib/subtitle-tags'

export function MovieSubtitleBadge({
  movie,
  label = '字幕'
}: {
  movie: { has_subtitle?: boolean; tags: readonly { name: string; name_zht?: string }[] }
  label?: string
}) {
  return movie.has_subtitle || movie.tags?.some(isSubtitleLocalizationTag) ? (
    <Badge variant="outline">{label}</Badge>
  ) : null
}

export function MovieResourceBadges({
  movie,
  subtitleLabel
}: {
  movie: DiscoverMovie
  subtitleLabel?: string
}) {
  return (
    <>
      <MovieSubtitleBadge movie={movie} label={subtitleLabel} />
      {movie.has_preview ? <Badge variant="outline">有预览</Badge> : null}
      {movie.magnets_count > 0 ? <Badge variant="outline">含磁力</Badge> : null}
      {movie.release_status === 'upcoming' ? <Badge variant="outline">即将发行</Badge> : null}
    </>
  )
}

export function MovieStateBadge({
  movie,
  hideViewed = false
}: {
  movie: MovieIdentity
  hideViewed?: boolean
}) {
  const { state } = useMovieState(movie)
  const isViewed = useIsMovieViewed(movie.id)

  if (state === 'in_library') {
    return <Badge variant="library">已入库</Badge>
  }
  if (state === 'saving') {
    return <Badge variant="downloading">下载中</Badge>
  }
  if (state === 'processing') {
    return <Badge variant="frosted">入库处理中</Badge>
  }
  if (!hideViewed && isViewed) {
    return <Badge variant="success">已浏览</Badge>
  }
  return null
}
