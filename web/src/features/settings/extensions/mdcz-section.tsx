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
    <SettingsSection icon={<SparklesIcon className="size-4" />} title="刮削引擎">
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
        <fieldset disabled={busy || query.isError} className="space-y-5 disabled:opacity-70">
          <SettingRow title="刮削模式" description="保存后对新入库和重新刮削生效。" inline>
            <select
              aria-label="刮削模式"
              className="h-9 rounded-md border border-input bg-background px-3 text-sm"
              value={current.enabled ? 'mdcz' : 'native'}
              onChange={event => update({ enabled: event.target.value === 'mdcz' })}
            >
              <option value="native">Miyabi 原版</option>
              <option value="mdcz">MDCz 增强</option>
            </select>
          </SettingRow>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
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
              {save.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}保存刮削设置
            </Button>
            <span role="status" className="text-xs text-muted-foreground">
              {saved ? '已保存。已有影片可在媒体库中重新刮削。' : draft ? '有未保存的修改' : ''}
            </span>
          </div>
          {configurationDirty ? (
            <p className="text-xs text-muted-foreground">
              MDCz 来源配置有未保存的修改，请先保存或放弃，再保存刮削设置。
            </p>
          ) : null}
          {save.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {describeApiError(save.error)}
            </p>
          ) : null}
          <details open={current.enabled || configurationDirty} className="rounded-md border p-3">
            <summary className="cursor-pointer text-sm font-medium">MDCz 增强设置</summary>
            <div className="mt-4 space-y-5">
              <p className="text-xs leading-5 text-muted-foreground">
                {current.managed
                  ? 'MDCz 已由安装包管理，可提供影片介绍、翻译和封面角标。连接地址和密钥由安装配置维护。'
                  : 'MDCz 增强需要桥接服务，可提供影片介绍、翻译和封面角标。'}
              </p>
              {current.status === 'unavailable' ? (
                <p role="alert" className="text-sm text-destructive">
                  MDCz 扩展不可用。媒体库和原版模式仍可使用，请检查安装配置和扩展目录。
                </p>
              ) : null}
              <div className="grid gap-4 sm:grid-cols-2">
                <label className="space-y-2 text-sm">
                  <span>桥接服务地址</span>
                  <Input
                    type="url"
                    autoComplete="off"
                    placeholder="http://mdcz-bridge:3840"
                    value={current.server_url}
                    readOnly={current.managed}
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
                    disabled={current.managed}
                    onChange={event => update({ api_key: event.target.value || undefined })}
                  />
                </label>
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <Button
                  type="button"
                  variant="outline"
                  disabled={!current.server_url || (!current.has_api_key && !current.api_key)}
                  onClick={() => test.mutate(current)}
                >
                  {test.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}测试连接
                </Button>
                {test.isSuccess ? (
                  <span role="status" className="text-xs text-muted-foreground">
                    连接与版本正常。可通过单片刮削验证来源站点。
                  </span>
                ) : null}
              </div>
              {test.isError ? (
                <p role="alert" className="text-sm text-destructive">
                  {describeApiError(test.error)}
                </p>
              ) : null}
              <SettingRow
                title="翻译标题与介绍"
                description="使用桥接服务中配置的翻译来源。关闭时保留原文。"
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
                description="按实际文件标记显示中字、破解等信息并导出至 Emby。多分集只显示共同属性。"
                inline
              >
                <Switch
                  aria-label="生成封面角标"
                  checked={current.badges}
                  onCheckedChange={badges => update({ badges })}
                />
              </SettingRow>
              {current.badges ? (
                <SettingRow
                  title="角标位置"
                  description="重新刮削时从保留的原图生成，避免重复叠加。"
                >
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
              <details className="rounded-md border p-3 text-xs leading-5 text-muted-foreground">
                <summary className="cursor-pointer font-medium text-foreground">使用说明</summary>
                <p className="mt-2">
                  {current.managed
                    ? '包内连接无需手工填写。在下方配置来源、代理和翻译服务。'
                    : '保存桥接地址与密钥后，可以在下方配置来源、代理和翻译服务。'}
                </p>
                <a
                  className="underline underline-offset-4"
                  href="https://github.com/Surc/miyabi/blob/master/docs/mdcz-extension.md"
                  target="_blank"
                  rel="noreferrer"
                >
                  安装与升级文档
                </a>
                <p className="mt-2">
                  角标依据文件名生成。STRM
                  的内嵌字幕和分辨率需要可信的文件名标记。可先重新刮削单部影片确认效果。
                </p>
              </details>
              <MDCzConfigurationSection
                key={connectionGeneration}
                connected={!!query.data?.server_url && !!query.data?.has_api_key}
                disabled={busy}
                onDirtyChange={setConfigurationDirty}
              />
            </div>
          </details>
        </fieldset>
      ) : null}
    </SettingsSection>
  )
}
