import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, expect, test, vi } from 'vitest'

import {
  type SubtitleConfig,
  type SubtitleTestResult,
  useSubtitleConfig,
  useTestSubtitleDownload,
  useUpdateSubtitleConfig
} from '@/api/subtitle'
import { SubtitleSection } from '@/features/settings/subtitle-section'

vi.mock('@/api/subtitle', () => ({
  useSubtitleConfig: vi.fn(),
  useTestSubtitleDownload: vi.fn(),
  useUpdateSubtitleConfig: vi.fn()
}))

const defaults: SubtitleConfig = {
  enabled: true,
  sources: { xunlei: true, subtitlecat: true, avsubtitles: true },
  language: 'zh-CN'
}

beforeEach(() => {
  vi.mocked(useSubtitleConfig).mockReturnValue({ data: defaults } as ReturnType<
    typeof useSubtitleConfig
  >)
  vi.mocked(useUpdateSubtitleConfig).mockReturnValue({ mutate: vi.fn() } as unknown as ReturnType<
    typeof useUpdateSubtitleConfig
  >)
  vi.mocked(useTestSubtitleDownload).mockReturnValue({
    mutate: vi.fn(),
    reset: vi.fn()
  } as unknown as ReturnType<typeof useTestSubtitleDownload>)
})

test('subtitle settings expose the existing defaults, three sources, language preference and manual test', () => {
  const html = renderToStaticMarkup(<SubtitleSection />)
  expect(html).toContain('字幕下载')
  expect(html).toContain('迅雷')
  expect(html).toContain('SubtitleCat')
  expect(html).toContain('AVSubtitles')
  expect(html.match(/role="switch" aria-checked="true"/g)).toHaveLength(4)
  expect(html).toContain('字幕语言偏好')
  expect(html).toContain('测试字幕的番号')
  expect(html).toContain('自动下载关闭时也可测试')
})

test.each([
  { status: 'downloaded', label: '已下载并通过校验' },
  { status: 'not_found', label: '未找到匹配字幕' },
  { status: 'failed', label: '字幕检索或下载失败' }
] as const)('test download distinguishes $status', ({ status, label }) => {
  vi.mocked(useTestSubtitleDownload).mockReturnValue({
    data: {
      code: 'ABP-123',
      status,
      candidates: 2,
      elapsed_ms: 1200,
      source: 'AVSubtitles',
      language: 'zh-TW',
      format: 'srt',
      bytes: 2048,
      errors: status === 'failed' ? ['source unavailable'] : undefined
    } satisfies SubtitleTestResult,
    reset: vi.fn()
  } as unknown as ReturnType<typeof useTestSubtitleDownload>)
  const html = renderToStaticMarkup(<SubtitleSection />)
  expect(html).toContain(label)
  if (status === 'downloaded') {
    expect(html).toContain('繁体中文')
    expect(html).toContain('SRT')
    expect(html).toContain('2.0 KB')
  } else if (status === 'failed') {
    expect(html).toContain('source unavailable')
  }
})

test('disabled sources stay disabled and show why a manual test is unavailable', () => {
  vi.mocked(useSubtitleConfig).mockReturnValue({
    data: {
      ...defaults,
      enabled: false,
      sources: { xunlei: false, subtitlecat: false, avsubtitles: false }
    }
  } as ReturnType<typeof useSubtitleConfig>)
  const html = renderToStaticMarkup(<SubtitleSection />)
  expect(html.match(/role="switch" aria-checked="false"/g)).toHaveLength(4)
  expect(html).toContain('请至少启用一个字幕源后再测试')
})

test('settings load failures remain retryable and do not enable controls', () => {
  vi.mocked(useSubtitleConfig).mockReturnValue({
    isError: true,
    refetch: vi.fn()
  } as unknown as ReturnType<typeof useSubtitleConfig>)
  const html = renderToStaticMarkup(<SubtitleSection />)
  expect(html).toContain('字幕下载设置读取失败')
  expect(html).toContain('重试')
  expect(html).toContain('disabled=""')
})
