import { useState, type ReactNode } from 'react'
import { ArrowDownIcon, ArrowUpIcon, LoaderCircleIcon } from 'lucide-react'
import { describeApiError } from '@/api/client'
import {
  useMDCzConfiguration,
  useUpdateMDCzConfiguration,
  type Configuration,
  type ConfigurationUpdate,
  type ConfigurationValues,
  type SecretName
} from '@/api/mdcz-configuration'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { InlineError } from '@/components/error-state'
import { SettingRow } from '../shared'

const selectClass = 'h-9 w-full rounded-md border border-input bg-background px-3 text-sm'
const siteNames: Record<string, string> = {
  dmm: 'DMM / FANZA',
  dmm_tv: 'DMM TV',
  mgstage: 'MGStage',
  javdb: 'JavDB',
  javbus: 'JavBus',
  r18_dev: 'R18',
  '1pondo': '一本道',
  '10musume': '天然むすめ',
  caribbeancom: '加勒比',
  official: '官方站点'
}
const siteName = (id: string) => siteNames[id] ?? id.toUpperCase()
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-2 text-sm">
      <span>{label}</span>
      {children}
    </label>
  )
}

export function MDCzConfigurationSection({
  connected,
  disabled,
  onDirtyChange
}: {
  connected: boolean
  disabled: boolean
  onDirtyChange: (dirty: boolean) => void
}) {
  const [opened, setOpened] = useState(false)
  const query = useMDCzConfiguration(connected && opened)
  return (
    <details
      className="rounded-md border p-3"
      onToggle={event => setOpened(event.currentTarget.open)}
    >
      <summary className="cursor-pointer text-sm font-medium">来源、代理与翻译参数</summary>
      <fieldset disabled={disabled} className="mt-4 space-y-4 disabled:opacity-70">
        <p className="text-xs leading-5 text-muted-foreground">
          此处配置已保存连接对应的 MDCz 服务，独立保存后立即供后续请求使用；不会启动重刮任务。Miyabi
          的原生网络设置单独生效。
        </p>
        {!connected ? (
          <p className="text-sm text-muted-foreground">请先填写并保存上方服务地址与密钥。</p>
        ) : (
          <>
            {query.isPending ? (
              <p role="status" className="text-sm text-muted-foreground">
                正在读取引擎配置…
              </p>
            ) : null}
            {query.isError ? (
              <InlineError onRetry={() => void query.refetch()} retrying={query.isFetching}>
                {describeApiError(query.error)}
              </InlineError>
            ) : null}
            {query.data ? (
              <ConfigurationForm
                initial={query.data}
                onDirtyChange={onDirtyChange}
                reload={() => query.refetch()}
                loading={query.isFetching}
              />
            ) : null}
          </>
        )}
      </fieldset>
    </details>
  )
}

