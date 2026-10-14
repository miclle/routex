import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getController,
  getControllerReview,
  getControllers,
  getControllerSlots,
  validControllerReason,
} from '@/api/key-delivery-controller'
import type { DeliveryAuthority } from '@/views/key-delivery-admin/lifetime'
import type { DeliverySlotSummary } from '@/types/key-delivery-controller'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { FormField } from '@/components/app/CatalogUI'
import { controllerPrefix, useControllerLifetime, type ControllerReviewProof } from './lifetime'

export default function ControllerWorkspace({
  authority,
  target,
  onClose,
}: {
  authority: DeliveryAuthority
  target: string
  onClose: () => void
}) {
  return (
    <Workspace
      key={`${authority.actor}:${target}`}
      authority={authority}
      target={target}
      onClose={onClose}
    />
  )
}
function Workspace({
  authority,
  target,
  onClose,
}: {
  authority: DeliveryAuthority
  target: string
  onClose: () => void
}) {
  const { t, i18n } = useTranslation('keyControllers')
  const cache = useQueryClient()
  const [slotCursor, setSlotCursor] = useState(''),
    [controllerCursor, setControllerCursor] = useState('')
  const [slot, setSlot] = useState<DeliverySlotSummary>(),
    [controller, setController] = useState('')
  const life = useControllerLifetime(
    authority,
    target,
    `${target}:${slot?.id ?? ''}:${slot?.birth ?? ''}:${controller}`,
  )
  const [mode, setMode] = useState<'enroll' | 'detail'>()
  const [name, setName] = useState(''),
    [hours, setHours] = useState('24'),
    [reason, setReason] = useState(''),
    [optIn, setOptIn] = useState(false)
  const generation = life.generation
  const prefix = [...controllerPrefix(authority.actor, target), generation]
  const canRead = !!target && life.current('secrets.read'),
    canWrite = canRead && life.current('secrets.write')
  const slotsKey = [...prefix, 'slots', slotCursor]
  const slots = useQuery({
    queryKey: slotsKey,
    queryFn: ({ signal }) => life.read(() => getControllerSlots(target, slotCursor, signal)),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const slotsFresh = canRead && slots.isSuccess && !slots.isFetching && !slots.error
  const slotFresh =
    slotsFresh &&
    !!slot &&
    slots.data.items.some((item) => item.id === slot.id && item.birth === slot.birth)
  const controllersKey = [...prefix, 'controllers', slot?.id ?? '', controllerCursor]
  const controllers = useQuery({
    queryKey: controllersKey,
    queryFn: ({ signal }) => life.read(() => getControllers(slot!.id, controllerCursor, signal)),
    enabled: slotFresh,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const controllersFresh =
    slotFresh && controllers.isSuccess && !controllers.isFetching && !controllers.error
  const controllerFresh =
    controllersFresh &&
    !!controller &&
    controllers.data.items.some((item) => item.id === controller)
  const reviewKey = [...prefix, 'configuration', slot?.id ?? '']
  const review = useQuery({
    queryKey: reviewKey,
    queryFn: ({ signal }) => life.read(() => getControllerReview(slot!.id, signal)),
    enabled: slotFresh && mode === 'enroll',
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const detailKey = [...prefix, 'controller', slot?.id ?? '', controller]
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => life.read(() => getController(controller, slot!.id, signal)),
    enabled: controllerFresh && mode === 'detail',
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const currentReview =
    slotFresh && mode === 'enroll' && review.isSuccess && !review.isFetching && !review.error
      ? review.data
      : undefined
  const facts =
    controllerFresh && mode === 'detail' && detail.isSuccess && !detail.isFetching && !detail.error
      ? detail.data
      : undefined
  const proof = (key: readonly unknown[]): ControllerReviewProof => ({
    key,
    count: cache.getQueryState(key)?.dataUpdateCount ?? -1,
  })
  const date = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  const clearDraft = () => {
    setName('')
    setHours('24')
    setReason('')
    setOptIn(false)
  }
  const close = () => {
    if (life.pendingNow()) return
    life.discard()
    clearDraft()
    setMode(undefined)
    setSlot(undefined)
    setController('')
    onClose()
  }
  const prepareEnrollment = () => {
    const duration = Number(hours)
    if (
      !currentReview ||
      !canWrite ||
      life.locked ||
      !optIn ||
      !name.trim() ||
      new TextEncoder().encode(name.trim()).length > 100 ||
      !validControllerReason(reason.trim()) ||
      !Number.isInteger(duration) ||
      duration < 2 ||
      duration > 720
    )
      return
    life.prepare({
      kind: 'enroll',
      slot: slot!.id,
      etag: currentReview.review_etag,
      proof: proof(reviewKey),
      input: {
        request_id: crypto.randomUUID(),
        name: name.trim(),
        expires_at: new Date(Date.now() + duration * 3600000).toISOString(),
        opt_in: true,
        descriptor_revision: currentReview.configuration.descriptor_revision,
        configuration_digest: currentReview.configuration.configuration_digest,
        reason: reason.trim(),
      },
    })
  }
  const prepareDetail = (kind: 'assign' | 'revoke') => {
    if (!facts || !canWrite || life.locked || !validControllerReason(reason.trim())) return
    const common = {
      slot: slot!.id,
      controller: facts.id,
      etag: facts.review_etag,
      proof: proof(detailKey),
    }
    if (kind === 'revoke') life.prepare({ ...common, kind, input: { reason: reason.trim() } })
    else if (facts.current_configuration && optIn)
      life.prepare({
        ...common,
        kind,
        input: {
          request_id: crypto.randomUUID(),
          opt_in: true,
          descriptor_revision: facts.current_configuration.descriptor_revision,
          configuration_digest: facts.current_configuration.configuration_digest,
          reason: reason.trim(),
        },
      })
  }
  const formValid =
    name.trim() &&
    new TextEncoder().encode(name.trim()).length <= 100 &&
    validControllerReason(reason.trim()) &&
    /^[1-9][0-9]{0,2}$/.test(hours) &&
    Number(hours) >= 2 &&
    Number(hours) <= 720 &&
    optIn
  return (
    <>
      <Dialog
        open={!!target}
        title={t('title')}
        description={t('scope')}
        width={900}
        busy={life.pending}
        onOpenChange={(open) => {
          if (!open) close()
        }}
      >
        <div className="space-y-4">
          {life.notice && (
            <p role={['saved', 'copied', 'receipt'].includes(life.notice) ? 'status' : 'alert'}>
              {t(`notice.${life.notice}`)}
            </p>
          )}
          {!slotsFresh ? (
            <div className="space-y-3">
              <p role={slots.isError ? 'alert' : 'status'}>
                {t(!canRead ? 'authority' : slots.isError ? 'readFailed' : 'loading')}
              </p>
              <Button
                variant="outline"
                disabled={!canRead || slots.isFetching || life.pending}
                onClick={() => {
                  if (!life.lockedNow()) void slots.refetch()
                }}
              >
                {t('refresh')}
              </Button>
            </div>
          ) : (
            <>
              <h3 className="font-medium">{t('slots')}</h3>
              <Table aria-label={t('slots')}>
                <thead>
                  <tr>
                    {['slot', 'revision', 'status', 'actions'].map((key) => (
                      <th key={key}>{t(key)}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {slots.data.items.map((item) => (
                    <tr key={item.id}>
                      <td className="break-all font-mono text-xs">{item.id}</td>
                      <td>{item.descriptor_revision}</td>
                      <td>{t('notReviewed')}</td>
                      <td>
                        <Button
                          variant="outline"
                          disabled={life.locked}
                          onClick={() => {
                            if (life.lockedNow()) return
                            setSlot(item)
                            setControllerCursor('')
                            setController('')
                            setMode(undefined)
                            clearDraft()
                            life.discard()
                          }}
                        >
                          {t('viewControllers')}
                        </Button>
                      </td>
                    </tr>
                  ))}
                  {!slots.data.items.length && (
                    <tr>
                      <td colSpan={4}>{t('noSlots')}</td>
                    </tr>
                  )}
                </tbody>
              </Table>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={life.locked}
                  onClick={() => {
                    if (!life.lockedNow()) void slots.refetch()
                  }}
                >
                  {t('refresh')}
                </Button>
                <Button
                  variant="outline"
                  disabled={life.locked || !slotCursor}
                  onClick={() => {
                    if (life.lockedNow()) return
                    setSlotCursor('')
                    setSlot(undefined)
                  }}
                >
                  {t('firstPage')}
                </Button>
                <Button
                  variant="outline"
                  disabled={life.locked || !slots.data.next_cursor}
                  onClick={() => {
                    if (life.lockedNow()) return
                    setSlotCursor(slots.data.next_cursor)
                    setSlot(undefined)
                  }}
                >
                  {t('nextPage')}
                </Button>
              </div>
              {slot && (
                <section className="space-y-3" aria-label={t('controllers')}>
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <h3 className="font-medium">{t('controllers')}</h3>
                    <Button
                      disabled={!slotFresh || !canWrite || life.locked}
                      onClick={() => {
                        if (life.lockedNow()) return
                        setMode('enroll')
                        setController('')
                        clearDraft()
                        life.discard()
                        void cache.invalidateQueries({ queryKey: reviewKey })
                      }}
                    >
                      {t('enroll')}
                    </Button>
                  </div>
                  {!controllersFresh ? (
                    <p role={controllers.isError ? 'alert' : 'status'}>
                      {t(
                        !slotFresh ? 'slotChanged' : controllers.isError ? 'readFailed' : 'loading',
                      )}
                    </p>
                  ) : (
                    <Table aria-label={t('controllers')}>
                      <thead>
                        <tr>
                          {['name', 'status', 'expires', 'actions'].map((key) => (
                            <th key={key}>{t(key)}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {controllers.data.items.map((item) => (
                          <tr key={item.id}>
                            <td>
                              <p>{item.name}</p>
                              <p className="break-all font-mono text-xs text-muted-foreground">
                                {item.id}
                              </p>
                            </td>
                            <td>
                              {t(`lifecycle.${item.lifecycle_status}`)}
                              <p className="text-xs text-muted-foreground">{t('notReviewed')}</p>
                            </td>
                            <td>{date(item.expires_at)}</td>
                            <td>
                              <Button
                                variant="outline"
                                disabled={life.locked}
                                onClick={() => {
                                  if (life.lockedNow()) return
                                  setController(item.id)
                                  setMode('detail')
                                  clearDraft()
                                  life.discard()
                                }}
                              >
                                {t('reviewController')}
                              </Button>
                            </td>
                          </tr>
                        ))}
                        {!controllers.data.items.length && (
                          <tr>
                            <td colSpan={4}>{t('noControllers')}</td>
                          </tr>
                        )}
                      </tbody>
                    </Table>
                  )}
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      disabled={!slotFresh || controllers.isFetching || life.locked}
                      onClick={() => {
                        if (!life.lockedNow()) void controllers.refetch()
                      }}
                    >
                      {t('refresh')}
                    </Button>
                    <Button
                      variant="outline"
                      disabled={!controllersFresh || life.locked || !controllerCursor}
                      onClick={() => {
                        if (!life.lockedNow()) setControllerCursor('')
                      }}
                    >
                      {t('firstPage')}
                    </Button>
                    <Button
                      variant="outline"
                      disabled={!controllersFresh || life.locked || !controllers.data?.next_cursor}
                      onClick={() => {
                        if (!life.lockedNow()) setControllerCursor(controllers.data!.next_cursor)
                      }}
                    >
                      {t('nextPage')}
                    </Button>
                  </div>
                </section>
              )}
            </>
          )}
          {slotsFresh && life.saved && (
            <section className="space-y-3 rounded-lg border p-4" aria-label={t('savedReceipt')}>
              <p>{t('savedReceipt')}</p>
              <p className="break-all font-mono">{life.saved.id}</p>
              <p>{t('savedBoundary')}</p>
              {life.bearer ? (
                <>
                  <p role="alert">{t('oneTime')}</p>
                  <FormField label={t('bearer')}>
                    <Input type="password" value={life.bearer} readOnly autoComplete="off" />
                  </FormField>
                  <Button variant="outline" onClick={() => void life.copy(life.bearer)}>
                    {t('copyBearer')}
                  </Button>
                  <Button variant="outline" onClick={life.discard}>
                    {t('clearBearer')}
                  </Button>
                </>
              ) : (
                <p>{t('notRecoverable')}</p>
              )}
            </section>
          )}
          {life.intent && !canRead && <p role="alert">{t('notice.unknown')}</p>}
          {life.intent && (
            <Button variant="outline" disabled={life.pending} onClick={life.discard}>
              {t('discard')}
            </Button>
          )}
        </div>
      </Dialog>
      <Dialog
        open={!!mode && !!target && !life.intent && !life.saved}
        title={t(mode === 'enroll' ? 'enroll' : 'reviewController')}
        description={t('configurationBoundary')}
        width={760}
        busy={life.pending}
        onOpenChange={(open) => {
          if (!open) {
            setMode(undefined)
            clearDraft()
            life.discard()
          }
        }}
      >
        {!canRead || !(mode === 'enroll' ? currentReview : facts) ? (
          <div className="space-y-3">
            <p role={review.isError || detail.isError ? 'alert' : 'status'}>
              {t(
                !canRead
                  ? 'authority'
                  : review.isError || detail.isError
                    ? 'readFailed'
                    : 'loading',
              )}
            </p>
            <Button
              variant="outline"
              disabled={!canRead || !slotFresh || review.isFetching || detail.isFetching}
              onClick={() => {
                if (mode === 'enroll') void review.refetch()
                else if (controllerFresh) void detail.refetch()
              }}
            >
              {t('refresh')}
            </Button>
          </div>
        ) : (
          <div className="space-y-4">
            {mode === 'enroll' && currentReview ? (
              <>
                <p className="break-all font-mono text-sm">{slot!.id}</p>
                <p>
                  {t('reviewedRevision', {
                    revision: currentReview.configuration.descriptor_revision,
                  })}
                </p>
                <FormField label={t('name')}>
                  <Input
                    value={name}
                    disabled={life.locked}
                    autoComplete="off"
                    onChange={(event) => setName(event.target.value)}
                  />
                </FormField>
                <FormField label={t('expiryHours')}>
                  <Input
                    type="number"
                    min={2}
                    max={720}
                    step={1}
                    value={hours}
                    disabled={life.locked}
                    onChange={(event) => setHours(event.target.value)}
                  />
                </FormField>
                <p className="text-sm text-muted-foreground">{t('expiryHelp')}</p>
              </>
            ) : (
              facts && (
                <>
                  <dl className="grid gap-2 text-sm sm:grid-cols-[12rem_1fr]">
                    <dt>{t('controller')}</dt>
                    <dd className="break-all font-mono">{facts.id}</dd>
                    <dt>{t('name')}</dt>
                    <dd>{facts.name}</dd>
                    <dt>{t('status')}</dt>
                    <dd>{t(`configuration.${facts.configuration_status}`)}</dd>
                    <dt>{t('expires')}</dt>
                    <dd>{date(facts.expires_at)}</dd>
                    <dt>{t('assignedRevision')}</dt>
                    <dd>{facts.assigned_descriptor_revision}</dd>
                  </dl>
                  {facts.last_acknowledgement ? (
                    <section className="space-y-2 rounded-lg border p-3">
                      <h3>{t('report')}</h3>
                      <p>{t('reportBoundary')}</p>
                      <dl className="grid gap-2 text-sm sm:grid-cols-[12rem_1fr]">
                        <dt>{t('revision')}</dt>
                        <dd>{facts.last_acknowledgement.descriptor_revision}</dd>
                        <dt>{t('agent')}</dt>
                        <dd>{t(`agentState.${facts.last_acknowledgement.agent_state}`)}</dd>
                        <dt>{t('output')}</dt>
                        <dd>{t(`outputState.${facts.last_acknowledgement.output_state}`)}</dd>
                        <dt>{t('recorded')}</dt>
                        <dd>{date(facts.last_acknowledgement.recorded_at)}</dd>
                      </dl>
                    </section>
                  ) : (
                    <p>{t('noReport')}</p>
                  )}
                </>
              )
            )}
            {(currentReview?.configuration ?? facts?.current_configuration) && (
              <details className="rounded-lg border p-3">
                <summary className="cursor-pointer text-sm font-medium">
                  {t('agentConfiguration')}
                </summary>
                <Textarea
                  aria-label={t('agentConfiguration')}
                  className="mt-3 h-48 font-mono text-xs"
                  readOnly
                  spellCheck={false}
                  value={
                    (currentReview?.configuration ?? facts?.current_configuration)!
                      .agent_configuration
                  }
                />
              </details>
            )}
            <FormField label={t('reason')}>
              <Textarea
                value={reason}
                disabled={life.locked || !canWrite}
                onChange={(event) => setReason(event.target.value)}
              />
            </FormField>
            {(mode === 'enroll' || facts?.current_configuration) && (
              <label className="flex items-center gap-3 text-sm">
                <Switch
                  checked={optIn}
                  disabled={!canWrite || life.locked}
                  onCheckedChange={setOptIn}
                />
                <span>{t('optIn')}</span>
              </label>
            )}
            {mode === 'enroll' ? (
              <Button disabled={!canWrite || life.locked || !formValid} onClick={prepareEnrollment}>
                {t('reviewEnrollment')}
              </Button>
            ) : (
              <div className="flex flex-wrap gap-2">
                <Button
                  disabled={
                    !canWrite ||
                    life.locked ||
                    !facts?.current_configuration ||
                    !optIn ||
                    !validControllerReason(reason.trim())
                  }
                  onClick={() => prepareDetail('assign')}
                >
                  {t('reviewAssignment')}
                </Button>
                <Button
                  variant="outline"
                  disabled={
                    !canWrite ||
                    life.locked ||
                    facts?.configuration_status === 'revoked' ||
                    !validControllerReason(reason.trim())
                  }
                  onClick={() => prepareDetail('revoke')}
                >
                  {t('reviewRevocation')}
                </Button>
                <Button
                  variant="outline"
                  disabled={life.locked || detail.isFetching}
                  onClick={() => void detail.refetch()}
                >
                  {t('refresh')}
                </Button>
              </div>
            )}
          </div>
        )}
      </Dialog>
      <Dialog
        open={!!life.intent && !!target}
        title={t(life.uncertain ? 'unknownTitle' : 'confirmTitle')}
        description={t('confirmationHelp')}
        width={680}
        busy={life.pending}
        onOpenChange={(open) => {
          if (!open) life.discard()
        }}
      >
        {life.intent && (
          <div className="space-y-4">
            {canRead ? (
              <>
                <p>{t(`operation.${life.intent.kind}`)}</p>
                <dl className="space-y-2 text-sm">
                  <dt>{t('slot')}</dt>
                  <dd className="break-all font-mono">{life.intent.slot}</dd>
                  {'controller' in life.intent && (
                    <>
                      <dt>{t('controller')}</dt>
                      <dd className="break-all font-mono">{life.intent.controller}</dd>
                    </>
                  )}
                  {life.intent.kind === 'enroll' && (
                    <>
                      <dt>{t('name')}</dt>
                      <dd>{life.intent.input.name}</dd>
                      <dt>{t('expires')}</dt>
                      <dd>{date(life.intent.input.expires_at)}</dd>
                    </>
                  )}
                  <dt>{t('reason')}</dt>
                  <dd className="break-all">{life.intent.input.reason}</dd>
                </dl>
              </>
            ) : (
              <p role="alert">{t('authority')}</p>
            )}
            {life.notice && <p role="alert">{t(`notice.${life.notice}`)}</p>}
            {canRead && life.receipt && (
              <div className="space-y-2 rounded-lg border p-3">
                <p>{t('receiptLocated')}</p>
                <p className="break-all font-mono">{life.receipt.controller.id}</p>
                <p>{t('receiptBoundary')}</p>
                <p>{t('notRecoverable')}</p>
              </div>
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="outline" disabled={life.pending} onClick={life.discard}>
                {t('discard')}
              </Button>
              {life.uncertain && life.intent.kind === 'enroll' && (
                <Button
                  variant="outline"
                  disabled={!canRead || life.pending}
                  onClick={() => void life.lookupReceipt()}
                >
                  {t('locateReceipt')}
                </Button>
              )}
              <Button
                disabled={
                  !canWrite ||
                  !slotsFresh ||
                  life.pending ||
                  (!life.uncertain && !life.proofCurrent(life.intent.proof))
                }
                onClick={() => void life.submit()}
              >
                {t(life.pending ? 'pending' : life.uncertain ? 'retry' : 'confirm')}
              </Button>
            </div>
          </div>
        )}
      </Dialog>
    </>
  )
}
