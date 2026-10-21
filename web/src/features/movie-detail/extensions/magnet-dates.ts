export type MagnetOrder = 'recommended' | 'newest' | 'oldest'

// Validate calendar fields before Date.parse: JS otherwise rolls invalid dates
// (e.g. February 30) into the next month. Naive source timestamps use UTC.
export function magnetPublication(raw: string): { day: string; timestamp: number } | null {
  const text = raw.trim()
  const m =
    /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(\.\d{1,3})?)?(Z|[+-]\d{2}:\d{2})?)?$/.exec(
      text
    )
  if (!m) return null
  const [year, month, day] = [Number(m[1]), Number(m[2]), Number(m[3])]
  if (
    year < 1000 ||
    month < 1 ||
    month > 12 ||
    day < 1 ||
    day > new Date(Date.UTC(year, month, 0)).getUTCDate()
  )
    return null
  if (m[4] && (Number(m[4]) > 23 || Number(m[5]) > 59 || Number(m[6] ?? 0) > 59)) return null
  if (m[8] && m[8] !== 'Z' && (Number(m[8].slice(1, 3)) > 23 || Number(m[8].slice(4)) > 59))
    return null
  const timestamp = Date.parse(
    m[4]
      ? `${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6] ?? '00'}${m[7] ?? ''}${m[8] ?? 'Z'}`
      : `${text}T00:00:00Z`
  )
  return Number.isFinite(timestamp) ? { day: text.slice(0, 10), timestamp } : null
}

// Presentation only: never mutate React Query data or the backend's picker order.
export function sortMagnets<T extends { created_at: string }>(
  items: readonly T[],
  order: MagnetOrder
): T[] {
  const copy = [...items]
  if (order === 'recommended') return copy
  const dated = copy.map((item, index) => ({
    item,
    index,
    date: magnetPublication(item.created_at)
  }))
  dated.sort((a, b) => {
    if (!a.date) return b.date ? 1 : a.index - b.index
    if (!b.date) return -1
    return (
      (order === 'newest'
        ? b.date.timestamp - a.date.timestamp
        : a.date.timestamp - b.date.timestamp) || a.index - b.index
    )
  })
  return dated.map(row => row.item)
}
