import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getConnectionMetadata,
  saveConnectionMetadata,
  trimConnectionMetadata,
  validConnectionName,
  validConnectionReason,
} from '@/api/connection-metadata'
import type { ConnectionMetadata } from '@/types/connection-metadata'
import type { Session } from '@/types/auth'
import type { ConnectionNameSubmittedIntent, SubmittedIntentClaim } from '@/types/uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { protocolLabel } from '@/lib/protocols'
import { useConnectionQueryRevision } from './connection-authority'

interface Props {
  actor: string
  providerId: string
  connectionId: string
  generation: number
  ready: boolean
  writable: () => boolean
  permissionsKey: readonly unknown[]
  catalogueKey: readonly unknown[]
  open: boolean
  onClose: () => void
  onSaved: () => void
}
interface Intent {
  payload: ConnectionNameSubmittedIntent
  claim: SubmittedIntentClaim
}
export default function ConnectionMetadataEditor(props: Props) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const shared = useUncertainIntents()
  const permissionGeneration = cache.getQueryState(props.permissionsKey)?.dataUpdateCount ?? 0
  const catalogueGeneration = cache.getQueryState(props.catalogueKey)?.dataUpdateCount ?? 0
  const key = [
    'admin',
    'connection-metadata',
    props.actor,
    props.providerId,
    props.connectionId,
    props.generation,
    permissionGeneration,
    catalogueGeneration,
  ]
  const authority = useConnectionQueryRevision([
    sessionKey,
    props.permissionsKey,
    props.catalogueKey,
  ])
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getConnectionMetadata(props.providerId, props.connectionId, signal),
    enabled: props.open && props.ready,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const resource = useConnectionQueryRevision([key])
  const fresh = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey)
    const target = cache.getQueryState<ConnectionMetadata>(key)
    return (
      props.ready &&
      props.open &&
      authority.snapshot() === authority.revision &&
      resource.snapshot() === resource.revision &&
      auth?.data?.user.id === props.actor &&
      auth.dataUpdateCount === props.generation &&
      auth.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.isInvalidated &&
      !auth.error &&
      target?.status === 'success' &&
      target.fetchStatus === 'idle' &&
      !target.isInvalidated &&
      !target.error &&
      target.data === query.data &&
      target.data?.id === props.connectionId &&
      target.data.provider_id === props.providerId
    )
  }
  const writable = () => fresh() && props.writable() && query.data?.can_edit === true
  const [review, setReview] = useState<ConnectionMetadata | null>(null)
  const [name, setName] = useState('')
  const [reason, setReason] = useState('')
  const [intent, setIntent] = useState<Intent | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [abandoned, setAbandoned] = useState(false)
  const [confirmation, setConfirmation] = useState<'save' | 'abandon' | null>(null)
  const [busy, setBusy] = useState(false)
  const initialized = useRef(false)
  const pending = useRef<{ controller: AbortController; claim: SubmittedIntentClaim } | null>(null)
  const recovered = shared?.recover(props.actor)
  if (
    recovered?.kind === 'connection-name' &&
    recovered.payload.provider_id === props.providerId &&
    recovered.payload.connection_id === props.connectionId &&
    intent?.claim !== recovered.claim
  ) {
    setIntent({ payload: recovered.payload, claim: recovered.claim })
    setName(recovered.payload.input.name)
    setReason(recovered.payload.input.reason)
  }
  const current = fresh()
  const initialize = useEffectEvent(() => {
    if (
      !initialized.current &&
      !intent &&
      recovered?.kind !== 'connection-name' &&
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
  useEffect(() => () => pending.current?.controller.abort(), [])
  const snapshotCurrent = !!review && review.etag === query.data?.etag
  const validDraft =
    validConnectionName(trimConnectionMetadata(name)) &&
    validConnectionReason(trimConnectionMetadata(reason))
  const canPrepare = () =>
    writable() && !!shared && !intent && !busy && snapshotCurrent && validDraft
  function reviewCurrent() {
    if (!writable() || intent || busy || pending.current || !query.data) return
    setReview(query.data)
    setNotice('connectionMetadata.reviewed')
  }
  function abandon() {
    if (!fresh() || !intent || busy || pending.current || !shared?.isCurrent(intent.claim)) return
    if (!shared.clear(intent.claim)) return
    setIntent(null)
    setConfirmation(null)
    setReview(null)
    setAbandoned(true)
    setNotice(null)
  }
  async function dispatch(retry: boolean) {
    if (!writable() || !shared || busy || pending.current) return
    let captured = intent
    if (!retry) {
      if (confirmation !== 'save' || !canPrepare() || !review) return
      const payload: ConnectionNameSubmittedIntent = {
        provider_id: props.providerId,
        connection_id: props.connectionId,
        etag: review.etag,
        input: { name: trimConnectionMetadata(name), reason: trimConnectionMetadata(reason) },
      }
      const claim = shared.capture(props.actor, { kind: 'connection-name', payload })
      if (!claim) {
        setNotice('connectionMetadata.unavailable')
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
      captured.payload.provider_id !== props.providerId ||
      captured.payload.connection_id !== props.connectionId
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
    setNotice('connectionMetadata.uncertain')
    try {
      await saveConnectionMetadata(
        props.providerId,
        props.connectionId,
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
      setNotice('connectionMetadata.saved')
      props.onSaved()
    } catch {
      if (pending.current === operation) setNotice('connectionMetadata.uncertain')
    } finally {
      if (pending.current === operation) {
        pending.current = null
        setBusy(false)
      }
    }
  }
  const context = query.data
  return (
    <>
      <Dialog
        open={props.open && props.ready && confirmation === null}
        onOpenChange={(open) => {
          if (!open) props.onClose()
        }}
        busy={busy}
        width={720}
        title={t('connectionMetadata.title')}
        description={t('connectionMetadata.description')}
      >
        <QueryState
          pending={query.isFetching}
          error={query.error}
          retry={() => void query.refetch()}
        />
        {current && context && (
          <form
            className="space-y-5"
            noValidate
            onSubmit={(event) => {
              event.preventDefault()
              if (canPrepare()) setConfirmation('save')
            }}
          >
            <FormField label={t('common.connectionName')}>
              <Input
                value={name}
                autoComplete="off"
                disabled={!writable() || busy || !!intent}
                onChange={(event) => setName(event.target.value)}
              />
            </FormField>
            <FormField label={t('common.protocolType')}>
              <Input value={protocolLabel(context.protocol)} readOnly />
            </FormField>
            <FormField label={t('common.baseURL')}>
              <Input value={context.base_url} readOnly />
            </FormField>
            <FormField label={t('egress:selection')}>
              <Input
                value={
                  context.egress_mode === 'proxy'
                    ? (context.egress_id ?? '')
                    : t(context.egress_mode === 'direct' ? 'egress:direct' : 'egress:inherited')
                }
                readOnly
              />
            </FormField>
            <p className="text-sm text-muted-foreground">{t('connectionMetadata.readonlyHelp')}</p>
            <FormField label={t('connectionMetadata.reason')}>
              <Input
                value={reason}
                autoComplete="off"
                disabled={!writable() || busy || !!intent}
                onChange={(event) => setReason(event.target.value)}
              />
            </FormField>
            {!intent && !validDraft && (
              <p className="text-sm text-muted-foreground">{t('connectionMetadata.validation')}</p>
            )}
            {!intent && !snapshotCurrent && <p role="alert">{t('connectionMetadata.stale')}</p>}
            {!writable() && <p role="alert">{t('connectionMetadata.readOnly')}</p>}
            {abandoned && <p role="alert">{t('connectionMetadata.abandoned')}</p>}
            {notice && <p role="status">{t(notice)}</p>}
            {intent && <p role="alert">{t('connectionMetadata.uncertain')}</p>}
            <div className="flex flex-wrap justify-end gap-2">
              {!intent && !snapshotCurrent && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={!writable() || busy}
                  onClick={reviewCurrent}
                >
                  {t('connectionMetadata.review')}
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
                    {t('connectionMetadata.abandon')}
                  </Button>
                  <Button
                    type="button"
                    disabled={!writable() || busy || !shared?.isCurrent(intent.claim)}
                    onClick={() => void dispatch(true)}
                  >
                    {t('connectionMetadata.retry')}
                  </Button>
                </>
              )}
              <Button type="button" variant="outline" disabled={busy} onClick={props.onClose}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={!canPrepare()}>
                {t('connectionMetadata.save')}
              </Button>
            </div>
          </form>
        )}
      </Dialog>
      <Dialog
        open={props.open && current && confirmation === 'save'}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null)
        }}
        title={t('connectionMetadata.confirmTitle')}
        description={t('connectionMetadata.confirmDescription')}
      >
        {!snapshotCurrent && <p role="alert">{t('connectionMetadata.stale')}</p>}
        <p>{t('connectionMetadata.confirmName', { name: trimConnectionMetadata(name) })}</p>
        <p className="mt-2">
          {t('connectionMetadata.confirmReason', { reason: trimConnectionMetadata(reason) })}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirmation(null)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={!canPrepare()} onClick={() => void dispatch(false)}>
            {t('connectionMetadata.confirm')}
          </Button>
        </div>
      </Dialog>
      <Dialog
        open={props.open && current && confirmation === 'abandon'}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null)
        }}
        title={t('connectionMetadata.abandonTitle')}
        description={t('connectionMetadata.abandonDescription')}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirmation(null)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={busy || !intent || !shared?.isCurrent(intent.claim)} onClick={abandon}>
            {t('connectionMetadata.confirmAbandon')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
