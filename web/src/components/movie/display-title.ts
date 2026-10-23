// Format only the display value. Saved metadata, filenames and API contracts
// keep their original title; existing films therefore need no rescrape.
export function movieDisplayTitle(code: string, title: string) {
  code = code.trim()
  title = title.trim()
  if (!code) return title
  if (!title) return code
  const escaped = code.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const existing = new RegExp(`(^|[^A-Za-z0-9_.-])${escaped}(?=$|[^A-Za-z0-9_.-])`, 'i')
  const match = existing.exec(title)
  if (match) {
    let start = match.index + (match[1] ?? '').length
    let end = match.index + match[0].length
    const pairs: Record<string, string> = { '[': ']', '【': '】', '(': ')', '（': '）' }
    const closing = pairs[title[start - 1] ?? '']
    if (start > 0 && closing && closing === title[end]) {
      start--
      end++
    }
    title = title.slice(0, start) + title.slice(end)
    title = title.replace(/^[\s·:：\-–—]+|[\s·:：\-–—]+$/g, '').replace(/\s{2,}/g, ' ')
  }
  return title ? `${code} · ${title}` : code
}
