import { useState } from 'react'
import { LoaderCircleIcon, SparklesIcon } from 'lucide-react'
import { describeApiError } from '@/api/client'
import { useMDCzSettings, useUpdateMDCz, useTestMDCz, type MDCzUpdate } from '@/api/mdcz'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { InlineError } from '@/components/error-state'
import { SettingRow, SettingsSection } from '../shared'
import { MDCzConfigurationSection } from './mdcz-configuration'

export function MDCzSection() {
  const query = useMDCzSettings()
  const save = useUpdateMDCz()
  const test = useTestMDCz()
  const [draft, setDraft] = useState<MDCzUpdate | null>(null)
  const [saved, setSaved] = useState(false)
  const [configurationDirty, setConfigurationDirty] = useState(false)
  const [connectionGeneration, setConnectionGeneration] = useState(0)
  const current: MDCzUpdate | undefined = draft ?? query.data
  const busy = save.isPending || test.isPending
  const update = (change: Partial<MDCzUpdate>) => {
    if (!current) return
    setDraft({ ...current, ...change })
    setSaved(false)
    test.reset()
    save.reset()
  }
  return (
    <SettingsSection icon={<SparklesIcon className="size-4" />} title="MDCz 增强刮削">
      <p className="text-xs leading-5 text-muted-foreground">
        启用并保存后，主资料来源切换为
        MDCz，生成影片介绍与资源封面角标。任务进度和重试沿用媒体库中的刮削任务。
      </p>
      {query.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          正在读取设置…
        </p>
      ) : null}
      {query.isError ? (
        <InlineError onRetry={() => void query.refetch()} retrying={query.isFetching}>
          设置加载失败，请重试。
        </InlineError>
      ) : null}
      {current ? (
        <fieldset disabled={busy} className="space-y-5 disabled:opacity-70">
          <SettingRow
            title="启用增强刮削"
            description="对新入库及重新刮削生效；已有资料不会立即批量改写。"
            inline
          >
            <Switch
              aria-label="启用 MDCz 增强刮削"
              checked={current.enabled}
              onCheckedChange={enabled => update({ enabled })}
            />
          </SettingRow>
          <div className="grid gap-4 sm:grid-cols-2">
            <label className="space-y-2 text-sm">
              <span>桥接服务地址</span>
              <Input
                type="url"
                autoComplete="off"
                placeholder="http://mdcz-bridge:3840"
                value={current.server_url}
                onChange={event => update({ server_url: event.target.value })}
              />
            </label>
            <label className="space-y-2 text-sm">
              <span>访问密钥</span>
              <Input
                type="password"
                autoComplete="new-password"
                placeholder={current.has_api_key ? '已保存，留空保留' : '填写桥接服务密钥'}
                value={current.api_key ?? ''}
                onChange={event => update({ api_key: event.target.value || undefined })}
              />
            </label>
          </div>
          <SettingRow
            title="翻译标题与介绍"
            description="使用桥接服务中配置的翻译来源；关闭时保留原文。"
            inline
          >
            <Switch
              aria-label="翻译标题与介绍"
              checked={current.translate}
              onCheckedChange={translate => update({ translate })}
            />
          </SettingRow>
          <SettingRow
            title="生成封面角标"
            description="按实际文件标记显示中字、破解等信息，导出至 Emby；多分集只显示共同属性。"
            inline
          >
            <Switch
              aria-label="生成封面角标"
              checked={current.badges}
              onCheckedChange={badges => update({ badges })}
            />
          </SettingRow>
          {current.badges ? (
            <SettingRow title="角标位置" description="重新刮削时从保留的原图生成，避免重复叠加。">
              <select
                aria-label="角标位置"
                className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                value={current.position}
                onChange={event =>
                  update({ position: event.target.value as MDCzUpdate['position'] })
                }
              >
                <option value="bottomLeft">左下角</option>
                <option value="bottomRight">右下角</option>
                <option value="topLeft">左上角</option>
                <option value="topRight">右上角</option>
              </select>
            </SettingRow>
          ) : null}
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="outline"
              disabled={!current.server_url || (!current.has_api_key && !current.api_key)}
              onClick={() => test.mutate(current)}
            >
              {test.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}测试连接
            </Button>
            <Button
              disabled={!draft || configurationDirty}
              onClick={() =>
                save.mutate(current, {
                  onSuccess: () => {
                    setDraft(null)
                    setSaved(true)
                    setConnectionGeneration(value => value + 1)
                  }
                })
              }
            >
              {save.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}保存连接与开关
            </Button>
            <span role="status" className="text-xs text-muted-foreground">
              {saved
                ? '已保存；已有影片可在媒体库中重新刮削。'
                : test.isSuccess
                  ? '连接与版本正常；来源站点需通过单片刮削验证。'
                  : draft
                    ? '有未保存的修改'
                    : ''}
            </span>
          </div>
          {configurationDirty ? (
            <p className="text-xs text-muted-foreground">
              下方来源配置有未保存修改，请先保存或放弃，再更改连接与开关。
            </p>
          ) : null}
          {test.isError || save.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {describeApiError(test.error ?? save.error)}
            </p>
          ) : null}
          <details className="rounded-md border p-3 text-xs leading-5 text-muted-foreground">
            <summary className="cursor-pointer font-medium text-foreground">使用说明</summary>
            <p className="mt-2">
              填写与 Miyabi
              配套的桥接服务地址，保存后可展开下方配置来源、代理和翻译服务。首次部署见安装文档。连接失败可关闭增强刮削，继续使用原有来源。
            </p>
            <a
              className="underline underline-offset-4"
              href="https://github.com/Surc/miyabi/blob/feat/mdcz-extension/docs/mdcz-extension.md"
              target="_blank"
              rel="noreferrer"
            >
              安装与升级文档
            </a>
            <p className="mt-2">
              文件名标记不等于视频内容检测。STRM
              无法自动确认内嵌字幕或实际分辨率；没有可信标记时不显示相应角标。启用后先选择一部影片重新刮削，确认效果再批量重建。
            </p>
          </details>
        </fieldset>
      ) : null}
      <MDCzConfigurationSection
        key={connectionGeneration}
        connected={!!query.data?.server_url && !!query.data?.has_api_key}
        disabled={busy}
        onDirtyChange={setConfigurationDirty}
      />
    </SettingsSection>
  )
}
