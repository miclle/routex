import { useId, useLayoutEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  applyRuntimeArchiveRollback,
  getRuntimeArchiveRollbackReceipt,
  previewRuntimeArchiveRollback,
  validRuntimeArchiveRollbackReason,
} from '@/api/runtime-archive-rollback'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  RuntimeArchiveRollbackInput,
  RuntimeArchiveRollbackPreview,
  RuntimeArchiveRollbackReceipt,
} from '@/types/runtime-archive-rollback'

type Intent = { input: RuntimeArchiveRollbackInput; etag: string }
type View = {
  basis: string
  pending: boolean
  preview: RuntimeArchiveRollbackPreview | null
  receipt: RuntimeArchiveRollbackReceipt | null
  issue: 'preview' | 'uncertain' | 'receipt' | null
}

export function ArchiveRollback({
  actorID,
  authority,
  archiveID,
  archiveBirth,
  canRead,
  canWrite,
  onClose,
}: {
  actorID: string
  authority: string
  archiveID: string
  archiveBirth: string
  canRead: () => boolean
  canWrite: () => boolean
  onClose: () => void
}) {
  const { t } = useTranslation('systemStatus')
  const cache = useQueryClient()
  const reasonErrorID = useId()
  const target = JSON.stringify([actorID, archiveID, archiveBirth])
  const basis = JSON.stringify([target, authority])
  const [owner, setOwner] = useState(target)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [reason, setReason] = useState('')
  const [invalidReason, setInvalidReason] = useState(false)
  const [view, setView] = useState<View>({
    basis,
    pending: false,
    preview: null,
    receipt: null,
    issue: null,
  })
  // Rendered facts belong to a generation; event refs only own cancellation and
  // the synchronous duplicate guard. A renewed read never replaces an intent.
  if (owner !== target) {
    setOwner(target)
    setIntent(null)
    setReason('')
    setInvalidReason(false)
  }
  if (view.basis !== basis)
    setView({ basis, pending: false, preview: null, receipt: null, issue: null })
  const operation = useRef<AbortController | null>(null)
  const mounted = useRef(false)
  const original = useRef<Intent | null>(null)
  const latest = useRef({ basis, target, canRead, canWrite, view, intent, reason })
  useLayoutEffect(() => {
    latest.current = { basis, target, canRead, canWrite, view, intent, reason }
  })
  useLayoutEffect(() => {
    mounted.current = true
    original.current = null
    return () => {
      mounted.current = false
      operation.current?.abort()
      operation.current = null
      original.current = null
    }
  }, [target])
  useLayoutEffect(() => {
    operation.current?.abort()
    operation.current = null
  }, [basis])

  function begin(write: boolean) {
    const session = cache.getQueryData<Session>(sessionKey)
    if (
      !mounted.current ||
      operation.current ||
      latest.current.basis !== basis ||
      latest.current.target !== target ||
      latest.current.view !== view ||
      latest.current.intent !== intent ||
      original.current !== intent ||
      latest.current.reason !== reason ||
      view.basis !== basis ||
      view.pending ||
      !canRead() ||
      (write && !canWrite()) ||
      session?.user.id !== actorID ||
      session.user.role !== 'admin' ||
      (write && !session.csrf_token)
    )
      return null
    const controller = new AbortController()
    operation.current = controller
    const read = canRead
    const writable = canWrite
    const current = () =>
      mounted.current &&
      operation.current === controller &&
      !controller.signal.aborted &&
      latest.current.basis === basis &&
      latest.current.target === target &&
      read() &&
      latest.current.canRead() &&
      (!write || (writable() && latest.current.canWrite()))
    return { controller, current, csrf: session.csrf_token }
  }
  function finish(controller: AbortController) {
    if (operation.current !== controller) return
    operation.current = null
    setView((value) => (value.basis === basis ? { ...value, pending: false } : value))
  }
  function matching(receipt: RuntimeArchiveRollbackReceipt, captured: Intent) {
    return (
      receipt.archive_id === archiveID &&
      receipt.archive_created_at === archiveBirth &&
      receipt.request_id === captured.input.request_id &&
      receipt.reason === captured.input.reason &&
      receipt.publication.request_id === captured.input.request_id &&
      receipt.publication.reason === captured.input.reason &&
      receipt.publication.actor_id === actorID
    )
  }
  async function review() {
    if (original.current) return
    const op = begin(false)
    if (!op) return
    setView({ basis, pending: true, preview: null, receipt: null, issue: null })
    try {
      const value = await previewRuntimeArchiveRollback(archiveID, op.controller.signal)
      if (!op.current()) return
      if (value.archive_id !== archiveID || value.archive_created_at !== archiveBirth)
        throw new Error('Archive identity changed')
      setView({ basis, pending: true, preview: value, receipt: null, issue: null })
    } catch {
      if (op.current())
        setView({ basis, pending: true, preview: null, receipt: null, issue: 'preview' })
    } finally {
      finish(op.controller)
    }
  }
  async function apply() {
    if (!original.current) {
      if (
        view.basis !== basis ||
        !view.preview?.can_apply ||
        view.preview.blocker_codes.length > 0 ||
        !canRead() ||
        !canWrite()
      )
        return
      if (!validRuntimeArchiveRollbackReason(reason.trim())) {
        setInvalidReason(true)
        return
      }
    }
    const op = begin(true)
    if (!op) return
    let captured = original.current
    if (!captured) {
      captured = {
        input: { request_id: crypto.randomUUID(), reason: reason.trim() },
        etag: view.preview!.etag,
      }
      original.current = captured
      setIntent(captured)
    }
    setInvalidReason(false)
    setView((value) => ({ ...value, basis, pending: true, receipt: null, issue: null }))
    try {
      const receipt = await applyRuntimeArchiveRollback(
        archiveID,
        captured.input,
        captured.etag,
        op.csrf,
        op.controller.signal,
      )
      if (!op.current()) return
      if (!matching(receipt, captured)) throw new Error('Receipt identity changed')
      setView({ basis, pending: true, preview: null, receipt, issue: null })
    } catch {
      if (op.current())
        setView({ basis, pending: true, preview: null, receipt: null, issue: 'uncertain' })
    } finally {
      finish(op.controller)
    }
  }
  async function refreshReceipt() {
    const captured = original.current
    if (!captured) return
    const op = begin(false)
    if (!op) return
    setView({ basis, pending: true, preview: null, receipt: null, issue: null })
    try {
      const receipt = await getRuntimeArchiveRollbackReceipt(
        archiveID,
        captured.input.request_id,
        op.controller.signal,
      )
      if (!op.current()) return
      if (!matching(receipt, captured)) throw new Error('Receipt identity changed')
      setView({ basis, pending: true, preview: null, receipt, issue: null })
    } catch {
      if (op.current())
        setView({ basis, pending: true, preview: null, receipt: null, issue: 'receipt' })
    } finally {
      finish(op.controller)
    }
  }
  function close() {
    mounted.current = false
    operation.current?.abort()
    operation.current = null
    original.current = null
    onClose()
  }
  const visible = owner === target && canRead()
  const facts = visible && view.basis === basis ? view : null
  const saved = facts?.receipt
  const preview = facts?.preview
  const retained = visible ? intent : null
  return (
    <Dialog
      open={true}
      onOpenChange={(open) => {
        if (!open) close()
      }}
      title={t('archiveRollbackTitle')}
      description={t('archiveRollbackBoundary')}
      width={880}
    >
      {!visible && <p role="status">{t('refreshing')}</p>}
      {visible && (
        <div className="space-y-4">
          <p className="break-all font-mono text-xs">{archiveID}</p>
          <p className="text-sm text-muted-foreground">{t('archiveRollbackTransportBoundary')}</p>
          {!retained && (
            <Button variant="outline" disabled={facts?.pending} onClick={() => void review()}>
              {t('archiveRollbackPreview')}
            </Button>
          )}
          {facts?.pending && <p role="status">{t('archiveRollbackPending')}</p>}
          {facts?.issue && (
            <p role="alert" className="text-destructive">
              {t(`archiveRollbackIssue_${facts.issue}`)}
            </p>
          )}
          {preview && (
            <>
              {preview.blocker_codes.length > 0 && (
                <p role="alert">{t('archiveRollbackBlocked')}</p>
              )}
              <Table aria-label={t('archiveRollbackConnections')}>
                <thead>
                  <tr>
                    <th>{t('archiveRollbackConnection')}</th>
                    <th>{t('archiveRollbackBefore')}</th>
                    <th>{t('archiveRollbackAfter')}</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.connections.map((row) => (
                    <tr key={row.connection_id}>
                      <td>
                        <p>{row.name}</p>
                        <p className="font-mono text-xs">{row.connection_id}</p>
                        <p>
                          {t(row.enabled ? 'archiveRollbackEnabled' : 'archiveRollbackDisabled')}
                        </p>
                      </td>
                      <td>
                        <ConnectionFacts value={row.before} />
                      </td>
                      <td>
                        <ConnectionFacts value={row.after} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              <Table aria-label={t('archiveRollbackDeclarations')}>
                <thead>
                  <tr>
                    <th>{t('archiveRollbackProviderModel')}</th>
                    <th>{t('archiveRollbackBefore')}</th>
                    <th>{t('archiveRollbackAfter')}</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.declarations.map((row) => (
                    <tr key={row.provider_model_id}>
                      <td className="font-mono text-xs">{row.provider_model_id}</td>
                      <td>
                        {t('archiveRollbackInputs', {
                          image: t(
                            row.before.image_input ? 'archiveRollbackYes' : 'archiveRollbackNo',
                          ),
                          pdf: t(row.before.pdf_input ? 'archiveRollbackYes' : 'archiveRollbackNo'),
                        })}
                      </td>
                      <td>
                        {t('archiveRollbackInputs', {
                          image: t(
                            row.after.image_input ? 'archiveRollbackYes' : 'archiveRollbackNo',
                          ),
                          pdf: t(row.after.pdf_input ? 'archiveRollbackYes' : 'archiveRollbackNo'),
                        })}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              <Table aria-label={t('archiveRollbackWeights')}>
                <thead>
                  <tr>
                    <th>{t('archiveRollbackBinding')}</th>
                    <th>{t('archiveRollbackProtocol')}</th>
                    <th>{t('archiveRollbackBefore')}</th>
                    <th>{t('archiveRollbackAfter')}</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.weights.map((row) => (
                    <tr key={row.binding_id}>
                      <td className="font-mono text-xs">
                        <p>{row.binding_id}</p>
                        <p>{row.model_id}</p>
                        <p>{row.provider_model_id}</p>
                      </td>
                      <td>{row.protocol}</td>
                      <td>{row.before}</td>
                      <td>{row.after}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
              {preview.connections.length + preview.declarations.length + preview.weights.length ===
                0 && <p>{t('archiveRollbackNoChanges')}</p>}
            </>
          )}
          {(preview || retained) && (
            <label className="block text-sm">
              {t('archiveRollbackReason')}
              <Input
                value={retained?.input.reason ?? reason}
                disabled={!!retained || facts?.pending || !canWrite()}
                maxLength={2000}
                aria-invalid={invalidReason}
                aria-describedby={invalidReason ? reasonErrorID : undefined}
                onChange={(event) => {
                  setReason(event.target.value)
                  setInvalidReason(false)
                }}
              />
            </label>
          )}
          {invalidReason && (
            <p id={reasonErrorID} role="alert" className="text-destructive">
              {t('archiveRollbackInvalidReason')}
            </p>
          )}
          {saved && (
            <div className="space-y-2 rounded-md border p-3">
              <p role="status">{t('archiveRollbackRecorded')}</p>
              <p>{t(`archiveRollbackPublication_${saved.publication.status}`)}</p>
              <p>
                {t(
                  saved.superseded
                    ? 'archiveRollbackSuperseded'
                    : saved.runtime_applied
                      ? 'archiveRollbackCurrentApplied'
                      : 'archiveRollbackCurrentUnknown',
                )}
              </p>
              <p className="text-sm text-muted-foreground">{t('archiveRollbackReceiptBoundary')}</p>
            </div>
          )}
          <div className="flex flex-wrap justify-end gap-2">
            {retained && (
              <Button
                variant="outline"
                disabled={facts?.pending}
                onClick={() => void refreshReceipt()}
              >
                {t('archiveRollbackRefreshReceipt')}
              </Button>
            )}
            {canWrite() && !saved && (retained || preview) && (
              <Button
                disabled={
                  facts?.pending ||
                  (!retained &&
                    (!preview?.can_apply || preview.blocker_codes.length > 0 || !reason.trim()))
                }
                onClick={() => void apply()}
              >
                {t(retained ? 'archiveRollbackRetry' : 'archiveRollbackConfirm')}
              </Button>
            )}
            <Button variant="outline" onClick={close}>
              {t('routingClose')}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  )
}

function ConnectionFacts({
  value,
}: {
  value: RuntimeArchiveRollbackPreview['connections'][number]['before']
}) {
  const { t } = useTranslation('systemStatus')
  return (
    <dl className="space-y-1 break-all text-xs">
      {(
        ['base_url', 'protocol', 'adapter', 'api_version', 'egress_mode', 'egress_id'] as const
      ).map((field) => (
        <div key={field}>
          <dt className="text-muted-foreground">{t(`archiveRollbackField_${field}`)}</dt>
          <dd>
            {field === 'egress_mode' && value[field] === ''
              ? t('archiveRollbackLegacyDefault')
              : (value[field] ?? t('archiveRollbackNotSet'))}
          </dd>
        </div>
      ))}
    </dl>
  )
}