function ConfigurationForm({
  initial,
  onDirtyChange,
  reload,
  loading
}: {
  initial: Configuration
  onDirtyChange: (dirty: boolean) => void
  reload: () => Promise<unknown>
  loading: boolean
}) {
  const save = useUpdateMDCzConfiguration()
  const [draft, setDraft] = useState<ConfigurationUpdate | null>(null)
  const [saved, setSaved] = useState(false)
  const current = { ...initial.values, ...draft?.values }
  const busy = save.isPending || loading
  const update = (values: ConfigurationUpdate['values']) => {
    setDraft(previous => ({
      configuration_version: previous?.configuration_version ?? initial.configuration_version,
      configuration_id: previous?.configuration_id ?? initial.configuration_id,
      values: { ...previous?.values, ...values }
    }))
    onDirtyChange(true)
    setSaved(false)
    save.reset()
  }
  const discard = () => {
    setDraft(null)
    onDirtyChange(false)
    setSaved(false)
    save.reset()
  }
  const text = (name: keyof ConfigurationValues, label: string, placeholder?: string) => (
    <Field label={label}>
      <Input
        value={String(current[name])}
        placeholder={placeholder}
        autoComplete="off"
        onChange={event => update({ [name]: event.target.value })}
      />
    </Field>
  )
  const secret = (name: SecretName, label: string) => (
    <div className="space-y-2">
      <Field label={label}>
        <Input
          type="password"
          autoComplete="new-password"
          value={draft?.values[name] ?? ''}
          placeholder={initial.secrets[name] ? '已保存，留空保留' : '尚未配置'}
          onChange={event => update({ [name]: event.target.value || undefined })}
        />
      </Field>
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span>
          {draft?.values[name] === ''
            ? '保存时清除'
            : draft?.values[name]
              ? '待更新'
              : initial.secrets[name]
                ? '已保存凭据'
                : '未保存凭据'}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => update({ [name]: draft?.values[name] === '' ? undefined : '' })}
        >
          {draft?.values[name] === '' ? '撤销清除' : '清除'}
        </Button>
      </div>
    </div>
  )
  const select = <K extends keyof ConfigurationValues>(
    name: K,
    label: string,
    options: [string, string][]
  ) => (
    <Field label={label}>
      <select
        className={selectClass}
        value={String(current[name])}
        onChange={event => update({ [name]: event.target.value })}
      >
        {options.map(([value, title]) => (
          <option value={value} key={value}>
            {title}
          </option>
        ))}
      </select>
    </Field>
  )
  const move = (index: number, offset: number) => {
    const sites = [...current.sites]
    const target = index + offset
    if (target < 0 || target >= sites.length) return
    ;[sites[index], sites[target]] = [sites[target]!, sites[index]!]
    update({ sites })
  }
  return (
    <fieldset disabled={busy} className="space-y-5 disabled:opacity-70">
      <details open className="space-y-3">
        <summary className="cursor-pointer text-sm font-medium">
          刮削来源 · 已启用 {current.sites.length} 个
        </summary>
        <p className="text-xs leading-5 text-muted-foreground">
          至少保留一个来源。上下移动调整来源顺序；具体字段仍遵循 MDCz 的聚合规则。
        </p>
        <div className="max-h-64 overflow-y-auto rounded-md border">
          {current.sites.map((site, index) => (
            <div
              key={site}
              className="flex items-center gap-2 border-b px-3 py-1.5 last:border-b-0"
            >
              <span className="min-w-0 flex-1 truncate text-sm">
                {index + 1}. {siteName(site)}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`上移 ${siteName(site)}`}
                disabled={index === 0}
                onClick={() => move(index, -1)}
              >
                <ArrowUpIcon />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={`下移 ${siteName(site)}`}
                disabled={index === current.sites.length - 1}
                onClick={() => move(index, 1)}
              >
                <ArrowDownIcon />
              </Button>
              <Switch
                aria-label={`启用 ${siteName(site)}`}
                checked
                onCheckedChange={() => update({ sites: current.sites.filter(id => id !== site) })}
              />
            </div>
          ))}
        </div>
        <div className="grid gap-2 sm:grid-cols-2">
          {initial.available_sites
            .filter(site => !current.sites.includes(site))
            .map(site => (
              <label key={site} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={false}
                  onChange={() => update({ sites: [...current.sites, site] })}
                />
                {siteName(site)}
              </label>
            ))}
        </div>
      </details>
      <details className="space-y-4">
        <summary className="cursor-pointer text-sm font-medium">网络与站点登录</summary>
        <SettingRow
          title="使用代理"
          description="填写桥接容器能访问的 HTTP、HTTPS 或 SOCKS5 地址。"
          inline
        >
          <Switch
            aria-label="MDCz 使用代理"
            checked={current.use_proxy}
            onCheckedChange={use_proxy => update({ use_proxy })}
          />
        </SettingRow>
        {secret('proxy_url', '代理完整地址（可含账号密码）')}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="请求超时（秒）">
            <Input
              type="number"
              min={1}
              max={300}
              value={current.timeout}
              onChange={event => update({ timeout: Number(event.target.value) })}
            />
          </Field>
          <Field label="失败重试次数">
            <Input
              type="number"
              min={0}
              max={10}
              value={current.retries}
              onChange={event => update({ retries: Number(event.target.value) })}
            />
          </Field>
          {text('javdb_url', 'JavDB 镜像地址（可留空）', 'https://...')}
          {text('javbus_url', 'JavBus 镜像地址（可留空）', 'https://...')}
        </div>
        {current.use_proxy ? (
          <div className="space-y-2">
            <p className="text-sm">这些来源直连</p>
            <div className="grid gap-2 sm:grid-cols-3">
              {initial.available_sites.map(site => (
                <label key={site} className="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    checked={current.direct_sites.includes(site)}
                    onChange={event =>
                      update({
                        direct_sites: event.target.checked
                          ? [...current.direct_sites, site]
                          : current.direct_sites.filter(id => id !== site)
                      })
                    }
                  />
                  {siteName(site)}
                </label>
              ))}
            </div>
          </div>
        ) : null}
        {secret('javdb_cookie', 'JavDB Cookie')}
        {secret('javbus_cookie', 'JavBus Cookie')}
        {secret('fantia_cookie', 'Fantia Cookie')}
      </details>
      <details open className="space-y-4">
        <summary className="cursor-pointer text-sm font-medium">翻译服务</summary>
        <p className="text-xs leading-5 text-muted-foreground">
          翻译总开关在上方。选择付费服务时按其账号计费；密钥仅保存于桥接服务，页面不会回显。
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          {select('translation_engine', '翻译引擎', [
            ['google', 'Google'],
            ['deepl', 'DeepL'],
            ['baidu', '百度'],
            ['openai', '大模型 API']
          ])}
          {select('target_language', '目标语言', [
            ['zh-CN', '简体中文'],
            ['zh-TW', '繁体中文']
          ])}
        </div>
        {current.translation_engine === 'deepl' ? secret('deepl_key', 'DeepL API Key') : null}
        {current.translation_engine === 'baidu' ? (
          <>
            {select('baidu_service', '百度接口', [
              ['general', '通用翻译'],
              ['llm', '大模型翻译']
            ])}
            {text('baidu_app_id', '百度 App ID')}
            {current.baidu_service === 'general'
              ? secret('baidu_secret', '百度密钥')
              : secret('baidu_key', '百度 API Key')}
          </>
        ) : null}
        {current.translation_engine === 'openai' ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              {text('llm_base_url', 'API 基础地址')}
              {text('llm_model', '模型名称')}
              {select('llm_service_type', '服务类型', [
                ['openai-compatible', 'OpenAI 兼容'],
                ['google', 'Google'],
                ['deepseek', 'DeepSeek']
              ])}
              {select('llm_api_format', '接口格式', [
                ['responses', 'Responses'],
                ['chat-completions', 'Chat Completions']
              ])}
            </div>
            {secret('llm_key', '大模型 API Key')}
          </>
        ) : null}
      </details>
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          disabled={!draft || !current.sites.length}
          onClick={() =>
            draft &&
            save.mutate(draft, {
              onSuccess: () => {
                setDraft(null)
                onDirtyChange(false)
                setSaved(true)
              }
            })
          }
        >
          {save.isPending ? <LoaderCircleIcon className="animate-spin" /> : null}保存来源与翻译配置
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            discard()
            void reload()
          }}
        >
          {draft ? '放弃修改并重读' : '重新读取'}
        </Button>
        <span role="status" className="text-xs text-muted-foreground">
          {saved ? '已保存，无需重启服务。' : draft ? '配置有未保存修改' : ''}
        </span>
      </div>
      {save.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {describeApiError(save.error)}
        </p>
      ) : null}
    </fieldset>
  )
}
