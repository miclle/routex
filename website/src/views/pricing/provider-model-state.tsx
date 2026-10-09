import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { saveProviderModelState } from '@/api/provider-model-state'
import type { ProviderModel } from '@/types/catalog'
import type { ProviderModelStateInput } from '@/types/provider-model-state'
import type {
  ProviderModelStateSubmittedIntent,
  SubmittedIntentClaim,
} from '@/types/uncertain-intents'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { useProviderModelAuthority, type ProviderModelAuthority } from './provider-model-authority'

type Intent = { payload: ProviderModelStateSubmittedIntent; claim: SubmittedIntentClaim }
export default function ProviderModelState({
  model,
  reload,
  visible = true,
  authority,
}: {
  model: ProviderModel
  visible?: boolean
  reload: () => Promise<ProviderModel | undefined>
  authority: ProviderModelAuthority
}) {
  const { t } = useTranslation('pricing'),
    { t: copy } = useTranslation('catalog')
  const boundary = useProviderModelAuthority(model.id, visible, authority)
  const shared = useUncertainIntents()
  const [reviewed, setReviewed] = useState(model)
  const [enabled, setEnabled] = useState(model.enabled)
  const [image, setImage] = useState(model.supports_image_input)
  const [pdf, setPdf] = useState(model.supports_pdf_input)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [confirmation, setConfirmation] = useState<ProviderModelStateInput | null>(null)
  const [conflict, setConflict] = useState(false)
  const conflictRead = useRef<{ key: string; count: number } | null>(null)
  const [conflictSnapshot, setConflictSnapshot] = useState<{ key: string; count: number } | null>(
    null,
  )
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<{ key: string; generation: number } | null>(null)
  const [separate, setSeparate] = useState<{ key: string; count: number } | null>(null)
  const pending = useRef<{
    controller: AbortController
    token: ReturnType<typeof boundary.capture>
  } | null>(null)
  const retained = shared?.recover(authority.actor)
  if (
    retained?.kind === 'provider-model-state' &&
    retained.payload.provider_id === authority.providerId &&
    retained.payload.connection_id === authority.connectionId &&
    retained.payload.provider_model_id === model.id &&
    retained.claim !== intent?.claim
  ) {
    setIntent({ payload: retained.payload, claim: retained.claim })
    if (retained.payload.input.enabled !== undefined) setEnabled(retained.payload.input.enabled)
    if (retained.payload.input.capability_review_etag !== undefined) {
      setImage(retained.payload.input.supports_image_input)
      setPdf(retained.payload.input.supports_pdf_input)
    }
  }
  const readable = boundary.renderReadable
  const release = useEffectEvent(() => {
    if (pending.current && !boundary.current(pending.current.token)) {
      pending.current.controller.abort()
      pending.current = null
      setBusy(false)
    }
  })
  useEffect(() => release(), [readable, boundary.revision, authority.generation])
  useEffect(
    () => () => {
      const operation = pending.current
      pending.current = null
      operation?.controller.abort()
    },
    [],
  )
  const stale =
    conflict ||
    reviewed.etag !== model.etag ||
    reviewed.capability_review_etag !== model.capability_review_etag
  const availabilityChanged = enabled !== reviewed.enabled
  const capabilitiesChanged =
    image !== reviewed.supports_image_input || pdf !== reviewed.supports_pdf_input
  const capabilityReview =
    typeof reviewed.capability_review_etag === 'string' &&
    /^[0-9a-f]{64}$/.test(reviewed.capability_review_etag)
  const canPrepare = () =>
    boundary.writable() &&
    !!shared &&
    !shared.recover(authority.actor) &&
    !intent &&
    !pending.current &&
    !busy &&
    !stale
  const message = (key: string) => setNotice({ key, generation: authority.generation })
  function prepare(reaffirm = false) {
    if (!canPrepare() || (!reaffirm && !availabilityChanged && !capabilitiesChanged)) return
    if (reaffirm || capabilitiesChanged) {
      if (!capabilityReview) return
      setConfirmation({
        etag: reviewed.etag,
        capability_review_etag: reviewed.capability_review_etag!,
        supports_image_input: image,
        supports_pdf_input: pdf,
        ...(availabilityChanged ? { enabled } : {}),
      })
    } else setConfirmation({ etag: reviewed.etag, enabled })
  }
  function cancel() {
    if (!pending.current) setConfirmation(null)
  }
  async function dispatch(retry: boolean) {
    if (!boundary.writable() || !shared || pending.current) return
    let captured = intent
    if (!retry) {
      if (!confirmation || !canPrepare()) return
      const payload: ProviderModelStateSubmittedIntent = {
        provider_id: authority.providerId,
        connection_id: authority.connectionId,
        provider_model_id: model.id,
        input: { ...confirmation },
      }
      const claim = shared.capture(authority.actor, { kind: 'provider-model-state', payload })
      if (!claim) return
      captured = { payload, claim }
      setIntent(captured)
    }
    if (
      !captured ||
      !shared.isCurrent(captured.claim) ||
      captured.payload.provider_id !== authority.providerId ||
      captured.payload.connection_id !== authority.connectionId ||
      captured.payload.provider_model_id !== model.id
    )
      return
    const csrf = boundary.csrf()
    if (!csrf) return
    const operation = { controller: new AbortController(), token: boundary.capture() }
    pending.current = operation
    setBusy(true)
    setConfirmation(null)
    message('transportEvidence.uncertain')
    try {
      const result = await saveProviderModelState(
        model.id,
        captured.payload.input,
        csrf,
        operation.controller.signal,
      )
      if (
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        !boundary.current(operation.token) ||
        !boundary.writable() ||
        !shared.isCurrent(captured.claim)
      )
        return
      if (!shared.clear(captured.claim)) return
      setIntent(null)
      setReviewed(result)
      setEnabled(result.enabled)
      setImage(result.supports_image_input)
      setPdf(result.supports_pdf_input)
      setConflict(false)
      message('transportEvidence.stateSaved')
      // Invalidate only after current mounted actor/target/Session authority accepts the exact result.
      void boundary.cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
    } catch (error) {
      if (
        pending.current !== operation ||
        operation.controller.signal.aborted ||
        !boundary.current(operation.token)
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
          key: JSON.stringify(authority.catalogueKey),
          count: boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0,
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
  async function review(separateChange = false) {
    if (!boundary.readable() || pending.current || (intent && !separateChange)) return
    const token = boundary.capture()
    const initial = boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0
    if (separateChange) setSeparate({ key: JSON.stringify(authority.catalogueKey), count: initial })
    try {
      const result = await reload()
      // A fresh model is published by the parent query. Never let a late refresh callback replace private state.
      if (!intent && result && boundary.current(token)) {
        setReviewed(result)
        setConflict(false)
        message('transportEvidence.reviewed')
      }
    } catch {
      if (boundary.current(token)) message('transportEvidence.reviewFailed')
    }
  }
  const canReview = () =>
    !conflictRead.current ||
    conflictRead.current.key !== JSON.stringify(authority.catalogueKey) ||
    (boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0) >
      conflictRead.current.count
  function adoptReview() {
    if (
      !canReview() ||
      !boundary.writable() ||
      intent ||
      pending.current ||
      shared?.recover(authority.actor)
    )
      return
    setReviewed(model)
    conflictRead.current = null
    setConflictSnapshot(null)
    setConflict(false)
    message('transportEvidence.reviewed')
  }
  const canAbandon = () =>
    !!separate &&
    boundary.readable() &&
    !!intent &&
    !pending.current &&
    shared?.isCurrent(intent.claim) === true &&
    separate.key === JSON.stringify(authority.catalogueKey) &&
    (boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0) > separate.count
  function abandon() {
    if (!canAbandon() || !intent || !shared?.clear(intent.claim)) return
    setIntent(null)
    setSeparate(null)
    setReviewed(model)
    setConflict(false)
    message('transportEvidence.abandoned')
  }
  // These reactive facts control rendering; imperative helpers above remain
  // authoritative when an event callback dispatches or changes an intent.
  const prepareAvailable =
    boundary.renderWritable && !!shared && !retained && !intent && !busy && !stale
  const reviewAvailable =
    !conflictSnapshot ||
    conflictSnapshot.key !== JSON.stringify(authority.catalogueKey) ||
    (boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0) >
      conflictSnapshot.count
  const abandonAvailable =
    !!separate &&
    readable &&
    !!intent &&
    !busy &&
    shared?.isCurrent(intent.claim) === true &&
    separate.key === JSON.stringify(authority.catalogueKey) &&
    (boundary.cache.getQueryState(authority.catalogueKey)?.dataUpdateCount ?? 0) > separate.count
  if (!readable) return null
  const unlocked = boundary.renderWritable && !busy && !intent
  return (
    <section className="space-y-4 rounded-lg border p-6" aria-label={t('supplyState')}>
      <h3 className="font-semibold">{t('supplyState')}</h3>
      <p className="text-sm text-muted-foreground">{t('supplyStateHelp')}</p>
      <dl className="grid gap-3 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-muted-foreground">{t('supplyState')}</dt>
          <dd>{t(model.enabled ? 'supplyEnabled' : 'supplyDisabled')}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('supportsImageInput')}</dt>
          <dd>{t(model.supports_image_input ? 'supported' : 'unsupported')}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t('supportsPdfInput')}</dt>
          <dd>{t(model.supports_pdf_input ? 'supported' : 'unsupported')}</dd>
        </div>
      </dl>
      <p
        role={model.capabilities_transport_current === false ? 'alert' : undefined}
        className="text-sm text-muted-foreground"
      >
        {copy(
          model.capabilities_transport_current === true
            ? 'transportEvidence.capabilitiesCurrent'
            : model.capabilities_transport_current === false
              ? 'transportEvidence.capabilitiesStale'
              : 'transportEvidence.capabilitiesUnknown',
        )}
      </p>
      {boundary.renderWritable && (
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            prepare()
          }}
        >
          <label className="flex items-center gap-3 text-sm">
            <Switch
              aria-label={t('enableSupply')}
              checked={enabled}
              onCheckedChange={(value) => {
                if (unlocked && !pending.current) setEnabled(value)
              }}
              disabled={!unlocked}
            />
            {t('enableSupply')}
          </label>
          <fieldset className="space-y-3">
            <legend className="text-sm font-medium">{t('inputCapabilities')}</legend>
            <label className="flex items-center gap-3 text-sm">
              <Switch
                aria-label={t('supportsImageInput')}
                checked={image}
                onCheckedChange={(value) => {
                  if (unlocked && !pending.current) setImage(value)
                }}
                disabled={!unlocked}
              />
              {t('supportsImageInput')}
            </label>
            <label className="flex items-center gap-3 text-sm">
              <Switch
                aria-label={t('supportsPdfInput')}
                checked={pdf}
                onCheckedChange={(value) => {
                  if (unlocked && !pending.current) setPdf(value)
                }}
                disabled={!unlocked}
              />
              {t('supportsPdfInput')}
            </label>
          </fieldset>
          {stale && !intent && <p role="alert">{copy('transportEvidence.stale')}</p>}
          {intent && <p role="alert">{copy('transportEvidence.uncertain')}</p>}
          {notice?.generation === authority.generation && <p role="status">{copy(notice.key)}</p>}
          <div className="flex flex-wrap gap-2">
            <Button
              type="submit"
              disabled={
                !prepareAvailable ||
                (!availabilityChanged && !capabilitiesChanged) ||
                (capabilitiesChanged && !capabilityReview)
              }
            >
              {t('saveState')}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={!prepareAvailable || !capabilityReview}
              onClick={() => prepare(true)}
            >
              {copy('transportEvidence.reaffirmCapabilities')}
            </Button>
            {stale && !intent && (
              <>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!boundary.renderReadable || busy}
                  onClick={() => void review()}
                >
                  {t('reload')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!boundary.renderWritable || busy || !reviewAvailable}
                  onClick={adoptReview}
                >
                  {copy('transportEvidence.review')}
                </Button>
              </>
            )}
            {intent && (
              <>
                <Button
                  type="button"
                  disabled={!boundary.renderWritable || busy}
                  onClick={() => void dispatch(true)}
                >
                  {copy('transportEvidence.retry')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={!boundary.renderReadable || busy}
                  onClick={() => void review(true)}
                >
                  {copy('transportEvidence.separate')}
                </Button>
              </>
            )}
          </div>
        </form>
      )}
      <Dialog
        open={!!confirmation}
        onOpenChange={(open) => {
          if (!open) cancel()
        }}
        busy={busy}
        title={copy('transportEvidence.confirmState')}
        description={copy(
          confirmation?.capability_review_etag !== undefined
            ? confirmation.enabled !== undefined
              ? 'transportEvidence.confirmCombined'
              : 'transportEvidence.confirmCapabilities'
            : 'transportEvidence.confirmAvailability',
        )}
      >
        <dl className="space-y-2 text-sm">
          {confirmation?.enabled !== undefined && (
            <div>
              <dt>{t('supplyState')}</dt>
              <dd>{t(confirmation.enabled ? 'supplyEnabled' : 'supplyDisabled')}</dd>
            </div>
          )}
          {confirmation?.capability_review_etag !== undefined && (
            <>
              <div>
                <dt>{t('supportsImageInput')}</dt>
                <dd>{t(confirmation.supports_image_input ? 'supported' : 'unsupported')}</dd>
              </div>
              <div>
                <dt>{t('supportsPdfInput')}</dt>
                <dd>{t(confirmation.supports_pdf_input ? 'supported' : 'unsupported')}</dd>
              </div>
            </>
          )}
        </dl>
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="outline" onClick={cancel}>
            {t('cancel')}
          </Button>
          <Button disabled={!prepareAvailable} onClick={() => void dispatch(false)}>
            {copy('transportEvidence.confirm')}
          </Button>
        </div>
      </Dialog>
      <Dialog
        open={!!separate}
        onOpenChange={(open) => {
          if (!open && !pending.current) setSeparate(null)
        }}
        busy={busy}
        title={copy('transportEvidence.abandonTitle')}
        description={copy('transportEvidence.abandonDescription')}
      >
        <p>
          {copy('transportEvidence.currentModel', {
            name: model.upstream_name,
            status: t(model.enabled ? 'supplyEnabled' : 'supplyDisabled'),
          })}
        </p>
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
    </section>
  )
}
