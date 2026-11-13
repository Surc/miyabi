import type { LibraryMovie } from '@/api/library'
import { MovieCard } from '@/components/movie'
import { LibraryVersionBadge } from '@/components/movie/library-version-badge'
import { MovieSubtitleBadge } from '@/components/movie/movie-badges'
import { MovieDetailTrigger } from '@/features/movie-detail/detail-trigger'
import { LibraryMovieStatus } from './movie-status'
import { LibraryMovieActions } from './movie-actions'

export function LibraryMovieCard({ movie, busy = false }: { movie: LibraryMovie; busy?: boolean }) {
  return (
    <div className="relative h-full min-w-0">
      <MovieDetailTrigger
        movie={{ libraryId: movie.id }}
        className="block h-full min-w-0 rounded-2xl outline-ring"
      >
        <MovieCard
          movie={movie}
          titleTooltip={false}
          coverOverlay={<LibraryVersionBadge id={movie.id} />}
        >
          <div className="flex min-h-7 flex-wrap items-center gap-1.5 pr-9">
            <LibraryMovieStatus movie={movie} />
            <MovieSubtitleBadge movie={movie} />
          </div>
        </MovieCard>
      </MovieDetailTrigger>
      <div className="absolute right-2 bottom-2">
        <LibraryMovieActions movie={movie} disabled={busy} />
      </div>
    </div>
  )
}
