import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentProps,
} from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getModelWeightRollbackCommand,
  getModelWeightVersion,
  listModelWeightVersions,
  reviewModelWeightRollback,
  rollbackModelWeights,
  trimModelWeightReason,
  validModelWeightReason,
} from '@/api/model-weight-history'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  ModelWeightRollbackInput,
  ModelWeightRollbackResult,
  ModelWeightRow,
} from '@/types/model-weight-history'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { FormField } from '@/components/app/CatalogUI'
import { protocolLabel } from '@/lib/protocols'
import { ModelRecordedDate } from './model-metadata'

type Intent = { input: ModelWeightRollbackInput; etag: string; modelBirth: string | null }
const effectKeys = {
  changed: 'weightHistory.effects.changed',
  noop: 'weightHistory.effects.noop',
} as const
const sourceKeys = {
  observed_baseline: 'weightHistory.sources.observed_baseline',
  legacy_editor: 'weightHistory.sources.legacy_editor',
  rollback: 'weightHistory.sources.rollback',
} as const
const applicationKeys = {
  applied: 'weightHistory.application.applied',
  pending: 'weightHistory.application.pending',
  superseded: 'weightHistory.application.superseded',
  unknown: 'weightHistory.application.unknown',
} as const
const blockerKeys: Record<string, string> = {
  write_not_authorized: 'weightHistory.blockers.write_not_authorized',
  version_not_restorable: 'weightHistory.blockers.version_not_restorable',
  topology_changed: 'weightHistory.blockers.topology_changed',
  model_inactive: 'weightHistory.blockers.model_inactive',
  positive_route_unavailable: 'weightHistory.blockers.positive_route_unavailable',
}

function useFreshQuery(key: readonly unknown[]) {
  const cache = useQueryClient(),
    hash = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((e) => {
        if (JSON.stringify(e.query.queryKey) === hash) notify()
      }),
    [cache, hash],
  )
  const snapshot = useCallback(() => {
    const s = cache.getQueryState(JSON.parse(hash))
    return s?.status === 'success' && s.fetchStatus === 'idle' && !s.isInvalidated
  }, [cache, hash])
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}
function WeightTable({
  rows,
  kind = 'recorded',
}: {
  rows: ModelWeightRow[]
  kind?: 'recorded' | 'current' | 'proposed'
}) {
  const { t } = useTranslation('catalog')
  const labels = {
    recorded: 'weightHistory.weights',
    current: 'weightHistory.currentSet',
    proposed: 'weightHistory.proposedSet',
  }
  const columns = {
    recorded: 'weightHistory.recordedWeight',
    current: 'weightHistory.currentWeight',
    proposed: 'weightHistory.proposedWeight',
  }
  return (
    <Table aria-label={t(labels[kind])}>
      <thead>
        <tr>
          <th>{t('weightHistory.binding')}</th>
          <th>{t('weightHistory.protocol')}</th>
          <th>{t(columns[kind])}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.binding_id}>
            <td>
              <span className="block font-mono text-xs">{row.binding_id}</span>
              <span className="block text-xs text-muted-foreground">
                {row.provider_model_id} · {row.connection_id} · {row.provider_id}
              </span>
            </td>
            <td>{protocolLabel(row.protocol)}</td>
            <td>{row.weight}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  )
}

