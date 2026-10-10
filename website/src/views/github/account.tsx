import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { namedIdentityMethod, type NamedIdentityMethod } from './method'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { useApprovalCacheRevision } from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'
import type { GitHubIdentity, GitHubLocalProof } from '@/types/github'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { FormField, QueryState } from '@/components/app/CatalogUI'

export function GitHubProofFields({
  method = 'github',
  required,
  password,
  code,
  recovery,
  reason,
  disabled,
  setPassword,
  setCode,
  setRecovery,
  setReason,
}: {
  method?: NamedIdentityMethod
  required: boolean
  password: string
  code: string
  recovery: boolean
  reason: string
  disabled: boolean
  setPassword: (value: string) => void
  setCode: (value: string) => void
  setRecovery: (value: boolean) => void
  setReason: (value: string) => void
}) {
  const { t } = useTranslation(method)
  return (
    <fieldset disabled={disabled} className="space-y-4">
      <FormField label={t('password')}>
        <Input
          type="password"
          autoComplete="current-password"
          value={password}
          onValueChange={setPassword}
          required
        />
      </FormField>
      {required && (
        <>
          <FormField label={t(recovery ? 'recoveryCode' : 'code')}>
            <Input
              value={code}
              onValueChange={setCode}
              autoComplete="one-time-code"
              inputMode={recovery ? 'text' : 'numeric'}
              maxLength={recovery ? 128 : 6}
              required
            />
          </FormField>
          <Button
            type="button"
            variant="ghost"
            onClick={() => {
              setRecovery(!recovery)
              setCode('')
            }}
          >
            {t(recovery ? 'useAuthenticator' : 'useRecovery')}
          </Button>
        </>
      )}
      <FormField label={t('reason')}>
        <Input value={reason} onValueChange={setReason} required />
      </FormField>
    </fieldset>
  )
}
export default function GitHubAccount({ method = 'github' }: { method?: NamedIdentityMethod }) {
  const session = useSession()
  return (
    <Identity
      key={`${method}:${session.data?.user.id ?? ''}`}
      method={method}
      actor={session.data?.user.id ?? ''}
    />
  )
}
function Identity({ actor, method }: { actor: string; method: NamedIdentityMethod }) {
  const api = namedIdentityMethod(method)
  const { t } = useTranslation(method)
  const cache = useQueryClient()
  const navigate = useNavigate()
  const generation = useSessionGeneration()
  const parent = useApprovalCacheRevision([['auth', 'session']])
  const [expired, setExpired] = useState(false)
  function authority() {
    const state = cache.getQueryState<Session>(['auth', 'session'])
    return (
      !!actor &&
      !expired &&
      parent.snapshot() === parent.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated &&
      state.data?.user.id === actor
    )
  }
  const queryKey = [method, 'identity', actor, generation, parent.revision]
  const query = useQuery({
    queryKey,
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Identity unavailable')
      const result = await api.getIdentity(signal)
      if (signal.aborted || !authority()) throw new Error('Identity unavailable')
      return result
    },
  })
  const resource = useApprovalCacheRevision([queryKey])
  function readable() {
    const state = cache.getQueryState<GitHubIdentity>(queryKey)
    return (
      authority() &&
      resource.snapshot() === resource.revision &&
      query.isSuccess &&
      !query.isFetching &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data
    )
  }
  const page = readable() ? query.data : undefined
  const [action, setAction] = useState<'bind' | 'unlink' | null>(null)
  const [review, setReview] = useState<GitHubIdentity | null>(null)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState('')
  const [confirmation, setConfirmation] = useState<symbol | null>(null)
  const confirmationOwner = useRef<symbol | null>(null)
  const [intent, setIntent] = useState<{
    action: 'bind' | 'unlink'
    review: GitHubIdentity
    input: GitHubLocalProof
  } | null>(null)
  const live = useRef({ mounted: true, turn: 0, locked: false, revoked: false, submitted: false })
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    const owner = live.current
    owner.mounted = true
    const expire = () => {
      owner.revoked = true
      confirmationOwner.current = null
      setConfirmation(null)
      owner.turn++
      owner.locked = false
      request.current?.abort()
      setExpired(true)
      setIntent(null)
      setPassword('')
      setCode('')
      setAction(null)
      setBusy(false)
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      owner.mounted = false
      confirmationOwner.current = null
      owner.turn++
      request.current?.abort()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [])
  const authoritySnapshot = parent.snapshot
  const resourceSnapshot = resource.snapshot
  const resourceKey = JSON.stringify(queryKey)
  useLayoutEffect(() => {
    let previous = `${authoritySnapshot()}|${resourceSnapshot()}`
    return cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const key = event.query.queryKey
      const relevant =
        (key.length === 2 && key[0] === 'auth' && key[1] === 'session') ||
        JSON.stringify(key) === resourceKey
      if (!relevant) return
      const next = `${authoritySnapshot()}|${resourceSnapshot()}`
      if (next === previous) return
      previous = next
      const owner = live.current
      owner.turn++
      request.current?.abort()
      owner.locked = false
      setBusy(false)
      confirmationOwner.current = null
      setConfirmation(null)
      setPassword('')
      setCode('')
      if (owner.submitted) setIssue('outcomeUnknown')
    })
  }, [cache, authoritySnapshot, resourceSnapshot, resourceKey])
  const stale = !!page && !!review && page.review_etag !== review.review_etag
  function open(next: 'bind' | 'unlink') {
    if (!readable() || live.current.locked || live.current.submitted || intent || !page) return
    setAction(next)
    setReview(page)
    setPassword('')
    setCode('')
    setReason('')
    setIssue('')
    confirmationOwner.current = null
    setConfirmation(null)
  }
  function close() {
    if (live.current.locked || live.current.submitted || intent) return
    setAction(null)
    setPassword('')
    setCode('')
    setReview(null)
    confirmationOwner.current = null
    setConfirmation(null)
  }
  async function dispatch(retry = false) {
    const owner = live.current
    if (!owner.mounted || owner.locked || owner.revoked || !readable() || !page) return
    if (
      !retry &&
      (!confirmation ||
        confirmationOwner.current !== confirmation ||
        owner.submitted ||
        !review ||
        !action ||
        stale ||
        intent)
    )
      return
    if (retry && intent?.action !== 'unlink') return
    const local: GitHubLocalProof = {
      password,
      proof: review?.mfa_required ? (recovery ? { recovery_code: code.trim() } : { code }) : {},
      reason,
    }
    const input = local
    const captured = retry
      ? intent!
      : { action: action!, review: { ...review! }, input: { ...input, proof: { ...input.proof } } }
    const session = cache.getQueryData<Session>(['auth', 'session'])
    if (!session?.csrf_token || !api.validProof(captured.input, captured.review.mfa_required)) {
      setIssue('invalid')
      return
    }
    confirmationOwner.current = null
    owner.locked = true
    owner.submitted = true
    const turn = ++owner.turn
    setBusy(true)
    setIssue('')
    setIntent(captured)
    setPassword('')
    setCode('')
    request.current = new AbortController()
    const current = () => owner.mounted && !owner.revoked && owner.turn === turn && readable()
    let settlingSession = false
    try {
      if (captured.action === 'bind') {
        const url = await api.beginProof(
          'bind',
          captured.review.review_etag,
          captured.input,
          captured.review.mfa_required,
          session.csrf_token,
          request.current.signal,
        )
        if (current()) window.location.assign(url)
        return
      } else {
        await api.unlink(
          captured.review.review_etag,
          captured.input,
          captured.review.mfa_required,
          session.csrf_token,
          request.current.signal,
        )
      }
      if (!current()) return
      settlingSession = true
      const refreshed = await api.readSession(request.current.signal)
      if (!current()) return
      if (refreshed && refreshed.user.id !== actor) {
        void cache.invalidateQueries({ queryKey: sessionKey })
        return
      }
      owner.submitted = false
      const renewed =
        !!refreshed &&
        (refreshed.csrf_token !== session.csrf_token ||
          refreshed.user.role !== session.user.role ||
          refreshed.user.name !== session.user.name ||
          refreshed.user.email !== session.user.email)
      if (!refreshed || renewed) {
        await cache.cancelQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
        if (!current()) return
        owner.locked = false
        request.current = null
        setIntent(null)
        setAction(null)
        setReview(null)
        confirmationOwner.current = null
        setConfirmation(null)
        setBusy(false)
        cache.removeQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
        cache.getMutationCache().clear()
        cache.setQueryData(setupKey, { initialized: true })
        cache.setQueryData(sessionKey, refreshed)
        if (!refreshed) navigate('/login', { replace: true })
        return
      }
      setIntent(null)
      setAction(null)
      setReview(null)
      confirmationOwner.current = null
      setConfirmation(null)
      setIssue('unlinked')
      owner.locked = false
      request.current = null
      void cache.invalidateQueries({ queryKey: [method] })
      void cache.invalidateQueries({ queryKey: ['account', 'sessions'] })
    } catch (error) {
      if (owner.mounted && owner.turn === turn) {
        setIssue('outcomeUnknown')
        if (
          settlingSession ||
          (error instanceof api.RequestError && [401, 403].includes(error.status))
        )
          void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
      }
    } finally {
      if (owner.mounted && owner.turn === turn) {
        owner.locked = false
        setBusy(false)
      }
    }
  }
  async function abandonIntent() {
    const owner = live.current
    if (!owner.mounted || owner.locked || !readable() || !intent) return
    if (intent?.action === 'bind') {
      owner.locked = true
      const turn = ++owner.turn
      request.current?.abort()
      const controller = new AbortController()
      request.current = controller
      setBusy(true)
      const current = () => owner.mounted && !owner.revoked && owner.turn === turn && readable()
      try {
        const session = await api.readSession(controller.signal)
        if (!current() || !session || session.user.id !== actor) return
        await api.abandon(session.csrf_token, controller.signal)
        if (!current()) return
      } catch {
        if (owner.mounted && owner.turn === turn) setIssue('abandonUnknown')
        return
      } finally {
        if (owner.mounted && owner.turn === turn) {
          owner.locked = false
          setBusy(false)
        }
      }
    }
    if (!owner.mounted || !readable()) return
    owner.turn++
    owner.submitted = false
    request.current?.abort()
    request.current = null
    setIntent(null)
    setReview(null)
    setConfirmation(null)
    confirmationOwner.current = null
    setPassword('')
    setCode('')
    setReason('')
    setIssue('abandoned')
    void query.refetch()
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('identityTitle')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <QueryState
          pending={authority() && query.isFetching}
          error={authority() ? query.error : null}
          retry={() => void query.refetch()}
        />
        {page && (
          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="font-medium">{page.name || t('title')}</p>
              <Badge variant="outline">{t(page.bound ? 'bound' : 'notBound')}</Badge>
            </div>
            <Button
              variant="outline"
              disabled={busy || !!intent || (!page.bound && !page.available)}
              onClick={() => open(page.bound ? 'unlink' : 'bind')}
            >
              {t(page.bound ? 'unlink' : 'bind')}
            </Button>
          </div>
        )}
        {(issue === 'unlinked' || issue === 'linked') && page && <p role="status">{t(issue)}</p>}
        <Dialog
          open={!!action && !!page}
          onOpenChange={(value) => {
            if (!value) close()
          }}
          title={t(action === 'unlink' ? 'unlink' : 'bind')}
          description={t(action === 'unlink' ? 'unlinkHelp' : 'bindHelp')}
          busy={busy}
        >
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              if (
                live.current.mounted &&
                !live.current.revoked &&
                !live.current.locked &&
                !live.current.submitted &&
                !intent &&
                readable() &&
                !stale &&
                review
              ) {
                if (
                  api.validProof(
                    {
                      password,
                      proof: review.mfa_required
                        ? recovery
                          ? { recovery_code: code.trim() }
                          : { code }
                        : {},
                      reason,
                    },
                    review.mfa_required,
                  )
                ) {
                  const next = Symbol('github-confirmation')
                  confirmationOwner.current = next
                  setConfirmation(next)
                } else setIssue('invalid')
              }
            }}
          >
            {stale && <p role="alert">{t('conflict')}</p>}
            {issue && <p role="alert">{t(issue)}</p>}
            <GitHubProofFields
              method={method}
              required={review?.mfa_required === true}
              password={password}
              code={code}
              recovery={recovery}
              reason={reason}
              disabled={busy || !!intent || stale || !review}
              setPassword={setPassword}
              setCode={setCode}
              setRecovery={setRecovery}
              setReason={setReason}
            />
            {!intent && (
              <Button type="submit" disabled={busy || stale}>
                {t('review')}
              </Button>
            )}
            {(stale || intent) && (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (!live.current.locked && readable()) void query.refetch()
                }}
              >
                {t('reviewCurrent')}
              </Button>
            )}
            {intent?.action === 'unlink' && (
              <Button type="button" disabled={busy} onClick={() => void dispatch(true)}>
                {t('retryOriginal')}
              </Button>
            )}
            {intent && (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => void abandonIntent()}
              >
                {t('abandon')}
              </Button>
            )}
            {(stale || !review) && !intent && (
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  if (readable() && !live.current.locked) {
                    setReview(page ?? null)
                    confirmationOwner.current = null
                    setConfirmation(null)
                  }
                }}
              >
                {t('acceptReview')}
              </Button>
            )}
          </form>
        </Dialog>
        <Dialog
          open={!!confirmation && !!page && !!action && !intent}
          onOpenChange={(value) => {
            if (!value && !live.current.locked) {
              confirmationOwner.current = null
              setConfirmation(null)
            }
          }}
          title={t('confirmTitle')}
          description={t(action === 'unlink' ? 'unlinkHelp' : 'bindHelp')}
          busy={busy}
        >
          <div className="space-y-4">
            <p>{reason}</p>
            <Button disabled={busy || stale} onClick={() => void dispatch()}>
              {t('confirm')}
            </Button>
          </div>
        </Dialog>
      </CardContent>
    </Card>
  )
}
