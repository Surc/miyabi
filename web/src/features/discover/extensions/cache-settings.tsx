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
  { key: 'pages', label: 'Ŀ��ҳ��', min: 1, max: 24, unit: 'ҳ' },
  { key: 'rating_hours', label: '���ָ���', min: 1, max: 168, unit: 'Сʱ' },
  { key: 'magnet_minutes', label: '��������Դ��ǩ����', min: 1, max: 1440, unit: '����' },
  { key: 'list_minutes', label: '�б�˳�����', min: 1, max: 60, unit: '����' }
]
export function BrowseCacheSettingsButton() {
  const [open, setOpen] = useState(false)
  const settings = useBrowseSettings()
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Settings2Icon />
        Ԥ���� {settings.data?.pages ?? 6} ҳ
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Ԥ�����뻺��</DialogTitle>
            <DialogDescription>
              ��ɸѡ�����Ʒ���㣬ÿҳ 20 ��������Ŀ��ʱ�Զ��������롣�����ڱ�������˱��档
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
      setTransferMessage('�����ѵ�����')
    }
  })
  const importing = useMutation({
    mutationFn: importBrowseCache,
    onSuccess: async data => {
      await reconcileCache(data)
      setTransferMessage('�����ѵ��룬�ѱ������ؽ��µ����ݡ�')
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
          description={`${field.min}�C${field.max} ${field.unit}`}
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
        <p>��Ʒ���ϡ���Ա����ǩ�����ֺ������ͼƬ�����ڷ���ˣ��ɵ���Ǩ�ơ�</p>
        <p>ͼƬ����ǰҳ���أ��������ݱ�������̨���¡�</p>
      </div>
      <p className="text-sm">
        {stats.data
          ? `�ѻ��� ${stats.data.movies} �� �� ${stats.data.images} ��ͼƬ �� ${formatSize(stats.data.bytes)}`
          : '����ͳ�ƻ��桭'}
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
            {exporting.isPending ? '�����С�' : '��������'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => fileInput.current?.click()}
          >
            {importing.isPending ? '�����С�' : '���뻺��'}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".zip,application/zip"
            aria-label="ѡ�񻺴� ZIP �ļ�"
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
          ���뱾Ӧ�õ����� ZIP �ļ������ 512 MB�����ϲ����沢�����������ݡ�
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
            ȷ������{scope === 'images' ? 'ͼƬ' : scope === 'data' ? '�����붯̬����' : 'ȫ����չ'}
            ���棿ý��⡢���ĺ������ļ��ᱣ����
          </p>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope(undefined)}>
              ȡ��
            </Button>
            <Button
              variant="destructive"
              size="sm"
              disabled={busy}
              onClick={() => clear.mutate(scope)}
            >
              ȷ������
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope('images')}>
            ����ͼƬ
          </Button>
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope('data')}>
            ��������
          </Button>
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setScope('all')}>
            ȫ������
          </Button>
        </div>
      )}
      {save.isError || clear.isError || stats.isError ? (
        <InlineError>�������ʧ�ܣ������ԡ�</InlineError>
      ) : null}
      <div className="flex justify-end">
        <Button disabled={!valid || busy} onClick={() => save.mutate(values)}>
          {save.isPending ? '�����С�' : '����'}
        </Button>
      </div>
    </div>
  )
}
export function BrowseCacheSection() {
  return (
    <SettingsSection icon={<DatabaseIcon className="size-4" />} title="��Ʒ�б�����">
      <SettingRow
        title="Ԥ������־û���"
        description="����Ԥ��������¼�������롢����������ͼƬ�����ϻ��档"
      >
        <BrowseCacheSettingsButton />
      </SettingRow>
    </SettingsSection>
  )
}
