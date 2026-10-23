import { useState } from 'react'
import { useMDCzSynopsis } from '@/api/mdcz'
import { Button } from '@/components/ui/button'
import { describeApiError } from '@/api/client'
import { useSettingsStore } from '@/stores/settings'

export function MovieSynopsis({ code, plot }: { code: string; plot?: string }) {
  const privacy = useSettingsStore(state => state.nsfwMode)
  if (privacy || (!code.trim() && !plot?.trim())) return null
  return plot?.trim() ? (
    <SynopsisText key={code} text={plot} />
  ) : (
    <RemoteSynopsis key={code} code={code} />
  )
}

function RemoteSynopsis({ code }: { code: string }) {
  const query = useMDCzSynopsis(code)
  if (query.data?.plot) return <SynopsisText text={query.data.plot} />
  if (query.isPending)
    return (
      <p role="status" className="text-xs text-muted-foreground">
        正在读取影片介绍…
      </p>
    )
  if (query.isError)
    return (
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">影片介绍</h3>
        <p role="alert" className="text-xs text-muted-foreground">
          {describeApiError(query.error)}
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          重试
        </Button>
      </section>
    )
  if (!query.data?.enabled) return null
  return <p className="text-xs text-muted-foreground">来源暂未提供影片介绍。</p>
}

function SynopsisText({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false)
  const collapsible = text.length > 120 || text.split('\n').length > 3
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold">影片介绍</h3>
      <p
        className={`text-sm leading-7 whitespace-pre-line text-muted-foreground ${collapsible && !expanded ? 'line-clamp-3' : ''}`}
      >
        {text}
      </p>
      {collapsible ? (
        <Button
          variant="ghost"
          size="sm"
          className="px-0"
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? '收起介绍' : '展开介绍'}
        </Button>
      ) : null}
    </section>
  )
}
