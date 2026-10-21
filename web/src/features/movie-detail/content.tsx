import { useRecordMovieView } from '@/api/browse-history'
import { useDiscoverMagnets, useDiscoverMovie, useResolveDiscoverMovie } from '@/api/discover'
import { useLibraryMovie } from '@/api/library'
import { ErrorState, InlineError } from '@/components/error-state'
import type { MovieDetailTarget } from './dialog-context'
import { MovieHero } from './hero'
import { MovieMagnets } from './magnets'
import { MovieMediaPreviews } from './extensions/trailer'
import { JavLibraryRating, MovieComments } from './extensions/social'
import { MovieRecommendations } from './recommendations'
import { MovieDetailSkeleton } from './skeleton'

export function MovieDetailContent({ movie }: { movie: MovieDetailTarget }) {
  if ('libraryId' in movie) return <LibraryMovieDetail movieId={movie.libraryId} />
  return 'id' in movie ? (
    <IdentifiedMovieDetail movieId={movie.id} />
  ) : (
    <MovieDetailByCode code={movie.code} />
  )
}

function LibraryMovieDetail({ movieId }: { movieId: number }) {
  const detail = useLibraryMovie(movieId)
  if (detail.isPending) return <MovieDetailSkeleton />
  if (!detail.data) {
    return (
      <ErrorState
        message="媒体库详情加载失败"
        onRetry={() => void detail.refetch()}
        retrying={detail.isFetching}
      />
    )
  }
  return (
    <div className="space-y-10">
      {detail.isRefetchError ? (
        <InlineError onRetry={() => void detail.refetch()} retrying={detail.isFetching}>
          刷新失败，请重试。
        </InlineError>
      ) : null}
      <MovieHero
        movie={detail.data}
        libraryStatus={detail.data.scrape_status}
        extraStats={<JavLibraryRating code={detail.data.code} />}
      />
      <MovieMediaPreviews
        images={detail.data.preview_images}
        video={detail.data.preview_video}
        cover={detail.data.cover}
      />
      <MovieComments javdbId={detail.data.id} code={detail.data.code} />
      {detail.data.id ? <LibraryCatalogueExtras movieId={detail.data.id} /> : null}
    </div>
  )
}

// Catalogue extras remain available for linked films, but cannot block or
// replace the saved library metadata when JavDB is unavailable.
function LibraryCatalogueExtras({ movieId }: { movieId: string }) {
  const detail = useDiscoverMovie(movieId)
  const magnets = useDiscoverMagnets(movieId)
  useRecordMovieView(movieId)
  return (
    <>
      <MovieMagnets movieID={movieId} query={magnets} />
      {detail.data ? (
        <>
          <MovieRecommendations title="TA（们）还出演过" movies={detail.data.actor_movies} />
          <MovieRecommendations title="你可能也喜欢" movies={detail.data.related_movies} />
        </>
      ) : null}
    </>
  )
}

function MovieDetailByCode({ code }: { code: string }) {
  const identity = useResolveDiscoverMovie(code)
  if (identity.isPending) return <MovieDetailSkeleton />
  if (!identity.data) {
    return (
      <ErrorState
        message={`未能从 JavDB 确认 ${code} 的影片详情`}
        onRetry={() => void identity.refetch()}
        retrying={identity.isFetching}
      />
    )
  }
  return <IdentifiedMovieDetail key={identity.data.id} movieId={identity.data.id} />
}

function IdentifiedMovieDetail({ movieId }: { movieId: string }) {
  const detail = useDiscoverMovie(movieId)
  const magnets = useDiscoverMagnets(movieId)
  useRecordMovieView(movieId)

  if (detail.isPending) return <MovieDetailSkeleton />
  if (!detail.data) {
    return (
      <ErrorState
        message="影片详情加载失败"
        onRetry={() => void detail.refetch()}
        retrying={detail.isFetching}
      />
    )
  }

  return (
    <div className="space-y-10">
      {detail.isRefetchError ? (
        <InlineError onRetry={() => void detail.refetch()} retrying={detail.isFetching}>
          刷新失败，请重试。
        </InlineError>
      ) : null}
      <MovieHero movie={detail.data} extraStats={<JavLibraryRating code={detail.data.code} />} />
      <MovieMediaPreviews
        images={detail.data.preview_images}
        video={detail.data.preview_video}
        cover={detail.data.cover}
      />
      <MovieMagnets movieID={movieId} query={magnets} />
      <MovieComments javdbId={movieId} code={detail.data.code} />
      <MovieRecommendations title="TA（们）还出演过" movies={detail.data.actor_movies} />
      <MovieRecommendations title="你可能也喜欢" movies={detail.data.related_movies} />
    </div>
  )
}