// This actor/Model-keyed owner survives temporary private-fact hiding during renewed reads.
export default function RoutingWeightHistory({
  actor,
  modelID,
  modelBirth,
  generation,
  permissionGeneration,
  resourceGeneration,
  readable,
  canWrite,
  readReady,
  writeReady,
  open,
  finalFocus,
  onOpenChange,
  onSaved,
}: {
  actor: string
  modelID: string
  modelBirth: string | null | undefined
  generation: number
  permissionGeneration: number
  resourceGeneration: number
  readable: boolean
  canWrite: boolean
  readReady: () => boolean
  writeReady: () => boolean
  open: boolean
  finalFocus?: ComponentProps<typeof Dialog>['finalFocus']
  onOpenChange: (value: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation('catalog'),
    cache = useQueryClient()
  const stamp = JSON.stringify([
    actor,
    modelID,
    modelBirth,
    generation,
    permissionGeneration,
    resourceGeneration,
  ])
  const [cursor, setCursor] = useState<string | undefined>()
  const [target, setTarget] = useState<string | null>(null)
  const [reviewTarget, setReviewTarget] = useState<{
    id: string
    stamp: string
    page: number
    detail: number
  } | null>(null)
  const [reason, setReason] = useState('')
  const [confirmation, setConfirmation] = useState<{ stamp: string; count: number } | null>(null)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [result, setResult] = useState<{ value: ModelWeightRollbackResult; stamp: string } | null>(
    null,
  )
  const [pending, setPending] = useState(false),
    [failed, setFailed] = useState(false)
  const live = useRef({ stamp, readable, readReady, writeReady }),
    mounted = useRef(false)
  const operation = useRef<AbortController | null>(null),
    birth = useRef(modelBirth)
  const pageKey = [
    'admin',
    'model-weight-history',
    actor,
    modelID,
    modelBirth,
    generation,
    permissionGeneration,
    resourceGeneration,
    cursor,
  ]
  const detailKey = [...pageKey.slice(0, -1), 'detail', target]
  const reviewKey = [
    ...pageKey.slice(0, -1),
    'review',
    reviewTarget?.id,
    reviewTarget?.stamp,
    reviewTarget?.page,
    reviewTarget?.detail,
  ]
  const page = useQuery({
    queryKey: pageKey,
    queryFn: ({ signal }) => listModelWeightVersions(modelID, cursor, signal),
    enabled: open && readable,
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getModelWeightVersion(modelID, target!, signal),
    enabled: open && readable && !!target,
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const pageCount = cache.getQueryState(pageKey)?.dataUpdateCount ?? 0
  const detailCount = cache.getQueryState(detailKey)?.dataUpdateCount ?? 0
  const reviewedReads = reviewTarget?.page === pageCount && reviewTarget?.detail === detailCount
  const review = useQuery({
    queryKey: reviewKey,
    queryFn: ({ signal }) => reviewModelWeightRollback(modelID, reviewTarget!.id, signal),
    enabled: open && readable && reviewedReads && reviewTarget?.stamp === stamp && !!reviewTarget,
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const pageFresh = useFreshQuery(pageKey),
    detailFresh = useFreshQuery(detailKey),
    reviewFresh = useFreshQuery(reviewKey)
  const shownDetail =
    readable && pageFresh && detailFresh && detail.data?.version.model_created_at === modelBirth
      ? detail.data
      : null
  const shownReview =
    readable &&
    reviewFresh &&
    reviewedReads &&
    reviewTarget?.stamp === stamp &&
    shownDetail &&
    review.data?.version_id === shownDetail.version.version_id
      ? review.data
      : null
  const eligible =
    !!shownReview?.eligible &&
    shownReview.can_rollback &&
    shownReview.blocker_codes.length === 0 &&
    canWrite &&
    !!shownDetail?.version.valid_weight_set &&
    shownReview.proposed_weights.length === shownDetail.weights.length &&
    shownReview.proposed_weights.every((row, i) =>
      Object.keys(row).every(
        (key) =>
          row[key as keyof ModelWeightRow] === shownDetail.weights[i][key as keyof ModelWeightRow],
      ),
    )
  const reviewCount = cache.getQueryState(reviewKey)?.dataUpdateCount ?? 0
  const confirmed = confirmation?.stamp === stamp && confirmation.count === reviewCount && eligible
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      operation.current?.abort()
    }
  }, [])
  useLayoutEffect(() => {
    const renewed = live.current.stamp !== stamp
    live.current = { stamp, readable, readReady, writeReady }
    if (renewed) setCursor(undefined)
    if (renewed || !readable || !readReady()) {
      operation.current?.abort()
      operation.current = null
      setPending(false)
    }
    if (modelBirth !== undefined && birth.current !== undefined && modelBirth !== birth.current) {
      operation.current?.abort()
      operation.current = null
      setIntent(null)
      setResult(null)
      setReason('')
      setTarget(null)
      setReviewTarget(null)
      setConfirmation(null)
      setCursor(undefined)
      setPending(false)
      setFailed(false)
    }
    if (modelBirth !== undefined) birth.current = modelBirth
    return cache.getQueryCache().subscribe(() => {
      if (!live.current.readReady()) {
        operation.current?.abort()
      }
    })
  }, [cache, stamp, readable, readReady, writeReady, modelBirth])
  const current = (captured: string) =>
    mounted.current &&
    live.current.stamp === captured &&
    live.current.readable &&
    live.current.readReady()
  const fresh = (key: readonly unknown[]) => {
    const state = cache.getQueryState(key)
    return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
  }
  function select(id: string) {
    if (!fresh(pageKey) || !readReady() || intent || pending) return
    setTarget(id)
    setReviewTarget(null)
    setConfirmation(null)
    setFailed(false)
  }
  function freshReview() {
    if (!shownDetail || !fresh(pageKey) || !fresh(detailKey) || !readReady() || intent || pending)
      return
    setConfirmation(null)
    setReviewTarget({
      id: shownDetail.version.version_id,
      stamp,
      page: pageCount,
      detail: detailCount,
    })
    if (
      reviewTarget?.id === shownDetail.version.version_id &&
      reviewTarget.stamp === stamp &&
      reviewedReads
    )
      void review.refetch()
  }
  async function dispatch(kind: 'post' | 'get', captured: Intent) {
    if (operation.current || !readReady() || (kind === 'post' && !writeReady())) return
    const controller = new AbortController(),
      capturedStamp = stamp
    operation.current = controller
    setPending(true)
    setFailed(false)
    // A new read/retry cannot leave prior local application evidence looking current.
    setResult((previous) => (previous ? { ...previous, stamp: '' } : null))
    try {
      const value =
        kind === 'get'
          ? await getModelWeightRollbackCommand(modelID, captured.input, controller.signal)
          : await rollbackModelWeights(
              modelID,
              captured.etag,
              captured.input,
              cache.getQueryData<Session>(sessionKey)!.csrf_token,
              controller.signal,
            )
      if (controller.signal.aborted || !current(capturedStamp)) return
      setResult({ value, stamp: capturedStamp })
      if (kind === 'post') onSaved()
    } catch {
      if (!controller.signal.aborted && current(capturedStamp)) setFailed(true)
    } finally {
      if (operation.current === controller) {
        operation.current = null
        if (mounted.current) setPending(false)
      }
    }
  }
  function confirm() {
    if (
      intent ||
      operation.current ||
      pending ||
      !confirmed ||
      !fresh(pageKey) ||
      !fresh(detailKey) ||
      !fresh(reviewKey) ||
      reviewTarget?.page !== cache.getQueryState(pageKey)?.dataUpdateCount ||
      reviewTarget?.detail !== cache.getQueryState(detailKey)?.dataUpdateCount ||
      !shownReview ||
      !writeReady() ||
      !readReady() ||
      modelBirth === undefined
    )
      return
    const trimmed = trimModelWeightReason(reason)
    if (!validModelWeightReason(trimmed)) return
    const captured = {
      input: {
        version_id: shownReview.version_id,
        request_id: crypto.randomUUID(),
        reason: trimmed,
      },
      etag: shownReview.review_etag,
      modelBirth,
    }
    setIntent(captured)
    setConfirmation(null)
    void dispatch('post', captured)
  }
  const busy = page.isFetching || detail.isFetching || review.isFetching || pending
  return (
    <Dialog
      open={open && readable}
      finalFocus={finalFocus}
      onOpenChange={onOpenChange}
      title={t('weightHistory.title')}
      description={t('weightHistory.description')}
      width={900}
      busy={pending}
    >
      <div className="space-y-5">
        <p className="text-sm text-muted-foreground">{t('weightHistory.boundary')}</p>
        {intent ? (
          <section className="space-y-3 rounded-lg border p-4">
            <h3 className="font-semibold">{t('weightHistory.originalCommand')}</h3>
            <p className="break-all font-mono text-xs">
              {intent.input.request_id} · {intent.input.version_id}
            </p>
            <p className="whitespace-pre-wrap text-sm">{intent.input.reason}</p>
            {!result && <p role="status">{t('weightHistory.uncertain')}</p>}
            {result && (
              <>
                <p role="status">
                  {t('weightHistory.receipt', {
                    effect: t(effectKeys[result.value.receipt.effect]),
                    version: result.value.receipt.saved_version_id ?? t('weightHistory.unknown'),
                  })}
                </p>
                <p>
                  {t(
                    applicationKeys[
                      result.stamp === stamp ? result.value.application_status : 'unknown'
                    ],
                  )}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t('weightHistory.receiptBoundary')}
                </p>
              </>
            )}
            {failed && <p role="alert">{t('weightHistory.commandUnavailable')}</p>}
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                disabled={pending || !readReady()}
                onClick={() => void dispatch('get', intent)}
              >
                {t('weightHistory.recover')}
              </Button>
              <Button
                variant="outline"
                disabled={pending || !writeReady() || !canWrite}
                onClick={() => void dispatch('post', intent)}
              >
                {t('weightHistory.retryOriginal')}
              </Button>
              {result &&
                result.stamp === stamp &&
                ['applied', 'superseded'].includes(result.value.application_status) && (
                  <Button
                    variant="outline"
                    disabled={pending || !readReady()}
                    onClick={() => {
                      if (
                        !current(stamp) ||
                        operation.current ||
                        pending ||
                        !result ||
                        result.stamp !== stamp ||
                        !['applied', 'superseded'].includes(result.value.application_status)
                      )
                        return
                      setIntent(null)
                      setResult(null)
                      setTarget(null)
                      setReviewTarget(null)
                      setConfirmation(null)
                      setReason('')
                      setCursor(undefined)
                      void page.refetch()
                    }}
                  >
                    {t('weightHistory.anotherReview')}
                  </Button>
                )}
            </div>
          </section>
        ) : (
          <>
            {page.isFetching && <p role="status">{t('weightHistory.loading')}</p>}
            {page.isError && <p role="alert">{t('weightHistory.readUnavailable')}</p>}
            <Button
              variant="outline"
              disabled={busy || !readReady()}
              onClick={() => {
                if (!current(stamp) || operation.current || busy) return
                void page.refetch()
              }}
            >
              {t('weightHistory.refreshHistory')}
            </Button>
            {pageFresh && page.data && (
              <>
                {!page.data.items.length && <p>{t('weightHistory.empty')}</p>}
                <Table aria-label={t('weightHistory.versions')}>
                  <thead>
                    <tr>
                      <th>{t('weightHistory.version')}</th>
                      <th>{t('weightHistory.captured')}</th>
                      <th>{t('weightHistory.source')}</th>
                      <th>{t('weightHistory.actor')}</th>
                      <th>{t('weightHistory.reason')}</th>
                      <th>{t('weightHistory.details')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {page.data.items.map((v) => (
                      <tr key={v.version_id}>
                        <td className="font-mono text-xs">{v.version_id}</td>
                        <td>
                          <ModelRecordedDate value={v.captured_at} />
                        </td>
                        <td>{t(sourceKeys[v.source])}</td>
                        <td className="font-mono text-xs">{v.actor_id}</td>
                        <td>{v.reason ?? t('weightHistory.notRecorded')}</td>
                        <td>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={busy}
                            aria-label={t('weightHistory.viewVersion', { version: v.version_id })}
                            onClick={() => select(v.version_id)}
                          >
                            {t('weightHistory.details')}
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
                <div className="flex gap-2">
                  {cursor && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        setCursor(undefined)
                        setTarget(null)
                        setReviewTarget(null)
                        setConfirmation(null)
                      }}
                    >
                      {t('weightHistory.firstPage')}
                    </Button>
                  )}
                  {page.data.next_cursor && (
                    <Button
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        setCursor(page.data!.next_cursor!)
                        setTarget(null)
                        setReviewTarget(null)
                        setConfirmation(null)
                      }}
                    >
                      {t('weightHistory.nextPage')}
                    </Button>
                  )}
                </div>
              </>
            )}
            {target && (detail.isFetching || detail.isError) && (
              <p role={detail.isError ? 'alert' : 'status'}>
                {t(detail.isError ? 'weightHistory.readUnavailable' : 'weightHistory.loading')}
              </p>
            )}
            {shownDetail && (
              <section className="space-y-3 rounded-lg border p-4">
                <h3 className="break-all font-mono text-sm">{shownDetail.version.version_id}</h3>
                <p className="text-sm">
                  {t(
                    shownDetail.version.valid_weight_set
                      ? 'weightHistory.recordedValid'
                      : 'weightHistory.recordedInvalid',
                  )}
                </p>
                <dl className="grid gap-2 text-sm sm:grid-cols-2">
                  <div>
                    <dt className="text-muted-foreground">{t('weightHistory.modelBirth')}</dt>
                    <dd>
                      <ModelRecordedDate value={shownDetail.version.model_created_at} />
                    </dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">{t('weightHistory.bindingCount')}</dt>
                    <dd>{shownDetail.version.binding_count}</dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">{t('weightHistory.parentVersion')}</dt>
                    <dd className="break-all font-mono text-xs">
                      {shownDetail.version.parent_version_id ?? t('weightHistory.notRecorded')}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">{t('weightHistory.restoredVersion')}</dt>
                    <dd className="break-all font-mono text-xs">
                      {shownDetail.version.rollback_version_id ?? t('weightHistory.notRecorded')}
                    </dd>
                  </div>
                </dl>
                <WeightTable rows={shownDetail.weights} />
                <Button variant="outline" disabled={busy || !readReady()} onClick={freshReview}>
                  {t('weightHistory.reviewCurrent')}
                </Button>
              </section>
            )}
            {reviewTarget?.stamp === stamp && (review.isFetching || review.isError) && (
              <p role={review.isError ? 'alert' : 'status'}>
                {t(review.isError ? 'weightHistory.readUnavailable' : 'weightHistory.loading')}
              </p>
            )}
            {shownReview && (
              <section
                aria-label={t('weightHistory.comparison')}
                className="space-y-3 rounded-lg border p-4"
              >
                <h3 className="font-semibold">{t('weightHistory.comparison')}</h3>
                <p className="text-xs text-muted-foreground">
                  <ModelRecordedDate value={shownReview.observed_at} />
                </p>
                <h4 className="font-medium">{t('weightHistory.currentSet')}</h4>
                <WeightTable rows={shownReview.current_weights} kind="current" />
                <h4 className="font-medium">{t('weightHistory.proposedSet')}</h4>
                <WeightTable rows={shownReview.proposed_weights} kind="proposed" />
                <p className="text-xs text-muted-foreground">{t('weightHistory.reviewBoundary')}</p>
                {shownReview.blocker_codes.map((code) => (
                  <p role="alert" key={code}>
                    {t(
                      Object.hasOwn(blockerKeys, code)
                        ? blockerKeys[code]
                        : 'weightHistory.blockers.unknown',
                    )}
                  </p>
                ))}
                {!eligible && <p role="status">{t('weightHistory.blocked')}</p>}
                <FormField label={t('weightHistory.reason')}>
                  <Input
                    value={reason}
                    onValueChange={(value) => {
                      setReason(value)
                      setConfirmation(null)
                    }}
                    disabled={pending || !canWrite || !writeReady()}
                    autoComplete="off"
                  />
                </FormField>
                {reason && !validModelWeightReason(trimModelWeightReason(reason)) && (
                  <p role="alert">{t('weightHistory.invalidReason')}</p>
                )}
                {confirmed ? (
                  <div className="space-y-2">
                    <p role="alert">{t('weightHistory.confirmation')}</p>
                    <Button
                      variant="outline"
                      className="border-destructive text-destructive"
                      disabled={
                        pending ||
                        !writeReady() ||
                        !validModelWeightReason(trimModelWeightReason(reason))
                      }
                      onClick={confirm}
                    >
                      {t('weightHistory.confirmRestore')}
                    </Button>
                  </div>
                ) : (
                  <Button
                    disabled={
                      !eligible ||
                      !writeReady() ||
                      !validModelWeightReason(trimModelWeightReason(reason))
                    }
                    onClick={() => {
                      if (eligible && writeReady() && readReady())
                        setConfirmation({ stamp, count: reviewCount })
                    }}
                  >
                    {t('weightHistory.restore')}
                  </Button>
                )}
              </section>
            )}
          </>
        )}
      </div>
    </Dialog>
  )
}
