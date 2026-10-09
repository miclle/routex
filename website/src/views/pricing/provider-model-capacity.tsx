import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import {
  getProviderModelCapacity,
  saveProviderModelCapacity,
  validCapacityText,
  trimCapacityText,
} from '@/api/provider-model-capacity'
import type {
  ProviderModelCapacity,
  ProviderModelCapacityInput,
} from '@/types/provider-model-capacity'
import type {
  ProviderModelCapacitySubmittedIntent,
  SubmittedIntentClaim,
} from '@/types/uncertain-intents'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useConnectionQueryRevision } from '@/views/providers/connection-authority'
import { useProviderModelAuthority, type ProviderModelAuthority } from './provider-model-authority'

type Intent = { payload: ProviderModelCapacitySubmittedIntent; claim: SubmittedIntentClaim }
export default function ProviderModelCapacityCard({
  modelId,
  visible = true,
  authority,
}: {
  modelId: string
  visible?: boolean
  authority: ProviderModelAuthority
}) {
  return (
    <CapacityCard
      key={JSON.stringify([authority.actor, authority.providerId, authority.connectionId, modelId])}
      modelId={modelId}
      visible={visible}
      authority={authority}
    />
  )
}
function CapacityCard({
  modelId,
  visible,
  authority,
}: {
  modelId: string
  visible: boolean
  authority: ProviderModelAuthority
}) {
  const { t, i18n } = useTranslation('pricing'),
    { t: copy } = useTranslation('catalog')
  const boundary = useProviderModelAuthority(modelId, visible, authority)
  const shared = useUncertainIntents()
  const permissionGeneration =
    boundary.cache.getQueryState(authority.permissionsKey)?.dataUpdateCount ?? 0
  const catalogueGeneration =
    boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0
  const queryKey = [
    'admin',
    'provider-model-capacity',
    authority.actor,
    authority.providerId,
    authority.connectionId,
    modelId,
    authority.generation,
    permissionGeneration,
    catalogueGeneration,
  ]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      const token = boundary.capture()
      const data = await getProviderModelCapacity(modelId, signal)
      if (signal.aborted || !boundary.current(token))
        throw new Error('Provider Model capacity read unavailable')
      return data
    },
    enabled: boundary.renderReadable,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const resource = useConnectionQueryRevision([queryKey])
  const [reviewed, setReviewed] = useState<ProviderModelCapacity | null>(null)
  const [input, setInput] = useState(''),
    [output, setOutput] = useState('')
  const [evidence, setEvidence] = useState(''),
    [reason, setReason] = useState('')
  const [open, setOpen] = useState(false),
    [confirmation, setConfirmation] = useState<ProviderModelCapacityInput | null>(null)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [conflict, setConflict] = useState(false),
    [busy, setBusy] = useState(false)
  const conflictRead = useRef<{ key: string; count: number } | null>(null)
  const [conflictSnapshot, setConflictSnapshot] = useState<{ key: string; count: number } | null>(
    null,
  )
  const [notice, setNotice] = useState<{ key: string; generation: number } | null>(null)
  const [separate, setSeparate] = useState<{ key: string; count: number } | null>(null)
  const pending = useRef<{
    controller: AbortController
    token: ReturnType<typeof boundary.capture>
    resource: string
  } | null>(null)
  const retained = shared?.recover(authority.actor)
  if (
    retained?.kind === 'provider-model-capacity' &&
    retained.payload.provider_id === authority.providerId &&
    retained.payload.connection_id === authority.connectionId &&
    retained.payload.provider_model_id === modelId &&
    retained.claim !== intent?.claim
  ) {
    setIntent({ payload: retained.payload, claim: retained.claim })
    setInput(String(retained.payload.input.max_input_tokens))
    setOutput(String(retained.payload.input.max_output_tokens))
    setEvidence(retained.payload.input.evidence)
    setReason(retained.payload.input.reason)
  }
  const resourceFresh = () => {
    const state = boundary.cache.getQueryState<ProviderModelCapacity>(queryKey)
    return (
      resource.snapshot() === resource.revision &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data &&
      state.data?.provider_model_id === modelId
    )
  }
  const fresh = () => boundary.readable() && resourceFresh()
  const current = boundary.renderReadable && resourceFresh(),
    record = current ? query.data : undefined
  const release = useEffectEvent(() => {
    if (
      pending.current &&
      (!boundary.current(pending.current.token) ||
        resource.snapshot() !== pending.current.resource ||
        !fresh())
    ) {
      pending.current.controller.abort()
      pending.current = null
      setBusy(false)
    }
  })
  useEffect(() => release(), [current, boundary.revision, resource.revision, authority.generation])
  useEffect(
    () => () => {
      const operation = pending.current
      pending.current = null
      operation?.controller.abort()
    },
    [],
  )
  const message = (key: string) => setNotice({ key, generation: authority.generation })
  const writable = () => fresh() && boundary.writable()
  const stale = conflict || !reviewed || reviewed.etag !== record?.etag
  const canPrepare = () =>
    writable() &&
    !!shared &&
    !shared.recover(authority.actor) &&
    !intent &&
    !pending.current &&
    !busy &&
    !stale
  function edit() {
    if (!writable() || pending.current || !record) return
    if (!reviewed && !intent) {
      setReviewed(record)
      setInput(record.configured ? String(record.max_input_tokens) : '')
      setOutput(record.configured ? String(record.max_output_tokens) : '')
      setEvidence(record.evidence)
    }
    setOpen(true)
  }
  function close() {
    if (!pending.current) {
      setOpen(false)
      setConfirmation(null)
    }
  }
  function prepare() {
    if (!canPrepare()) return
    const number = (value: string) =>
      /^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0
    if (!number(input) || !number(output)) {
      message('transportEvidence.capacityNumberError')
      return
    }
    const draft = {
      max_input_tokens: Number(input),
      max_output_tokens: Number(output),
      evidence: trimCapacityText(evidence),
      reason: trimCapacityText(reason),
    }
    if (!validCapacityText(draft.evidence) || !validCapacityText(draft.reason)) {
      message('transportEvidence.capacityTextError')
      return
    }
    setConfirmation(draft)
  }
  async function dispatch(retry: boolean) {
    if (!writable() || !shared || pending.current) return
    let captured = intent
    if (!retry) {
      if (!confirmation || !reviewed || !canPrepare()) return
      const payload: ProviderModelCapacitySubmittedIntent = {
        provider_id: authority.providerId,
        connection_id: authority.connectionId,
        provider_model_id: modelId,
        etag: reviewed.etag,
        input: { ...confirmation },
      }
      const claim = shared.capture(authority.actor, { kind: 'provider-model-capacity', payload })
      if (!claim) return
      captured = { payload, claim }
      setIntent(captured)
      setInput(String(payload.input.max_input_tokens))
      setOutput(String(payload.input.max_output_tokens))
    }
    if (
      !captured ||
      !shared.isCurrent(captured.claim) ||
      captured.payload.provider_id !== authority.providerId ||
      captured.payload.connection_id !== authority.connectionId ||
      captured.payload.provider_model_id !== modelId
    )
      return
    const csrf = boundary.csrf()
    if (!csrf) return
    const operation = {
      controller: new AbortController(),
      token: boundary.capture(),
      resource: resource.snapshot(),
    }
    pending.current = operation
    setBusy(true)
    setConfirmation(null)
    message('transportEvidence.uncertain')
    try {
      const result = await saveProviderModelCapacity(
        modelId,
        captured.payload.etag,
        captured.payload.input,
        csrf,
        operation.controller.signal,
      )
      if (
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        !boundary.current(operation.token) ||
        resource.snapshot() !== operation.resource ||
        !writable() ||
        !shared.isCurrent(captured.claim)
      )
        return
      if (!shared.clear(captured.claim)) return
      setIntent(null)
      setOpen(false)
      setReviewed(null)
      setConflict(false)
      message('transportEvidence.capacitySaved')
      boundary.cache.setQueryData(queryKey, result)
    } catch (error) {
      if (
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        !boundary.current(operation.token) ||
        resource.snapshot() !== operation.resource ||
        !fresh()
      )
        return
      if (
        !retry &&
        axios.isAxiosError(error) &&
        error.response?.status === 409 &&
        shared.isCurrent(captured.claim) &&
        shared.clear(captured.claim)
      ) {
        setIntent(null)
        conflictRead.current = {
          key: JSON.stringify(queryKey),
          count: boundary.cache.getQueryState(queryKey)?.dataUpdateCount ?? 0,
        }
        setConflictSnapshot(conflictRead.current)
        setConflict(true)
        message('transportEvidence.stale')
      } else message('transportEvidence.uncertain')
    } finally {
      if (pending.current === operation) {
        pending.current = null
        setBusy(false)
      }
    }
  }
  async function refresh(separateChange = false) {
    if (!boundary.readable() || pending.current || (intent && !separateChange)) return
    if (separateChange)
      setSeparate({
        key: JSON.stringify(queryKey),
        count: boundary.cache.getQueryState(queryKey)?.dataUpdateCount ?? 0,
      })
    await query.refetch()
  }
  const canReview = () =>
    !conflictRead.current ||
    conflictRead.current.key !== JSON.stringify(queryKey) ||
    (boundary.cache.getQueryState(queryKey)?.dataUpdateCount ?? 0) > conflictRead.current.count
  function review() {
    if (
      canReview() &&
      writable() &&
      record &&
      !intent &&
      !pending.current &&
      !shared?.recover(authority.actor)
    ) {
      setReviewed(record)
      conflictRead.current = null
      setConflictSnapshot(null)
      setConflict(false)
      message('transportEvidence.reviewed')
    }
  }
  const canAbandon = () =>
    !!separate &&
    fresh() &&
    !!intent &&
    !pending.current &&
    shared?.isCurrent(intent.claim) === true &&
    separate.key === JSON.stringify(queryKey) &&
    (boundary.cache.getQueryState(queryKey)?.dataUpdateCount ?? 0) > separate.count
  function abandon() {
    if (!canAbandon() || !intent || !record || !shared?.clear(intent.claim)) return
    setIntent(null)
    setSeparate(null)
    setReviewed(record)
    setConflict(false)
    message('transportEvidence.abandoned')
  }
  // These reactive facts control rendering; imperative helpers above remain
  // authoritative when an event callback dispatches or changes an intent.
  const renderWritable = current && boundary.renderWritable
  const prepareAvailable = renderWritable && !!shared && !retained && !intent && !busy && !stale
  const reviewAvailable =
    !conflictSnapshot ||
    conflictSnapshot.key !== JSON.stringify(queryKey) ||
    (boundary.cache.getQueryState(queryKey)?.dataUpdateCount ?? 0) > conflictSnapshot.count
  const abandonAvailable =
    !!separate &&
    current &&
    !!intent &&
    !busy &&
    shared?.isCurrent(intent.claim) === true &&
    separate.key === JSON.stringify(queryKey) &&
    (boundary.cache.getQueryState(queryKey)?.dataUpdateCount ?? 0) > separate.count
  if (!boundary.renderReadable) return null
  const locked = !renderWritable || busy || !!intent
  return (
    <>
      <section className="space-y-4 rounded-lg border p-6" aria-label={t('capacityTitle')}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h3 className="font-semibold">{t('capacityTitle')}</h3>
          {record && boundary.renderWritable && (
            <Button variant="outline" onClick={edit}>
              {t('capacityEdit')}
            </Button>
          )}
        </div>
        <p className="text-sm text-muted-foreground">{t('capacityHelp')}</p>
        <QueryState pending={query.isFetching} error={query.error} retry={() => void refresh()} />
        {record && (
          <>
            <Badge variant="outline">
              {t(record.configured ? 'capacityConfigured' : 'capacityUnconfigured')}
            </Badge>
            <p
              role={record.configured && record.transport_current === false ? 'alert' : undefined}
              className="text-sm"
            >
              {copy(
                record.transport_current === true && record.configured
                  ? 'transportEvidence.capacityCurrent'
                  : record.configured && record.transport_current === false
                    ? 'transportEvidence.capacityStale'
                    : 'transportEvidence.capacityAbsent',
              )}
            </p>
            {!record.configured && (
              <p className="text-sm">{t('capacityProtocol', { protocol: record.protocol })}</p>
            )}
            {record.configured && (
              <dl className="grid gap-4 text-sm sm:grid-cols-3">
                {[
                  ['capacityInput', record.max_input_tokens.toLocaleString(i18n.language)],
                  ['capacityOutput', record.max_output_tokens.toLocaleString(i18n.language)],
                  ['protocol', record.protocol],
                  ['capacityEvidence', record.evidence],
                  ['capacityRevision', record.revision],
                  ['capacityUpdated', new Date(record.updated_at).toLocaleString(i18n.language)],
                ].map(([label, value]) => (
                  <div key={label}>
                    <dt className="text-muted-foreground">{t(label)}</dt>
                    <dd className="mt-1 break-words whitespace-pre-wrap">{value}</dd>
                  </div>
                ))}
              </dl>
            )}
            <p className="text-sm text-muted-foreground">{t('capacityValidityHelp')}</p>
          </>
        )}
        {intent && <p role="alert">{copy('transportEvidence.uncertain')}</p>}
        {notice?.generation === authority.generation && (
          <p role="status" className="text-sm">
            {copy(notice.key)}
          </p>
        )}
      </section>
      <Dialog
        open={open && current && !confirmation && !separate}
        onOpenChange={(value) => {
          if (!value) close()
        }}
        title={t('capacityEdit')}
        description={t('capacityEditorHelp')}
        busy={busy}
        width={640}
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            prepare()
          }}
        >
          {reviewed && (
            <p className="text-sm text-muted-foreground">
              {t('capacityReviewedRevision', {
                revision: reviewed.revision,
                protocol: reviewed.protocol,
              })}
            </p>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <FormField label={t('capacityInput')}>
              <Input
                aria-label={t('capacityInput')}
                inputMode="numeric"
                autoComplete="off"
                value={input}
                onChange={(event) => {
                  if (!locked && !pending.current) setInput(event.target.value)
                }}
                disabled={locked}
              />
            </FormField>
            <FormField label={t('capacityOutput')}>
              <Input
                aria-label={t('capacityOutput')}
                inputMode="numeric"
                autoComplete="off"
                value={output}
                onChange={(event) => {
                  if (!locked && !pending.current) setOutput(event.target.value)
                }}
                disabled={locked}
              />
            </FormField>
          </div>
          <p className="text-sm text-muted-foreground">{t('capacityTokenHelp')}</p>
          <FormField label={t('capacityEvidence')}>
            <Input
              aria-label={t('capacityEvidence')}
              autoComplete="off"
              value={evidence}
              onChange={(event) => {
                if (!locked && !pending.current) setEvidence(event.target.value)
              }}
              disabled={locked}
            />
          </FormField>
          <FormField label={t('capacityReason')}>
            <Input
              aria-label={t('capacityReason')}
              autoComplete="off"
              value={reason}
              onChange={(event) => {
                if (!locked && !pending.current) setReason(event.target.value)
              }}
              disabled={locked}
            />
          </FormField>
          {stale && !intent && <p role="alert">{copy('transportEvidence.stale')}</p>}
          {intent && <p role="alert">{copy('transportEvidence.uncertain')}</p>}
          {retained && !intent && <p role="alert">{copy('transportEvidence.otherIntent')}</p>}
          {notice?.generation === authority.generation && <p role="status">{copy(notice.key)}</p>}
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={!prepareAvailable}>
              {t('capacitySave')}
            </Button>
            {intent && (
              <>
                <Button
                  type="button"
                  disabled={!renderWritable || busy}
                  onClick={() => void dispatch(true)}
                >
                  {copy('transportEvidence.retry')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!current || busy}
                  onClick={() => void refresh(true)}
                >
                  {copy('transportEvidence.separate')}
                </Button>
              </>
            )}
            {stale && !intent && (
              <>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!boundary.renderReadable || busy}
                  onClick={() => void refresh()}
                >
                  {t('capacityReview')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!renderWritable || busy || !reviewAvailable}
                  onClick={review}
                >
                  {copy('transportEvidence.review')}
                </Button>
              </>
            )}
            <Button type="button" variant="outline" disabled={busy} onClick={close}>
              {t('cancel')}
            </Button>
          </div>
        </form>
      </Dialog>
      <Dialog
        open={open && current && !!confirmation}
        onOpenChange={(value) => {
          if (!value && !pending.current) setConfirmation(null)
        }}
        busy={busy}
        title={copy('transportEvidence.confirmCapacity')}
        description={copy('transportEvidence.confirmCapacityHelp')}
      >
        {confirmation && (
          <dl className="space-y-2 text-sm">
            {[
              ['capacityInput', String(confirmation.max_input_tokens)],
              ['capacityOutput', String(confirmation.max_output_tokens)],
              ['capacityEvidence', confirmation.evidence],
              ['capacityReason', confirmation.reason],
            ].map(([label, value]) => (
              <div key={label}>
                <dt>{t(label)}</dt>
                <dd className="break-words">{value}</dd>
              </div>
            ))}
          </dl>
        )}
        <div className="mt-6 flex justify-end gap-2">
          <Button
            variant="outline"
            onClick={() => {
              if (!pending.current) setConfirmation(null)
            }}
          >
            {t('cancel')}
          </Button>
          <Button disabled={!prepareAvailable} onClick={() => void dispatch(false)}>
            {copy('transportEvidence.confirm')}
          </Button>
        </div>
      </Dialog>
      <Dialog
        open={open && current && !!separate}
        onOpenChange={(value) => {
          if (!value && !pending.current) setSeparate(null)
        }}
        busy={busy}
        title={copy('transportEvidence.abandonTitle')}
        description={copy('transportEvidence.abandonDescription')}
      >
        {record && (
          <p>{copy('transportEvidence.currentCapacity', { revision: record.revision })}</p>
        )}
        <div className="mt-6 flex justify-end gap-2">
          <Button
            variant="outline"
            onClick={() => {
              if (!pending.current) setSeparate(null)
            }}
          >
            {t('cancel')}
          </Button>
          <Button disabled={!abandonAvailable} onClick={abandon}>
            {copy('transportEvidence.abandon')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
