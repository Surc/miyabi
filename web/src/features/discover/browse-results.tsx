import type { BrowseMoviesParams } from '@/api/discover'
import { CachedBrowseResults } from './extensions/cached-results'

export function BrowseResults({
  params,
  onPageChange
}: {
  params: BrowseMoviesParams & { page: number }
  onPageChange: (page: number) => void
}) {
  return <CachedBrowseResults params={params} page={params.page} onPageChange={onPageChange} />
}
