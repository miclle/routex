import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  getKeyDeliveryDescriptor,
  getKeyDeliveryReceipt,
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
  const life = useRef({
    mounted: true,
    turn: 0,
    busy: false,
    confirmed: null as ReconcileIntent | null,
    actor: authority.actor,
    ticket: null as OwnerTicket | null,
  })
  const [receipt, setReceipt] = useState<KeyDeliveryReceipt | null>(null)
  const [descriptor, setDescriptor] = useState<KeyDeliveryDescriptor | null>(null)
  const [reason, setReason] = useState('')
  const [pending, setPending] = useState(false)
  const [confirm, setConfirm] = useState<ReconcileIntent | null>(null)
  const [intent, setIntent] = useState<ReconcileIntent | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  useEffect(() => {
    const cell = life.current
    cell.mounted = true
    return () => {
      cell.mounted = false
      cell.turn++
      cell.confirmed = null
      cell.ticket?.controller.abort()
    }
  }, [life])
  useEffect(() => {
    life.current.turn++
    life.current.busy = false
    life.current.confirmed = null
    life.current.ticket?.controller.abort()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- authority renewal destroys private facts and the captured confirmation before another operation
    setPending(false)
    setConfirm(null)
    setReceipt(null)
    setDescriptor(null)
    if (life.current.actor !== authority.actor) {
      life.current.actor = authority.actor
      setIntent(null)
      setReason('')
    }
  }, [authority, life])
  function begin() {
    if (!life.current.mounted || life.current.busy || !authority.fresh) return null
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
    return life.current.mounted && life.current.turn === op.turn && authority.current(op.ticket)
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
    setReceipt(null)
    setDescriptor(null)
    try {
      if (requestId) {
        const value = await getKeyDeliveryReceipt(owner, requestId, op.ticket.controller.signal)
        if (current(op)) setReceipt(value)
      } else {
        const value = await getKeyDeliveryDescriptor(owner, keyId, op.ticket.controller.signal)
        if (current(op)) setDescriptor(value)
      }
    } catch {
      if (current(op)) setNotice('receiptUnavailable')
    } finally {
      finish(op)
    }
  }
  async function reconcile(value: ReconcileIntent) {
    if (!life.current.mounted || life.current.confirmed !== value) return
    life.current.confirmed = null
    setConfirm(null)
    const op = begin()
    if (!op) return
    setIntent(value)
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
      setIntent(null)
      setReceipt(result.delivery)
      onChanged()
    } catch {
      if (current(op)) setNotice('reconcileFailed')
    } finally {
      finish(op)
    }
  }
  async function copy() {
    const value = receipt?.descriptor ?? descriptor
    if (!value || life.current.busy) return
    const op = begin()
    if (!op) return
    try {
      await navigator.clipboard.writeText(JSON.stringify(value, null, 2))
      if (current(op)) setNotice('copied')
    } catch {
      if (current(op)) setNotice('copyFailed')
    } finally {
      finish(op)
    }
  }
  const value = receipt?.descriptor ?? descriptor
  return (
    <>
      <Dialog
        open={authority.fresh}
        onOpenChange={(open) => {
          if (!open && !life.current.busy) {
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
            </dl>
          )}
          {value && (
            <>
              <pre className="max-h-64 overflow-auto rounded-md border bg-muted p-3 text-xs">
                {JSON.stringify(value, null, 2)}
              </pre>
              <p>{t('descriptorBoundary')}</p>
              <Button variant="outline" disabled={pending} onClick={() => void copy()}>
                {t('copyDescriptor')}
              </Button>
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
                      !authority.fresh ||
                      life.current.busy ||
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
                  if (!life.current.mounted || life.current.busy) return
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
          if (!open) {
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
