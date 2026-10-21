import { CalendarIcon } from 'lucide-react'
import { magnetPublication, type MagnetOrder } from './magnet-dates'

export function MagnetSort({
  value,
  onChange
}: {
  value: MagnetOrder
  onChange: (order: MagnetOrder) => void
}) {
  return (
    <label className="flex items-center gap-2 text-sm text-muted-foreground">
      排序
      <select
        aria-label="磁力排序"
        className="rounded-lg border bg-background px-3 py-2 text-foreground"
        value={value}
        onChange={event => onChange(event.target.value as MagnetOrder)}
      >
        <option value="recommended">默认推荐</option>
        <option value="newest">发布时间：最新优先</option>
        <option value="oldest">发布时间：最早优先</option>
      </select>
    </label>
  )
}

export function MagnetPublication({ value }: { value: string }) {
  const date = magnetPublication(value)
  return (
    <span
      className="inline-flex items-center gap-1.5 text-xs"
      title="来源报告的磁力发布时间；同一哈希合并时保留来源中最早日期"
    >
      <CalendarIcon className="size-3.5" />
      {date ? <time dateTime={date.day}>发布于 {date.day}</time> : '发布时间未知'}
    </span>
  )
}
