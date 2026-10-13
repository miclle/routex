import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useTranslation } from 'react-i18next'
import { getInsightsModels, analyzeInsights, isInsightsQuestion } from '@/api/insights'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { InsightsAnalysis, InsightsAnalysisInput } from '@/types/insights'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Table } from '@/components/ui/table'
import { readInsightsRange, type InsightsRange } from './range'
import { usageTime } from '@/views/usage/format'

export function useInsightsAI({
  actor,
  authorityStamp,
  authorized,
  range,
  question,
  report,
  supported,
}: {
  actor: string
  authorityStamp: string
  authorized: () => boolean
  range: InsightsRange
  question?: string
  report: InsightsAnalysisInput['report']
  supported: boolean
}) {
  const { t, i18n } = useTranslation('insights')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const completionText = (completion: InsightsAnalysis['completion']) => {
    switch (completion) {
      case 'completed':
        return t('aiCompletion_completed')
      case 'handoff':
        return t('aiCompletion_handoff')
      case 'blocked':
        return t('aiCompletion_blocked')
      case 'incomplete':
        return t('aiCompletion_incomplete')
      default:
        return t('aiCompletion_unknown')
    }
  }
  const cache = useQueryClient()
  const [model, setModel] = useState('')
  const [review, setReview] = useState<{
    context: string
    epoch: number
    input: InsightsAnalysisInput
  } | null>(null)
  const [response, setResponse] = useState<{
    context: string
    language: 'en' | 'zh'
    question?: string
    value: InsightsAnalysis
  } | null>(null)
  const [error, setError] = useState<{ context: string; key: string } | null>(null)
  const [pending, setPending] = useState(false)
  const controller = useRef<AbortController | null>(null)
  const lock = useRef(false)
  const mounted = useRef(false)
  const epoch = useRef(0)
  const key = useMemo(() => ['insights-models', actor, authorityStamp], [actor, authorityStamp])
  const models = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getInsightsModels(signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
  })
  const modelSnapshot = useCallback(() => {
    const state = cache.getQueryState(key)
    return JSON.stringify([
      state?.status,
      state?.fetchStatus,
      state?.isInvalidated,
      state?.dataUpdateCount,
    ])
  }, [cache, key])
  const subscribe = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (event.type === 'updated' || event.type === 'removed') &&
          JSON.stringify(event.query.queryKey) === JSON.stringify(key)
        )
          changed()
      }),
    [cache, key],
  )
  const modelStamp = useSyncExternalStore(subscribe, modelSnapshot, modelSnapshot)
  const context = JSON.stringify([
    authorityStamp,
    modelStamp,
    model,
    range,
    question,
    report,
    supported,
  ])
  const modelState = cache.getQueryState(key)
  const optionsReady =
    models.isSuccess &&
    !models.isFetching &&
    !models.isError &&
    modelState?.status === 'success' &&
    modelState.fetchStatus === 'idle' &&
    !modelState.isInvalidated
  const selected = optionsReady ? models.data.find((item) => item.model_id === model) : undefined
  const current = useCallback(
    () =>
      mounted.current &&
      authorized() &&
      modelSnapshot() === modelStamp &&
      cache.getQueryState(key)?.status === 'success' &&
      cache.getQueryState(key)?.fetchStatus === 'idle' &&
      !cache.getQueryState(key)?.isInvalidated,
    [authorized, cache, key, modelSnapshot, modelStamp],
  )
  const invalidate = useCallback(() => {
    epoch.current += 1
    controller.current?.abort()
    setReview(null)
    setResponse(null)
    setError(null)
  }, [])
  useLayoutEffect(() => {
    mounted.current = true
    const stop = cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const eventKey = event.query.queryKey
      if (
        JSON.stringify(eventKey) === JSON.stringify(key) ||
        (eventKey[0] === 'auth' && eventKey[1] === 'session') ||
        (eventKey[0] === 'permissions' && eventKey[1] === actor)
      ) {
        epoch.current += 1
        controller.current?.abort()
      }
    })
    return () => {
      mounted.current = false
      epoch.current += 1
      controller.current?.abort()
      stop()
    }
  }, [cache, key, actor])
  useLayoutEffect(() => {
    epoch.current += 1
    controller.current?.abort()
  }, [context])
  function prepare() {
    if (!current() || lock.current || !selected || !supported) return
    const filters = readInsightsRange(range)
    if (typeof filters === 'string') {
      setError({ context, key: filters })
      return
    }
    const input: InsightsAnalysisInput = {
      report,
      ...(question === undefined ? {} : { question }),
      model_id: selected.model_id,
      from: filters.from!,
      to: filters.to!,
      timezone: filters.timezone,
      language: i18n.resolvedLanguage === 'zh' ? 'zh' : 'en',
    }
    if (question !== undefined && !isInsightsQuestion(question)) {
      setError({ context, key: 'questionInvalid' })
      return
    }
    if (new TextEncoder().encode(JSON.stringify(input)).length > 2048) {
      setError({ context, key: 'questionEnvelopeTooLarge' })
      return
    }
    invalidate()
    setReview({ context, epoch: epoch.current, input })
  }
  async function submit() {
    if (
      !review ||
      review.epoch !== epoch.current ||
      review.context !== context ||
      !current() ||
      lock.current ||
      !selected ||
      selected.model_id !== review.input.model_id
    )
      return
    const session = cache.getQueryData<Session | null>(sessionKey)
    if (!session?.csrf_token || session.user.id !== actor) return
    const captured = review
    const capturedEpoch = ++epoch.current
    const abort = new AbortController()
    controller.current = abort
    lock.current = true
    setPending(true)
    setReview(null)
    setResponse(null)
    setError(null)
    try {
      const value = await analyzeInsights({ ...captured.input }, session.csrf_token, abort.signal)
      if (abort.signal.aborted || epoch.current !== capturedEpoch || !current()) return
      setResponse({
        context: captured.context,
        language: captured.input.language,
        ...(captured.input.question === undefined ? {} : { question: captured.input.question }),
        value,
      })
    } catch (caught) {
      if (abort.signal.aborted || epoch.current !== capturedEpoch || !current()) return
      const status = isAxiosError(caught) ? caught.response?.status : undefined
      setError({
        context: captured.context,
        key: [401, 403, 404].includes(status ?? 0)
          ? 'aiDenied'
          : status === 429
            ? 'aiQuota'
            : status === 422
              ? 'aiOverflow'
              : status === 400
                ? 'invalidRange'
                : 'aiUnavailable',
      })
    } finally {
      lock.current = false
      if (controller.current === abort) controller.current = null
      if (mounted.current) setPending(false)
    }
  }
  const answer = response?.context === context && optionsReady ? response : null
  return {
    invalidate,
    controls: (
      <div className="space-y-3 border-t pt-3">
        <h3 className="font-medium">{t('aiTitle')}</h3>
        <p className="text-xs text-muted-foreground">
          {t(report === 'provider_quality' ? 'aiProviderDisclosure' : 'aiDisclosure')}
        </p>
        <p className="text-xs text-muted-foreground">{t('aiStateless')}</p>
        <FormField label={t('aiModel')}>
          <select
            aria-label={t('aiModel')}
            value={selected?.model_id ?? ''}
            onChange={(event) => {
              invalidate()
              setModel(event.target.value)
            }}
            className="h-9 w-full rounded-md border bg-background px-2 text-sm"
            disabled={!optionsReady}
          >
            <option value="">{t('aiSelectModel')}</option>
            {optionsReady &&
              models.data.map((item) => (
                <option key={item.model_id} value={item.model_id}>
                  {item.name || item.model_id}
                </option>
              ))}
          </select>
        </FormField>
        {(models.isPending || models.isFetching) && (
          <p role="status" className="text-sm">
            {t('aiModelsLoading')}
          </p>
        )}
        {models.isError && (
          <p role="alert" className="text-sm">
            {t('aiModelsUnavailable')}
          </p>
        )}
        {optionsReady && models.data.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('aiNoModels')}</p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={models.isFetching || pending}
            onClick={() => {
              invalidate()
              void models.refetch()
            }}
          >
            {t('aiRefreshModels')}
          </Button>
          <Button disabled={!selected || !supported || pending} onClick={prepare}>
            {t('aiReview')}
          </Button>
          {pending && (
            <Button variant="outline" onClick={invalidate}>
              {t('aiCancel')}
            </Button>
          )}
        </div>
        {error?.context === context && (
          <p role="alert" className="text-sm">
            {t(error.key)}
          </p>
        )}
        <Dialog
          open={review?.context === context && optionsReady}
          onOpenChange={(open) => {
            if (!open) invalidate()
          }}
          title={t('aiConfirmTitle')}
          description={t('aiConfirmDescription')}
        >
          {review && (
            <div className="space-y-4 text-sm">
              <p className="font-medium">
                {t(
                  review.input.report === 'provider_quality'
                    ? 'shortcutProvider'
                    : 'shortcutTokens',
                )}
              </p>
              <p>{selected?.name || review.input.model_id}</p>
              {review.input.question !== undefined && (
                <div>
                  <p className="font-medium">{t('aiReviewedQuestion')}</p>
                  <p className="whitespace-pre-wrap break-words">{review.input.question}</p>
                </div>
              )}
              {review.input.question !== undefined && (
                <p className="text-muted-foreground">{t('aiQuestionBoundary')}</p>
              )}
              <p>
                {t('range', {
                  from: usageTime(review.input.from, locale, review.input.timezone),
                  to: usageTime(review.input.to, locale, review.input.timezone),
                  timezone: review.input.timezone,
                })}
              </p>
              <p>
                {t('aiLanguage', {
                  language: t(review.input.language === 'zh' ? 'aiChinese' : 'aiEnglish'),
                })}
              </p>
              <p className="text-muted-foreground">{t('aiCaptureHelp')}</p>
              <Button onClick={() => void submit()} disabled={pending}>
                {t('aiConfirm')}
              </Button>
            </div>
          )}
        </Dialog>
      </div>
    ),
    answer: (
      <section className="space-y-3 border-t pt-4" aria-label={t('aiTitle')}>
        <h3 className="font-medium">{t('aiTitle')}</h3>
        <p className="text-xs text-muted-foreground">{t('aiCaptureHelp')}</p>
        {pending && <p role="status">{t('aiPending')}</p>}
        {!pending && !answer && <p className="text-sm text-muted-foreground">{t('aiAwaiting')}</p>}
        {answer && (
          <>
            {answer.question !== undefined && (
              <div>
                <p className="font-medium">{t('aiReviewedQuestion')}</p>
                <p className="whitespace-pre-wrap break-words text-sm">{answer.question}</p>
              </div>
            )}
            <p role="status">{completionText(answer.value.completion)}</p>
            <p className="text-xs text-muted-foreground">
              {t('aiLanguage', {
                language: t(answer.language === 'zh' ? 'aiChinese' : 'aiEnglish'),
              })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('aiRequestID', { id: answer.value.request_id })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t(
                answer.value.capture.source === 'persisted_call_attempts'
                  ? 'qualitySource'
                  : 'source',
              )}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('range', {
                from: usageTime(answer.value.capture.from, locale, answer.value.capture.timezone),
                to: usageTime(answer.value.capture.to, locale, answer.value.capture.timezone),
                timezone: answer.value.capture.timezone,
              })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('queriedAt', {
                time: usageTime(
                  answer.value.capture.queried_at,
                  locale,
                  answer.value.capture.timezone,
                ),
              })}
            </p>
            <p className="text-xs text-muted-foreground">
              {answer.value.capture.latest_completed_at
                ? t('latestCompletedAt', {
                    time: usageTime(
                      answer.value.capture.latest_completed_at,
                      locale,
                      answer.value.capture.timezone,
                    ),
                  })
                : t('noCompletedCalls')}
            </p>
            {answer.value.capture.may_lag && (
              <p className="text-xs text-muted-foreground">{t('mayLag')}</p>
            )}
            {answer.value.completion === 'completed' && (
              <p className="whitespace-pre-wrap break-words text-sm">{answer.value.text}</p>
            )}
            <p className="text-xs text-muted-foreground">{t('aiAdvisory')}</p>
            {answer.value.labels.length > 0 && (
              <Table
                aria-label={t(report === 'provider_quality' ? 'aiProviderLabels' : 'aiLabels')}
              >
                <thead>
                  <tr>
                    <th>{t('aiLabel')}</th>
                    <th>
                      {t(report === 'provider_quality' ? 'aiRecordedProvider' : 'aiRecordedModel')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {answer.value.labels.map((item) => (
                    <tr key={item.label}>
                      <td>{item.label}</td>
                      <td className="break-all">
                        {item.unknown
                          ? t('unknownModel')
                          : item.name || item.provider_id || item.model_id}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </>
        )}
      </section>
    ),
  }
}
