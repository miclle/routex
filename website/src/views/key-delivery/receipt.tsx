import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  getKeyDeliveryReceipt,
  getKeyDeliveryReceiptForKey,
  reconcileManagedKey,
} from '@/api/key-delivery'
import type {
  KeyDeliveryDescriptor,
  KeyDeliveryOwner,
  KeyDeliveryReceipt,
} from '@/types/key-delivery'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FormField } from '@/components/app/CatalogUI'
import type { OwnerAuthority, OwnerTicket } from './lifetime'
import { ApplicationConfiguration } from './application-config'

type ReconcileIntent = {
  keyId: string
  etag: string
  input: { request_id: string; reason: string }
}

export default function DeliveryReceiptDialog({
  owner,
  authority,
  requestId,
  keyId,
  onClose,
  onChanged,
  onResolved,
}: {
  owner: KeyDeliveryOwner
  authority: OwnerAuthority
  requestId?: string
  keyId: string
  onClose: () => void
  onChanged: () => void
  onResolved?: (receipt: KeyDeliveryReceipt) => void
}) {
  const { t } = useTranslation('keyDeliveryOwner')
  const target = JSON.stringify([
    owner.kind,
    owner.kind === 'project' ? owner.projectId : '',
    keyId,
    requestId ?? '',
  ])
  const deliveryStatusLabels = {
    pending: t('delivery_pending'),
    acknowledged: t('delivery_acknowledged'),
    unknown: t('delivery_unknown'),
    not_attempted: t('delivery_not_attempted'),
  }
  const keyStatusLabels = {
    pending: t('key_pending'),
    active: t('key_active'),
    disabled: t('key_disabled'),
    revoked: t('key_revoked'),
  }
  const runtimeStatusLabels = {
    pending: t('runtime_pending'),
    confirmed: t('runtime_confirmed'),
    unknown: t('runtime_unknown'),
  }
  const replacementCallStatusLabels = {
    not_applicable: t('replacementCall_not_applicable'),
    not_recorded: t('replacementCall_not_recorded'),
    completed: t('replacementCall_completed'),
  }
  const life = useRef({
    mounted: true,
    turn: 0,
    busy: false,
    confirmed: null as ReconcileIntent | null,
    actor: authority.actor,
    authority,
    target,
    ticket: null as OwnerTicket | null,
    factGeneration: 0,
    copyFact: null as { value: KeyDeliveryDescriptor; generation: number } | null,
    receiptValue: null as KeyDeliveryReceipt | null,
  })
  const [receiptFact, setReceipt] = useState<{
    value: KeyDeliveryReceipt
    target: string
    authority: OwnerAuthority
    generation: number
  } | null>(null)
  const receipt =
    authority.fresh && receiptFact?.target === target && receiptFact.authority === authority
      ? receiptFact.value
      : null
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [confirm, setConfirm] = useState<ReconcileIntent | null>(null)
  const [intent, setIntent] = useState<ReconcileIntent | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  useLayoutEffect(() => {
    const cell = life.current
    cell.mounted = true
    return () => {
      cell.mounted = false
      cell.turn++
      cell.factGeneration++
      cell.copyFact = null
      cell.receiptValue = null
      cell.confirmed = null
      cell.ticket?.controller.abort()
    }
  }, [life])
  useLayoutEffect(() => {
    life.current.turn++
    life.current.factGeneration++
    life.current.copyFact = null
    life.current.receiptValue = null
    life.current.busy = false
    life.current.confirmed = null
    life.current.ticket?.controller.abort()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- authority renewal destroys private facts and the captured confirmation before another operation
    setPending(false)
    setConfirm(null)
    setReceipt(null)
    if (life.current.actor !== authority.actor || life.current.target !== target) {
      setIntent(null)
      setReason('')
      setNotice(null)
    }
    life.current.actor = authority.actor
    life.current.authority = authority
    life.current.target = target
  }, [authority, life, target])
  function begin() {
    if (
      !life.current.mounted ||
      life.current.busy ||
      !authority.fresh ||
      life.current.authority !== authority ||
      life.current.target !== target
    )
      return null
    let ticket: OwnerTicket
    try {
      ticket = authority.capture()
    } catch {
      return null
    }
    life.current.busy = true
    life.current.ticket = ticket
    setPending(true)
    setNotice(null)
    return { ticket, turn: ++life.current.turn }
  }
  function current(op: { ticket: OwnerTicket; turn: number }) {
    return (
      life.current.mounted &&
      life.current.authority === authority &&
      life.current.target === target &&
      life.current.turn === op.turn &&
      authority.current(op.ticket)
    )
  }
  function finish(op: { ticket: OwnerTicket; turn: number }) {
    authority.finish(op.ticket)
    if (life.current.mounted && life.current.turn === op.turn) {
      life.current.busy = false
      setPending(false)
    }
  }
  async function read() {
    const op = begin()
    if (!op) return
    // Invalidate retained copy callbacks synchronously, before React clears the
    // displayed fact. Authority can remain identical across this new read.
    const generation = ++life.current.factGeneration
    life.current.copyFact = null
    life.current.receiptValue = null
    life.current.confirmed = null
    setConfirm(null)
    setReceipt(null)
    try {
      const value = requestId
        ? await getKeyDeliveryReceipt(owner, requestId, op.ticket.controller.signal)
        : await getKeyDeliveryReceiptForKey(owner, keyId, op.ticket.controller.signal)
      // An uncertain issuance has no Key ID yet; its exact owner/request read
      // supplies it. A row reopened by Key ID must preserve that reviewed Key.
      if (keyId !== '' && value.key_id !== keyId)
        throw new Error('Receipt does not match the reviewed Key')
      if (current(op)) {
        life.current.receiptValue = value
        life.current.copyFact = value.descriptor ? { value: value.descriptor, generation } : null
        setReceipt({ value, target, authority, generation })
      }
    } catch {
      if (current(op)) setNotice('receiptUnavailable')
    } finally {
      finish(op)
    }
  }
  async function reconcile(value: ReconcileIntent) {
    if (
      !life.current.mounted ||
      life.current.confirmed !== value ||
      value.keyId === '' ||
      (keyId !== '' && value.keyId !== keyId) ||
      (keyId === '' && !requestId) ||
      (requestId !== undefined && value.input.request_id !== requestId)
    )
      return
    life.current.confirmed = null
    setConfirm(null)
    const op = begin()
    if (!op) return
    const generation = ++life.current.factGeneration
    life.current.copyFact = null
    life.current.receiptValue = null
    setIntent(value)
    setReceipt(null)
    try {
      const result = await reconcileManagedKey(
        owner,
        value.keyId,
        value.input,
        value.etag,
        op.ticket.csrf,
        op.ticket.controller.signal,
      )
      if (!current(op)) return
      if (result.delivery.key_id !== value.keyId)
        throw new Error('Receipt does not match the reviewed Key')
      setIntent(null)
      life.current.receiptValue = result.delivery
      life.current.copyFact = result.delivery.descriptor
        ? { value: result.delivery.descriptor, generation }
        : null
      setReceipt({ value: result.delivery, target, authority, generation })
      onChanged()
    } catch {
      if (current(op)) setNotice('reconcileFailed')
    } finally {
      finish(op)
    }
  }
  async function copy(value: KeyDeliveryDescriptor, generation: number, configuration?: string) {
    if (
      life.current.busy ||
      life.current.factGeneration !== generation ||
      life.current.copyFact?.generation !== generation ||
      life.current.copyFact.value !== value
    )
      return
    const op = begin()
    if (!op) return
    try {
      await navigator.clipboard.writeText(configuration ?? JSON.stringify(value, null, 2))
      if (current(op)) setNotice(configuration === undefined ? 'copied' : 'configurationCopied')
    } catch {
      if (current(op))
        setNotice(configuration === undefined ? 'copyFailed' : 'configurationCopyFailed')
    } finally {
      finish(op)
    }
  }
  const value = receipt?.descriptor
  const copyGeneration = receiptFact?.generation ?? -1
  return (
    <>
      <Dialog
        open={authority.fresh}
        onOpenChange={(open) => {
          if (
            !open &&
            !life.current.busy &&
            life.current.authority === authority &&
            life.current.target === target
          ) {
            life.current.turn++
            life.current.confirmed = null
            onClose()
          }
        }}
        title={t('receiptTitle')}
        description={t('receiptDescription')}
        busy={pending}
        width={640}
      >
        <div className="space-y-4">
          <Button variant="outline" disabled={pending} onClick={() => void read()}>
            {t('readReceipt')}
          </Button>
          {receipt && (
            <dl className="space-y-2 text-sm">
              <div>
                <dt>{t('requestId')}</dt>
                <dd className="break-all font-mono">{receipt.request_id}</dd>
              </div>
              <div>
                <dt>{t('deliveryStatus')}</dt>
                <dd>{deliveryStatusLabels[receipt.delivery_status]}</dd>
              </div>
              <div>
                <dt>{t('keyStatus')}</dt>
                <dd>{keyStatusLabels[receipt.key_status]}</dd>
              </div>
              <div>
                <dt>{t('runtimeStatus')}</dt>
                <dd>{runtimeStatusLabels[receipt.runtime_status]}</dd>
              </div>
              <div>
                <dt>{t('replacementCallStatus')}</dt>
                <dd>{replacementCallStatusLabels[receipt.replacement_call_status]}</dd>
                <dd className="text-muted-foreground">{t('replacementCallBoundary')}</dd>
              </div>
            </dl>
          )}
          {value && (
            <>
              <ApplicationConfiguration
                descriptor={value}
                pending={pending}
                onCopy={(text) => void copy(value, copyGeneration, text)}
              />
              <details className="rounded-md border p-3">
                <summary className="cursor-pointer text-sm font-medium">
                  {t('deliveryDetails')}
                </summary>
                <pre className="mt-3 max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs">
                  {JSON.stringify(value, null, 2)}
                </pre>
                <Button
                  className="mt-3"
                  variant="outline"
                  disabled={pending}
                  onClick={() => void copy(value, copyGeneration)}
                >
                  {t('copyDescriptor')}
                </Button>
              </details>
              <p>{t('descriptorBoundary')}</p>
            </>
          )}
          {receipt?.delivery_status === 'acknowledged' &&
            receipt.runtime_status !== 'confirmed' &&
            receipt.key_status !== 'revoked' && (
              <>
                <FormField label={t('reason')}>
                  <Input
                    name="reconcile_reason"
                    value={reason}
                    maxLength={1000}
                    disabled={!!intent}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </FormField>
                <Button
                  disabled={pending || !reason.trim()}
                  onClick={() => {
                    if (
                      !life.current.mounted ||
                      life.current.authority !== authority ||
                      life.current.target !== target ||
                      !authority.fresh ||
                      life.current.busy ||
                      life.current.receiptValue !== receipt ||
                      life.current.factGeneration !== receiptFact?.generation ||
                      !reason.trim() ||
                      /[\p{Cc}\p{Cs}]/u.test(reason)
                    )
                      return
                    try {
                      const ticket = authority.capture()
                      authority.finish(ticket)
                    } catch {
                      return
                    }
                    const value = intent ?? {
                      keyId: receipt.key_id,
                      etag: receipt.review_etag,
                      input: { request_id: receipt.request_id, reason: reason.trim() },
                    }
                    life.current.confirmed = value
                    setConfirm(value)
                  }}
                >
                  {t(intent ? 'retryOriginal' : 'reviewReconcile')}
                </Button>
              </>
            )}
          {onResolved &&
            receipt?.runtime_status === 'confirmed' &&
            (receipt.key_status === 'active' || receipt.key_status === 'disabled') && (
              <Button
                disabled={pending}
                onClick={() => {
                  if (
                    !life.current.mounted ||
                    life.current.busy ||
                    life.current.authority !== authority ||
                    life.current.target !== target ||
                    life.current.receiptValue !== receipt ||
                    life.current.factGeneration !== receiptFact?.generation
                  )
                    return
                  let ticket: OwnerTicket
                  try {
                    ticket = authority.capture()
                  } catch {
                    return
                  }
                  if (authority.current(ticket)) onResolved(receipt)
                  authority.finish(ticket)
                }}
              >
                {t('useReceipt')}
              </Button>
            )}
          {notice && <p role="status">{t(notice)}</p>}
        </div>
      </Dialog>
      <Dialog
        open={authority.fresh && !!confirm}
        onOpenChange={(open) => {
          if (!open && life.current.authority === authority && life.current.target === target) {
            life.current.confirmed = null
            setConfirm(null)
          }
        }}
        title={t('confirmTitle')}
        description={t('confirmReconcile')}
        busy={pending}
      >
        <Button
          onClick={() => {
            if (confirm) void reconcile(confirm)
          }}
        >
          {t('confirm')}
        </Button>
      </Dialog>
    </>
  )
}
