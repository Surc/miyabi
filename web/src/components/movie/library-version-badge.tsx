import { LoaderCircleIcon } from 'lucide-react'
import { useLibraryVersion, type ResourceVersion } from '@/api/library-versions'
import { useMovieState, type MovieIdentity } from '@/api/movie-states'
import { Badge } from '@/components/ui/badge'

export function VersionLabels({ value }: { value?: ResourceVersion }) {
  if (!value?.available || !value.labels.length) return null
  return (
    <div
      className="pointer-events-none absolute bottom-2 left-2 flex max-w-[calc(100%-1rem)] flex-wrap gap-1"
      title={`根据实际入库文件名标记，不同文件的版本分别显示${value.unmarked ? '；另有未标记文件' : ''}`}
    >
      {value.labels.map(label => (
        <Badge key={label} variant="frosted">
          {label}
        </Badge>
      ))}
    </div>
  )
}

export function LibraryVersionBadge({ id }: { id?: number }) {
  const query = useLibraryVersion(id)
  if (!id) return null
  if (query.isFetching || query.isPending)
    return (
      <Badge variant="frosted" role="status" className="absolute bottom-2 left-2">
        <LoaderCircleIcon className="animate-spin" />
        版本加载中
      </Badge>
    )
  if (query.isError) return null
  return <VersionLabels value={query.data} />
}

export function OwnedVersionBadge({ movie }: { movie: MovieIdentity }) {
  const state = useMovieState(movie)
  return (
    <LibraryVersionBadge
      id={!state.isPlaceholderData && state.state === 'in_library' ? state.library_id : undefined}
    />
  )
}
