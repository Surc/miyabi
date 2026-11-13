import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRoundIcon } from 'lucide-react'
import { toast } from 'sonner'

import { browseKeys } from '@/api/browse-cache'
import { apiDelete, apiGet, apiPost, describeApiError } from '@/api/client'
import { discoverKeys } from '@/api/movie-detail-cache'
import { InlineError } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SettingsSection } from '../shared'

const sessionStatuses = {
  signed_out: { label: '未登录', variant: 'outline' },
  signed_in: { label: '已登录', variant: 'success' },
  expired: { label: '登录已失效', variant: 'destructive' },
  unverified: { label: '暂无法确认', variant: 'outline' }
} as const

type JavDBSession = { configured: boolean; status: keyof typeof sessionStatuses }
type SessionAction = 'login' | 'logout'
const path = '/api/settings/javdb/session'
const queryKey = ['settings', 'javdb-session'] as const

export function JavDBSessionSection() {
  const queryClient = useQueryClient()
  const session = useQuery({
    queryKey,
    queryFn: ({ signal }) => apiGet<JavDBSession>(path, undefined, signal),
    refetchOnWindowFocus: true
  })
  const [pending, setPending] = useState<SessionAction | null>(null)
  const [error, setError] = useState('')
  const status = session.isError ? 'unverified' : (session.data?.status ?? 'unverified')
  const { label, variant } = sessionStatuses[status]

  async function updateSession(action: SessionAction, send: () => Promise<JavDBSession>) {
    setPending(action)
    setError('')
    try {
      const next = await send()
      await queryClient.cancelQueries({ queryKey })
      queryClient.setQueryData(queryKey, next)
      void queryClient.invalidateQueries({ queryKey: discoverKeys.all })
      void queryClient.invalidateQueries({ queryKey: [...browseKeys.all, 'rating', 'javdb'] })
      toast.success(action === 'login' ? 'JavDB 登录成功' : '已退出 JavDB 登录')
    } catch (error) {
      setError(describeApiError(error))
      toast.error(action === 'login' ? 'JavDB 登录失败' : '退出 JavDB 登录失败')
    } finally {
      setPending(null)
    }
  }

  function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const fields = new FormData(form)
    const username = String(fields.get('username') ?? '').trim()
    const password = String(fields.get('password') ?? '')
    form.reset()
    void updateSession('login', () => apiPost<JavDBSession>(path, { username, password }))
  }

  return (
    <SettingsSection icon={<KeyRoundIcon className="size-4" />} title="JavDB 登录">
      <div className="flex flex-wrap items-center gap-3">
        <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
          登录状态
          <Badge variant={session.isFetching ? 'outline' : variant}>
            {session.isFetching ? '正在检查…' : label}
          </Badge>
        </p>
        {session.data?.configured && !session.isError ? (
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={pending !== null || session.isFetching}
            onClick={() => void session.refetch()}
          >
            检查登录状态
          </Button>
        ) : null}
      </div>
      {session.isError ? (
        <InlineError onRetry={() => void session.refetch()} retrying={session.isFetching}>
          {describeApiError(session.error)}
        </InlineError>
      ) : null}
      {session.data ? (
        <form method="post" onSubmit={login}>
          <fieldset className="space-y-3" disabled={pending !== null}>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="space-y-1 text-sm">
                <span>JavDB 账号</span>
                <Input name="username" autoComplete="username" maxLength={254} required />
              </label>
              <label className="space-y-1 text-sm">
                <span>JavDB 密码</span>
                <Input
                  name="password"
                  type="password"
                  autoComplete="current-password"
                  maxLength={1024}
                  required
                />
              </label>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="submit">{session.data.configured ? '重新登录' : '登录'}</Button>
              {session.data.configured ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void updateSession('logout', () => apiDelete<JavDBSession>(path))}
                >
                  退出登录
                </Button>
              ) : null}
            </div>
          </fieldset>
        </form>
      ) : null}
      {pending ? (
        <p role="status" className="text-sm text-muted-foreground">
          {pending === 'login' ? '正在登录 JavDB…' : '正在退出登录…'}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </SettingsSection>
  )
}
