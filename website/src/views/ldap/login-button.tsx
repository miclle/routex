import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getLDAPMethod, loginLDAP, validLDAPLogin, LDAPRequestError } from '@/api/ldap'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { FormField } from '@/components/app/CatalogUI'
import { useApprovalCacheRevision } from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'
import type { LDAPLoginResult } from '@/types/ldap'

export default function LDAPLoginButton({
  disabled = false,
  acquire,
}: {
  disabled?: boolean
  acquire: () => {
    current: () => boolean
    release: () => void
    accept: (result: LDAPLoginResult, current: () => boolean) => Promise<void>
  } | null
}) {
  const { t } = useTranslation('ldap')
  const cache = useQueryClient()
  const boundary = useApprovalCacheRevision([
    ['auth', 'session'],
    ['ldap', 'public'],
  ])
  const method = useQuery({
    queryKey: ['ldap', 'public'],
    queryFn: ({ signal }) => getLDAPMethod(signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false),
    [issue, setIssue] = useState(''),
    [username, setUsername] = useState(''),
    [password, setPassword] = useState('')
  const owner = useRef({ mounted: true, turn: 0, locked: false, used: false, revoked: false })
  const request = useRef<AbortController | null>(null)
  function readable() {
    const state = cache.getQueryState(['ldap', 'public']),
      session = cache.getQueryState<Session | null>(['auth', 'session'])
    return (
      boundary.snapshot() === boundary.revision &&
      method.isSuccess &&
      !method.isFetching &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated &&
      state.data === method.data &&
      (!session ||
        (session.status === 'success' &&
          session.fetchStatus === 'idle' &&
          !session.isInvalidated &&
          !session.error &&
          session.data === null))
    )
  }
  useEffect(() => {
    const live = owner.current
    live.mounted = true
    const expire = () => {
      live.revoked = true
      live.turn++
      request.current?.abort()
      setOpen(false)
      setUsername('')
      setPassword('')
      setBusy(false)
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      live.mounted = false
      live.turn++
      request.current?.abort()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [])
  const snapshot = boundary.snapshot
  useLayoutEffect(() => {
    let previous = snapshot()
    return cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const key = event.query.queryKey
      if (
        key.length !== 2 ||
        !((key[0] === 'auth' && key[1] === 'session') || (key[0] === 'ldap' && key[1] === 'public'))
      )
        return
      const next = snapshot()
      if (next === previous) return
      previous = next
      const live = owner.current
      live.turn++
      request.current?.abort()
      live.locked = false
      setUsername('')
      setPassword('')
      setOpen(false)
      setBusy(false)
    })
  }, [cache, snapshot])
  async function submit() {
    const live = owner.current
    if (
      disabled ||
      live.locked ||
      live.used ||
      live.revoked ||
      !readable() ||
      !method.data?.available
    )
      return
    const input = { username, password }
    if (!validLDAPLogin(input)) {
      setIssue('invalidLogin')
      return
    }
    const lease = acquire()
    if (!lease) return
    live.locked = true
    live.used = true
    const turn = ++live.turn
    setBusy(true)
    setIssue('')
    setPassword('')
    request.current = new AbortController()
    const current = () =>
      live.mounted && !live.revoked && live.turn === turn && readable() && lease.current()
    try {
      const result = await loginLDAP(input, request.current.signal)
      if (!current()) return
      setUsername('')
      setOpen(false)
      await lease.accept(result, current)
    } catch (error) {
      if (live.mounted && live.turn === turn) {
        const rejected = error instanceof LDAPRequestError && error.status === 401
        live.used = !rejected
        setIssue(rejected ? 'invalidLogin' : 'loginUnknown')
      }
    } finally {
      lease.release()
      if (live.mounted && live.turn === turn) {
        live.locked = false
        setBusy(false)
      }
    }
  }
  if (!readable() || !method.data?.available) return null
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        <span>{t('alternative')}</span>
        <span className="h-px flex-1 bg-border" />
      </div>
      <Button
        type="button"
        variant="outline"
        className="w-full"
        disabled={disabled || busy || issue === 'loginUnknown'}
        onClick={() => {
          if (owner.current.locked || owner.current.used || !readable()) return
          setOpen(true)
        }}
      >
        {t('signIn', { name: method.data.name })}
      </Button>
      {issue === 'loginUnknown' && (
        <p role="alert">
          {t(issue)}{' '}
          <a href="/login" className="underline">
            {t('restartLogin')}
          </a>
        </p>
      )}
      <Dialog
        open={open && readable()}
        onOpenChange={(value) => {
          if (!value && !owner.current.locked) {
            setOpen(false)
            setUsername('')
            setPassword('')
            if (!owner.current.used) setIssue('')
          }
        }}
        title={t('loginTitle', { name: method.data.name })}
        description={t('loginHelp')}
        busy={busy}
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            void submit()
          }}
        >
          <fieldset disabled={disabled || busy || issue === 'loginUnknown'} className="space-y-4">
            <FormField label={t('username')}>
              <Input
                autoComplete="username"
                value={username}
                onValueChange={setUsername}
                required
              />
            </FormField>
            <FormField label={t('directoryPassword')}>
              <Input
                type="password"
                autoComplete="current-password"
                value={password}
                onValueChange={setPassword}
                required
              />
            </FormField>
            <Button type="submit">{t('loginSubmit')}</Button>
          </fieldset>
          {issue && <p role="alert">{t(issue)}</p>}
        </form>
      </Dialog>
    </div>
  )
}
