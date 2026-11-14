import { usePublicationStatus } from '@/api/publication'
import { InlineError } from '@/components/error-state'

export function PublicationRecoveryStatus() {
  const query = usePublicationStatus()
  if (query.isError) {
    return (
      <InlineError onRetry={() => void query.refetch()} retrying={query.isFetching}>
        无法读取导出恢复状态，请刷新后确认，不要手动删除恢复文件。
      </InlineError>
    )
  }
  if (query.isPending) {
    return <p className="text-xs text-muted-foreground">正在读取导出恢复状态…</p>
  }
  if (!query.data?.blocked) return null
  return (
    <div role="alert" className="rounded-md border border-destructive/40 p-3 text-sm">
      <p className="font-medium">导出写入已暂停保护现有文件</p>
      <p className="mt-1 text-xs text-muted-foreground">
        {query.data.pending > 0
          ? `有 ${query.data.pending} 项发布需要恢复核验。`
          : '无法确认发布恢复记录。'}
        已入库内容仍可浏览。请保留现有文件和备份，检查日志中的恢复原因；处理后重试任务会重新核验。
      </p>
    </div>
  )
}
