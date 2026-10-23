// Format only the display value. Saved metadata, filenames and API contracts
// keep their original title; existing films therefore need no rescrape.
export function movieDisplayTitle(code: string, title: string) {
  code = code.trim()
  title = title.trim()
  if (!code) return title
  if (!title) return code
  const escaped = code.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const existing = new RegExp(`(^|[^A-Za-z0-9])${escaped}(?=$|[^A-Za-z0-9])`, 'i')
  const match = existing.exec(title)
  if (match) {
    title =
      title.slice(0, match.index + (match[1] ?? '').length) +
      title.slice(match.index + match[0].length)
    title = title
      .replace(/^[\s·:：\-–—[\]【】()（）]+|[\s·:：\-–—[\]【】()（）]+$/g, '')
      .replace(/\s{2,}/g, ' ')
  }
  return title ? `${code} · ${title}` : code
}
