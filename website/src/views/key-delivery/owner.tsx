import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import {
  getKeyDeliveryContext,
  getKeyDeliveryTarget,
  issueManagedKey,
  resolveKeyDeliveryTarget,
} from '@/api/key-delivery'
import type {
  KeyDeliveryContext,
  KeyDeliveryMode,
  KeyDeliveryOwner,
  KeyDeliveryReceipt,
  KeyDeliveryResolveInput,
  KeyDeliveryTarget,
  ManagedKeyInput,
} from '@/types/key-delivery'
import type { PersonalKey } from '@/types/catalog'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FormField } from '@/components/app/CatalogUI'
import type { OwnerAuthority, OwnerTicket } from './lifetime'
import DeliveryReceiptDialog from './receipt'

function exactText(value: string, bytes: number, runes = bytes) {
  return (
    !!value &&
    new TextEncoder().encode(value).length <= bytes &&
    [...value].length <= runes &&
    !/[\p{Cc}\p{Cs}]/u.test(value)
  )
}

export interface ManualKeyInput {
  name: string
  model_ids: string[]
  expires_at: string | null
}
type Intent =
  | { kind: 'resolve'; input: KeyDeliveryResolveInput; etag: string }
  | { kind: 'issue'; input: ManagedKeyInput; etag: string; predecessor?: string }
interface Props {
  owner: KeyDeliveryOwner
  authority: OwnerAuthority
  models: { id: string; name: string }[]
  predecessor?: PersonalKey
  onManual: (input: ManualKeyInput) => Promise<void>
  onReceipt: (receipt: KeyDeliveryReceipt) => void
  onClose: () => void
  title: string
  description: string
  manualLabel: string
}

