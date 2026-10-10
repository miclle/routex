import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import {
  getOAuthConfig,
  saveOAuthConfig,
  setOAuthStatus,
  beginOAuthProof,
  validOAuthConfig,
  copyOAuthInput,
  validOAuthProof,
  readOAuthSession,
  OAuthRequestError,
} from '@/api/oauth'
import { validApprovalReason } from '@/api/registration-approval'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import {
  useApprovalCacheRevision,
  approvalActorCurrent,
} from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'
import type { OAuthConfig, OAuthConfigInput, OAuthLocalProof } from '@/types/oauth'
import { OAuthProofFields } from './account'
import { Card, CardContent } from '@/components/ui/card'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Intent =
  | { kind: 'save'; etag: string; body: OAuthConfigInput }
  | { kind: 'status'; etag: string; body: { enabled: boolean; reason: string } }
  | { kind: 'verify'; etag: string; body: OAuthLocalProof; mfa: boolean }
export default function OAuthConfiguration() {
  const session = useSession()
  usePermissions()
  return <Configuration key={session.data?.user.id ?? ''} actor={session.data?.user.id ?? ''} />
}
function Configuration({ actor }: { actor: string }) {
  const { t } = useTranslation('oauth')
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
  const queryKey = ['oauth', 'config', actor, generation, parent.revision]
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
      if (!authority()) throw new Error('OAuth configuration unavailable')
      const result = await getOAuthConfig(signal)
      if (signal.aborted || !authority()) throw new Error('OAuth configuration unavailable')
      return result
    },
  })
  const resource = useApprovalCacheRevision([queryKey])
  function readable() {
    const state = cache.getQueryState<OAuthConfig>(queryKey)
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
  const [review, setReview] = useState<OAuthConfig | null>(null)
  const [draft, setDraft] = useState<OAuthConfigInput>({
    name: '',
    authorization_url: '',
    token_url: '',
    user_info_url: '',
    client_auth_method: '',
    scopes: [],
    subject_path: [],
    client_id: '',
    callback_url: '',
    secret_action: 'keep',
    client_secret: '',
    reason: '',
  })
  const [scopeText, setScopeText] = useState('')
  const [pathText, setPathText] = useState('[]')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [proofReason, setProofReason] = useState('')
  const [statusReason, setStatusReason] = useState('')
  const [confirm, setConfirm] = useState<Intent | null>(null)
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
      owner.turn++
      request.current?.abort()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [])
  const authoritySnapshot = parent.snapshot
  useLayoutEffect(() => {
    let previous = authoritySnapshot()
    return cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const key = event.query.queryKey
      const relevant =
        key.length === 2 &&
        ((key[0] === 'auth' && key[1] === 'session') ||
          (key[0] === 'permissions' && key[1] === actor))
      if (!relevant) return
      const next = authoritySnapshot()
      if (next === previous) return
      previous = next
      const owner = live.current
      owner.turn++
      request.current?.abort()
      owner.locked = false
      setBusy(false)
      setConfirm(null)
      if (owner.submitted) setIssue('outcomeUnknown')
    })
  }, [actor, cache, authoritySnapshot])
  const stale = !!page && !!review && page.review_etag !== review.review_etag
  const securityChange =
    !!review &&
    (draft.authorization_url !== review.authorization_url ||
      draft.token_url !== review.token_url ||
      draft.user_info_url !== review.user_info_url ||
      draft.client_auth_method !== review.client_auth_method ||
      JSON.stringify(draft.scopes) !== JSON.stringify(review.scopes) ||
      JSON.stringify(draft.subject_path) !== JSON.stringify(review.subject_path) ||
      draft.client_id !== review.client_id ||
      draft.callback_url !== review.callback_url ||
      draft.secret_action === 'replace')
  function edit<K extends keyof OAuthConfigInput>(key: K, value: OAuthConfigInput[K]) {
    if (live.current.locked || live.current.submitted || intent || !readable()) return
    setDraft({ ...draft, [key]: value })
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
        !validOAuthConfig(draft) ||
        (draft.secret_action === 'keep' && !review.secret_configured)
      ) {
        setIssue('invalid')
        return
      }
      setConfirm({ kind, etag, body: copyOAuthInput(draft) })
    } else if (kind === 'status') {
      if (!validApprovalReason(statusReason) || (!review.enabled && !review.verified)) {
        setIssue('invalid')
        return
      }
      setConfirm({ kind, etag, body: { enabled: !review.enabled, reason: statusReason } })
    } else {
      if (review.enabled) {
        setIssue('disableBeforeVerify')
        return
      }
      const body: OAuthLocalProof = {
        password,
        proof: review.mfa_required ? (recovery ? { recovery_code: code.trim() } : { code }) : {},
        reason: proofReason,
      }
      if (!validOAuthProof(body, review.mfa_required)) {
        setIssue('invalid')
        return
      }
      setConfirm({ kind, etag, body, mfa: review.mfa_required })
    }
    setIssue('')
  }
  async function dispatch(retry = false) {
    const owner = live.current
    const captured = retry ? intent : confirm
    if (
      owner.locked ||
      owner.revoked ||
      !readable() ||
      !captured ||
      (!retry && (owner.submitted || intent || stale || captured.etag !== page?.review_etag)) ||
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
    setConfirm(null)
    setPassword('')
    setCode('')
    setDraft((value) => ({ ...value, client_secret: '' }))
    request.current = new AbortController()
    const current = () => owner.mounted && !owner.revoked && owner.turn === turn && readable()
    let settlingSession = false
    try {
      if (captured.kind === 'verify') {
        const url = await beginOAuthProof(
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
            ? await saveOAuthConfig(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
            : await setOAuthStatus(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
        if (!current()) return
        settlingSession = true
        const refreshed = await readOAuthSession(request.current.signal)
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
        setScopeText(result.scopes.join(' '))
        setPathText(JSON.stringify(result.subject_path))
        setReview(result)
        setIssue('saved')
        setPassword('')
        setCode('')
        setDraft({
          name: result.name,
          authorization_url: result.authorization_url,
          token_url: result.token_url,
          user_info_url: result.user_info_url,
          client_auth_method: result.client_auth_method,
          scopes: [...result.scopes],
          subject_path: [...result.subject_path],
          client_id: result.client_id,
          callback_url: result.callback_url,
          secret_action: 'keep',
          client_secret: '',
          reason: '',
        })
        void cache.invalidateQueries({ queryKey: ['oauth'] })
      }
    } catch (error) {
      if (owner.mounted && owner.turn === turn) {
        setIssue('outcomeUnknown')
        if (
          settlingSession ||
          (error instanceof OAuthRequestError && [401, 403].includes(error.status))
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
    setConfirm(null)
    setPassword('')
    setCode('')
    setDraft((value) => ({ ...value, client_secret: '' }))
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
                  setScopeText(page.scopes.join(' '))
                  setPathText(JSON.stringify(page.subject_path))
                  setReview(page)
                  setDraft({
                    name: page.name,
                    authorization_url: page.authorization_url,
                    token_url: page.token_url,
                    user_info_url: page.user_info_url,
                    client_auth_method: page.client_auth_method,
                    scopes: [...page.scopes],
                    subject_path: [...page.subject_path],
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
                  setConfirm(null)
                  setIssue('abandoned')
                  setPassword('')
                  setCode('')
                  setDraft((value) => ({ ...value, client_secret: '' }))
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
                setScopeText(page.scopes.join(' '))
                setPathText(JSON.stringify(page.subject_path))
                setReview(page)
                setDraft({
                  name: page.name,
                  authorization_url: page.authorization_url,
                  token_url: page.token_url,
                  user_info_url: page.user_info_url,
                  client_auth_method: page.client_auth_method,
                  scopes: [...page.scopes],
                  subject_path: [...page.subject_path],
                  client_id: page.client_id,
                  callback_url: page.callback_url,
                  secret_action: 'keep',
                  client_secret: '',
                  reason: '',
                })
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
                    void navigator.clipboard
                      .writeText(page.callback_url)
                      .catch(() => setIssue('copyFailed'))
                }}
              >
                {t('copyCallback')}
              </Button>
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
                    setConfirm(null)
                  }}
                />
              </FormField>
              <p className="text-sm text-muted-foreground">{t('secretHelp')}</p>
              {(['authorization_url', 'token_url', 'user_info_url'] as const).map((key) => (
                <FormField key={key} label={t(key)}>
                  <Input
                    type="url"
                    value={draft[key]}
                    onValueChange={(value) => edit(key, value)}
                    required
                  />
                </FormField>
              ))}
              <FormField label={t('clientAuthMethod')}>
                <select
                  className="h-11 w-full rounded-md border border-input bg-background px-3 text-sm"
                  value={draft.client_auth_method}
                  onChange={(event) =>
                    edit(
                      'client_auth_method',
                      event.target.value as OAuthConfigInput['client_auth_method'],
                    )
                  }
                  required
                >
                  <option value="" disabled>
                    {t('chooseAuthMethod')}
                  </option>
                  <option value="client_secret_basic">{t('basicAuth')}</option>
                  <option value="client_secret_post">{t('postAuth')}</option>
                </select>
              </FormField>
              <FormField label={t('scopes')}>
                <Input
                  aria-describedby="oauth-scopes-help"
                  value={scopeText}
                  onValueChange={(value) => {
                    if (live.current.locked || live.current.submitted || intent || !readable())
                      return
                    setScopeText(value)
                    edit('scopes', value.trim() ? value.trim().split(/\s+/u) : [])
                  }}
                />
              </FormField>
              <p id="oauth-scopes-help" className="text-sm text-muted-foreground">
                {t('scopesHelp')}
              </p>
              <FormField label={t('subjectPath')}>
                <Input
                  aria-describedby="oauth-subject-path-help"
                  value={pathText}
                  onValueChange={(value) => {
                    if (live.current.locked || live.current.submitted || intent || !readable())
                      return
                    setPathText(value)
                    let path: unknown
                    try {
                      path = JSON.parse(value)
                    } catch {
                      path = null
                    }
                    edit(
                      'subject_path',
                      Array.isArray(path) && path.every((part) => typeof part === 'string')
                        ? path
                        : [],
                    )
                  }}
                  required
                />
              </FormField>
              <p id="oauth-subject-path-help" className="text-sm text-muted-foreground">
                {t('subjectPathHelp')}
              </p>
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
            <OAuthProofFields
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
          if (!value && !live.current.locked) setConfirm(null)
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
