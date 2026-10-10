import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import {
  getSAMLConfig,
  saveSAMLConfig,
  setSAMLStatus,
  beginSAMLProof,
  validSAMLConfig,
  copySAMLInput,
  sameSAMLCertificate,
  validSAMLProof,
  readSAMLSession,
  SAMLRequestError,
} from '@/api/saml'
import { validApprovalReason } from '@/api/registration-approval'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import {
  useApprovalCacheRevision,
  approvalActorCurrent,
} from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'
import type { SAMLConfig, SAMLConfigInput, SAMLProofInput } from '@/types/saml'
import { SAMLProofFields } from './account'
import { Card, CardContent } from '@/components/ui/card'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Intent =
  | { kind: 'save'; etag: string; body: SAMLConfigInput }
  | { kind: 'status'; etag: string; body: { enabled: boolean; reason: string } }
  | { kind: 'verify'; etag: string; body: SAMLProofInput; mfa: boolean }
export default function SAMLConfiguration() {
  const session = useSession()
  usePermissions()
  return <Configuration key={session.data?.user.id ?? ''} actor={session.data?.user.id ?? ''} />
}
function Configuration({ actor }: { actor: string }) {
  const { t } = useTranslation('saml')
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
  const queryKey = ['saml', 'config', actor, generation, parent.revision]
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
      if (!authority()) throw new Error('SAML configuration unavailable')
      const result = await getSAMLConfig(signal)
      if (signal.aborted || !authority()) throw new Error('SAML configuration unavailable')
      return result
    },
  })
  const resource = useApprovalCacheRevision([queryKey])
  function readable() {
    const state = cache.getQueryState<SAMLConfig>(queryKey)
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
  const [review, setReview] = useState<SAMLConfig | null>(null)
  const [draft, setDraft] = useState<SAMLConfigInput>({
    name: '',
    idp_issuer: '',
    sso_url: '',
    sp_entity_id: '',
    acs_url: '',
    signing_certificate_pem: '',
    reason: '',
  })
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
      setCode('')
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
      if (owner.submitted) setIssue('outcomeUnknown')
    })
  }, [actor, cache, authoritySnapshot, resourceSnapshot, resourceKey])
  const stale = !!page && !!review && page.review_etag !== review.review_etag
  const securityChange =
    !!review &&
    (draft.idp_issuer !== review.idp_issuer ||
      draft.sso_url !== review.sso_url ||
      draft.sp_entity_id !== review.sp_entity_id ||
      draft.acs_url !== review.acs_url ||
      !sameSAMLCertificate(draft.signing_certificate_pem, review.signing_certificate_pem))

  function edit<K extends keyof SAMLConfigInput>(key: K, value: SAMLConfigInput[K]) {
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
      if (!validSAMLConfig(draft)) {
        setIssue('invalid')
        return
      }
      const next: Intent = { kind, etag, body: copySAMLInput(draft) }
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
      const body: SAMLProofInput = {
        password,
        proof: review.mfa_required ? (recovery ? { recovery_code: code.trim() } : { code }) : {},
        reason: proofReason,
      }
      if (!validSAMLProof(body, review.mfa_required)) {
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
    setCode('')
    request.current = new AbortController()
    const current = () => owner.mounted && !owner.revoked && owner.turn === turn && readable()
    let settlingSession = false
    try {
      let result: SAMLConfig | undefined
      if (captured.kind === 'verify') {
        const url = await beginSAMLProof(
          'verify',
          captured.etag,
          captured.body,
          captured.mfa,
          session.csrf_token,
          request.current.signal,
        )
        if (current()) window.location.assign(url)
        return
      } else {
        result =
          captured.kind === 'save'
            ? await saveSAMLConfig(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
            : await setSAMLStatus(
                captured.etag,
                captured.body,
                session.csrf_token,
                request.current.signal,
              )
      }
      if (!current()) return
      settlingSession = true
      const refreshed = await readSAMLSession(request.current.signal)
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
      setPassword('')
      setCode('')
      if (result) {
        setReview(result)
        setIssue('saved')
        setDraft({
          name: result.name,
          idp_issuer: result.idp_issuer,
          sso_url: result.sso_url,
          sp_entity_id: result.sp_entity_id,
          acs_url: result.acs_url,
          signing_certificate_pem: result.signing_certificate_pem,
          reason: '',
        })
      } else {
        setReview(null)
        setIssue('verifiedSaved')
      }
      void cache.invalidateQueries({ queryKey: ['saml'] })
    } catch (error) {
      if (owner.mounted && owner.turn === turn) {
        setIssue('outcomeUnknown')
        if (
          settlingSession ||
          (error instanceof SAMLRequestError && [401, 403].includes(error.status))
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
    setCode('')
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
                    idp_issuer: page.idp_issuer,
                    sso_url: page.sso_url,
                    sp_entity_id: page.sp_entity_id,
                    signing_certificate_pem: page.signing_certificate_pem,
                    acs_url: page.acs_url,
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
                  setCode('')
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
                  idp_issuer: page.idp_issuer,
                  sso_url: page.sso_url,
                  sp_entity_id: page.sp_entity_id,
                  signing_certificate_pem: page.signing_certificate_pem,
                  acs_url: page.acs_url,
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
              {(['name', 'idp_issuer', 'sso_url', 'sp_entity_id', 'acs_url'] as const).map(
                (key) => (
                  <FormField key={key} label={t(key)}>
                    <Input
                      value={draft[key]}
                      onValueChange={(value) => edit(key, value)}
                      required
                    />
                  </FormField>
                ),
              )}
              <FormField label={t('signing_certificate_pem')}>
                <Textarea
                  value={draft.signing_certificate_pem}
                  onChange={(event) => edit('signing_certificate_pem', event.target.value)}
                  required
                />
              </FormField>
              <p className="text-sm text-muted-foreground">{t('certificateHelp')}</p>
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
            <SAMLProofFields
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
