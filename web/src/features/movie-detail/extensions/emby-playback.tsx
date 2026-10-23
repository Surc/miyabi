import { LoaderCircleIcon, PlayIcon } from 'lucide-react'
import { useEmbyPlayback, isEmbyPlayURL } from '@/api/emby-playback'
import { Button } from '@/components/ui/button'
import { describeApiError } from '@/api/client'

export function MovieEmbyPlayback({ code }: { code: string }) {
  // Other clients have different intents; do not send them an Android play URL
  // or silently substitute an item-details page for direct playback.
  if (typeof navigator === 'undefined' || !/Android/i.test(navigator.userAgent))
    return (
      <div className="space-y-2">
        <Button disabled>
          <PlayIcon />在 Emby App 中播放
        </Button>
        <p className="text-xs text-muted-foreground">直接开播需安卓 Emby App 3.4.26 或更新版本。</p>
      </div>
    )
  return <AndroidEmbyPlayback code={code} />
}

function AndroidEmbyPlayback({ code }: { code: string }) {
  const query = useEmbyPlayback(code)
  const target = query.data
  const ready = !query.isError && target?.status === 'ready' && isEmbyPlayURL(target.url)
  let message = '需先在安卓 Emby App 登录同一服务器；点击后直接播放，默认从首个视频文件开始。'
  if (!code.trim()) message = '缺少影片番号，无法定位 Emby 播放资源。'
  else if (query.isError) message = describeApiError(query.error)
  else if (query.isPending) message = '正在确认 Emby 中的播放资源…'
  else if (target?.status === 'disabled') message = '请先在设置中启用并配置 Emby。'
  else if (target?.status === 'not_exported') message = '影片尚未导出播放文件，请先完成入库与同步。'
  else if (target?.status === 'not_found')
    message = 'Emby 尚未收录该播放文件，请扫描 Emby 媒体库后重试。'
  else if (!ready) message = '未能取得有效的 Emby 直接播放地址，请重试。'

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        {ready ? (
          <Button asChild>
            <a href={target.url}>
              <PlayIcon />在 Emby App 中播放
            </a>
          </Button>
        ) : (
          <Button disabled>
            {query.isFetching ? <LoaderCircleIcon className="animate-spin" /> : <PlayIcon />}
            在 Emby App 中播放
          </Button>
        )}
        {!query.isPending || query.isError ? (
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            重新检查
          </Button>
        ) : null}
      </div>
      <p role={query.isError ? 'alert' : 'status'} className="text-xs text-muted-foreground">
        {message}
      </p>
    </div>
  )
}