export default function KeyIssueDialog({
  owner,
  authority,
  models,
  predecessor,
  onManual,
  onReceipt,
  onClose,
  title,
  description,
  manualLabel,
}: Props) {
  const { t } = useTranslation('keyDeliveryOwner')
  const targetStatusLabels = {
    pending: t('target_pending'),
    ready: t('target_ready'),
    error: t('target_error'),
  }
  const isolationStatusLabels = {
    missing: t('isolation_missing'),
    operator_attested: t('isolation_operator_attested'),
    revoked: t('isolation_revoked'),
    stale: t('isolation_stale'),
  }
  const keyStatusLabels = {
    pending: t('key_pending'),
    active: t('key_active'),
    disabled: t('key_disabled'),
    revoked: t('key_revoked'),
  }
  const lifetime = useRef({
    mounted: true,
    turn: 0,
    busy: false,
    confirmation: null as Intent | null,
    actor: authority.actor,
    ticket: null as OwnerTicket | null,
  })
  const [context, setContext] = useState<KeyDeliveryContext | null>(null)
  const [mode, setMode] = useState<KeyDeliveryMode | ''>(predecessor?.delivery_mode ?? '')
  const [name, setName] = useState('')
  const [expiry, setExpiry] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [application, setApplication] = useState('')
  const [environment, setEnvironment] = useState('')
  const [subject, setSubject] = useState('')
  const [reason, setReason] = useState('')
  const [consumerId, setConsumerId] = useState('')
  const [targetId, setTargetId] = useState('')
  const [reviewReceipt, setReviewReceipt] = useState(false)
  const [target, setTarget] = useState<KeyDeliveryTarget | null>(null)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [confirmation, setConfirmation] = useState<Intent | null>(null)
  const [pending, setPending] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  useEffect(() => {
    const cell = lifetime.current
    cell.mounted = true
    return () => {
      cell.mounted = false
      cell.turn++
      cell.confirmation = null
      cell.ticket?.controller.abort()
    }
  }, [lifetime])
  useEffect(() => {
    lifetime.current.turn++
    lifetime.current.busy = false
    lifetime.current.confirmation = null
    lifetime.current.ticket?.controller.abort()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- authority renewal destroys private facts and the captured confirmation before another operation
    setPending(false)
    setConfirmation(null)
    setContext(null)
    setTarget(null)
    if (lifetime.current.actor !== authority.actor) {
      lifetime.current.actor = authority.actor
      setIntent(null)
      setConsumerId('')
      setTargetId('')
      setReviewReceipt(false)
      setSubject('')
      setReason('')
      setApplication('')
      setEnvironment('')
      setName('')
      setSelected([])
    }
    if (!authority.fresh) return
    let ticket: OwnerTicket
    try {
      ticket = authority.capture()
    } catch {
      return
    }
    lifetime.current.ticket = ticket
    const turn = lifetime.current.turn
    void getKeyDeliveryContext(owner, ticket.controller.signal)
      .then((value) => {
        if (
          !lifetime.current.mounted ||
          turn !== lifetime.current.turn ||
          !authority.current(ticket)
        )
          return
        setContext(value)
        setMode((current) =>
          predecessor ? (predecessor.delivery_mode ?? 'manual') : current || value.default_mode,
        )
      })
      .catch(() => {
        if (lifetime.current.mounted && turn === lifetime.current.turn && authority.current(ticket))
          setNotice('readFailed')
      })
      .finally(() => authority.finish(ticket))
    return () => ticket.controller.abort()
  }, [authority, owner, predecessor, lifetime])
  function start(needsContext = true) {
    if (
      !lifetime.current.mounted ||
      lifetime.current.busy ||
      !authority.fresh ||
      (needsContext && !context)
    )
      return null
    let ticket: OwnerTicket
    try {
      ticket = authority.capture()
    } catch {
      return null
    }
    lifetime.current.busy = true
    lifetime.current.ticket = ticket
    setPending(true)
    setNotice(null)
    return { ticket, turn: ++lifetime.current.turn }
  }
  function valid(op: { ticket: OwnerTicket; turn: number }) {
    return (
      lifetime.current.mounted && lifetime.current.turn === op.turn && authority.current(op.ticket)
    )
  }
  function finish(op: { ticket: OwnerTicket; turn: number }) {
    authority.finish(op.ticket)
    if (lifetime.current.mounted && lifetime.current.turn === op.turn) {
      lifetime.current.busy = false
      setPending(false)
    }
  }
  function review(value: Intent) {
    if (!lifetime.current.mounted || lifetime.current.busy || !authority.fresh) return
    try {
      const ticket = authority.capture()
      authority.finish(ticket)
    } catch {
      return
    }
    lifetime.current.confirmation = value
    setConfirmation(value)
  }
  function dismissReview() {
    if (lifetime.current.busy) return
    lifetime.current.confirmation = null
    setConfirmation(null)
  }
  async function dispatch(value: Intent) {
    // A retained pre-dismiss Confirm closure must never admit another request.
    if (
      !lifetime.current.mounted ||
      lifetime.current.confirmation !== value ||
      lifetime.current.busy
    )
      return
    const op = start()
    if (!op) return
    lifetime.current.confirmation = null
    setConfirmation(null)
    setIntent(value)
    try {
      if (value.kind === 'resolve') {
        const result = await resolveKeyDeliveryTarget(
          owner,
          value.input,
          value.etag,
          op.ticket.csrf,
          op.ticket.controller.signal,
        )
        if (!valid(op)) return
        setTarget(result)
        setTargetId(result.id)
        setConsumerId(result.consumer_id)
        setIntent(null)
      } else {
        const result = await issueManagedKey(
          owner,
          value.input,
          value.etag,
          op.ticket.csrf,
          op.ticket.controller.signal,
          value.predecessor,
        )
        if (!valid(op)) return
        setIntent(null)
        onReceipt(result.delivery)
      }
    } catch {
      if (valid(op)) setNotice('uncertain')
    } finally {
      finish(op)
    }
  }
  function prepareResolve() {
    if (
      !context ||
      !exactText(application.trim(), 400, 100) ||
      !/^[a-zA-Z0-9_-]{1,64}$/.test(environment) ||
      !exactText(subject, 256) ||
      !exactText(reason.trim(), 4000, 1000)
    ) {
      setNotice('fieldsRequired')
      return
    }
    review({
      kind: 'resolve',
      etag: context.review_etag,
      input: {
        request_id: crypto.randomUUID(),
        consumer_id: consumerId,
        application_name: application.trim(),
        environment,
        principal: { method: 'jwt', subject },
        reason: reason.trim(),
      },
    })
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!authority.fresh || !context || lifetime.current.busy || intent) return
    if (expiry && (!Number.isFinite(Date.parse(expiry)) || Date.parse(expiry) <= Date.now())) {
      setNotice('expiryInvalid')
      return
    }
    const metadata: ManualKeyInput = predecessor
      ? { name: '', model_ids: [], expires_at: null }
      : {
          name: name.trim(),
          model_ids: [...selected],
          expires_at: expiry ? new Date(expiry).toISOString() : null,
        }
    if (!predecessor && !selected.length) {
      setNotice('scopeRequired')
      return
    }
    if (!predecessor && !metadata.name) {
      setNotice('metadataRequired')
      return
    }
    if (mode === 'manual') {
      if (!context.allowed_modes.includes('manual')) return
      const op = start()
      if (!op) return
      void onManual(metadata)
        .catch(() => {
          if (valid(op)) setNotice('requestFailed')
        })
        .finally(() => finish(op))
      return
    }
    if (selected.length > 128 && !predecessor) {
      setNotice('scopeTooLarge')
      return
    }
    if (
      mode !== 'managed_vault' ||
      !context.allowed_modes.includes(mode) ||
      !context.managed_available ||
      target?.status !== 'ready' ||
      !target.control_verified ||
      target.isolation.status !== 'operator_attested' ||
      !exactText(subject, 256) ||
      !exactText(reason.trim(), 4000, 1000)
    ) {
      setNotice('notReady')
      return
    }
    review({
      kind: 'issue',
      etag: context.review_etag,
      predecessor: predecessor?.id,
      input: {
        ...metadata,
        request_id: crypto.randomUUID(),
        reason: reason.trim(),
        delivery: {
          consumer_id: target.consumer_id,
          environment: target.environment,
          principal: { method: 'jwt', subject },
          target_review: target.review_etag,
        },
      },
    })
  }
  async function readFacts() {
    const op = start(false)
    if (!op) return
    setTarget(null)
    try {
      const fresh = await getKeyDeliveryContext(owner, op.ticket.controller.signal)
      const current = targetId
        ? await getKeyDeliveryTarget(owner, targetId, op.ticket.controller.signal)
        : null
      if (!valid(op)) return
      setContext(fresh)
      setTarget(current)
      setMode((value) => value || fresh.default_mode)
      setNotice(intent ? 'originalRetained' : null)
    } catch {
      if (valid(op)) setNotice('readFailed')
    } finally {
      finish(op)
    }
  }
  const locked = pending || !!intent
  return (
    <>
      <Dialog
        open={authority.fresh && !reviewReceipt}
        onOpenChange={(open) => {
          if (!open && !lifetime.current.busy) {
            lifetime.current.confirmation = null
            lifetime.current.turn++
            onClose()
          }
        }}
        title={title}
        description={description}
        busy={pending}
        width={predecessor ? 560 : 800}
      >
        <form onSubmit={submit} className="space-y-5">
          {!context ? (
            <>
              <p role="status">{t('reading')}</p>
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => void readFacts()}
              >
                {t('readFacts')}
              </Button>
            </>
          ) : (
            <>
              <fieldset disabled={locked} className="space-y-5">
                {!predecessor ? (
                  <>
                    <FormField label={t('name')}>
                      <Input
                        name="name"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        required
                        maxLength={100}
                      />
                    </FormField>
                    <fieldset className="space-y-2">
                      <legend className="mb-2 text-sm font-medium">{t('models')}</legend>
                      {models.map((model) => (
                        <label key={model.id} className="flex items-center gap-2 text-sm">
                          <input
                            type="checkbox"
                            name="model_ids"
                            value={model.id}
                            checked={selected.includes(model.id)}
                            onChange={(e) =>
                              setSelected((current) =>
                                e.target.checked
                                  ? [...current, model.id]
                                  : current.filter((id) => id !== model.id),
                              )
                            }
                          />
                          {model.name}
                        </label>
                      ))}
                    </fieldset>
                    <FormField label={t('expiry')}>
                      <Input
                        name="expires_at"
                        type="datetime-local"
                        value={expiry}
                        onChange={(e) => setExpiry(e.target.value)}
                      />
                    </FormField>
                  </>
                ) : (
                  <p className="rounded-md border bg-muted/30 p-3 text-sm">
                    {t('rotation', {
                      name: predecessor.name,
                      status: keyStatusLabels[predecessor.status],
                    })}
                  </p>
                )}
                <FormField label={t('mode')}>
                  <select
                    aria-label={t('mode')}
                    value={mode}
                    disabled={!!predecessor}
                    onChange={(e) => {
                      setMode(e.target.value as KeyDeliveryMode)
                      setTarget(null)
                    }}
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                  >
                    <option value="">{t('chooseMode')}</option>
                    {context.allowed_modes.map((value) => (
                      <option key={value} value={value}>
                        {t(value)}
                      </option>
                    ))}
                  </select>
                </FormField>
                {mode === 'managed_vault' && (
                  <>
                    <p className="text-sm text-muted-foreground">{t('jwtOnly')}</p>
                    <FormField label={t('application')}>
                      <Input
                        name="application_name"
                        value={application}
                        maxLength={100}
                        onChange={(e) => {
                          setApplication(e.target.value)
                          setTarget(null)
                          setConsumerId('')
                          setTargetId('')
                        }}
                      />
                    </FormField>
                    <FormField label={t('environment')}>
                      <Input
                        name="environment"
                        value={environment}
                        maxLength={64}
                        onChange={(e) => {
                          setEnvironment(e.target.value)
                          setTarget(null)
                          setTargetId('')
                        }}
                      />
                    </FormField>
                    <FormField label={t('subject')}>
                      <Input
                        name="subject"
                        value={subject}
                        maxLength={256}
                        autoComplete="off"
                        onChange={(e) => {
                          setSubject(e.target.value)
                          setTarget(null)
                          setTargetId('')
                        }}
                      />
                    </FormField>
                    <FormField label={t('reason')}>
                      <Input
                        name="reason"
                        value={reason}
                        maxLength={1000}
                        onChange={(e) => setReason(e.target.value)}
                      />
                    </FormField>
                    <Button type="button" variant="outline" onClick={prepareResolve}>
                      {t('resolve')}
                    </Button>
                  </>
                )}
              </fieldset>
              {mode === 'managed_vault' && (
                <>
                  {!context.managed_available && <p role="status">{t('unavailable')}</p>}
                  {target && (
                    <dl className="space-y-2 text-sm">
                      <div>
                        <dt>{t('consumer')}</dt>
                        <dd className="break-all font-mono">{target.consumer_id}</dd>
                      </div>
                      <div>
                        <dt>{t('targetStatus')}</dt>
                        <dd>{targetStatusLabels[target.status]}</dd>
                      </div>
                      <div>
                        <dt>{t('control')}</dt>
                        <dd>{t(target.control_verified ? 'recorded' : 'missing')}</dd>
                      </div>
                      <div>
                        <dt>{t('isolation')}</dt>
                        <dd>{isolationStatusLabels[target.isolation.status]}</dd>
                      </div>
                    </dl>
                  )}
                  <Button
                    type="button"
                    variant="outline"
                    disabled={pending}
                    onClick={() => void readFacts()}
                  >
                    {t('readFacts')}
                  </Button>
                </>
              )}
              {intent && (
                <div className="space-y-3 rounded-md border p-3">
                  <p role="status">{t('uncertain')}</p>
                  <Button type="button" disabled={pending} onClick={() => review(intent)}>
                    {t('retryOriginal')}
                  </Button>
                  {intent.kind === 'issue' && (
                    <Button
                      type="button"
                      variant="outline"
                      disabled={pending}
                      onClick={() => setReviewReceipt(true)}
                    >
                      {t('readReceipt')}
                    </Button>
                  )}
                </div>
              )}
              <Button
                type="submit"
                disabled={
                  locked ||
                  !mode ||
                  (mode === 'managed_vault' &&
                    (!context.managed_available || target?.status !== 'ready'))
                }
              >
                {pending
                  ? t('pending')
                  : predecessor
                    ? t('rotate')
                    : mode === 'managed_vault'
                      ? t('reviewIssue')
                      : manualLabel}
              </Button>
            </>
          )}
          {notice && (
            <p role="alert" className="text-sm text-destructive">
              {t(notice)}
            </p>
          )}
        </form>
      </Dialog>
      {reviewReceipt && intent?.kind === 'issue' && (
        <DeliveryReceiptDialog
          owner={owner}
          authority={authority}
          requestId={intent.input.request_id}
          keyId=""
          onClose={() => setReviewReceipt(false)}
          onChanged={() => {}}
          onResolved={(value) => {
            if (value.request_id !== intent.input.request_id) return
            setIntent(null)
            onReceipt(value)
          }}
        />
      )}
      <Dialog
        open={authority.fresh && !!confirmation}
        onOpenChange={(open) => {
          if (!open) dismissReview()
        }}
        title={t('confirmTitle')}
        description={t(confirmation?.kind === 'resolve' ? 'confirmResolve' : 'confirmIssue')}
        busy={pending}
      >
        <p className="break-all text-sm">{confirmation?.input.reason}</p>
        <div className="mt-5 flex justify-end gap-3">
          <Button variant="outline" onClick={dismissReview}>
            {t('cancel')}
          </Button>
          <Button
            onClick={() => {
              if (confirmation) void dispatch(confirmation)
            }}
          >
            {t('confirm')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
