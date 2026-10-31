import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { DatabaseIcon, Settings2Icon } from 'lucide-react'
import {
  browseKeys,
  clearBrowseCache,
  defaultBrowseSettings,
  exportBrowseCache,
  importBrowseCache,
  resetBrowseSessions,
  saveBrowseSettings,
  useBrowseCacheStats,
  useBrowseSettings,
  type BrowseSettings,
  type CacheStats
} from '@/api/browse-cache'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { InlineError } from '@/components/error-state'
import { formatSize } from '@/lib/format'
import { discoverKeys } from '@/api/movie-detail-cache'
import { describeApiError } from '@/api/client'
import { SettingsSection, SettingRow } from '@/features/settings/shared'

const fields: Array<{
  key: keyof BrowseSettings
  label: string
  min: number
  max: number
  unit: string
}> = [
  { key: 'pages', label: '目标页数', min: 1, max: 24, unit: '页' },
  { key: 'rating_hours', label: '评分更新', min: 1, max: 168, unit: '小时' },
  { key: 'magnet_minutes', label: '磁力与资源标签更新', min: 1, max: 1440, unit: '分钟' },
  { key: 'list_minutes', label: '列表顺序更新', min: 1, max: 60, unit: '分钟' }
]
export function BrowseCacheSettingsButton() {
  const [open, setOpen] = useState(false)
  const settings = useBrowseSettings()
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Settings2Icon />
        预加载 {settings.data?.pages ?? 6} 页
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>预加载与缓存</DialogTitle>
            <DialogDescription>
              按筛选后的作品计算，每页 20 部；不足目标时自动继续补齐。配置在本机服务端保存。
            </DialogDescription>
          </DialogHeader>
          {open ? (
            <CacheSettingsForm
              initial={settings.data ?? defaultBrowseSettings}
              close={() => setOpen(false)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  )
}
function CacheSettingsForm({ initial, close }: { initial: BrowseSettings; close: () => void }) {
  const client = useQueryClient()
  const [values, setValues] = useState(initial)
  const [scope, setScope] = useState<string>()
  const fileInput = useRef<HTMLInputElement>(null)
  const [transferMessage, setTransferMessage] = useState('')
  const stats = useBrowseCacheStats(true)
  const save = useMutation({
    mutationFn: saveBrowseSettings,
    onSuccess: settings => {
      client.setQueryData(browseKeys.settings, settings)
      void client.invalidateQueries({ queryKey: [...browseKeys.all, 'rating'] })
      void client.invalidateQueries({ queryKey: ['movie-extras', 'javlibrary-rating'] })
      close()
    }
  })
  async function reconcileCache(data: CacheStats) {
    const affected = (key: readonly unknown[]) =>
      key[0] === browseKeys.all[0] ||
      key[0] === discoverKeys.all[0] ||
      (key[0] === 'movie-extras' && key[1] === 'javlibrary-rating')
    await client.cancelQueries({ predicate: query => affected(query.queryKey) })
    client.removeQueries({
      predicate: query => affected(query.queryKey) && !query.getObserversCount()
    })
    client.setQueryData(browseKeys.stats, data)
    resetBrowseSessions()
    await client.invalidateQueries({
      predicate: query => affected(query.queryKey) && query.queryKey[1] !== 'stats'
    })
  }
  const clear = useMutation({
    mutationFn: async (scope: string) => {
      resetBrowseSessions()
      return clearBrowseCache(scope)
    },
    onSuccess: async data => {
      await reconcileCache(data)
      setScope(undefined)
    }
  })
  const exporting = useMutation({
    mutationFn: exportBrowseCache,
    onSuccess: blob => {
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `miyabi-cache-${new Date().toISOString().slice(0, 10)}.zip`
      link.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      setTransferMessage('缓存已导出。')
    }
  })
  const importing = useMutation({
    mutationFn: importBrowseCache,
    onSuccess: async data => {
      await reconcileCache(data)
      setTransferMessage('缓存已导入，已保留本地较新的数据。')
    }
  })
  const valid = fields.every(
    field =>
      Number.isInteger(values[field.key]) &&
      values[field.key] >= field.min &&
      values[field.key] <= field.max
  )
  const busy = save.isPending || clear.isPending || exporting.isPending || importing.isPending
  return (
    <div className="space-y-4">
      {fields.map(field => (
        <SettingRow
          key={field.key}
          title={field.label}
          description={`${field.min}–${field.max} ${field.unit}`}
          inline
        >
          <div className="flex items-center gap-2">
            <Input
              aria-label={field.label}
              type="number"
              min={field.min}
              max={field.max}
              value={values[field.key]}
              disabled={busy}
              onChange={event =>
                setValues(old => ({ ...old, [field.key]: Number(event.target.value) }))
              }
              className="w-24"
            />
            <span className="text-xs text-muted-foreground">{field.unit}</span>
          </div>
        </SettingRow>
      ))}
      <div className="space-y-1 border-t pt-3 text-sm text-muted-foreground">
        <p>作品资料、演员、标签、评分和已浏览图片保存在服务端，可导出迁移。</p>
        <p>图片仅当前页加载；过期数据保留，后台更新。</p>
      </div>
      <p className="text-sm">
        {stats.data
          ? `已缓存 ${stats.data.movies} 部 · ${stats.data.images} 张图片 · ${formatSize(stats.data.bytes)}`
          : '正在统计缓存…'}
      </p>
      <div className="space-y-2">
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => {
              setTransferMessage('')
              exporting.reset()
              importing.reset()
              exporting.mutate()
            }}
          >
            {exporting.isPending ? '导出中…' : '导出缓存'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => fileInput.current?.click()}
          >
            {importing.isPending ? '导入中…' : '导入缓存'}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".zip,application/zip"
            aria-label="选择缓存 ZIP 文件"
            className="hidden"
            disabled={busy}
            onChange={event => {
              const file = event.target.files?.[0]
              event.target.value = ''
              if (!file) return
              setTransferMessage('')
              exporting.reset()
              importing.reset()
              importing.mutate(file)
            }}
          />
        </div>
        <p className="text-xs text-muted-foreground">
          导入本应用导出的 ZIP 文件（最大 512 MB），合并缓存并保留较新数据。
        </p>
        {transferMessage ? (
          <p role="status" className="text-sm">
            {transferMessage}
          </p>
        ) : null}
        {exporting.isError || importing.isError ? (
          <InlineError>{describeApiError(exporting.error ?? importing.error)}</InlineError>
        ) : null}
      </div>
      {scope ? (
        <div className="space-y-2 rounded-lg border p-3">
          <p className="text-sm">
            确认清理{scope === 'images' ? '图片' : scope === 'data' ? '资料与动态数据' : '全部扩展'}
            缓存？媒体库、订阅和网盘文件会保留。
          </p>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope(undefined)}>
              取消
            </Button>
            <Button
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={() => clear.mutate(scope)}
            >
              确认清理
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope('images')}>
            清理图片
          </Button>
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope('data')}>
            清理资料
          </Button>
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope('all')}>
            全部清理
          </Button>
        </div>
      )}
      {save.isError || clear.isError || stats.isError ? (
        <InlineError>缓存操作失败，请重试。</InlineError>
      ) : null}
      <div className="flex justify-end">
        <Button disabled={!valid || busy} onClick={() => save.mutate(values)}>
          {save.isPending ? '保存中…' : '保存'}
        </Button>
      </div>
    </div>
  )
}
export function BrowseCacheSection() {
  return (
    <SettingsSection icon={<DatabaseIcon className="size-4" />} title="作品列表缓存">
      <SettingRow
        title="预加载与持久缓存"
        description="配置预加载与更新间隔，导入、导出或清理图片和资料缓存。"
      >
        <BrowseCacheSettingsButton />
      </SettingRow>
    </SettingsSection>
  )
}
