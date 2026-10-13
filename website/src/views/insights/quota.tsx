import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useTranslation } from 'react-i18next'
import {
  exportQuotaInsights,
  getQuotaInsights,
  getQuotaScopeOptions,
  reviewQuotaScope,
} from '@/api/insights-quota'
import { downloadReportCSV } from '@/api/usage-export'
import type {
  QuotaInsightsDimension,
  QuotaInsightsInput,
  QuotaInsightsKind,
  QuotaInsightsReport,
} from '@/types/insights-quota'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import QuotaInsightsResult from './quota-result'

export function useQuotaInsights({
  kind,
  actor,
  authorityStamp,
  authorized,
  permissions,
}: {
  kind: QuotaInsightsKind | null
  actor: string
  authorityStamp: string
  authorized: () => boolean
  permissions: string[]
}) {
  const { t } = useTranslation('insights')
  const cache = useQueryClient()
  const [dimension, setDimension] = useState<QuotaInsightsDimension>('tokens')
  const [scopeKind, setScopeKind] = useState<'member' | 'project'>(
    permissions.includes('members.read') ? 'member' : 'project',
  )
  const [scopeID, setScopeID] = useState('')
  const [q, setQ] = useState('')
  const [request, setRequest] = useState<{
    id: number
    input: QuotaInsightsInput
    targetGeneration: number
    pickerGeneration: number
  } | null>(null)
  const [notice, setNotice] = useState('')
  const [exporting, setExporting] = useState(false)
  const alive = useRef(true),
    sequence = useRef(0),
    lock = useRef(false),
    exportLock = useRef(false),
    exportController = useRef<AbortController | null>(null)
  const canMembers = permissions.includes('members.read')
  const canWriteProjects = permissions.includes('projects.write')
  const adminProjects = canWriteProjects && permissions.includes('projects.read_all')
  const pickerKey = useMemo(
    () => ['insights-quota-options', actor, authorityStamp, scopeKind, adminProjects, q],
    [actor, authorityStamp, scopeKind, adminProjects, q],
  )
  const options = useInfiniteQuery({
    queryKey: pickerKey,
    queryFn: ({ pageParam, signal }) =>
      getQuotaScopeOptions(scopeKind, adminProjects, q, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: kind === 'key_quota' && (scopeKind !== 'member' || canMembers),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
  })
  const targetKey = useMemo(
    () => ['insights-quota-target', actor, authorityStamp, scopeKind, scopeID],
    [actor, authorityStamp, scopeKind, scopeID],
  )
  const target = useQuery({
    queryKey: targetKey,
    queryFn: ({ signal }) => reviewQuotaScope(scopeKind, scopeID, actor, canWriteProjects, signal),
    enabled: kind === 'key_quota' && !!scopeID && (scopeKind !== 'member' || canMembers),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchInterval: kind === 'key_quota' ? 30000 : false,
  })
  const subscribe = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (event.type === 'updated' || event.type === 'removed') &&
          [pickerKey, targetKey].some(
            (key) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          changed()
      }),
    [cache, pickerKey, targetKey],
  )
  const snapshot = useCallback(
    () =>
      JSON.stringify(
        [pickerKey, targetKey].map((key) => {
          const state = cache.getQueryState(key)
          return [state?.status, state?.fetchStatus, state?.isInvalidated, state?.dataUpdateCount]
        }),
      ),
    [cache, pickerKey, targetKey],
  )
  useSyncExternalStore(subscribe, snapshot, snapshot)
  const scopeCurrent = useCallback(
    (captured: NonNullable<typeof request>) => {
      if (captured.input.kind !== 'key_quota') return true
      return [
        [targetKey, captured.targetGeneration],
        [pickerKey, captured.pickerGeneration],
      ].every(([key, generation]) => {
        const state = cache.getQueryState(key as readonly unknown[])
        return (
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          state.dataUpdateCount === generation
        )
      })
    },
    [cache, targetKey, pickerKey],
  )
  const targetReady =
    target.isSuccess &&
    !target.isFetching &&
    !target.isError &&
    !cache.getQueryState(targetKey)?.isInvalidated
  const targetGeneration = cache.getQueryState(targetKey)?.dataUpdateCount ?? 0
  const reportKey = useMemo(
    () => ['insights-quota-report', actor, authorityStamp, request],
    [actor, authorityStamp, request],
  )
  const live = useCallback(() => alive.current && authorized(), [authorized])
  const report = useQuery({
    queryKey: reportKey,
    enabled: request !== null,
    queryFn: async ({ signal }) => {
      if (!request) throw new DOMException('Aborted', 'AbortError')
      try {
        if (!live() || signal.aborted || sequence.current !== request.id || !scopeCurrent(request))
          throw new DOMException('Aborted', 'AbortError')
        if (request.input.scope.kind !== 'readable_teams') {
          await reviewQuotaScope(
            request.input.scope.kind,
            request.input.scope.id,
            actor,
            canWriteProjects,
            signal,
          )
          if (
            !live() ||
            signal.aborted ||
            sequence.current !== request.id ||
            !scopeCurrent(request)
          )
            throw new DOMException('Aborted', 'AbortError')
        }
        const result = await getQuotaInsights(request.input, signal)
        if (!live() || signal.aborted || sequence.current !== request.id || !scopeCurrent(request))
          throw new DOMException('Aborted', 'AbortError')
        return result
      } finally {
        if (sequence.current === request.id) lock.current = false
      }
    },
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      exportController.current?.abort()
      void cache.cancelQueries({ queryKey: ['insights-quota-report', actor, authorityStamp] })
      void cache.cancelQueries({ queryKey: ['insights-quota-options', actor, authorityStamp] })
      void cache.cancelQueries({ queryKey: ['insights-quota-target', actor, authorityStamp] })
      cache.removeQueries({ queryKey: ['insights-quota-report', actor, authorityStamp] })
      cache.removeQueries({ queryKey: ['insights-quota-options', actor, authorityStamp] })
      cache.removeQueries({ queryKey: ['insights-quota-target', actor, authorityStamp] })
    }
  }, [cache, actor, authorityStamp])
  useLayoutEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        if (!request || (event.type !== 'updated' && event.type !== 'removed')) return
        if (
          [targetKey, pickerKey].some(
            (key) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          ) &&
          !scopeCurrent(request)
        ) {
          exportController.current?.abort()
          void cache.cancelQueries({ queryKey: reportKey, exact: true })
        }
      }),
    [cache, request, targetKey, pickerKey, reportKey, scopeCurrent],
  )
  function clear() {
    sequence.current++
    lock.current = false
    exportController.current?.abort()
    void cache.cancelQueries({ queryKey: reportKey, exact: true })
    setRequest(null)
    setNotice('')
  }
  const pickerReady =
    options.isSuccess &&
    !options.isFetching &&
    !options.isError &&
    !cache.getQueryState(pickerKey)?.isInvalidated
  const candidates = pickerReady
    ? [
        ...new Map(
          options.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  function run(cursor: string | null = null) {
    if (
      !live() ||
      !kind ||
      lock.current ||
      exportLock.current ||
      (kind === 'key_quota' &&
        (!scopeID || !pickerReady || !targetReady || (scopeKind === 'member' && !canMembers)))
    )
      return
    clear()
    lock.current = true
    const input: QuotaInsightsInput = {
      kind,
      dimension,
      scope:
        kind === 'team_quota'
          ? { kind: 'readable_teams', id: '' }
          : { kind: scopeKind, id: scopeID },
      cursor,
    }
    setRequest({
      id: ++sequence.current,
      input,
      targetGeneration,
      pickerGeneration: cache.getQueryState(pickerKey)?.dataUpdateCount ?? 0,
    })
  }
  const ready =
    !!request &&
    scopeCurrent(request) &&
    report.isSuccess &&
    !report.isFetching &&
    !cache.getQueryState(reportKey)?.isInvalidated
  const current: QuotaInsightsReport | undefined = ready ? report.data : undefined
  async function exportCSV() {
    if (!live() || !request || !current || exportLock.current) return
    const captured = request,
      state = cache.getQueryState(reportKey)
    const generation = state?.dataUpdateCount
    const controller = new AbortController()
    exportController.current = controller
    exportLock.current = true
    setExporting(true)
    setNotice('')
    try {
      if (captured.input.scope.kind !== 'readable_teams')
        await reviewQuotaScope(
          captured.input.scope.kind,
          captured.input.scope.id,
          actor,
          canWriteProjects,
          controller.signal,
        )
      if (
        !live() ||
        controller.signal.aborted ||
        sequence.current !== captured.id ||
        !scopeCurrent(captured)
      )
        return
      const blob = await exportQuotaInsights(captured.input, controller.signal)
      const latest = cache.getQueryState(reportKey)
      if (
        !live() ||
        controller.signal.aborted ||
        sequence.current !== captured.id ||
        !scopeCurrent(captured) ||
        latest?.status !== 'success' ||
        latest.fetchStatus !== 'idle' ||
        latest.isInvalidated ||
        latest.dataUpdateCount !== generation
      )
        return
      downloadReportCSV(
        blob,
        captured.input.kind === 'team_quota' ? 'routex-team-quota.csv' : 'routex-key-quota.csv',
      )
      setNotice('quotaExportReady')
    } catch (error) {
      if (
        !live() ||
        controller.signal.aborted ||
        sequence.current !== captured.id ||
        !scopeCurrent(captured)
      )
        return
      const status = isAxiosError(error) ? error.response?.status : undefined
      setNotice(
        [401, 403, 404].includes(status ?? 0)
          ? 'quotaDenied'
          : status === 422
            ? 'quotaOverflow'
            : 'quotaExportFailed',
      )
    } finally {
      exportLock.current = false
      if (exportController.current === controller) exportController.current = null
      if (live()) setExporting(false)
    }
  }
  const status = isAxiosError(report.error) ? report.error.response?.status : undefined
  return {
    clear,
    busy: !!request && report.isFetching,
    exporting,
    exportCSV,
    cancelExport: () => {
      exportController.current?.abort()
      setNotice('')
    },
    canExport: !!current && !exporting,
    controls: (
      <form
        className="space-y-3"
        aria-label={t('quotaReview')}
        onSubmit={(event) => {
          event.preventDefault()
          run()
        }}
      >
        <p className="text-sm text-muted-foreground">{t('quotaFixedWindow')}</p>
        <FormField label={t('quotaDimension')}>
          <select
            className="h-9 w-full rounded-md border bg-background px-2 text-sm"
            aria-label={t('quotaDimension')}
            value={dimension}
            disabled={exporting}
            onChange={(event) => {
              clear()
              setDimension(event.target.value as QuotaInsightsDimension)
            }}
          >
            <option value="tokens">{t('quotaTokens')}</option>
            <option value="money">{t('quotaMoney')}</option>
          </select>
        </FormField>
        {kind === 'key_quota' && (
          <>
            <FormField label={t('quotaScopeKind')}>
              <select
                className="h-9 w-full rounded-md border bg-background px-2 text-sm"
                aria-label={t('quotaScopeKind')}
                value={scopeKind}
                disabled={exporting}
                onChange={(event) => {
                  clear()
                  setScopeID('')
                  setQ('')
                  setScopeKind(event.target.value as 'member' | 'project')
                }}
              >
                <option value="member" disabled={!canMembers}>
                  {t('quotaMemberScope')}
                </option>
                <option value="project">{t('quotaProjectScope')}</option>
              </select>
            </FormField>
            <FormField label={t('quotaScopeSearch')}>
              <Input
                name="quota-search"
                value={q}
                maxLength={200}
                disabled={exporting}
                onChange={(event) => {
                  clear()
                  setQ(event.target.value)
                }}
              />
            </FormField>
            <FormField label={t(scopeKind === 'member' ? 'quotaMemberScope' : 'quotaProjectScope')}>
              <select
                className="h-9 w-full rounded-md border bg-background px-2 text-sm"
                aria-label={t('quotaSelectedScope')}
                value={scopeID}
                disabled={!pickerReady || exporting}
                onChange={(event) => {
                  clear()
                  setScopeID(event.target.value)
                }}
              >
                <option value="">{t('quotaSelectScope')}</option>
                {scopeID && !candidates.some((item) => item.id === scopeID) && (
                  <option value={scopeID}>{scopeID}</option>
                )}
                {candidates.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name || item.id}
                  </option>
                ))}
              </select>
            </FormField>
            {options.isFetching && <p role="status">{t('quotaScopesLoading')}</p>}
            {options.isError && <p role="alert">{t('quotaScopesUnavailable')}</p>}
            {scopeID && target.isFetching && <p role="status">{t('quotaTargetLoading')}</p>}
            {scopeID && target.isError && <p role="alert">{t('quotaDenied')}</p>}
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={options.isFetching || exporting}
                onClick={() => {
                  clear()
                  if (live()) void options.refetch()
                }}
              >
                {t('quotaRefreshScopes')}
              </Button>
              {options.hasNextPage && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={options.isFetching || exporting}
                  onClick={() => {
                    clear()
                    if (live()) void options.fetchNextPage()
                  }}
                >
                  {t('quotaMoreScopes')}
                </Button>
              )}
            </div>
            <p className="text-xs text-muted-foreground">{t('quotaScopeHelp')}</p>
          </>
        )}
        <Button
          type="submit"
          disabled={
            report.isFetching ||
            exporting ||
            (kind === 'key_quota' && (!scopeID || !pickerReady || !targetReady))
          }
        >
          {t('quotaRun')}
        </Button>
        {!!request && report.isFetching && (
          <Button type="button" variant="outline" onClick={clear}>
            {t('cancel')}
          </Button>
        )}
      </form>
    ),
    result: (
      <div className="space-y-4">
        {notice && <p role="status">{t(notice)}</p>}
        {request && report.isFetching && <p role="status">{t('quotaLoading')}</p>}
        {request && report.isError && (
          <p role="alert">
            {t(
              [401, 403, 404].includes(status ?? 0)
                ? 'quotaDenied'
                : status === 422
                  ? 'quotaOverflow'
                  : 'quotaUnavailable',
            )}
          </p>
        )}
        {!request && (
          <p className="py-8 text-center text-sm text-muted-foreground">
            {t('quotaAwaitingReview')}
          </p>
        )}
        {current && (
          <>
            <QuotaInsightsResult report={current} />
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" disabled={exporting} onClick={() => run()}>
                {t('quotaRefresh')}
              </Button>
              {current.next_cursor && (
                <Button
                  variant="outline"
                  disabled={exporting}
                  onClick={() => run(current.next_cursor)}
                >
                  {t('quotaNextPage')}
                </Button>
              )}
            </div>
          </>
        )}
      </div>
    ),
  }
}
