import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { namedIdentityMethod, type NamedIdentityMethod } from './method'
import { validApprovalReason } from '@/api/registration-approval'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import {
  useApprovalCacheRevision,
  approvalActorCurrent,
} from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'
import type { GitHubConfig, GitHubConfigInput, GitHubLocalProof } from '@/types/github'
import { GitHubProofFields } from './account'
import { Card, CardContent } from '@/components/ui/card'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Intent =
  | { kind: 'save'; etag: string; body: GitHubConfigInput }
  | { kind: 'status'; etag: string; body: { enabled: boolean; reason: string } }
  | { kind: 'verify'; etag: string; body: GitHubLocalProof; mfa: boolean }
export default function GitHubConfiguration({
  method = 'github',
}: {
  method?: NamedIdentityMethod
}) {
  const session = useSession()
  usePermissions()
  return (
    <Configuration
      key={`${method}:${session.data?.user.id ?? ''}`}
      method={method}
      actor={session.data?.user.id ?? ''}
    />
  )
}
function Configuration({ actor, method }: { actor: string; method: NamedIdentityMethod }) {
  const api = namedIdentityMethod(method)
  const { t } = useTranslation(method)
  const cache = useQueryClient()
  const navigate = useNavigate()
  const generation = useSessionGeneration()
  const parent = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
  ])
  const [expired, setExpired] = useState(false)
  function authority() {
    return (
      !expired &&
      parent.snapshot() === parent.revision &&
      approvalActorCurrent(cache, actor, 'registration.write', true)
    )
  }
  const queryKey = [method, 'config', actor, generation, parent.revision]
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
      if (!authority()) throw new Error('Identity configuration unavailable')
      const result = await api.getConfig(signal)
      if (signal.aborted || !authority()) throw new Error('Identity configuration unavailable')
      return result
    },
  })
  const resource = useApprovalCacheRevision([queryKey])
  function readable() {
    const state = cache.getQueryState<GitHubConfig>(queryKey)
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
  const [open, setOpen] = useState(false)
  const [review, setReview] = useState<GitHubConfig | null>(null)
  const [draft, setDraft] = useState<GitHubConfigInput>({
    name: '',
    client_id: '',
    callback_url: '',
    secret_action: 'keep',
    client_secret: '',
    reason: '',
  })
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [proofReason, setProofReason] = useState('')
  const [statusReason, setStatusReason] = useState('')
  const [confirm, setConfirm] = useState<Intent | null>(null)
  const confirmationOwner = useRef<Intent | null>(null)
  function confirmIntent(next: Intent) {
    confirmationOwner.current = next
    setConfirm(next)
  }
  const [intent, setIntent] = useState<Intent | null>(null)
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState('')
  const live = useRef({ mounted: true, turn: 0, locked: false, revoked: false, submitted: false })
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    const owner = live.current
    owner.mounted = true
    const expire = () => {
      owner.revoked = true
      owner.turn++
      owner.locked = false
      request.current?.abort()
      setExpired(true)
      setIntent(null)
      confirmationOwner.current = null
      setConfirm(null)
      setPassword('')
      setCode('')
      setDraft((value) => ({ ...value, client_secret: '' }))
      setOpen(false)
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
        (key.length === 2 &&
          ((key[0] === 'auth' && key[1] === 'session') ||
            (key[0] === 'permissions' && key[1] === actor))) ||
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
      setConfirm(null)
      setPassword('')
      setCode('')
      setDraft((value) => ({ ...value, client_secret: '' }))
      if (owner.submitted) setIssue('outcomeUnknown')
    })
  }, [actor, cache, authoritySnapshot, resourceSnapshot, resourceKey])
  const stale = !!page && !!review && page.review_etag !== review.review_etag
  const securityChange =
    !!review &&
    (draft.client_id !== review.client_id ||
      draft.callback_url !== review.callback_url ||
      draft.secret_action === 'replace')
  function edit<K extends keyof GitHubConfigInput>(key: K, value: GitHubConfigInput[K]) {
    if (live.current.locked || live.current.submitted || intent || !readable()) return
    setDraft({ ...draft, [key]: value })
    confirmationOwner.current = null
    setConfirm(null)
  }
  function prepare(kind: Intent['kind']) {
    if (
      !page ||
      !review ||
      !readable() ||
      live.current.locked ||
      live.current.submitted ||
      intent ||
      stale
    )
      return
    const etag = review.review_etag
    if (kind === 'save') {
      if (
        !api.validConfig(draft) ||
        (draft.secret_action === 'keep' && !review.secret_configured)
      ) {
        setIssue('invalid')
        return
      }
      confirmIntent({ kind, etag, body: { ...draft } })
    } else if (kind === 'status') {
      if (!validApprovalReason(statusReason) || (!review.enabled && !review.verified)) {
        setIssue('invalid')
        return
      }
      confirmIntent({ kind, etag, body: { enabled: !review.enabled, reason: statusReason } })
    } else {
      if (review.enabled) {
        setIssue('disableBeforeVerify')
        return
      }
      const body: GitHubLocalProof = {
        password,
        proof: review.mfa_required ? (recovery ? { recovery_code: code.trim() } : { code }) : {},
        reason: proofReason,
      }
      if (!api.validProof(body, review.mfa_required)) {
        setIssue('invalid')
        return
      }
      confirmIntent({ kind, etag, body, mfa: review.mfa_required })
    }
    setIssue('')
  }
  async function dispatch(retry = false) {
    const owner = live.current
    const captured = retry ? intent : confirm
    if (
      !owner.mounted ||
      owner.locked ||
      owner.revoked ||
      !readable() ||
      !captured ||
      (!retry &&
        (confirmationOwner.current !== captured ||
          owner.submitted ||
          intent ||
          stale ||
          captured.etag !== page?.review_etag)) ||
      (retry && captured.kind === 'verify')
    )
      return
    const session = cache.getQueryData<Session>(['auth', 'session'])
    if (!session?.csrf_token) return
    owner.locked = true
    owner.submitted = true
    const turn = ++owner.turn
    setBusy(true)
    setIssue('')
    setIntent(captured)
    confirmationOwner.current = null
    setConfirm(null)
    setPassword('')
    setCode('')
    setDraft((value) => ({ ...value, client_secret: '' }))
    request.current = new AbortController()
    const current = () => owner.mounted && !owner.revoked && owner.turn === turn && readable()
    let settlingSession = false
    try {
      if (captured.kind === 'verify') {
        const url = await api.beginProof(
          'verify',
          captured.etag,
          captured.body,
          captured.mfa,
          session.csrf_token,
          request.current.signal,
        )
        if (current()) window.location.assign(url)
      } else {
        const result =
          captured.kind === 'save'
            ? await api.saveConfig(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
            : await api.setStatus(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
        if (!current()) return
        settlingSession = true
        const refreshed = await api.readSession(request.current.signal)
        if (!current()) return
        if (refreshed && refreshed.user.id !== actor) {
          void cache.invalidateQueries({ queryKey: sessionKey })
          void cache.invalidateQueries({ queryKey: ['permissions', actor] })
          return
        }
        const renewed =
          !!refreshed &&
          (refreshed.csrf_token !== session.csrf_token ||
            refreshed.user.role !== session.user.role ||
            refreshed.user.name !== session.user.name ||
            refreshed.user.email !== session.user.email)
        if (!refreshed || renewed) {
          await cache.cancelQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
          if (!current()) return
          owner.submitted = false
          owner.locked = false
          request.current = null
          setIntent(null)
          confirmationOwner.current = null
          setConfirm(null)
          setReview(null)
          setOpen(false)
          setIssue('')
          setPassword('')
          setCode('')
          setBusy(false)
          cache.removeQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
          cache.getMutationCache().clear()
          cache.setQueryData(setupKey, { initialized: true })
          cache.setQueryData(sessionKey, refreshed)
          if (!refreshed) navigate('/login', { replace: true })
          return
        }
        owner.submitted = false
        owner.locked = false
        request.current = null
        setIntent(null)
        setReview(result)
        setIssue('saved')
        setPassword('')
        setCode('')
        setDraft({
          name: result.name,
          client_id: result.client_id,
          callback_url: result.callback_url,
          secret_action: 'keep',
          client_secret: '',
          reason: '',
        })
        void cache.invalidateQueries({ queryKey: [method] })
      }
    } catch (error) {
      if (owner.mounted && owner.turn === turn) {
        setIssue('outcomeUnknown')
        if (
          settlingSession ||
          (error instanceof api.RequestError && [401, 403].includes(error.status))
        ) {
          void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
          void cache.invalidateQueries({ queryKey: ['permissions', actor] })
        }
      }
    } finally {
      if (owner.mounted && owner.turn === turn) {
        owner.locked = false
        setBusy(false)
      }
    }
  }
  async function copySetup(value: string, failed: 'copyFailed' | 'originCopyFailed') {
    const owner = live.current,
      turn = owner.turn
    if (owner.locked || !readable()) return
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      if (owner.mounted && owner.turn === turn && readable()) setIssue(failed)
    }
  }
  function close() {
    if (live.current.locked || live.current.submitted || intent) return
    setOpen(false)
    confirmationOwner.current = null
    setConfirm(null)
    setPassword('')
    setCode('')
    setDraft((value) => ({ ...value, client_secret: '' }))
  }
  async function abandonIntent() {
    const owner = live.current
    if (!owner.mounted || owner.locked || !readable() || !intent) return
    if (intent?.kind === 'verify') {
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
    setConfirm(null)
    confirmationOwner.current = null
    setPassword('')
    setCode('')
    setDraft((value) => ({ ...value, client_secret: '' }))
    setIssue('abandoned')
    void query.refetch()
  }
  return (
    <>
      <QueryState
        pending={authority() && query.isFetching}
        error={authority() ? query.error : null}
        retry={() => void query.refetch()}
      />
      {page && (
        <Card>
          <CardContent className="flex items-center justify-between gap-6 p-4">
            <div className="space-y-2">
              <p className="font-medium">
                {page.name || t('title')}{' '}
                <Badge variant="outline">{t(page.enabled ? 'enabled' : 'disabled')}</Badge>
              </p>
              <p className="text-sm text-muted-foreground">{t('description')}</p>
              <Badge variant="outline">{t(page.verified ? 'verified' : 'notVerified')}</Badge>
            </div>
            <Button
              variant="outline"
              onClick={() => {
                if (!readable() || live.current.locked) return
                if (!live.current.submitted && !intent) {
                  setReview(page)
                  setDraft({
                    name: page.name,
                    client_id: page.client_id,
                    callback_url: page.callback_url,
                    secret_action: 'keep',
                    client_secret: '',
                    reason: '',
                  })
                  setIssue('')
                }
                setOpen(true)
              }}
            >
              {t('configure')}
            </Button>
          </CardContent>
        </Card>
      )}
      <Drawer
        open={open && !!page}
        onOpenChange={(value) => {
          if (!value) close()
        }}
        title={t('configure')}
        description={t('description')}
        busy={busy}
      >
        <div className="space-y-6">
          {(method === 'google' || method === 'discord') && (
            <section className="space-y-3 rounded-lg border p-4" aria-label={t('setupGuide')}>
              <h2 className="font-semibold">{t('setupGuide')}</h2>
              <p className="text-sm text-muted-foreground">{t('setupHelp')}</p>
              <FormField label={t('origin')}>
                <Input value={window.location.origin} readOnly />
              </FormField>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  if (!live.current.locked && readable())
                    void copySetup(window.location.origin, 'originCopyFailed')
                }}
              >
                {t('copyOrigin')}
              </Button>
              <p className="text-sm text-muted-foreground">{t('callbackHelp')}</p>
            </section>
          )}
          {issue && <p role={issue === 'saved' ? 'status' : 'alert'}>{t(issue)}</p>}
          {stale && <p role="alert">{t('conflict')}</p>}
          {intent && (
            <div className="space-y-3">
              <p>{t('originalUnknown')}</p>
              {intent.kind !== 'verify' && (
                <Button disabled={busy} onClick={() => void dispatch(true)}>
                  {t('retryOriginal')}
                </Button>
              )}
              <Button variant="outline" disabled={busy} onClick={() => void abandonIntent()}>
                {t('abandon')}
              </Button>
            </div>
          )}
          {(stale || !review || intent) && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                if (!live.current.locked && readable()) void query.refetch()
              }}
            >
              {t('reviewCurrent')}
            </Button>
          )}
          {(!review || stale) && !intent && (
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                if (live.current.locked || !readable() || !page) return
                setReview(page)
                setDraft({
                  name: page.name,
                  client_id: page.client_id,
                  callback_url: page.callback_url,
                  secret_action: 'keep',
                  client_secret: '',
                  reason: '',
                })
                confirmationOwner.current = null
                setConfirm(null)
                setIssue('')
              }}
            >
              {t('acceptReview')}
            </Button>
          )}
          <form
            className="space-y-5"
            aria-label={t('configure')}
            onSubmit={(event) => {
              event.preventDefault()
              prepare('save')
            }}
          >
            <fieldset className="space-y-5" disabled={busy || !!intent || stale || !review}>
              <FormField label={t('name')}>
                <Input value={draft.name} onValueChange={(value) => edit('name', value)} required />
              </FormField>
              <FormField label={t('clientID')}>
                <Input
                  value={draft.client_id}
                  onValueChange={(value) => edit('client_id', value)}
                  required
                />
              </FormField>
              <FormField label={t('callback')}>
                <Input
                  type="url"
                  value={draft.callback_url}
                  onValueChange={(value) => edit('callback_url', value)}
                  required
                />
              </FormField>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  if (!live.current.locked && readable() && page?.callback_url)
                    void copySetup(page.callback_url, 'copyFailed')
                }}
              >
                {t('copyCallback')}
              </Button>
              <FormField label={t('clientSecret')}>
                <Input
                  type="password"
                  autoComplete="new-password"
                  value={draft.client_secret}
                  onValueChange={(value) => {
                    if (live.current.locked || live.current.submitted || intent || !readable())
                      return
                    setDraft({
                      ...draft,
                      client_secret: value,
                      secret_action: value ? 'replace' : 'keep',
                    })
                    confirmationOwner.current = null
                    setConfirm(null)
                  }}
                />
              </FormField>
              <p className="text-sm text-muted-foreground">{t('secretHelp')}</p>
              <FormField label={t('reason')}>
                <Input
                  value={draft.reason}
                  onValueChange={(value) => edit('reason', value)}
                  required
                />
              </FormField>
              <p className="text-sm text-muted-foreground">
                {t(securityChange ? 'securityChange' : 'nameChange')}
              </p>
            </fieldset>
            <Button disabled={busy || !!intent || stale || !review} type="submit">
              {t('save')}
            </Button>
          </form>
          <section className="space-y-4">
            <h2 className="font-semibold">{t('verify')}</h2>
            <p className="text-sm text-muted-foreground">{t('verifyHelp')}</p>
            <GitHubProofFields
              method={method}
              required={review?.mfa_required === true}
              password={password}
              code={code}
              recovery={recovery}
              reason={proofReason}
              disabled={busy || !!intent || stale || !review}
              setPassword={setPassword}
              setCode={setCode}
              setRecovery={setRecovery}
              setReason={setProofReason}
            />
            <Button
              disabled={busy || !!intent || stale || !review || review.enabled}
              onClick={() => prepare('verify')}
            >
              {t('verify')}
            </Button>
          </section>
          <section className="space-y-4">
            <h2 className="font-semibold">{t('availability')}</h2>
            <p className="text-sm text-muted-foreground">{t('enableHelp')}</p>
            <FormField label={t('reason')}>
              <Input
                value={statusReason}
                onValueChange={setStatusReason}
                disabled={busy || !!intent || stale || !review}
              />
            </FormField>
            {method === 'google' || method === 'discord' ? (
              <div className="flex items-center justify-between gap-6">
                <p className="font-medium">{t('switchLabel')}</p>
                <Switch
                  aria-label={t('switchLabel')}
                  checked={review?.enabled === true}
                  disabled={
                    busy || !!intent || stale || !review || (!review.enabled && !review.verified)
                  }
                  onCheckedChange={(enabled) => {
                    if (enabled !== review?.enabled) prepare('status')
                  }}
                />
              </div>
            ) : (
              <Button
                disabled={
                  busy || !!intent || stale || !review || (!review.enabled && !review.verified)
                }
                onClick={() => prepare('status')}
              >
                {t(review?.enabled ? 'disable' : 'enable')}
              </Button>
            )}
          </section>
          <p className="text-sm text-muted-foreground">{t('unsupported')}</p>
        </div>
      </Drawer>
      <Dialog
        open={!!confirm && !!page && !intent}
        onOpenChange={(value) => {
          if (!value && !live.current.locked) {
            confirmationOwner.current = null
            setConfirm(null)
          }
        }}
        title={t('confirmTitle')}
        description={t(
          confirm?.kind === 'verify'
            ? 'verifyHelp'
            : confirm?.kind === 'status'
              ? 'statusHelp'
              : securityChange
                ? 'securityChange'
                : 'nameChange',
        )}
        busy={busy}
      >
        <div className="space-y-4">
          <p>{confirm?.body.reason}</p>
          <Button disabled={busy || stale} onClick={() => void dispatch()}>
            {t('confirm')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
