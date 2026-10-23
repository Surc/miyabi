import { expect, test } from 'vitest'
import { movieDisplayTitle } from '@/components/movie/display-title'

test.each([
  ['ABC-123', '作品名称', 'ABC-123 · 作品名称'],
  ['ABC-123', 'ABC-123 作品名称', 'ABC-123 · 作品名称'],
  ['ABC-123', '[abc-123] 作品名称', 'ABC-123 · 作品名称'],
  ['ABC-123', '作品名称 (ABC-123)', 'ABC-123 · 作品名称'],
  ['ABC-123', 'ABC-1234 作品名称', 'ABC-123 · ABC-1234 作品名称'],
  ['ABC-123', 'ABC-123', 'ABC-123'],
  ['', '作品名称', '作品名称'],
  ['ABC-123', '', 'ABC-123'],
  ['TUSHY.26.01.01', 'TUSHYX26X01X01 Title', 'TUSHY.26.01.01 · TUSHYX26X01X01 Title']
])(
  'formats a code-prefixed title without corrupting identifiers: %s / %s',
  (code, title, expected) => {
    expect(movieDisplayTitle(code, title)).toBe(expected)
  }
)
