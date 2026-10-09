import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import {
  getProviderMetadata,
  saveProviderMetadata,
  trimProviderMetadata,
  validProviderName,
  validProviderReason,
} from '@/api/provider-metadata'
import type { ProviderMetadata } from '@/types/provider-metadata'
import type { Session } from '@/types/auth'
import type { ProviderNameSubmittedIntent, SubmittedIntentClaim } from '@/types/uncertain-intents'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useConnectionQueryRevision } from './connection-authority'

interface Intent {
  payload: ProviderNameSubmittedIntent
  claim: SubmittedIntentClaim
}
export default function ProviderMetadataCard({ providerId }: { providerId: string }) {
  const session = useSession()
  return (
    <NameEditor
      key={`${session.data?.user.id ?? ''}:${providerId}`}
      actor={session.data?.user.id ?? ''}
      providerId={providerId}
    />
  )
}
function NameEditor({ actor, providerId }: { actor: string; providerId: string }) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient(),
    shared = useUncertainIntents(),
    access = usePermissions()
  const permissionKey = ['permissions', actor]
  const authority = useConnectionQueryRevision([sessionKey, permissionKey])
  const sessionGeneration = cache.getQueryState(sessionKey)?.dataUpdateCount ?? 0
  const permissionGeneration = cache.getQueryState(permissionKey)?.dataUpdateCount ?? 0
  const authFresh = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey),
      permissions = cache.getQueryState<string[]>(permissionKey)
    return (
      !!actor &&
      auth?.data?.user.id === actor &&
      auth.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.isInvalidated &&
      !auth.error &&
      permissions?.status === 'success' &&
      permissions.fetchStatus === 'idle' &&
      !permissions.isInvalidated &&
      !permissions.error &&
      permissions.data?.includes('providers.read') === true
    )
  }
  const key = [
    'admin',
    'provider-metadata',
    actor,
    providerId,
    sessionGeneration,
    permissionGeneration,
  ]
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getProviderMetadata(providerId, signal),
    enabled: authFresh(),
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const resource = useConnectionQueryRevision([key])
  const [expired, setExpired] = useState(false)
  const fresh = () => {
    const target = cache.getQueryState<ProviderMetadata>(key)
    return (
      !expired &&
      authFresh() &&
      authority.snapshot() === authority.revision &&
      resource.snapshot() === resource.revision &&
      target?.status === 'success' &&
      target.fetchStatus === 'idle' &&
      !target.isInvalidated &&
      !target.error &&
      target.data === query.data &&
      target.data?.id === providerId
    )
  }
  const writable = () => fresh() && access.can('providers.write') && query.data?.can_edit === true
  const [review, setReview] = useState<ProviderMetadata | null>(null),
    [name, setName] = useState(''),
    [reason, setReason] = useState('')
  const [intent, setIntent] = useState<Intent | null>(null),
    [notice, setNotice] = useState<string | null>(null)
  const [confirmation, setConfirmation] = useState<'save' | 'abandon' | null>(null),
    [busy, setBusy] = useState(false)
  const initialized = useRef(false),
    pending = useRef<{ controller: AbortController; claim: SubmittedIntentClaim } | null>(null)
  const saveButton = useRef<HTMLButtonElement>(null)
  const current = fresh()
  const sessionState = cache.getQueryState(sessionKey)
  const authorityPending =
    sessionState?.fetchStatus !== 'idle' ||
    sessionState?.status === 'pending' ||
    access.isPending ||
    access.isFetching
  const authorityError = !!sessionState?.error || access.isError
  const recovered = current ? shared?.recover(actor) : null
  if (
    recovered?.kind === 'provider-name' &&
    recovered.payload.provider_id === providerId &&
    recovered.claim !== intent?.claim
  ) {
    setIntent({ payload: recovered.payload, claim: recovered.claim })
    const identityMatches = recovered.payload.etag.split('.')[0] === query.data?.etag.split('.')[0]
    setName(identityMatches ? recovered.payload.input.name : (query.data?.name ?? ''))
    setReason(identityMatches ? recovered.payload.input.reason : '')
    setNotice('providerMetadata.uncertain')
  }
  const initialize = useEffectEvent(() => {
    if (
      !initialized.current &&
      !intent &&
      recovered?.kind !== 'provider-name' &&
      fresh() &&
      query.data
    ) {
      initialized.current = true
      setReview(query.data)
      setName(query.data.name)
    }
  })
  useEffect(() => {
    initialize()
  }, [current, query.data])
  useEffect(() => {
    if (!current) pending.current?.controller.abort()
  }, [current])
  useEffect(() => {
    const expire = () => {
      pending.current?.controller.abort()
      setExpired(true)
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      window.removeEventListener('routex:session-expired', expire)
      pending.current?.controller.abort()
    }
  }, [])
  const intentIdentityCurrent =
    !intent || intent.payload.etag.split('.')[0] === query.data?.etag.split('.')[0]
  const reviewIdentityCurrent =
    !review || review.etag.split('.')[0] === query.data?.etag.split('.')[0]
  const snapshotCurrent = !!review && review.etag === query.data?.etag
  const validName = validProviderName(trimProviderMetadata(name))
  const changed = trimProviderMetadata(name) !== review?.name
  const canPrepare = () =>
    writable() && !!shared && !intent && !busy && snapshotCurrent && validName && changed
  const canConfirm = () => canPrepare() && validProviderReason(trimProviderMetadata(reason))
  function reviewCurrent() {
    if (!writable() || intent || busy || pending.current || !query.data) return
    if (!reviewIdentityCurrent) {
      setName(query.data.name)
      setReason('')
    }
    setReview(query.data)
    setNotice('providerMetadata.reviewed')
  }
  function abandon() {
    if (
      !fresh() ||
      !intent ||
      busy ||
      pending.current ||
      !shared?.isCurrent(intent.claim) ||
      !shared.clear(intent.claim)
    )
      return
    if (!intentIdentityCurrent) {
      setName(query.data?.name ?? '')
      setReason('')
    }
    setIntent(null)
    setConfirmation(null)
    setReview(null)
    setNotice('providerMetadata.abandoned')
  }
  async function dispatch(retry: boolean) {
    if (!writable() || !shared || busy || pending.current) return
    let captured = intent
    if (!retry) {
      if (confirmation !== 'save' || !canConfirm() || !review) return
      const payload: ProviderNameSubmittedIntent = {
        provider_id: providerId,
        etag: review.etag,
        input: { name: trimProviderMetadata(name), reason: trimProviderMetadata(reason) },
      }
      const claim = shared.capture(actor, { kind: 'provider-name', payload })
      if (!claim) {
        setNotice('providerMetadata.unavailable')
        return
      }
      captured = { payload, claim }
      setIntent(captured)
      setName(payload.input.name)
      setReason(payload.input.reason)
    }
    if (
      !captured ||
      !shared.isCurrent(captured.claim) ||
      captured.payload.provider_id !== providerId ||
      captured.payload.etag.split('.')[0] !== query.data?.etag.split('.')[0]
    )
      return
    const csrf = cache.getQueryData<Session>(sessionKey)?.csrf_token
    if (!csrf) return
    const operation = { controller: new AbortController(), claim: captured.claim }
    const capturedAuthority = authority.snapshot(),
      capturedResource = resource.snapshot()
    pending.current = operation
    setBusy(true)
    setConfirmation(null)
    setNotice('providerMetadata.uncertain')
    try {
      await saveProviderMetadata(
        providerId,
        captured.payload.etag,
        captured.payload.input,
        csrf,
        operation.controller.signal,
      )
      if (
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        authority.snapshot() !== capturedAuthority ||
        resource.snapshot() !== capturedResource ||
        !writable() ||
        !shared.isCurrent(operation.claim)
      )
        return
      if (!shared.clear(operation.claim)) return
      setIntent(null)
      setReview(null)
      initialized.current = false
      setNotice('providerMetadata.saved')
      void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
      void cache.invalidateQueries({ queryKey: ['admin', 'provider-metadata', actor, providerId] })
      void cache.invalidateQueries({ queryKey: ['admin', 'provider-status', actor, providerId] })
    } catch (error) {
      if (
        pending.current === operation &&
        !operation.controller.signal.aborted &&
        fresh() &&
        shared.isCurrent(operation.claim)
      )
        setNotice(
          axios.isAxiosError(error) && error.response?.status === 409
            ? 'providerMetadata.conflict'
            : 'providerMetadata.uncertain',
        )
    } finally {
      if (pending.current === operation) {
        pending.current = null
        setBusy(false)
      }
    }
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('providerMetadata.title')}</CardTitle>
        <p className="mt-2 text-sm text-muted-foreground">{t('providerMetadata.description')}</p>
      </CardHeader>
      <CardContent>
        {!authFresh() && (
          <p role="status">
            {t(
              authorityError
                ? 'providerMetadata.loadFailed'
                : authorityPending
                  ? 'providerMetadata.loading'
                  : 'providerMetadata.readDenied',
            )}
          </p>
        )}
        {authFresh() && (
          <QueryState
            pending={query.isFetching}
            error={query.error}
            retry={() => void query.refetch()}
          />
        )}
        {current && (
          <form
            className="max-w-3xl space-y-4"
            noValidate
            onSubmit={(event) => {
              event.preventDefault()
              if (canPrepare()) setConfirmation('save')
            }}
          >
            <label className="grid gap-2 sm:grid-cols-[160px_minmax(0,1fr)] sm:items-center">
              <span className="text-sm font-medium">{t('common.providerName')}</span>
              <Input
                value={
                  intentIdentityCurrent && reviewIdentityCurrent ? name : (query.data?.name ?? '')
                }
                autoComplete="off"
                disabled={!writable() || busy || !!intent}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            {!writable() && (
              <p className="text-sm text-muted-foreground">{t('providerMetadata.readOnly')}</p>
            )}
            {!intent && !validName && <p role="alert">{t('providerMetadata.nameValidation')}</p>}
            {!intent && !snapshotCurrent && <p role="alert">{t('providerMetadata.stale')}</p>}
            {notice && (
              <p role={intent ? 'alert' : 'status'}>
                {t(intentIdentityCurrent ? notice : 'providerMetadata.identityChanged')}
              </p>
            )}
            {intent && (
              <p className="text-sm text-muted-foreground">{t('providerMetadata.retained')}</p>
            )}
            <div className="flex flex-wrap gap-2 sm:pl-40">
              {!intent && !snapshotCurrent && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={!writable() || busy}
                  onClick={reviewCurrent}
                >
                  {t('providerMetadata.review')}
                </Button>
              )}
              {intent && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!fresh() || busy}
                    onClick={() => setConfirmation('abandon')}
                  >
                    {t('providerMetadata.abandon')}
                  </Button>
                  <Button
                    type="button"
                    disabled={
                      !writable() ||
                      busy ||
                      !intentIdentityCurrent ||
                      !shared?.isCurrent(intent.claim)
                    }
                    onClick={() => void dispatch(true)}
                  >
                    {t('providerMetadata.retry')}
                  </Button>
                </>
              )}
              <Button ref={saveButton} type="submit" disabled={!canPrepare()}>
                {t('providerMetadata.save')}
              </Button>
            </div>
          </form>
        )}
        <Dialog
          open={current && confirmation === 'save'}
          onOpenChange={(open) => {
            if (!open) setConfirmation(null)
          }}
          title={t('providerMetadata.confirmTitle')}
          description={t('providerMetadata.confirmDescription')}
          finalFocus={saveButton}
        >
          <p className="mb-4">
            {t('providerMetadata.confirmName', { name: trimProviderMetadata(name) })}
          </p>
          <FormField label={t('providerMetadata.reason')}>
            <Input
              value={reason}
              autoComplete="off"
              disabled={!writable() || busy}
              onChange={(event) => setReason(event.target.value)}
            />
          </FormField>
          {!validProviderReason(trimProviderMetadata(reason)) && (
            <p className="mt-2 text-sm text-muted-foreground">
              {t('providerMetadata.reasonValidation')}
            </p>
          )}
          {!snapshotCurrent && <p role="alert">{t('providerMetadata.stale')}</p>}
          <div className="mt-6 flex justify-end gap-2">
            <Button variant="outline" onClick={() => setConfirmation(null)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!canConfirm()} onClick={() => void dispatch(false)}>
              {t('providerMetadata.confirm')}
            </Button>
          </div>
        </Dialog>
        <Dialog
          open={current && confirmation === 'abandon'}
          onOpenChange={(open) => {
            if (!open) setConfirmation(null)
          }}
          title={t('providerMetadata.abandonTitle')}
          description={t('providerMetadata.abandonDescription')}
        >
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setConfirmation(null)}>
              {t('common.cancel')}
            </Button>
            <Button
              disabled={busy || !intent || !shared?.isCurrent(intent.claim)}
              onClick={abandon}
            >
              {t('providerMetadata.confirmAbandon')}
            </Button>
          </div>
        </Dialog>
      </CardContent>
    </Card>
  )
}
