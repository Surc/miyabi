import { useMDCzSettings } from '@/api/mdcz'
import { Badge } from '@/components/ui/badge'
import { ScrapingSection } from '../scraping-section'
import { MDCzSection } from './mdcz-section'

// Isolate the mode presentation here; the upstream native source UI/API stays intact.
export function ScrapingSettings() {
  const query = useMDCzSettings()
  const known = !!query.data && !query.isError
  const enhanced = known && query.data.enabled
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">刮削设置</h2>
        <Badge variant="outline">
          当前引擎：{known ? (enhanced ? 'MDCz' : 'Miyabi 原生') : '状态待确认'}
        </Badge>
      </div>
      <p className="text-xs leading-5 text-muted-foreground">
        引擎以已保存设置为准。MDCz 负责增强资料与封面，Miyabi 统一管理任务和入库；JavDB
        影片关联与独立评分继续保留。
      </p>
      <details
        key={enhanced ? 'mdcz' : 'native'}
        open={!enhanced}
        className="rounded-lg border p-4"
      >
        <summary className="cursor-pointer text-sm font-medium">
          原生刮削来源{enhanced ? ' · 已保留，当前不生效' : ''}
        </summary>
        <fieldset disabled={!known || enhanced} className="mt-4 disabled:opacity-50">
          <ScrapingSection />
        </fieldset>
      </details>
      <div className="rounded-lg border p-4">
        <MDCzSection />
      </div>
    </div>
  )
}
