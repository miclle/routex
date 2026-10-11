import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import {
  getModelLifecycle,
  updateModelLifecycle,
  validModelLifecycleReason,
} from '@/api/model-lifecycle'
import type {
  ModelLifecycleInput,
  ModelLifecycleResult,
  ModelLifecycleReview,
} from '@/types/model-lifecycle'
import type { Session } from '@/types/auth'
import { sessionKey } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Intent = { input: ModelLifecycleInput; etag: string }
export default function ModelLifecycleDialog({
  actor,
  modelID,
  modelBirth,
  generation,
  resourceGeneration,
  permissionGeneration,
  readable,
  readReady,
  writeReady,
  open,
  onOpenChange,
  onCaptureChange,
}: {
  actor: string
  modelID: string
  modelBirth: string | null | undefined
  generation: number
  resourceGeneration: number
  permissionGeneration: number
  readable: boolean
  readReady: () => boolean
  writeReady: () => boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  onCaptureChange: (locked: boolean) => void
}) {
  const { t, i18n } = useTranslation('catalog')
  const cache = useQueryClient()
  const [reason, setReason] = useState('')
  const [reviewRequired, setReviewRequired] = useState(false)
  const [reviewed, setReviewed] = useState<{
    value: ModelLifecycleReview
    authority: string
  } | null>(null)
  const [confirmation, setConfirmation] = useState<{
    review: ModelLifecycleReview
    status: ModelLifecycleInput['status']
    reason: string
    authority: string
  } | null>(null)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [result, setResult] = useState<{ value: ModelLifecycleResult; authority: string } | null>(
    null,
  )
  const [notice, setNotice] = useState<
    'conflict' | 'failed' | 'unknown' | 'pending' | 'reasonError' | null
  >(null)
  const [pending, setPending] = useState(false)
  const submitting = useRef(false)
  const controller = useRef<AbortController | null>(null)
  const version = useRef(0)
  const authority = JSON.stringify([generation, resourceGeneration, permissionGeneration, readable])
  const queryKey = [
    'admin',
    'model-lifecycle',
    actor,
    modelID,
    modelBirth,
    generation,
    resourceGeneration,
    permissionGeneration,
  ]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      const value = await getModelLifecycle(modelID, signal)
      if (modelBirth && value.created_at !== modelBirth)
        throw new Error('Model lifecycle target changed')
      return value
    },
    enabled: readable && open,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  function freshReview() {
    const state = cache.getQueryState(queryKey)
    return readable &&
      readReady() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated
      ? cache.getQueryData<ModelLifecycleReview>(queryKey)
      : undefined
  }
  const current =
    open &&
    readable &&
    query.isSuccess &&
    !query.isFetching &&
    !query.isError &&
    !cache.getQueryState(queryKey)?.isInvalidated
      ? query.data
      : undefined
  const basis =
    reviewed?.authority === authority ? reviewed.value : !reviewRequired ? current : undefined
  const confirming = confirmation?.authority === authority ? confirmation : null
  const confirmedResult = result?.authority === authority ? result.value : null
  useLayoutEffect(() => {
    const epoch = ++version.current
    const obsolete = controller.current
    controller.current = null
    submitting.current = false
    obsolete?.abort()
    queueMicrotask(() => {
      if (epoch !== version.current) return
      if (!controller.current && !submitting.current) setPending(false)
      if (obsolete) setNotice('unknown')
      setReviewed(null)
      setConfirmation(null)
      setResult(null)
    })
  }, [authority, open])
  useEffect(
    () => () => {
      ++version.current
      controller.current?.abort()
    },
    [],
  )
  async function review() {
    setReviewed(null)
    setConfirmation(null)
    setResult(null)
    const epoch = version.current
    const checked = await query.refetch()
    if (epoch === version.current && !checked.isError && checked.data && freshReview()) {
      setReviewed({ value: checked.data, authority })
      setReviewRequired(false)
      if (!intent) setNotice(null)
    }
  }
  async function dispatch(captured: Intent, retry: boolean) {
    const live = freshReview()
    if (
      submitting.current ||
      !writeReady() ||
      !live ||
      reviewRequired ||
      (!live.can_change &&
        !(
          retry &&
          captured.input.status === 'archived' &&
          live.status === 'archived' &&
          live.etag.slice(0, 64) === captured.etag.slice(0, 64)
        )) ||
      (live.status === 'archived' && !(retry && captured.input.status === 'archived'))
    )
      return
    if (
      !retry &&
      (live.etag !== captured.etag ||
        (captured.input.status === 'archived' && live.status !== 'disabled'))
    )
      return
    submitting.current = true
    setPending(true)
    setConfirmation(null)
    setResult(null)
    setIntent(captured)
    onCaptureChange(true)
    setNotice('unknown')
    const epoch = version.current
    const active = new AbortController()
    controller.current = active
    try {
      const response = await updateModelLifecycle(
        modelID,
        captured.input,
        captured.etag,
        cache.getQueryData<Session>(sessionKey)!.csrf_token,
        active.signal,
      )
      if (active.signal.aborted || epoch !== version.current || !writeReady() || !freshReview())
        return
      if (response.model.created_at !== live.created_at)
        throw new Error('Model lifecycle identity changed')
      setResult({ value: response, authority })
      if (response.runtime_applied) {
        setIntent(null)
        onCaptureChange(false)
        setNotice(null)
      } else setNotice('pending')
      // Catalogue renewal hides obsolete facts; it cannot establish the original operation.
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
      void cache.invalidateQueries({ queryKey: ['models'] })
      void query.refetch()
    } catch (error) {
      if (active.signal.aborted || epoch !== version.current) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      // An error can follow a committed write; it never proves the original intent failed.
      if (status && [401, 403, 404, 409].includes(status)) {
        setReviewRequired(true)
        setReviewed(null)
      }
      setNotice(status === 409 ? 'conflict' : 'unknown')
      if (status && [401, 403, 404].includes(status))
        void cache.invalidateQueries({ queryKey: ['permissions', actor] })
    } finally {
      if (controller.current === active) {
        controller.current = null
        submitting.current = false
        setPending(false)
      }
    }
  }
  function prepare(status: ModelLifecycleInput['status']) {
    if (!validModelLifecycleReason(reason)) {
      setNotice('reasonError')
      return
    }
    const live = freshReview()
    if (
      !writeReady() ||
      !live?.can_change ||
      live.status === 'archived' ||
      !basis ||
      reviewRequired ||
      basis.etag !== live.etag ||
      (status === 'archived' && live.status !== 'disabled')
    )
      return
    setNotice(null)
    setConfirmation({ review: structuredClone(basis), status, reason, authority })
  }
  const retryArchive =
    intent?.input.status === 'archived' &&
    current?.status === 'archived' &&
    current.etag.slice(0, 64) === intent.etag.slice(0, 64)
  const stateLabel = (status: ModelLifecycleReview['status']) =>
    t(`modelLifecycle.states.${status}`)
  return (
    <Dialog
      open={open && readable}
      onOpenChange={onOpenChange}
      busy={pending}
      title={t(
        confirming?.status === 'archived'
          ? 'modelLifecycle.archiveConfirmTitle'
          : confirming
            ? 'modelLifecycle.confirmTitle'
            : 'modelLifecycle.title',
      )}
      description={t('modelLifecycle.description')}
    >
      <QueryState pending={query.isFetching} error={query.error} retry={() => void review()} />
      {current && (
        <div className="space-y-4">
          <dl className="space-y-2 text-sm">
            <div>
              <dt className="text-muted-foreground">{t('modelLifecycle.name')}</dt>
              <dd className="break-all">{current.current_name}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('modelLifecycle.id')}</dt>
              <dd className="break-all font-mono">{current.model_id}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('modelLifecycle.status')}</dt>
              <dd>{stateLabel(current.status)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('modelLifecycle.observed')}</dt>
              <dd>
                {new Date(current.observed_at).toLocaleString(
                  i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                )}
              </dd>
            </div>
          </dl>
          <p className="text-sm text-muted-foreground">
            {t(
              current.runtime_applied
                ? 'modelLifecycle.runtimeApplied'
                : 'modelLifecycle.runtimeUnknown',
            )}
          </p>
          <p className="text-sm text-muted-foreground">{t('modelLifecycle.boundary')}</p>
          {notice && (
            <p role="alert" className="text-sm text-destructive">
              {t(`modelLifecycle.${notice}`)}
            </p>
          )}
          {confirmedResult && (
            <p role="status">
              {t(
                confirmedResult.runtime_applied
                  ? 'modelLifecycle.responseConfirmed'
                  : 'modelLifecycle.responsePending',
              )}
            </p>
          )}
          {intent ? (
            <>
              <p>
                {t('modelLifecycle.capturedStatus', { status: stateLabel(intent.input.status) })}
              </p>
              <p className="break-all text-sm">
                {t('modelLifecycle.capturedReason', { reason: intent.input.reason })}
              </p>
              <div className="flex flex-wrap justify-end gap-3">
                <Button variant="outline" onClick={() => void review()} disabled={pending}>
                  {t('modelLifecycle.review')}
                </Button>
                <Button
                  onClick={() => void dispatch(intent, true)}
                  disabled={
                    pending ||
                    reviewRequired ||
                    !writeReady() ||
                    (!current.can_change && !retryArchive) ||
                    (current.status === 'archived' && !retryArchive)
                  }
                >
                  {t(pending ? 'modelLifecycle.submitting' : 'modelLifecycle.retry')}
                </Button>
              </div>
            </>
          ) : confirming ? (
            <>
              <p>
                {t('modelLifecycle.confirmTarget', {
                  name: confirming.review.current_name,
                  status: stateLabel(confirming.status),
                })}
              </p>
              <p className="break-all text-sm">
                {t('modelLifecycle.capturedReason', { reason: confirming.reason })}
              </p>
              {confirming.status === 'archived' && (
                <p role="alert" className="text-sm text-destructive">
                  {t('modelLifecycle.archiveWarning')}
                </p>
              )}
              <div className="flex justify-end gap-3">
                <Button variant="outline" onClick={() => setConfirmation(null)}>
                  {t('modelLifecycle.cancel')}
                </Button>
                <Button
                  variant="outline"
                  className={
                    confirming.status === 'archived'
                      ? 'border-destructive text-destructive hover:bg-destructive/10'
                      : undefined
                  }
                  disabled={
                    !writeReady() ||
                    !current.can_change ||
                    current.status === 'archived' ||
                    (confirming.status === 'archived' && current.status !== 'disabled') ||
                    current.etag !== confirming.review.etag
                  }
                  onClick={() =>
                    void dispatch(
                      {
                        input: {
                          status: confirming.status,
                          reason: confirming.reason,
                        },
                        etag: confirming.review.etag,
                      },
                      false,
                    )
                  }
                >
                  {t(
                    confirming.status === 'archived'
                      ? 'modelLifecycle.confirmArchive'
                      : 'modelLifecycle.confirm',
                  )}
                </Button>
              </div>
            </>
          ) : (
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault()
                if (current.status !== 'archived')
                  prepare(current.status === 'active' ? 'disabled' : 'active')
              }}
            >
              {!current.can_change && (
                <p className="text-sm text-muted-foreground">
                  {t(
                    current.status === 'archived'
                      ? 'modelLifecycle.archived'
                      : 'modelLifecycle.readOnly',
                  )}
                </p>
              )}
              {current.status !== 'archived' && (
                <FormField label={t('modelLifecycle.reason')}>
                  <Input
                    name="lifecycle_reason"
                    value={reason}
                    onValueChange={setReason}
                    disabled={!current.can_change || !writeReady()}
                    autoComplete="off"
                  />
                </FormField>
              )}
              <div className="flex flex-wrap justify-end gap-3">
                <Button variant="outline" onClick={() => void review()}>
                  {t('modelLifecycle.review')}
                </Button>
                {current.status === 'disabled' && (
                  <Button
                    type="button"
                    variant="outline"
                    className="border-destructive text-destructive hover:bg-destructive/10"
                    disabled={
                      !current.can_change ||
                      !writeReady() ||
                      reviewRequired ||
                      basis?.etag !== current.etag
                    }
                    onClick={() => prepare('archived')}
                  >
                    {t('modelLifecycle.archive')}
                  </Button>
                )}
                {current.status !== 'archived' && (
                  <Button
                    type="submit"
                    disabled={
                      !current.can_change ||
                      !writeReady() ||
                      reviewRequired ||
                      basis?.etag !== current.etag
                    }
                  >
                    {t(
                      current.status === 'active'
                        ? 'modelLifecycle.disable'
                        : 'modelLifecycle.enable',
                    )}
                  </Button>
                )}
              </div>
            </form>
          )}
        </div>
      )}
    </Dialog>
  )
}
