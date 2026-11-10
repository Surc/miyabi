import { useMDCzSettings } from '@/api/mdcz'
import { Badge } from '@/components/ui/badge'
import { ScrapingSection } from '../scraping-section'
import { MDCzSection } from './mdcz-section'

export function ScrapingSettings() {
  const query = useMDCzSettings()
  const known = !!query.data && !query.isError
  const enhanced = known && query.data.enabled
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">刮削设置</h2>
        <Badge variant="outline">
          当前引擎：{known ? (enhanced ? 'MDCz 增强' : 'Miyabi 原版') : '状态待确认'}
        </Badge>
      </div>
      <div className="rounded-lg border p-4">
        <MDCzSection />
      </div>
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
    </div>
  )
}
