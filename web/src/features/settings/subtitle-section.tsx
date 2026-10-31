import { useState } from 'react'
import { CaptionsIcon, LoaderCircleIcon } from 'lucide-react'
import { toast } from 'sonner'

import { describeApiError } from '@/api/client'
import {
  type SubtitleConfig,
  type SubtitleLanguage,
  type SubtitleSource,
  useSubtitleConfig,
  useTestSubtitleDownload,
  useUpdateSubtitleConfig
} from '@/api/subtitle'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { SettingRow, SettingsSection } from './shared'

const sources: { id: SubtitleSource; name: string; description: string }[] = [
  { id: 'xunlei', name: '迅雷', description: '按番号检索迅雷字幕库' },
  { id: 'subtitlecat', name: 'SubtitleCat', description: '检索简体和繁体中文字幕' },
  { id: 'avsubtitles', name: 'AVSubtitles', description: '检索中文字幕，下载时需等待站点倒计时' }
]

const languageNames = { 'zh-CN': '简体中文', 'zh-TW': '繁体中文' } as const

export function SubtitleSection() {
  const settings = useSubtitleConfig()
  const update = useUpdateSubtitleConfig()
  const test = useTestSubtitleDownload()
  const [code, setCode] = useState('')
  const config = settings.data
  const disabled = !config || settings.isError || update.isPending || test.isPending
  const hasSources = config && Object.values(config.sources).some(Boolean)
  const result = test.data

  function save(patch: Partial<SubtitleConfig>) {
    if (!config) return
    test.reset()
    update.mutate(
      { ...config, ...patch },
      {
        onSuccess: () => toast.success('字幕下载设置已保存'),
        onError: error => toast.error(describeApiError(error))
      }
    )
  }

  return (
    <SettingsSection icon={<CaptionsIcon className="size-4" />} title="字幕下载">
      <SettingRow
        title="自动下载外置字幕"
        description="刮削完成后自动下载匹配字幕；关闭后保留已有字幕"
        inline
      >
        <Switch
          aria-label="自动下载外置字幕"
          checked={config?.enabled ?? true}
          disabled={disabled}
          onCheckedChange={enabled => save({ enabled })}
        />
      </SettingRow>

      {sources.map(source => (
        <SettingRow key={source.id} title={source.name} description={source.description} inline>
          <Switch
            aria-label={`启用 ${source.name} 字幕源`}
            checked={config?.sources[source.id] ?? true}
            disabled={disabled}
            onCheckedChange={enabled => {
              if (config) save({ sources: { ...config.sources, [source.id]: enabled } })
            }}
          />
        </SettingRow>
      ))}

      <SettingRow title="字幕语言偏好" description="优先选择所选语言，仍可补充另一种中文字幕">
        <Select
          value={config?.language ?? 'zh-CN'}
          disabled={disabled}
          onValueChange={language => save({ language: language as SubtitleLanguage })}
        >
          <SelectTrigger className="w-36" aria-label="字幕语言偏好">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="zh-CN">简体优先</SelectItem>
            <SelectItem value="zh-TW">繁体优先</SelectItem>
          </SelectContent>
        </Select>
      </SettingRow>

      <SettingRow
        title="测试下载"
        description="按当前字幕源和语言偏好实际下载并校验，不写入媒体库；自动下载关闭时也可测试"
      >
        <form
          className="flex max-w-full flex-wrap items-center gap-2"
          onSubmit={event => {
            event.preventDefault()
            if (config && !disabled && hasSources && code.trim()) {
              test.mutate({ code: code.trim(), config })
            }
          }}
        >
          <Input
            aria-label="测试字幕的番号"
            className="w-40 max-w-full"
            placeholder="如 SSIS-589"
            maxLength={128}
            value={code}
            disabled={disabled}
            onChange={event => {
              setCode(event.target.value)
              test.reset()
            }}
          />
          <Button
            type="submit"
            variant="outline"
            size="sm"
            disabled={disabled || !hasSources || !code.trim()}
          >
            {test.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}
            {test.isPending ? '正在测试' : '测试下载'}
          </Button>
        </form>
      </SettingRow>

      {config && !hasSources ? (
        <p className="text-xs text-muted-foreground">请至少启用一个字幕源后再测试。</p>
      ) : null}
      {test.isPending ? (
        <p role="status" className="text-xs text-muted-foreground">
          正在检索并下载字幕，最多等待 90 秒。
        </p>
      ) : null}
      {result ? (
        <div role="status" className="space-y-2 rounded-md border p-3 text-sm">
          <p>
            {result.code}：
            {result.status === 'downloaded'
              ? '已下载并通过校验'
              : result.status === 'not_found'
                ? '未找到匹配字幕'
                : '字幕检索或下载失败'}
          </p>
          <p className="text-xs text-muted-foreground">
            检索到 {result.candidates} 个候选 · 用时 {(result.elapsed_ms / 1000).toFixed(1)} 秒
          </p>
          {result.status === 'downloaded' ? (
            <>
              <p className="text-xs text-muted-foreground">
                {result.source} · {result.language ? languageNames[result.language] : '中文'} ·{' '}
                {result.format?.toUpperCase()} · {((result.bytes ?? 0) / 1024).toFixed(1)} KB
              </p>
              <p className="text-xs break-words">{result.name}</p>
            </>
          ) : null}
          {result.errors?.length ? (
            <details className="text-xs">
              <summary className="cursor-pointer text-muted-foreground">来源错误详情</summary>
              <ul className="mt-2 space-y-1 break-words whitespace-pre-wrap">
                {result.errors.map((error, index) => (
                  <li key={index}>{error}</li>
                ))}
              </ul>
            </details>
          ) : null}
        </div>
      ) : null}
      {test.isError ? <InlineError>{describeApiError(test.error)}</InlineError> : null}
      {settings.isError ? (
        <InlineError onRetry={() => void settings.refetch()} retrying={settings.isFetching}>
          字幕下载设置读取失败，请重试。
        </InlineError>
      ) : null}
    </SettingsSection>
  )
}
