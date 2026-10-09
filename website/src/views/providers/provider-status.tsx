import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { getProviderStatus, saveProviderStatus } from '@/api/provider-status'
import { trimProviderMetadata, validProviderReason } from '@/api/provider-metadata'
import { getPermissions } from '@/api/governance'
import type { ProviderStatus } from '@/types/provider-status'
import type { ProviderStatusSubmittedIntent, SubmittedIntentClaim } from '@/types/uncertain-intents'
import type { Session } from '@/types/auth'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useConnectionQueryRevision } from './connection-authority'

interface Intent {
  payload: ProviderStatusSubmittedIntent
  claim: SubmittedIntentClaim
}
export default function ProviderStatusCard({ providerId }: { providerId: string }) {
  const session = useSession()
  return (
    <StatusEditor
      key={`${session.data?.user.id ?? ''}:${providerId}`}
      actor={session.data?.user.id ?? ''}
      providerId={providerId}
    />
  )
}
function StatusEditor({ actor, providerId }: { actor: string; providerId: string }) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient(),
    shared = useUncertainIntents()
  const sessionAuthority = useConnectionQueryRevision([sessionKey])
  const generation = cache.getQueryState(sessionKey)?.dataUpdateCount ?? 0
  const sessionFresh = () => {
    const state = cache.getQueryState<Session | null>(sessionKey)
    return (
      !!actor &&
      state?.data?.user.id === actor &&
      state.dataUpdateCount === generation &&
      state.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated &&
      sessionAuthority.snapshot() === sessionAuthority.revision
    )
  }
  const permissionKey = ['permissions', actor, 'provider-status', providerId, generation]
  const access = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: sessionFresh(),
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const authority = useConnectionQueryRevision([sessionKey, permissionKey])
  const readable = () => {
    const state = cache.getQueryState<string[]>(permissionKey)
    return (
      sessionFresh() &&
      authority.snapshot() === authority.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data?.includes('providers.read') === true
    )
  }
  const permissionGeneration = cache.getQueryState(permissionKey)?.dataUpdateCount ?? 0
  const key = ['admin', 'provider-status', actor, providerId, generation, permissionGeneration]
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getProviderStatus(providerId, signal),
    enabled: readable(),
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const resource = useConnectionQueryRevision([key])
  const [expired, setExpired] = useState(false)
  const fresh = () => {
    const state = cache.getQueryState<ProviderStatus>(key)
    return (
      !expired &&
      readable() &&
      resource.snapshot() === resource.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data &&
      state.data?.id === providerId
    )
  }
  const writable = () =>
    fresh() &&
    cache.getQueryData<string[]>(permissionKey)?.includes('providers.write') === true &&
    query.data?.can_edit === true &&
    !!cache.getQueryData<Session>(sessionKey)?.csrf_token
  const [review, setReview] = useState<ProviderStatus | null>(null)
  const [enabled, setEnabled] = useState(false),
    [reason, setReason] = useState('')
  const [intent, setIntent] = useState<Intent | null>(null),
    [notice, setNotice] = useState<string | null>(null)
  const [conflict, setConflict] = useState(false),
    [busy, setBusy] = useState(false)
  const [confirmation, setConfirmation] = useState<'save' | 'abandon' | null>(null)
  const dialog = useRef<'save' | 'abandon' | null>(null)
  const pending = useRef<{
    controller: AbortController
    claim: SubmittedIntentClaim
    authority: string
    resource: string
  } | null>(null)
  const mounted = useRef(true),
    actionButton = useRef<HTMLButtonElement>(null)
  const current = fresh()
  const recovered = current ? shared?.recover(actor) : null
  if (
    recovered?.kind === 'provider-status' &&
    recovered.payload.provider_id === providerId &&
    recovered.claim !== intent?.claim
  ) {
    setIntent({ payload: recovered.payload, claim: recovered.claim })
    setEnabled(recovered.payload.input.enabled)
    setReason(recovered.payload.input.reason)
    setNotice('providerStatus.uncertain')
  }
  const release = useEffectEvent(() => {
    const operation = pending.current
    if (
      operation &&
      (!fresh() ||
        authority.snapshot() !== operation.authority ||
        resource.snapshot() !== operation.resource ||
        !shared?.isCurrent(operation.claim))
    ) {
      operation.controller.abort()
      pending.current = null
      setBusy(false)
    }
  })
  useEffect(() => release(), [current, authority.revision, resource.revision, recovered?.claim])
  useEffect(() => {
    mounted.current = true
    const expire = () => {
      pending.current?.controller.abort()
      setExpired(true)
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      mounted.current = false
      pending.current?.controller.abort()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [])
  const sameIdentity = (etag: string) => etag.slice(0, 64) === query.data?.etag.slice(0, 64)
  const identityCurrent = !intent || sameIdentity(intent.payload.etag)
  const stale = conflict || !review || review.etag !== query.data?.etag
  const validReason = validProviderReason(trimProviderMetadata(reason))
  const canPrepare = () => writable() && !!shared && !intent && !busy && !stale && validReason
  function close() {
    if (!mounted.current || pending.current) return
    dialog.current = null
    setConfirmation(null)
  }
  function begin() {
    if (!mounted.current || !writable() || !shared || pending.current || busy) return
    const retained = shared.recover(actor)
    if (
      retained &&
      (retained.kind !== 'provider-status' || retained.payload.provider_id !== providerId)
    ) {
      setNotice('providerStatus.otherIntent')
      return
    }
    if (!retained && !review && query.data) {
      setReview(query.data)
      setEnabled(!query.data.enabled)
    }
    dialog.current = 'save'
    setConfirmation('save')
  }
  function reviewCurrent() {
    if (!mounted.current || !writable() || !query.data || pending.current || shared?.recover(actor))
      return
    if (review && !sameIdentity(review.etag)) {
      setEnabled(!query.data.enabled)
      setReason('')
    }
    setReview(query.data)
    setConflict(false)
    setNotice('providerStatus.reviewed')
  }
  function abandon() {
    if (
      !mounted.current ||
      !fresh() ||
      !query.data ||
      pending.current ||
      dialog.current !== 'abandon' ||
      !intent ||
      !shared?.isCurrent(intent.claim) ||
      !shared.clear(intent.claim)
    )
      return
    setIntent(null)
    setReview(query.data)
    setEnabled(!query.data.enabled)
    setReason('')
    setConflict(false)
    setNotice('providerStatus.abandoned')
    dialog.current = 'save'
    setConfirmation('save')
  }
  async function dispatch(retry: boolean) {
    if (!mounted.current || !writable() || !shared || pending.current || busy) return
    let captured = intent
    if (!retry) {
      if (dialog.current !== 'save' || !canPrepare() || !review || shared.recover(actor)) return
      const payload: ProviderStatusSubmittedIntent = {
        provider_id: providerId,
        etag: review.etag,
        input: { enabled, reason: trimProviderMetadata(reason) },
      }
      const claim = shared.capture(actor, { kind: 'provider-status', payload })
      if (!claim) {
        setNotice('providerStatus.otherIntent')
        return
      }
      captured = { payload, claim }
      setIntent(captured)
      setReason(payload.input.reason)
    }
    if (
      !captured ||
      !shared.isCurrent(captured.claim) ||
      captured.payload.provider_id !== providerId ||
      !sameIdentity(captured.payload.etag)
    )
      return
    const csrf = cache.getQueryData<Session>(sessionKey)?.csrf_token
    if (!csrf) return
    const operation = {
      controller: new AbortController(),
      claim: captured.claim,
      authority: authority.snapshot(),
      resource: resource.snapshot(),
    }
    pending.current = operation
    setBusy(true)
    setNotice('providerStatus.uncertain')
    const valid = () =>
      mounted.current &&
      pending.current === operation &&
      !operation.controller.signal.aborted &&
      authority.snapshot() === operation.authority &&
      resource.snapshot() === operation.resource &&
      writable() &&
      shared.isCurrent(operation.claim)
    try {
      await saveProviderStatus(
        providerId,
        captured.payload.etag,
        captured.payload.input,
        csrf,
        operation.controller.signal,
      )
      if (!valid() || !shared.clear(operation.claim)) return
      setIntent(null)
      setReview(null)
      setReason('')
      setConflict(false)
      setNotice('providerStatus.savedCurrent')
      dialog.current = null
      setConfirmation(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
      void cache.invalidateQueries({ queryKey: ['admin', 'provider-metadata', actor, providerId] })
      void cache.invalidateQueries({ queryKey: ['admin', 'provider-status', actor, providerId] })
    } catch (error) {
      if (!valid()) return
      if (
        !retry &&
        axios.isAxiosError(error) &&
        [409, 412].includes(error.response?.status ?? 0) &&
        shared.clear(operation.claim)
      ) {
        setIntent(null)
        setConflict(true)
        setNotice('providerStatus.stale')
        void query.refetch()
      } else setNotice('providerStatus.uncertain')
    } finally {
      if (mounted.current && pending.current === operation) {
        pending.current = null
        setBusy(false)
      }
    }
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('providerStatus.title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <QueryState
          pending={!sessionFresh() || access.isFetching || (readable() && query.isFetching)}
          error={access.error ?? query.error}
          retry={() => {
            if (!sessionFresh()) void cache.invalidateQueries({ queryKey: sessionKey, exact: true })
            else if (access.isError) void access.refetch()
            else void query.refetch()
          }}
        />
        {sessionFresh() && access.isSuccess && !access.isFetching && !readable() && (
          <p role="alert">{t('providerStatus.readDenied')}</p>
        )}
        {current && query.data && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-4">
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Badge variant={query.data.enabled ? 'success' : 'outline'}>
                    {t(query.data.enabled ? 'providers.enabled' : 'common.disabled')}
                  </Badge>
                  <span className="text-sm">
                    {t(
                      query.data.enabled
                        ? 'providerStatus.enabledDescription'
                        : 'providerStatus.disabledDescription',
                    )}
                  </span>
                </div>
                <p className="text-sm text-muted-foreground">{t('providerStatus.retained')}</p>
              </div>
              <Button
                ref={actionButton}
                variant={query.data.enabled ? 'outline' : 'default'}
                className={
                  query.data.enabled
                    ? 'border-destructive text-destructive hover:bg-destructive/10'
                    : undefined
                }
                disabled={!writable() || !shared || busy}
                onClick={begin}
              >
                {t(
                  intent
                    ? 'providerStatus.reviewIntent'
                    : query.data.enabled
                      ? 'providerStatus.disable'
                      : 'providerStatus.enable',
                )}
              </Button>
            </div>
            {!writable() && (
              <p className="text-sm text-muted-foreground">{t('providerStatus.readOnly')}</p>
            )}
            {notice && (
              <p role={intent ? 'alert' : 'status'}>
                {t(identityCurrent ? notice : 'providerStatus.identityChanged')}
              </p>
            )}
            {intent && (
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={!current || busy}
                  onClick={() => {
                    if (!fresh() || pending.current || !intent || !shared?.isCurrent(intent.claim))
                      return
                    dialog.current = 'abandon'
                    setConfirmation('abandon')
                    void query.refetch()
                  }}
                >
                  {t('providerStatus.abandon')}
                </Button>
                <Button
                  disabled={
                    !writable() || busy || !identityCurrent || !shared?.isCurrent(intent.claim)
                  }
                  onClick={() => void dispatch(true)}
                >
                  {t('providerStatus.retry')}
                </Button>
              </div>
            )}
          </>
        )}
        <Dialog
          open={current && confirmation === 'save'}
          onOpenChange={(open) => {
            if (!open) close()
          }}
          busy={busy}
          finalFocus={actionButton}
          title={t(enabled ? 'providerStatus.confirmEnable' : 'providerStatus.confirmDisable')}
          description={t(enabled ? 'providerStatus.enableHelp' : 'providerStatus.disableHelp', {
            name: query.data?.name ?? providerId,
          })}
        >
          {current && (
            <>
              <p className="mb-4 text-sm text-muted-foreground">{t('providerStatus.retained')}</p>
              <FormField label={t('providerStatus.reason')}>
                <Input
                  value={reason}
                  autoComplete="off"
                  disabled={!writable() || busy || !!intent}
                  onChange={(event) => setReason(event.target.value)}
                />
              </FormField>
              {!intent && !validReason && (
                <p className="mt-2 text-sm text-muted-foreground">
                  {t('providerStatus.validation')}
                </p>
              )}
              {!intent && stale && <p role="alert">{t('providerStatus.stale')}</p>}
              {intent && (
                <p role="alert">
                  {t(
                    identityCurrent ? 'providerStatus.uncertain' : 'providerStatus.identityChanged',
                  )}
                </p>
              )}
              {!writable() && <p role="alert">{t('providerStatus.readOnly')}</p>}
              <div className="mt-6 flex justify-end gap-2">
                <Button variant="outline" disabled={busy} onClick={close}>
                  {t('common.cancel')}
                </Button>
                {!intent && stale && (
                  <Button variant="outline" disabled={!writable() || busy} onClick={reviewCurrent}>
                    {t('providerStatus.review')}
                  </Button>
                )}
                <Button
                  disabled={
                    intent
                      ? !writable() || busy || !identityCurrent || !shared?.isCurrent(intent.claim)
                      : !canPrepare()
                  }
                  onClick={() => void dispatch(!!intent)}
                >
                  {t(
                    intent
                      ? 'providerStatus.retry'
                      : enabled
                        ? 'providerStatus.confirmEnableAction'
                        : 'providerStatus.confirmDisableAction',
                  )}
                </Button>
              </div>
            </>
          )}
        </Dialog>
        <Dialog
          open={current && confirmation === 'abandon'}
          onOpenChange={(open) => {
            if (!open) close()
          }}
          busy={busy}
          finalFocus={actionButton}
          title={t('providerStatus.abandonTitle')}
          description={t('providerStatus.abandonDescription')}
        >
          <p>
            {t('providerStatus.current', {
              name: query.data?.name,
              status: t(query.data?.enabled ? 'providers.enabled' : 'common.disabled'),
            })}
          </p>
          <div className="mt-6 flex justify-end gap-2">
            <Button variant="outline" disabled={busy} onClick={close}>
              {t('common.cancel')}
            </Button>
            <Button
              disabled={!current || busy || !intent || !shared?.isCurrent(intent.claim)}
              onClick={abandon}
            >
              {t('providerStatus.confirmAbandon')}
            </Button>
          </div>
        </Dialog>
      </CardContent>
    </Card>
  )
}
