import { type SubtitleTask, useRetryTask } from '@/api/tasks'
import { subtitleStatus } from './subtitle-status'
import { Button } from '@/components/ui/button'

export function SubtitleProgressView({ task }: { task: SubtitleTask }) {
  const retry = useRetryTask()
  return (
    <div className="space-y-2 rounded-md border p-3">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm">
          {task.code} · {subtitleStatus(task)}
        </p>
        {task.can_retry ? (
          <Button
            size="sm"
            variant="outline"
            disabled={retry.isPending}
            onClick={() => retry.mutate(task.id)}
          >
            {retry.isPending ? '提交中…' : '重试字幕'}
          </Button>
        ) : null}
      </div>
      {task.error ? (
        <p role="alert" className="text-xs text-destructive">
          {task.error}
        </p>
      ) : null}
    </div>
  )
}
