import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import {
  getLDAPConfig,
  saveLDAPConfig,
  setLDAPStatus,
  submitLDAPProof,
  validLDAPConfig,
  copyLDAPInput,
  validLDAPBindingProof,
  readLDAPSession,
  LDAPRequestError,
} from '@/api/ldap'
import { validApprovalReason } from '@/api/registration-approval'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import {
  useApprovalCacheRevision,
  approvalActorCurrent,
} from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'
import type { LDAPConfig, LDAPConfigInput, LDAPBindingProof } from '@/types/ldap'
import { LDAPProofFields } from './account'
import { Card, CardContent } from '@/components/ui/card'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Intent =
  | { kind: 'save'; etag: string; body: LDAPConfigInput }
  | { kind: 'status'; etag: string; body: { enabled: boolean; reason: string } }
  | { kind: 'verify'; etag: string; body: LDAPBindingProof; mfa: boolean }
export default function LDAPConfiguration() {
  const session = useSession()
  usePermissions()
  return <Configuration key={session.data?.user.id ?? ''} actor={session.data?.user.id ?? ''} />
}
function Configuration({ actor }: { actor: string }) {
  const { t } = useTranslation('ldap')
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
  const queryKey = ['ldap', 'config', actor, generation, parent.revision]
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
      if (!authority()) throw new Error('LDAP configuration unavailable')
      const result = await getLDAPConfig(signal)
      if (signal.aborted || !authority()) throw new Error('LDAP configuration unavailable')
      return result
    },
  })
  const resource = useApprovalCacheRevision([queryKey])
  function readable() {
    const state = cache.getQueryState<LDAPConfig>(queryKey)
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
  const [review, setReview] = useState<LDAPConfig | null>(null)
  const [draft, setDraft] = useState<LDAPConfigInput>({
    name: '',
    endpoint: '',
    bind_dn: '',
    base_dn: '',
    identity_attribute: '',
    user_filter: '',
    secret_action: 'keep',
    bind_password: '',
    reason: '',
  })
  const [username, setUsername] = useState('')
  const [directoryPassword, setDirectoryPassword] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [proofReason, setProofReason] = useState('')
  const [statusReason, setStatusReason] = useState('')
  const [confirm, setConfirm] = useState<Intent | null>(null)
  const confirmationOwner = useRef<Intent | null>(null)
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
      setUsername('')
      setDirectoryPassword('')
      setCode('')
      setDraft((value) => ({ ...value, bind_password: '' }))
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
      setUsername('')
      setDirectoryPassword('')
      setCode('')
      setDraft((value) => ({ ...value, bind_password: '' }))
      if (owner.submitted) setIssue('outcomeUnknown')
    })
  }, [actor, cache, authoritySnapshot, resourceSnapshot, resourceKey])
  const stale = !!page && !!review && page.review_etag !== review.review_etag
  const securityChange =
    !!review &&
    (draft.endpoint !== review.endpoint ||
      draft.bind_dn !== review.bind_dn ||
      draft.base_dn !== review.base_dn ||
      draft.identity_attribute !== review.identity_attribute ||
      draft.user_filter !== review.user_filter ||
      draft.secret_action === 'replace')
  function edit<K extends keyof LDAPConfigInput>(key: K, value: LDAPConfigInput[K]) {
    if (live.current.locked || live.current.submitted || intent || !readable()) return
    setDraft({ ...draft, [key]: value })
    confirmationOwner.current = null
    setConfirm(null)
  }
  function prepare(kind: Intent['kind']) {
    if (
      !live.current.mounted ||
      live.current.revoked ||
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
        !validLDAPConfig(draft) ||
        (draft.secret_action === 'keep' && !review.secret_configured)
      ) {
        setIssue('invalid')
        return
      }
      const next: Intent = { kind, etag, body: copyLDAPInput(draft) }
      confirmationOwner.current = next
      setConfirm(next)
    } else if (kind === 'status') {
      if (!validApprovalReason(statusReason) || (!review.enabled && !review.verified)) {
        setIssue('invalid')
        return
      }
      const next: Intent = { kind, etag, body: { enabled: !review.enabled, reason: statusReason } }
      confirmationOwner.current = next
      setConfirm(next)
    } else {
      if (review.enabled) {
        setIssue('disableBeforeVerify')
        return
      }
      const body: LDAPBindingProof = {
        username,
        directory_password: directoryPassword,
        password,
        proof: review.mfa_required ? (recovery ? { recovery_code: code.trim() } : { code }) : {},
        reason: proofReason,
      }
      if (!validLDAPBindingProof(body, review.mfa_required)) {
        setIssue('invalid')
        return
      }
      const next: Intent = { kind, etag, body, mfa: review.mfa_required }
      confirmationOwner.current = next
      setConfirm(next)
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
    setUsername('')
    setDirectoryPassword('')
    setCode('')
    setDraft((value) => ({ ...value, bind_password: '' }))
    request.current = new AbortController()
    const current = () => owner.mounted && !owner.revoked && owner.turn === turn && readable()
    let settlingSession = false
    try {
      let result: LDAPConfig | undefined
      if (captured.kind === 'verify') {
        await submitLDAPProof(
          'verify',
          captured.etag,
          captured.body,
          captured.mfa,
          session.csrf_token,
          request.current.signal,
        )
      } else {
        result =
          captured.kind === 'save'
            ? await saveLDAPConfig(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
            : await setLDAPStatus(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
      }
      if (!current()) return
      settlingSession = true
      const refreshed = await readLDAPSession(request.current.signal)
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
        setUsername('')
        setDirectoryPassword('')
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
      setPassword('')
      setUsername('')
      setDirectoryPassword('')
      setCode('')
      if (result) {
        setReview(result)
        setIssue('saved')
        setDraft({
          name: result.name,
          endpoint: result.endpoint,
          bind_dn: result.bind_dn,
          base_dn: result.base_dn,
          user_filter: result.user_filter,
          identity_attribute: result.identity_attribute,
          secret_action: 'keep',
          bind_password: '',
          reason: '',
        })
      } else {
        setReview(null)
        setIssue('verifiedSaved')
      }
      void cache.invalidateQueries({ queryKey: ['ldap'] })
    } catch (error) {
      if (owner.mounted && owner.turn === turn) {
        setIssue('outcomeUnknown')
        if (
          settlingSession ||
          (error instanceof LDAPRequestError && [401, 403].includes(error.status))
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
  function close() {
    if (live.current.locked || live.current.submitted || intent) return
    setOpen(false)
    confirmationOwner.current = null
    setConfirm(null)
    setPassword('')
    setUsername('')
    setDirectoryPassword('')
    setCode('')
    setDraft((value) => ({ ...value, bind_password: '' }))
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
                    endpoint: page.endpoint,
                    bind_dn: page.bind_dn,
                    base_dn: page.base_dn,
                    identity_attribute: page.identity_attribute,
                    user_filter: page.user_filter,
                    secret_action: 'keep',
                    bind_password: '',
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
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  if (live.current.locked || !readable()) return
                  live.current.turn++
                  live.current.submitted = false
                  request.current?.abort()
                  setIntent(null)
                  confirmationOwner.current = null
                  setConfirm(null)
                  setIssue('abandoned')
                  setPassword('')
                  setUsername('')
                  setDirectoryPassword('')
                  setCode('')
                  setDraft((value) => ({ ...value, bind_password: '' }))
                  setReview(null)
                  void query.refetch()
                }}
              >
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
                  endpoint: page.endpoint,
                  bind_dn: page.bind_dn,
                  base_dn: page.base_dn,
                  identity_attribute: page.identity_attribute,
                  user_filter: page.user_filter,
                  secret_action: 'keep',
                  bind_password: '',
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
              {(['name', 'endpoint', 'bind_dn', 'base_dn', 'user_filter'] as const).map((key) => (
                <FormField key={key} label={t(key)}>
                  <Input value={draft[key]} onValueChange={(value) => edit(key, value)} required />
                </FormField>
              ))}
              <p className="text-sm text-muted-foreground">{t('endpointHelp')}</p>
              <FormField label={t('identityAttribute')}>
                <select
                  className="h-11 w-full rounded-md border border-input bg-background px-3 text-sm"
                  value={draft.identity_attribute}
                  onChange={(event) =>
                    edit(
                      'identity_attribute',
                      event.target.value as LDAPConfigInput['identity_attribute'],
                    )
                  }
                  required
                >
                  <option value="" disabled>
                    {t('chooseIdentityAttribute')}
                  </option>
                  <option value="entryUUID">{t('entryUUID')}</option>
                  <option value="objectGUID">{t('objectGUID')}</option>
                </select>
              </FormField>
              <p className="text-sm text-muted-foreground">{t('identityHelp')}</p>
              <FormField label={t('bindPassword')}>
                <Input
                  type="password"
                  autoComplete="new-password"
                  value={draft.bind_password}
                  onValueChange={(value) => {
                    if (live.current.locked || live.current.submitted || intent || !readable())
                      return
                    setDraft({
                      ...draft,
                      bind_password: value,
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
            <LDAPProofFields
              directory
              username={username}
              directoryPassword={directoryPassword}
              setUsername={setUsername}
              setDirectoryPassword={setDirectoryPassword}
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
            <Button
              disabled={
                busy || !!intent || stale || !review || (!review.enabled && !review.verified)
              }
              onClick={() => prepare('status')}
            >
              {t(review?.enabled ? 'disable' : 'enable')}
            </Button>
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
