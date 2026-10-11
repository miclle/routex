import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useSession } from '@/hooks/use-auth'
import {
  approvalActorCurrent,
  useApprovalCacheRevision,
} from '@/views/governance/approval-authority'
import {
  getAuthenticationPolicyReview,
  saveAuthenticationPolicy,
  AuthenticationPolicyError,
} from '@/api/auth-enforcement'
import { validApprovalReason } from '@/api/registration-approval'
import type {
  AuthenticationPolicyInput,
  AuthenticationPolicyReview,
  AuthenticationPolicyReceipt,
} from '@/types/auth-enforcement'
import { useReauthentication } from './reauthentication-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Drawer } from '@/components/ui/drawer'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { FormField } from '@/components/app/CatalogUI'
export default function AuthenticationEnforcement() {
  const session = useSession()
  return <Configuration key={session.data?.user.id ?? ''} actor={session.data?.user.id ?? ''} />
}
function Configuration({ actor }: { actor: string }) {
  const { t, i18n } = useTranslation('authEnforcement'),
    cache = useQueryClient(),
    reauthenticate = useReauthentication()
  const parent = useApprovalCacheRevision([
    ['auth', 'session'],
    ['permissions', actor],
  ])
  const authority = () =>
    parent.snapshot() === parent.revision &&
    approvalActorCurrent(cache, actor, 'registration.write', true)
  const [selection, setSelection] = useState('')
  const queryKey = ['auth-enforcement', 'policy', actor, parent.revision]
  const candidateKey = ['auth-enforcement', 'candidate', actor, parent.revision, selection]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Authentication policy authority unavailable')
      const v = await getAuthenticationPolicyReview(signal)
      if (signal.aborted || !authority())
        throw new Error('Authentication policy authority unavailable')
      return v
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const candidate = useQuery({
    queryKey: candidateKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Authentication policy authority unavailable')
      const v = await getAuthenticationPolicyReview(signal, selection)
      if (signal.aborted || !authority())
        throw new Error('Authentication policy authority unavailable')
      return v
    },
    enabled: authority() && /^usr_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(selection),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const resource = useApprovalCacheRevision([queryKey, candidateKey])
  const current = () => {
    const s = cache.getQueryState<AuthenticationPolicyReview>(queryKey)
    return (
      authority() &&
      resource.snapshot() === resource.revision &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.isInvalidated &&
      !s.error &&
      s.data === query.data
    )
  }
  const page = current() ? query.data : undefined
  const candidateCurrent = () => {
    const state = cache.getQueryState<AuthenticationPolicyReview>(candidateKey)
    return (
      current() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === candidate.data &&
      candidate.data?.review_emergency_admin?.id === selection
    )
  }

  const [open, setOpen] = useState(false),
    [confirm, setConfirm] = useState<symbol | null>(null),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState(''),
    [draft, setDraft] = useState<AuthenticationPolicyInput>({
      enforced: false,
      emergency_admin_id: '',
      reason: '',
    }),
    [review, setReview] = useState<AuthenticationPolicyReview | null>(null),
    [intent, setIntent] = useState<{ body: AuthenticationPolicyInput; etag: string } | null>(null)
  const [confirmed, setConfirmed] = useState<AuthenticationPolicyReceipt | null>(null)
  // Consent belongs to one review, independently of an uncertain original request.
  const confirmationOwner = useRef<symbol | null>(null)
  const invalidateConfirmation = useCallback(() => {
    confirmationOwner.current = null
    setConfirm(null)
  }, [setConfirm])
  const owner = useRef({
    mounted: true,
    turn: 0,
    locked: false,
    controller: null as AbortController | null,
    intent: false,
  })
  const activeView = draft.enforced ? (candidateCurrent() ? candidate.data : undefined) : page
  const scope = `${parent.revision}|${resource.revision}`
  useLayoutEffect(() => {
    const cell = owner.current
    cell.mounted = true
    return () => {
      confirmationOwner.current = null
      cell.mounted = false
      cell.turn++
      cell.controller?.abort()
      cell.locked = false
    }
  }, [])
  const watchedKeys = JSON.stringify([
    ['auth', 'session'],
    ['permissions', actor],
    queryKey,
    candidateKey,
  ])
  useLayoutEffect(() => {
    const watched = (JSON.parse(watchedKeys) as unknown[][]).map((v) => JSON.stringify(v))
    const fingerprint = () =>
      watched
        .map((k) => {
          const s = cache.getQueryState(JSON.parse(k))
          return `${s?.status}:${s?.fetchStatus}:${s?.isInvalidated}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
        })
        .join('|')
    let prior = fingerprint()
    return cache.getQueryCache().subscribe((event) => {
      if (
        (event.type !== 'updated' && event.type !== 'removed') ||
        !watched.includes(JSON.stringify(event.query.queryKey))
      )
        return
      const next = fingerprint()
      if (prior === next) return
      prior = next
      const cell = owner.current
      cell.turn++
      cell.controller?.abort()
      cell.locked = false
      setBusy(false)
      invalidateConfirmation()
      if (cell.intent) setNotice('uncertain')
    })
  }, [cache, watchedKeys, invalidateConfirmation])
  function release() {
    const cell = owner.current
    cell.locked = false
    cell.controller = null
    setBusy(false)
  }
  function requestConfirmation() {
    const identity = Symbol()
    confirmationOwner.current = identity
    setConfirm(identity)
  }
  async function dispatch(confirmation: symbol | null, retry = false) {
    const cell = owner.current
    if (
      cell.locked ||
      !cell.mounted ||
      !current() ||
      !page ||
      (!retry && (!confirmation || confirmationOwner.current !== confirmation || !activeView))
    )
      return
    const send = retry
      ? intent
      : review
        ? {
            body: {
              ...draft,
              reason: draft.reason.trim(),
              emergency_admin_id: draft.enforced ? draft.emergency_admin_id : '',
            },
            etag: review.review_etag,
          }
        : null
    if (send?.body.enforced && !candidateCurrent()) return
    if (
      !send ||
      !validApprovalReason(send.body.reason) ||
      (send.body.enforced &&
        !/^usr_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(send.body.emergency_admin_id))
    ) {
      setNotice('invalid')
      return
    }
    if (
      send.body.enforced &&
      (!activeView?.review_emergency_admin?.mfa_enabled ||
        !(activeView.oidc_available || activeView.ldap_available))
    )
      return
    if (
      !retry &&
      (review?.review_etag !== activeView?.review_etag ||
        (send.body.enforced &&
          activeView?.review_emergency_admin?.id !== send.body.emergency_admin_id))
    ) {
      setNotice('conflict')
      return
    }
    const auth = cache.getQueryData<import('@/types/auth').Session>(['auth', 'session'])
    if (!auth) return
    const turn = ++cell.turn,
      captured = scope
    cell.locked = true
    cell.intent = true
    cell.controller = new AbortController()
    setIntent(send)
    setBusy(true)
    setNotice('')
    invalidateConfirmation()
    try {
      const receipt = await saveAuthenticationPolicy(
        send.body,
        send.etag,
        auth.csrf_token,
        cell.controller.signal,
      )
      if (!cell.mounted || cell.turn !== turn || captured !== scope || !current()) return
      // Settle owned intent before own query/cache replacement can signal renewal.
      cell.intent = false
      setIntent(null)
      setReview(receipt)
      setConfirmed(receipt)
      release()
      setNotice(receipt.runtime_applied ? 'saved' : 'runtimePending')
      if (receipt.reauthentication_required) {
        reauthenticate([], receipt.runtime_applied)
        return
      }
      void cache.invalidateQueries({ queryKey: ['auth', 'enforcement'] })
      void cache.invalidateQueries({ queryKey: ['admin', 'registration'] })
      void query.refetch()
      if (send.body.enforced) void candidate.refetch()
    } catch (e) {
      if (!cell.mounted || cell.turn !== turn) return
      setNotice(
        e instanceof AuthenticationPolicyError && e.status === 409 ? 'conflict' : 'uncertain',
      )
    } finally {
      if (cell.mounted && cell.turn === turn) release()
    }
  }
  if (!authority()) return null
  return (
    <>
      <section className="flex items-center justify-between gap-6 rounded-lg border p-4">
        <div>
          <p className="flex items-center gap-2 font-medium">
            {t('title')}
            {page && <Badge variant="outline">{t(page.enforced ? 'enforced' : 'off')}</Badge>}
          </p>
          <p className="mt-2 text-sm text-muted-foreground">{t('description')}</p>
        </div>
        <Button
          variant="outline"
          disabled={!page || busy}
          onClick={() => {
            if (!page || !current()) return
            invalidateConfirmation()
            if (!intent) {
              setReview(page.enforced ? null : page)
              setSelection(page.emergency_admin_id ?? '')
              setDraft({
                enforced: page.enforced,
                emergency_admin_id: page.emergency_admin_id ?? '',
                reason: '',
              })
              setNotice('')
            }
            setOpen(true)
          }}
        >
          {t('configure')}
        </Button>
      </section>
      {!page && (
        <div role="alert">
          <p>{t('unavailable')}</p>
          <Button variant="outline" onClick={() => void query.refetch()}>
            {t('refresh')}
          </Button>
        </div>
      )}
      {notice &&
        page &&
        (!['saved', 'runtimePending'].includes(notice) ||
          confirmed?.review_etag === activeView?.review_etag) && (
          <p role={notice === 'saved' ? 'status' : 'alert'}>{t(notice)}</p>
        )}
      <Drawer
        open={open && !!page}
        onOpenChange={(value) => {
          if (!busy) {
            setOpen(value)
            invalidateConfirmation()
            if (!value && !intent) setDraft({ enforced: false, emergency_admin_id: '', reason: '' })
          }
        }}
        title={t('configure')}
        description={t('description')}
        busy={busy}
      >
        <form
          className="space-y-6"
          onSubmit={(e) => {
            e.preventDefault()
            if (
              !current() ||
              !activeView ||
              review?.review_etag !== activeView.review_etag ||
              intent ||
              busy ||
              (draft.enforced &&
                (!activeView.review_emergency_admin?.mfa_enabled ||
                  !(activeView.oidc_available || activeView.ldap_available)))
            )
              return
            if (!validApprovalReason(draft.reason.trim())) {
              setNotice('invalid')
              return
            }
            requestConfirmation()
          }}
        >
          <label className="flex items-center justify-between gap-4">
            <span>{t('enable')}</span>
            <Switch
              aria-label={t('enable')}
              checked={draft.enforced}
              disabled={busy || !!intent}
              onCheckedChange={(enforced) => {
                setDraft({ ...draft, enforced })
                setReview(null)
                invalidateConfirmation()
                if (!enforced) void query.refetch()
              }}
            />
          </label>
          {draft.enforced && (
            <FormField label={t('administrator')}>
              <Input
                aria-label={t('administrator')}
                value={draft.emergency_admin_id}
                disabled={busy || !!intent}
                maxLength={30}
                onChange={(e) => {
                  setDraft({ ...draft, emergency_admin_id: e.target.value })
                  setSelection(e.target.value)
                  setReview(null)
                  invalidateConfirmation()
                }}
              />
              <p className="text-sm text-muted-foreground">{t('administratorHelp')}</p>
            </FormField>
          )}
          <div className="space-y-2 text-sm">
            <p>
              {t('oidc')}: {t(page?.oidc_available ? 'available' : 'notAvailable')}
            </p>
            <p>
              {t('ldap')}: {t(page?.ldap_available ? 'available' : 'notAvailable')}
            </p>
          </div>
          <FormField label={t('reason')}>
            <Textarea
              aria-label={t('reason')}
              value={draft.reason}
              disabled={busy || !!intent}
              onChange={(e) => {
                invalidateConfirmation()
                setDraft({ ...draft, reason: e.target.value })
              }}
            />
          </FormField>
          {draft.enforced && activeView?.review_emergency_admin && (
            <div className="space-y-2 text-sm">
              <p className="break-all">{activeView.review_emergency_admin.id}</p>
              <p>
                {t('candidateBirth', {
                  time: new Date(activeView.review_emergency_admin.created_at).toLocaleString(
                    i18n.resolvedLanguage,
                  ),
                })}
              </p>
              <p>
                {t(activeView.review_emergency_admin.mfa_enabled ? 'available' : 'notAvailable')}
              </p>
            </div>
          )}
          {notice && <p role="alert">{t(notice)}</p>}
          {intent ? (
            <div className="flex flex-wrap gap-3">
              <Button
                type="button"
                disabled={busy || !current()}
                onClick={() => void dispatch(null, true)}
              >
                {t('retry')}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={busy || !current()}
                onClick={() => {
                  invalidateConfirmation()
                  owner.current.turn++
                  owner.current.controller?.abort()
                  owner.current.intent = false
                  setIntent(null)
                  setReview(page ?? null)
                  setNotice('conflict')
                }}
              >
                {t('abandon')}
              </Button>
            </div>
          ) : (
            <div className="flex justify-end gap-3">
              {review?.review_etag !== activeView?.review_etag && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={!activeView}
                  onClick={() => {
                    invalidateConfirmation()
                    setReview(activeView ?? null)
                    setNotice('')
                  }}
                >
                  {t('reviewCurrent')}
                </Button>
              )}
              <Button
                type="submit"
                disabled={
                  busy ||
                  !current() ||
                  !activeView ||
                  review?.review_etag !== activeView.review_etag
                }
              >
                {t('review')}
              </Button>
            </div>
          )}
        </form>
      </Drawer>
      <Dialog
        open={!!confirm && open && !!page}
        onOpenChange={(value) => {
          if (!value) invalidateConfirmation()
        }}
        title={t('confirmTitle')}
        description={t('confirmHelp')}
        busy={busy}
      >
        <div className="space-y-4">
          <p>{t(draft.enforced ? 'enforced' : 'off')}</p>
          {draft.enforced && <p className="break-all">{draft.emergency_admin_id}</p>}
          <p className="break-words">{draft.reason}</p>
          <Button disabled={busy || !current()} onClick={() => void dispatch(confirm)}>
            {t('confirm')}
          </Button>
          <Button variant="outline" disabled={busy} onClick={invalidateConfirmation}>
            {t('cancel')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
