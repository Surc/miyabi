import { expect, test, vi } from 'vitest'
import { useQuery } from '@tanstack/react-query'
import { usePublicationStatus } from '@/api/publication'

vi.mock('@tanstack/react-query', () => ({ useQuery: vi.fn() }))
test('publication protection continues polling after healthy and failed reads', () => {
  usePublicationStatus()
  const options = vi.mocked(useQuery).mock.calls[0]![0]
  // A constant interval covers false -> blocked and blocked -> recovered,
  // even when the writer has no corresponding task event.
  expect(options.refetchInterval).toBe(10_000)
  expect(options.queryKey).toEqual(['extensions', 'publication', 'status'])
  expect(options.retry).toBe(false)
})
