import { queryOptions, useQuery } from '@tanstack/react-query'
import { apiPost } from './client'
import { libraryKeys } from './library'
import { usePanAccount } from './pan'

export type ResourceVersion = {
  id: number
  available: boolean
  labels: string[]
  unmarked: boolean
}

// Mounting a page issues one bounded batch, while cards retain independent
// query entries under the existing library invalidation/account-reset key.
export function createVersionLoader(fetchVersions: (ids: number[]) => Promise<ResourceVersion[]>) {
  let pending: Array<{
    id: number
    signal: AbortSignal
    resolve: (value: ResourceVersion) => void
    reject: (error: unknown) => void
  }> = []
  async function batch(requests: typeof pending) {
    try {
      const values = await fetchVersions([...new Set(requests.map(request => request.id))])
      const byID = new Map(values.map(value => [value.id, value]))
      for (const request of requests) {
        const value = byID.get(request.id)
        if (request.signal.aborted) request.reject(request.signal.reason)
        else if (value) request.resolve(value)
        else request.reject(new Error('入库版本响应不完整'))
      }
    } catch (error) {
      for (const request of requests) request.reject(error)
    }
  }
  return (id: number, signal: AbortSignal) =>
    new Promise<ResourceVersion>((resolve, reject) => {
      pending.push({ id, signal, resolve, reject })
      if (pending.length === 1)
        queueMicrotask(() => {
          const requests = pending.filter(request => {
            if (!request.signal.aborted) return true
            request.reject(request.signal.reason)
            return false
          })
          pending = []
          for (let start = 0; start < requests.length; start += 100)
            void batch(requests.slice(start, start + 100))
        })
    })
}

const loadVersions = createVersionLoader(ids =>
  apiPost<ResourceVersion[]>('/api/extensions/library/resource-versions', { ids })
)

export function libraryVersionOptions(
  load: ReturnType<typeof createVersionLoader>,
  id: number | undefined,
  scope: readonly string[]
) {
  return queryOptions({
    queryKey: [...libraryKeys.all, 'resource-version', ...scope, id ?? 0],
    queryFn: ({ signal }) => load(id!, signal),
    enabled: !!id,
    staleTime: 0,
    refetchOnMount: 'always'
  })
}

export function useLibraryVersion(id?: number) {
  const account = usePanAccount()
  // A late response from another account/root must never populate this view.
  return useQuery({
    ...libraryVersionOptions(loadVersions, id, [
      account.data?.account?.id ?? '',
      account.data?.directory?.id ?? ''
    ]),
    enabled: !!id && !account.isPending
  })
}
