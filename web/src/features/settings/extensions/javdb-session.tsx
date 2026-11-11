import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRoundIcon } from 'lucide-react'

import { browseKeys } from '@/api/browse-cache'
import { apiDelete, apiGet, apiPost, describeApiError } from '@/api/client'
import { discoverKeys } from '@/api/movie-detail-cache'
import { InlineError } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SettingsSection } from '../shared'

type JavDBSession = { configured: boolean }
const path = '/api/settings/javdb/session'
const queryKey = ['settings', 'javdb-session'] as const

export function JavDBSessionSection() {
  const queryClient = useQueryClient()
  const session = useQuery({
    queryKey,
    queryFn: ({ signal }) => apiGet<JavDBSession>(path, undefined, signal)
  })
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  async function updateSession(send: () => Promise<JavDBSession>) {
    setPending(true)
    setError('')
    try {
      const next = await send()
      await queryClient.cancelQueries({ queryKey })
      queryClient.setQueryData(queryKey, next)
      void queryClient.invalidateQueries({ queryKey: discoverKeys.all })
      void queryClient.invalidateQueries({ queryKey: [...browseKeys.all, 'rating', 'javdb'] })
    } catch (error) {
      setError(describeApiError(error))
    } finally {
      setPending(false)
    }
  }

  function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const fields = new FormData(form)
    const username = String(fields.get('username') ?? '').trim()
    const password = String(fields.get('password') ?? '')
    form.reset()
    void updateSession(() => apiPost<JavDBSession>(path, { username, password }))
  }

  return (
    <SettingsSection icon={<KeyRoundIcon className="size-4" />} title="JavDB 登录">
      {session.isPending ? <p role="status">正在读取 JavDB 会话…</p> : null}
      {session.isError ? (
        <InlineError onRetry={() => void session.refetch()} retrying={session.isFetching}>
          {describeApiError(session.error)}
        </InlineError>
      ) : null}
      {session.data ? (
        <form method="post" onSubmit={login}>
          <fieldset className="space-y-3" disabled={pending}>
            <p role="status" className="text-sm text-muted-foreground">
              {session.data.configured
                ? '已保存 JavDB 会话。会话失效时可重新登录。'
                : '登录后可从 JavDB 查询影片详情和评分。'}
            </p>
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
                  onClick={() => void updateSession(() => apiDelete<JavDBSession>(path))}
                >
                  移除会话
                </Button>
              ) : null}
            </div>
          </fieldset>
        </form>
      ) : null}
      {pending ? <p role="status">正在更新 JavDB 会话…</p> : null}
      {error ? <InlineError>{error}</InlineError> : null}
    </SettingsSection>
  )
}
