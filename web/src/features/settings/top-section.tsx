import { TrophyIcon } from 'lucide-react'
import { toast } from 'sonner'

import { describeApiError } from '@/api/client'
import { useTopConfig, useTopInfo, useUpdateTopConfig, type TopConfig } from '@/api/top'
import { InlineError } from '@/components/error-state'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { SettingRow, SettingsSection } from './shared'

export function TopSection() {
  const settings = useTopConfig()
  const update = useUpdateTopConfig()
  const info = useTopInfo()
  const config = settings.data
  const disabled = !config || settings.isError || update.isPending

  function save(patch: Partial<TopConfig>) {
    if (!config) return
    update.mutate(
      { ...config, ...patch },
      {
        onSuccess: () => toast.success('TOP 榜单设置已保存'),
        onError: error => toast.error(describeApiError(error))
      }
    )
  }

  return (
    <SettingsSection icon={<TrophyIcon className="size-4" />} title="TOP 榜单">
      <p className="text-xs text-muted-foreground">修改后自动保存到飞牛应用，所有客户端共用。</p>
      <SettingRow title="TOP 导航入口" description="在底部导航中显示 TOP 榜单入口" inline>
        <Switch
          aria-label="显示 TOP 导航入口"
          checked={config?.show_navigation ?? true}
          disabled={disabled}
          onCheckedChange={show_navigation => save({ show_navigation })}
        />
      </SettingRow>
      <SettingRow title="详情页榜单标签" description="在影片详情中显示入榜名次和榜单名称" inline>
        <Switch
          aria-label="显示详情页榜单标签"
          checked={config?.show_rankings ?? true}
          disabled={disabled}
          onCheckedChange={show_rankings => save({ show_rankings })}
        />
      </SettingRow>
      <SettingRow title="默认榜单" description="从 TOP 导航入口进入时打开的榜单">
        <Select
          value={config?.default_board ?? 'all'}
          disabled={disabled || !info.data}
          onValueChange={default_board => save({ default_board })}
        >
          <SelectTrigger className="w-64 max-w-full" aria-label="默认 TOP 榜单">
            <SelectValue placeholder="加载榜单…" />
          </SelectTrigger>
          <SelectContent position="popper" align="start">
            <SelectGroup>
              {info.data?.boards.map(board => (
                <SelectItem key={board.id} value={board.id}>
                  {board.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </SettingRow>
      {settings.isError ? (
        <InlineError onRetry={() => void settings.refetch()} retrying={settings.isFetching}>
          TOP 榜单设置读取失败，请重试。
        </InlineError>
      ) : null}
      {info.isError ? (
        <InlineError onRetry={() => void info.refetch()} retrying={info.isFetching}>
          榜单列表加载失败，请重试。
        </InlineError>
      ) : null}
    </SettingsSection>
  )
}
