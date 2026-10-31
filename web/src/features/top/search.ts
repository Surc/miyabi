import { parseSearchPage } from '@/lib/search-schema'

export const TOP_CATEGORIES = [
  { id: 'all', label: '总榜' },
  { id: 'censored', label: '有码' },
  { id: 'uncensored', label: '无码' },
  { id: 'western', label: '欧美' },
  { id: 'fc2', label: 'FC2' }
]

export type TopSearch = { board?: string; page?: number }

export function validateTopSearch(search: Record<string, unknown>): TopSearch {
  const board = search.board
  if (
    board !== undefined &&
    (typeof board !== 'string' ||
      (!TOP_CATEGORIES.some(item => item.id === board) &&
        !/^(200[89]|201\d|202[0-5])$/.test(board)))
  ) {
    throw new Error('榜单无效')
  }
  const page = parseSearchPage(search.page)
  if (page && page > 1_000_000) throw new Error('页码无效')
  return { ...(board ? { board } : {}), ...(page ? { page } : {}) }
}
