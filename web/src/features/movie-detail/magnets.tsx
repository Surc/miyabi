import { Link } from '@tanstack/react-router'
import {
  CheckIcon,
  ChevronDownIcon,
  CloudDownloadIcon,
  CopyIcon,
  LoaderCircleIcon,
  SkipForwardIcon
} from 'lucide-react'
import { useState } from 'react'

import type { DiscoverMagnet, useDiscoverMagnets } from '@/api/discover'
import { describeApiError, isPanUnauthorized } from '@/api/client'
import { useDiscoverED2K } from '@/api/discover-ed2k'
import type { LibraryMovieVideo } from '@/api/library'
import {
  isOfflineTaskActive,
  useAddOffline,
  useMovieOfflineTasks,
  useOfflineControl,
  type OfflineSubmission
} from '@/api/offline'
import { usePanAccounts } from '@/api/pan'
import { InlineError } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu'
import { formatSize } from '@/lib/format'
import { notifyOfflineTask, notifyTaskError } from '@/features/tasks/task-toast'
import { MovieMagnetsSkeleton } from './skeleton'
import { sortMagnets, type MagnetOrder } from './extensions/magnet-dates'
import { MagnetPublication, MagnetSort } from './extensions/magnet-publication'
import { downloadLinkID, DownloadProtocol } from './download-protocol'

function SubmitLabel({
  pending,
  submitted,
  label
}: {
  pending: boolean
  submitted: boolean
  label: string
}) {
  return (
    <>
      {pending ? (
        <LoaderCircleIcon className="animate-spin" />
      ) : submitted ? (
        <CheckIcon />
      ) : (
        <CloudDownloadIcon />
      )}
      {label}
    </>
  )
}

const sourceLabels: Record<string, string> = {
  javdb: 'JavDB',
  javdb_comments: 'JavDB 评论区',
  javbus: 'JavBus',
  sukebei: 'Sukebei',
  projectjav: 'ProjectJav',
  freejavbt: 'FreeJavBT',
  pan_history: '115 离线补录',
  miyabi: 'Miyabi 入库记录'
}

const phaseLabels: Record<OfflineSubmission['phase'], string> = {
  available: '一键加入 115',
  downloading: '下载中',
  processing: '入库处理中',
  in_library: '已入库',
  downloaded: '已下载'
}

type DownloadResource = Pick<
  DiscoverMagnet,
  'hash' | 'name' | 'uri' | 'size' | 'created_at' | 'sources' | 'tags'
> & { kind: 'magnet' | 'ed2k'; page?: number; videos?: LibraryMovieVideo[] }
export type OfflineAccount = { id: string; name: string; directory: string }

