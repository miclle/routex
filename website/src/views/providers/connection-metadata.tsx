import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from 'react'
import axios from 'axios'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getConnectionMetadata,
  saveConnectionMetadata,
  trimConnectionMetadata,
  validConnectionName,
  validConnectionReason,
  validConnectionTransport,
  canonicalConnectionTransport,
} from '@/api/connection-metadata'
import type { ConnectionMetadata, ConnectionTransportInput } from '@/types/connection-metadata'
import type { Session } from '@/types/auth'
import type { ConnectionNameSubmittedIntent, SubmittedIntentClaim } from '@/types/uncertain-intents'
import { sessionKey } from '@/hooks/use-auth'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { selectClass } from '@/views/egress/editor'
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
  const live = useRef(props)
  useLayoutEffect(() => {
    live.current = props
  }, [props])
  const authority = useConnectionQueryRevision([
    sessionKey,
    props.permissionsKey,
    props.catalogueKey,
  ])
  const pending = useRef<{ controller: AbortController; claim: SubmittedIntentClaim } | null>(null)
  const mounted = useRef(false)
  const owner = useRef<object | null>(null)
  useLayoutEffect(() => {
    mounted.current = true
    const identity = {}
    owner.current = identity
    return () => {
      mounted.current = false
      owner.current = null
      pending.current?.controller.abort()
    }
  }, [props.actor, props.providerId, props.connectionId])
  const query = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const identity = owner.current,
        version = authority.snapshot()
      const record = await getConnectionMetadata(props.providerId, props.connectionId, signal)
      const auth = cache.getQueryState<Session | null>(sessionKey)
      if (
        signal.aborted ||
        !mounted.current ||
        owner.current !== identity ||
        authority.snapshot() !== version ||
        live.current.actor !== props.actor ||
        live.current.providerId !== props.providerId ||
        live.current.connectionId !== props.connectionId ||
        live.current.generation !== props.generation ||
        !live.current.open ||
        !live.current.ready ||
        auth?.status !== 'success' ||
        auth.fetchStatus !== 'idle' ||
        auth.error ||
        auth.isInvalidated ||
        auth.data?.user.id !== props.actor ||
        auth.dataUpdateCount !== props.generation
      )
        throw new Error('Connection metadata read unavailable')
      return record
    },
    enabled: props.open && props.ready,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const resource = useConnectionQueryRevision([key])
  const snapshotFresh = () => {
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
  const fresh = () =>
    mounted.current &&
    live.current.actor === props.actor &&
    live.current.providerId === props.providerId &&
    live.current.connectionId === props.connectionId &&
    live.current.generation === props.generation &&
    live.current.open &&
    live.current.ready &&
    snapshotFresh()
  const writable = () => fresh() && props.writable() && query.data?.can_edit === true
  const [review, setReview] = useState<ConnectionMetadata | null>(null)
  const [name, setName] = useState('')
  const [reason, setReason] = useState('')
  const [transport, setTransport] = useState<ConnectionTransportInput | null>(null)
  const [conflict, setConflict] = useState(false)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [abandoned, setAbandoned] = useState(false)
  const [confirmation, setConfirmation] = useState<'save' | 'abandon' | null>(null)
  const [busy, setBusy] = useState(false)
  const initialized = useRef(false)
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
    if (recovered.payload.input.transport) setTransport({ ...recovered.payload.input.transport })
  }
  const current = snapshotFresh()
  const renderWritable = current && props.writable() && query.data?.can_edit === true
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
      setTransport({
        base_url: query.data.base_url,
        protocol: query.data.protocol,
        adapter: query.data.adapter,
        api_version: query.data.api_version,
      })
    }
  })
  useEffect(() => {
    initialize()
  }, [current, query.data])
  useEffect(() => {
    if (!current && pending.current) {
      pending.current.controller.abort()
      pending.current = null
      setBusy(false)
    }
  }, [current])
  useEffect(() => () => pending.current?.controller.abort(), [])
  const snapshotCurrent = !conflict && !!review && review.etag === query.data?.etag
  const desiredTransport = transport ? canonicalConnectionTransport(transport) : null
  const transportChanged =
    !!review &&
    !!desiredTransport &&
    (['base_url', 'protocol', 'adapter', 'api_version'] as const).some(
      (key) => review[key] !== desiredTransport[key],
    )
  const transportAllowed =
    !transportChanged ||
    (query.data?.can_edit_transport === true &&
      !!desiredTransport &&
      validConnectionTransport(desiredTransport) &&
      (query.data.transport_locked !== true ||
        (desiredTransport.protocol === query.data.protocol &&
          desiredTransport.adapter === query.data.adapter)))
  const validDraft =
    validConnectionName(trimConnectionMetadata(name)) &&
    validConnectionReason(trimConnectionMetadata(reason))
  const canPrepare = () =>
    writable() &&
    !!shared &&
    !shared.recover(props.actor) &&
    !intent &&
    !busy &&
    !pending.current &&
    snapshotCurrent &&
    validDraft &&
    transportAllowed
  const prepareAvailable =
    renderWritable &&
    !!shared &&
    !recovered &&
    !intent &&
    !busy &&
    snapshotCurrent &&
    validDraft &&
    transportAllowed
  async function reviewCurrent() {
    if (!writable() || intent || shared?.recover(props.actor) || busy || pending.current) return
    const identity = owner.current,
      version = authority.snapshot()
    setBusy(true)
    try {
      const result = await query.refetch()
      const state = cache.getQueryState<ConnectionMetadata>(key)
      if (
        !mounted.current ||
        owner.current !== identity ||
        authority.snapshot() !== version ||
        !live.current.open ||
        !live.current.ready ||
        !props.writable() ||
        result.isError ||
        !result.data ||
        shared?.recover(props.actor) ||
        state?.status !== 'success' ||
        state.fetchStatus !== 'idle' ||
        state.error ||
        state.isInvalidated ||
        state.data !== result.data ||
        result.data.id !== props.connectionId ||
        result.data.provider_id !== props.providerId
      )
        return
      setReview(result.data)
      setConflict(false)
      setNotice('connectionMetadata.reviewed')
    } finally {
      if (mounted.current && owner.current === identity) setBusy(false)
    }
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
  function close() {
    if (mounted.current && !pending.current) props.onClose()
  }
  function dismissConfirmation() {
    if (!pending.current) setConfirmation(null)
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
        input: {
          name: trimConnectionMetadata(name),
          reason: trimConnectionMetadata(reason),
          ...(transportChanged && desiredTransport ? { transport: { ...desiredTransport } } : {}),
        },
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
    const capturedOwner = owner.current
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
        !mounted.current ||
        owner.current !== capturedOwner ||
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
    } catch (error) {
      if (
        !mounted.current ||
        owner.current !== capturedOwner ||
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        authority.snapshot() !== capturedAuthority ||
        !fresh()
      )
        return
      if (
        !retry &&
        captured.payload.input.transport !== undefined &&
        axios.isAxiosError(error) &&
        error.response?.status === 409 &&
        shared.isCurrent(operation.claim) &&
        shared.clear(operation.claim)
      ) {
        setIntent(null)
        setConflict(true)
        setNotice('connectionMetadata.stale')
      } else setNotice('connectionMetadata.uncertain')
    } finally {
      if (mounted.current && owner.current === capturedOwner && pending.current === operation) {
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
          if (!open) close()
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
                disabled={!renderWritable || busy || !!intent}
                onChange={(event) => setName(event.target.value)}
              />
            </FormField>
            <FormField label={t('common.protocolType')}>
              <select
                className={selectClass}
                aria-label={t('common.protocolType')}
                value={transport?.protocol ?? context.protocol}
                disabled={
                  !renderWritable ||
                  !context.can_edit_transport ||
                  context.transport_locked ||
                  busy ||
                  !!intent
                }
                onChange={(event) => {
                  if (
                    pending.current ||
                    !writable() ||
                    !context.can_edit_transport ||
                    context.transport_locked
                  )
                    return
                  setTransport((value) =>
                    value
                      ? {
                          ...value,
                          protocol: event.target.value as ConnectionMetadata['protocol'],
                          adapter: 'native',
                          api_version: null,
                        }
                      : value,
                  )
                }}
              >
                {(
                  [
                    'openai_chat',
                    'openai_responses',
                    'anthropic_messages',
                    'gemini_generate_content',
                  ] as const
                ).map((protocol) => (
                  <option key={protocol} value={protocol}>
                    {protocolLabel(protocol)}
                  </option>
                ))}
              </select>
            </FormField>
            {transport?.protocol === 'openai_chat' && (
              <FormField label={t('connectionTransport.adapter')}>
                <select
                  className={selectClass}
                  aria-label={t('connectionTransport.adapter')}
                  value={transport.adapter}
                  disabled={
                    !renderWritable ||
                    !context.can_edit_transport ||
                    context.transport_locked ||
                    busy ||
                    !!intent
                  }
                  onChange={(event) => {
                    if (
                      pending.current ||
                      !writable() ||
                      !context.can_edit_transport ||
                      context.transport_locked
                    )
                      return
                    setTransport((value) =>
                      value
                        ? {
                            ...value,
                            adapter: event.target.value as ConnectionMetadata['adapter'],
                            api_version: event.target.value === 'native' ? null : '',
                          }
                        : value,
                    )
                  }}
                >
                  <option value="native">{t('connectionTransport.native')}</option>
                  <option value="azure_openai_classic">{t('connectionTransport.azure')}</option>
                </select>
              </FormField>
            )}
            <FormField label={t('common.baseURL')}>
              <Input
                aria-label={t('common.baseURL')}
                value={transport?.base_url ?? context.base_url}
                autoComplete="off"
                disabled={!renderWritable || !context.can_edit_transport || busy || !!intent}
                onChange={(event) => {
                  if (!pending.current && writable() && context.can_edit_transport)
                    setTransport((value) =>
                      value ? { ...value, base_url: event.target.value } : value,
                    )
                }}
              />
            </FormField>
            {transport?.adapter === 'azure_openai_classic' && (
              <FormField label={t('connectionTransport.apiVersion')}>
                <Input
                  aria-label={t('connectionTransport.apiVersion')}
                  value={transport.api_version ?? ''}
                  autoComplete="off"
                  disabled={!renderWritable || !context.can_edit_transport || busy || !!intent}
                  onChange={(event) => {
                    if (!pending.current && writable() && context.can_edit_transport)
                      setTransport((value) =>
                        value ? { ...value, api_version: event.target.value } : value,
                      )
                  }}
                />
              </FormField>
            )}
            {!context.can_edit_transport && (
              <p className="text-sm text-muted-foreground">
                {t('connectionTransport.disableFirst')}
              </p>
            )}
            {context.transport_locked && (
              <p className="text-sm text-muted-foreground">{t('connectionTransport.locked')}</p>
            )}
            {!intent && transportChanged && (
              <Button
                type="button"
                variant="outline"
                disabled={!renderWritable || busy}
                onClick={() => {
                  if (!pending.current && writable())
                    setTransport({
                      base_url: context.base_url,
                      protocol: context.protocol,
                      adapter: context.adapter,
                      api_version: context.api_version,
                    })
                }}
              >
                {t('connectionTransport.resetDraft')}
              </Button>
            )}
            {transportChanged && (
              <p role="alert" className="text-sm">
                {t(
                  transportAllowed
                    ? 'connectionTransport.invalidates'
                    : 'connectionTransport.invalid',
                )}
              </p>
            )}
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
                disabled={!renderWritable || busy || !!intent}
                onChange={(event) => setReason(event.target.value)}
              />
            </FormField>
            {!intent && !validDraft && (
              <p className="text-sm text-muted-foreground">{t('connectionMetadata.validation')}</p>
            )}
            {!intent && !snapshotCurrent && <p role="alert">{t('connectionMetadata.stale')}</p>}
            {!renderWritable && <p role="alert">{t('connectionMetadata.readOnly')}</p>}
            {abandoned && <p role="alert">{t('connectionMetadata.abandoned')}</p>}
            {notice && <p role="status">{t(notice)}</p>}
            {intent && <p role="alert">{t('connectionMetadata.uncertain')}</p>}
            <div className="flex flex-wrap justify-end gap-2">
              {!intent && !snapshotCurrent && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={!renderWritable || busy}
                  onClick={() => void reviewCurrent()}
                >
                  {t('connectionMetadata.review')}
                </Button>
              )}
              {intent && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!current || busy}
                    onClick={() => setConfirmation('abandon')}
                  >
                    {t('connectionMetadata.abandon')}
                  </Button>
                  <Button
                    type="button"
                    disabled={!renderWritable || busy || !shared?.isCurrent(intent.claim)}
                    onClick={() => void dispatch(true)}
                  >
                    {t('connectionMetadata.retry')}
                  </Button>
                </>
              )}
              <Button type="button" variant="outline" disabled={busy} onClick={close}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={!prepareAvailable}>
                {t('connectionMetadata.save')}
              </Button>
            </div>
          </form>
        )}
      </Dialog>
      <Dialog
        open={props.open && current && confirmation === 'save'}
        onOpenChange={(open) => {
          if (!open) dismissConfirmation()
        }}
        title={t('connectionMetadata.confirmTitle')}
        description={t(
          transportChanged
            ? 'connectionTransport.confirmDescription'
            : 'connectionMetadata.confirmDescription',
        )}
        busy={busy}
      >
        {!snapshotCurrent && <p role="alert">{t('connectionMetadata.stale')}</p>}
        <p>{t('connectionMetadata.confirmName', { name: trimConnectionMetadata(name) })}</p>
        {transportChanged && review && desiredTransport && (
          <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-2">
            <div>
              <dt className="font-medium">{t('connectionTransport.before')}</dt>
              <dd className="break-all whitespace-pre-wrap">
                {t('connectionTransport.tuple', {
                  url: review.base_url,
                  protocol: protocolLabel(review.protocol),
                  adapter: review.adapter,
                  version: review.api_version ?? t('connectionTransport.noVersion'),
                })}
              </dd>
            </div>
            <div>
              <dt className="font-medium">{t('connectionTransport.after')}</dt>
              <dd className="break-all whitespace-pre-wrap">
                {t('connectionTransport.tuple', {
                  url: desiredTransport.base_url,
                  protocol: protocolLabel(desiredTransport.protocol),
                  adapter: desiredTransport.adapter,
                  version: desiredTransport.api_version ?? t('connectionTransport.noVersion'),
                })}
              </dd>
            </div>
          </dl>
        )}
        <p className="mt-2">
          {t('connectionMetadata.confirmReason', { reason: trimConnectionMetadata(reason) })}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="outline" onClick={dismissConfirmation}>
            {t('common.cancel')}
          </Button>
          <Button disabled={!prepareAvailable} onClick={() => void dispatch(false)}>
            {t('connectionMetadata.confirm')}
          </Button>
        </div>
      </Dialog>
      <Dialog
        open={props.open && current && confirmation === 'abandon'}
        onOpenChange={(open) => {
          if (!open) dismissConfirmation()
        }}
        title={t('connectionMetadata.abandonTitle')}
        description={t('connectionMetadata.abandonDescription')}
        busy={busy}
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={dismissConfirmation}>
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
