import type { ComponentProps, ReactNode } from 'react'

import { useRecordMovieView } from '@/api/browse-history'
import { describeApiError, isJavDBLoginRequired } from '@/api/client'
import { useDiscoverMagnets, useDiscoverMovie } from '@/api/discover'
import { type LibraryMovieVideo, useLibraryMovie } from '@/api/library'
import { useMovieState } from '@/api/movie-states'
import { ErrorState, InlineError } from '@/components/error-state'
import type { MovieDetailTarget } from './dialog-context'
import { MovieHero } from './hero'
import { MovieMagnets } from './magnets'
import { MovieMediaPreviews } from './extensions/trailer'
import { JavDBRating, JavLibraryRating, MovieComments } from './extensions/social'
import { MovieRecommendations } from './recommendations'
import { MovieDetailSkeleton } from './skeleton'
import { MovieEmbyPlayback } from './extensions/emby-playback'
import { MovieSynopsis } from './extensions/synopsis'
import { MovieTopRankings } from './extensions/top-rankings'
import { LibrarySubtitles } from './extensions/library-subtitles'

export function MovieDetailContent({
  movie,
  onClose
}: {
  movie: MovieDetailTarget
  onClose?: () => void
}) {
  if ('libraryId' in movie)
    return <LibraryMovieDetail movieId={movie.libraryId} onClose={onClose} />
  return 'id' in movie ? (
    <IdentifiedMovieDetail movieId={movie.id} code={movie.code} />
  ) : (
    <IdentifiedMovieDetail movieId={movie.code} code={movie.code} />
  )
}

function LibraryMovieDetail({ movieId, onClose }: { movieId: number; onClose?: () => void }) {
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
    <CatalogueSections
      movie={detail.data}
      libraryStatus={detail.data.scrape_status}
      isRefetchError={detail.isRefetchError}
      fetching={detail.isFetching}
      onRetry={() => void detail.refetch()}
    >
      <LibrarySubtitles movieId={movieId} videos={detail.data.videos} onMovieRemoved={onClose} />
      <MovieComments javdbId={detail.data.javdb_id ?? ''} code={detail.data.code} />
      {detail.data.javdb_id || detail.data.videos?.some(video => video.download_links?.length) ? (
        <LibraryCatalogueExtras
          movieId={detail.data.javdb_id ?? ''}
          code={detail.data.code}
          videos={detail.data.videos}
        />
      ) : null}
    </CatalogueSections>
  )
}

type HeroMovie = ComponentProps<typeof MovieHero>['movie']
type HeroStatus = ComponentProps<typeof MovieHero>['libraryStatus']

// Shared detail layout for both catalogue and library-backed movies; callers add
// their own extra sections (catalogue extras vs magnets + recommendations).
function CatalogueSections({
  movie,
  libraryStatus,
  isRefetchError,
  fetching,
  onRetry,
  children
}: {
  movie: HeroMovie
  libraryStatus?: HeroStatus
  isRefetchError: boolean
  fetching: boolean
  onRetry: () => void
  children: ReactNode
}) {
  return (
    <div className="space-y-10">
      {isRefetchError ? (
        <InlineError onRetry={onRetry} retrying={fetching}>
          刷新失败，请重试。
        </InlineError>
      ) : null}
      <MovieHero
        movie={movie}
        libraryStatus={libraryStatus}
        extraBadges={<MovieTopRankings code={movie.code} />}
        javdbRating={
          'library_id' in movie ? (
            movie.javdb_id ? (
              <JavDBRating movie={{ ...movie, id: movie.javdb_id }} />
            ) : undefined
          ) : (
            <JavDBRating movie={movie} />
          )
        }
        extraStats={<JavLibraryRating code={movie.code} />}
        actions={<MovieEmbyPlayback code={movie.code} />}
      />
      <MovieSynopsis code={movie.code} plot={movie.plot} />
      <MovieMediaPreviews
        key={movie.id || movie.code}
        images={movie.preview_images}
        video={movie.preview_video}
        cover={movie.cover}
      />
      {children}
    </div>
  )
}

// Catalogue extras remain available for linked films, but cannot block or
// replace the saved library metadata when the catalogue is unavailable.
function LibraryCatalogueExtras({
  movieId,
  code,
  videos
}: {
  movieId: string
  code: string
  videos?: LibraryMovieVideo[]
}) {
  const magnets = useDiscoverMagnets(movieId, code)
  return (
    <>
      <MovieMagnets
        movieID={movieId}
        javdbID={movieId}
        code={code}
        query={magnets}
        videos={videos}
      />
      {movieId ? <LibraryRecommendations movieId={movieId} code={code} /> : null}
    </>
  )
}

function LibraryRecommendations({ movieId, code }: { movieId: string; code: string }) {
  const detail = useDiscoverMovie(movieId, code)
  useRecordMovieView(movieId)
  return detail.data ? (
    <>
      <MovieRecommendations title="TA（们）还出演过" movies={detail.data.actor_movies} />
      <MovieRecommendations title="你可能也喜欢" movies={detail.data.related_movies} />
    </>
  ) : null
}

function IdentifiedMovieDetail({ movieId, code }: { movieId: string; code?: string }) {
  const detail = useDiscoverMovie(movieId, code)
  const magnets = useDiscoverMagnets(movieId, detail.data?.code ?? code)
  useRecordMovieView(movieId)

  if (!detail.data && detail.isPending) return <MovieDetailSkeleton />
  if (!detail.data) {
    return (
      <ErrorState
        message={
          isJavDBLoginRequired(detail.error) ? describeApiError(detail.error) : '影片详情加载失败'
        }
        onRetry={() => void detail.refetch()}
        retrying={detail.isFetching}
      />
    )
  }
  const javdbID =
    detail.data.sources?.find(source => source.provider === 'javdb')?.id ??
    (movieId !== detail.data.code ? movieId : '')

  return (
    <CatalogueSections
      movie={detail.data}
      isRefetchError={detail.isRefetchError}
      fetching={detail.isFetching}
      onRetry={() => void detail.refetch()}
    >
      <CatalogueLibrarySubtitles movie={detail.data} />
      <MovieMagnets movieID={movieId} javdbID={javdbID} code={detail.data.code} query={magnets} />
      <MovieComments javdbId={javdbID} code={detail.data.code} />
      <MovieRecommendations title="TA（们）还出演过" movies={detail.data.actor_movies} />
      <MovieRecommendations title="你可能也喜欢" movies={detail.data.related_movies} />
    </CatalogueSections>
  )
}

function CatalogueLibrarySubtitles({ movie }: { movie: { id: string; code: string } }) {
  const { state, library_id, isPlaceholderData } = useMovieState(movie)
  return !isPlaceholderData && state === 'in_library' && library_id ? (
    <LibrarySubtitles movieId={library_id} />
  ) : null
}