export function MovieMagnets({
  movieID,
  javdbID,
  code,
  query,
  videos
}: {
  movieID: string
  javdbID: string
  code: string
  query: ReturnType<typeof useDiscoverMagnets>
  videos?: LibraryMovieVideo[]
}) {
  const ed2k = useDiscoverED2K(javdbID)
  const links = new Map<string, DownloadResource>()
  for (const magnet of query.data ?? []) {
    links.set(magnet.hash, { ...magnet, kind: 'magnet' })
  }
  for (const page of ed2k.data?.pages ?? []) {
    for (const item of page.items) {
      if (!links.has(item.id)) {
        links.set(item.id, {
          ...item,
          hash: item.id,
          kind: 'ed2k',
          page: page.page,
          sources: [item.source ?? 'javdb_comments']
        })
      }
    }
  }
  const panAccounts = usePanAccounts()
  const unauthorized = isPanUnauthorized(panAccounts.error)
  const accounts: OfflineAccount[] = unauthorized
    ? []
    : (panAccounts.data ?? [])
        .filter(status => status.connected && status.account && status.directory)
        .map(status => ({
          id: status.account!.id,
          name: status.account!.name || status.account!.id,
          directory: status.directory!.name
        }))
  const hasAccounts = (panAccounts.data?.length ?? 0) > 0
  const offline = useMovieOfflineTasks(movieID, code)
  const tasks = new Map((offline.data ?? []).map(task => [task.hash, task] as const))
  for (const video of videos ?? []) {
    for (const link of video.download_links ?? []) {
      const resource = links.get(link.key)
      if (resource) {
        resource.videos = [...(resource.videos ?? []), video]
      } else {
        links.set(link.key, {
          hash: link.key,
          kind: link.key.startsWith('ed2k:') ? 'ed2k' : 'magnet',
          name: video.name,
          uri: link.uri,
          size: video.size,
          created_at: '',
          sources: [link.from_history ? 'pan_history' : 'miyabi'],
          tags: [],
          videos: [video]
        })
      }
    }
  }
  const checkingStatus = offline.isPending
  const [copiedHash, setCopiedHash] = useState('')
  const [copyError, setCopyError] = useState(false)
  const [order, setOrder] = useState<MagnetOrder>('recommended')

  async function copy(resource: DownloadResource) {
    setCopyError(false)
    try {
      await navigator.clipboard.writeText(resource.uri)
      setCopiedHash(resource.hash)
    } catch {
      setCopyError(true)
    }
  }

  function renderResource(resource: DownloadResource) {
    return (
      <ResourceCard
        key={`${movieID}:${resource.hash}`}
        movieID={resource.page ? javdbID : movieID}
        resource={resource}
        task={tasks.get(resource.hash)}
        checkingStatus={checkingStatus}
        statusError={offline.isError}
        accounts={accounts}
        copied={copiedHash === resource.hash}
        onCopy={() => void copy(resource)}
      />
    )
  }

  return (
    <div className="space-y-6">
      {hasAccounts && accounts.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          请先到{' '}
          <Link to="/settings/sources" className="text-foreground underline underline-offset-4">
            设置 → 源管理
          </Link>{' '}
          为 115 账号挂载媒体目录，再一键加入 115。
        </p>
      ) : null}
      {offline.isError ? (
        <InlineError onRetry={() => void offline.refetch()} retrying={offline.isFetching}>
          离线任务状态读取失败，请重试。
        </InlineError>
      ) : null}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-xl font-semibold tracking-normal">下载链接</h2>
          {links.size > 1 ? <MagnetSort value={order} onChange={setOrder} /> : null}
        </div>
        {sortMagnets(Array.from(links.values()), order).map(renderResource)}
        {movieID && query.isPending && links.size === 0 ? (
          <MovieMagnetsSkeleton />
        ) : query.isError ? (
          <InlineError onRetry={() => void query.refetch()} retrying={query.isFetching}>
            磁力加载失败
          </InlineError>
        ) : movieID && query.data?.length === 0 && links.size === 0 ? (
          <p className="text-sm text-muted-foreground">暂无下载链接</p>
        ) : null}
        {ed2k.isError ? (
          <InlineError
            onRetry={() => void (ed2k.isFetchNextPageError ? ed2k.fetchNextPage() : ed2k.refetch())}
            retrying={ed2k.isFetching}
          >
            {describeApiError(ed2k.error)}
          </InlineError>
        ) : ed2k.hasNextPage ? (
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={ed2k.isFetching}
            onClick={() => void ed2k.fetchNextPage()}
          >
            {ed2k.isFetchingNextPage ? '加载中…' : '查找更多 ED2K'}
          </Button>
        ) : null}
      </section>
      {copyError ? <InlineError>复制失败，请重试。</InlineError> : null}
    </div>
  )
}

