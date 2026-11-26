import { ArrowDownIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'

export function downloadLinkID(key: string) {
  return `download-link-${key}`
}

export function DownloadProtocol({ kind, target }: { kind: 'magnet' | 'ed2k'; target?: string }) {
  const label = kind === 'ed2k' ? 'ED2K' : '磁力'
  const color =
    kind === 'ed2k'
      ? 'border-transparent bg-violet-100 text-violet-800 dark:bg-violet-950 dark:text-violet-200'
      : 'border-transparent bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-200'
  return target ? (
    <Badge
      className={color}
      render={
        <button
          type="button"
          aria-label={`定位${label}链接`}
          onClick={() => {
            const card = document.getElementById(target)
            card?.scrollIntoView({ block: 'nearest' })
            card?.focus({ preventScroll: true })
          }}
        />
      }
    >
      {label}
      <ArrowDownIcon />
    </Badge>
  ) : (
    <Badge className={color}>{label}</Badge>
  )
}
