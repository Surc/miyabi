import type { SubtitleTask } from '../../api/tasks'

export function subtitleStatus(task: SubtitleTask) {
  if (task.paused) return '字幕任务已暂停'
  if (task.status === 'failed') return '影片已入库，字幕等待重试'
  if (task.status === 'done') return '字幕处理完成'
  if (task.retry_at) return '字幕暂时失败，等待自动重试'
  return task.status === 'running' ? '正在处理字幕' : '字幕已排队'
}