function ResourceCard({
  movieID,
  resource,
  task,
  checkingStatus,
  statusError,
  accounts,
  copied,
  onCopy
}: {
  movieID: string
  resource: DownloadResource
  task?: OfflineSubmission
  checkingStatus: boolean
  statusError: boolean
  accounts: OfflineAccount[]
  copied: boolean
  onCopy: () => void
}) {
  const add = useAddOffline(movieID)
  const next = useOfflineControl('next')
  const linked = Boolean(resource.videos?.length)
  const submitted = task !== undefined && task.phase !== 'available'
  const busy = add.isPending || (checkingStatus && !submitted)
  let label = '加入 115'
  if (add.isPending) {
    label = '提交中'
  } else if (submitted) {
    label = phaseLabels[task.phase]
  } else if (checkingStatus) {
    label = '读取状态'
  } else if (statusError) {
    label = '状态暂不可用'
  }

  const error = !add.isPending ? task?.error : undefined
  const disabled = busy || statusError || submitted || accounts.length === 0
  const pendingSubmit = busy || (task ? isOfflineTaskActive(task) : false)
  const submit = (accountID: string) =>
    add.mutate(
      resource.page
        ? { accountID, ed2k: resource.hash, page: resource.page }
        : { accountID, hash: resource.hash },
      {
        onSuccess: submission => notifyOfflineTask(submission),
        onError: submitError =>
          notifyTaskError(
            `offline:submit-error:${movieID}`,
            '加入 115 失败',
            describeApiError(submitError)
          )
      }
    )

  return (
    <Card
      size="sm"
      id={downloadLinkID(resource.hash)}
      tabIndex={-1}
      className="scroll-mt-4 focus:ring-2"
    >
      <CardContent className="space-y-3">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <DownloadProtocol kind={resource.kind} />
              <h3 className="min-w-0 text-base leading-6 font-semibold wrap-break-word">
                {resource.name}
              </h3>
              {linked ? <Badge variant="library">已入库</Badge> : null}
            </div>
            <p className="w-fit max-w-full font-mono text-xs break-all text-muted-foreground select-all">
              {resource.uri}
            </p>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm leading-6 text-muted-foreground">
              <span className="text-xs">
                {resource.size > 0 ? formatSize(resource.size) : '未知'}
              </span>
              <MagnetPublication value={resource.created_at} />
              {resource.sources?.map(source => (
                <Badge key={source} variant="outline">
                  {sourceLabels[source] ?? source}
                </Badge>
              ))}
              {resource.tags?.map(tag => (
                <Badge key={tag} variant="outline">
                  {tag}
                </Badge>
              ))}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2 self-end sm:self-center">
            {task?.can_switch ? (
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                title="尝试下一条磁力"
                disabled={next.isPending || statusError}
                onClick={() => next.mutate(task.task_id)}
              >
                {next.isPending ? (
                  <LoaderCircleIcon className="animate-spin" />
                ) : (
                  <SkipForwardIcon />
                )}
              </Button>
            ) : null}
            <Button type="button" variant="outline" size="sm" onClick={onCopy}>
              {copied ? <CheckIcon /> : <CopyIcon />}
              {copied ? '已复制' : '复制'}
            </Button>
            {!linked && accounts.length > 1 ? (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={<Button type="button" size="sm" disabled={disabled} />}
                >
                  <SubmitLabel pending={pendingSubmit} submitted={submitted} label={label} />
                  <ChevronDownIcon className="size-3.5 opacity-70" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-44">
                  <DropdownMenuLabel>选择加入的 115 账号</DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  {accounts.map(account => (
                    <DropdownMenuItem key={account.id} onClick={() => submit(account.id)}>
                      <CloudDownloadIcon />
                      <div className="min-w-0">
                        <p className="truncate text-sm">{account.name}</p>
                        <p className="truncate text-xs text-muted-foreground">
                          {account.directory}
                        </p>
                      </div>
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            ) : !linked && accounts.length === 1 ? (
              <Button
                type="button"
                size="sm"
                disabled={disabled}
                onClick={() => submit(accounts[0]!.id)}
              >
                <SubmitLabel pending={pendingSubmit} submitted={submitted} label={label} />
              </Button>
            ) : null}
          </div>
        </div>
        {linked ? (
          <div className="flex flex-wrap items-baseline gap-2 border-t pt-3 text-sm">
            <span className="text-xs text-muted-foreground">对应视频</span>
            {resource.videos?.map(video => (
              <span key={video.id} className="wrap-break-word">
                {video.name}
              </span>
            ))}
          </div>
        ) : null}
        {task?.phase === 'in_library' && task.processing ? (
          <p className="text-sm text-muted-foreground">文件已入库，后台整理中。</p>
        ) : null}
        {task && (task.attempt_count ?? 0) > 1 ? (
          <p className="text-xs text-muted-foreground">
            已尝试 {task.attempt_count} 条磁力{task.switch_reason ? ` · ${task.switch_reason}` : ''}
          </p>
        ) : null}
        {error ? <InlineError>{error}</InlineError> : null}
      </CardContent>
    </Card>
  )
}
