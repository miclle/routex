import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { ArrowRight, Bot, Download, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getPermissions } from '@/api/governance'
import { getUsage } from '@/api/usage'
import { downloadReportCSV, downloadUsageCSV, exportUsage } from '@/api/usage-export'
import { getProviderQualityReport, exportProviderQuality } from '@/api/insights-provider-quality'
import ProviderQualityResult from './provider-quality'
import type { ProviderQualityRange } from '@/types/insights'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { Page, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Table } from '@/components/ui/table'
import type { Session } from '@/types/auth'
import type { UsageFilters } from '@/types/usage'
import { readInsightsRange, type InsightsRange } from './range'
import InsightsResult from './result'
import { useInsightsAI } from './ai'

type PermissionKey = readonly ['permissions', string, 'insights', number]
function authorityState(cache: QueryClient, actor: string, key: PermissionKey) {
  const session = cache.getQueryState<Session | null>(sessionKey)
  const permissions = cache.getQueryState<string[]>(key)
  return {
    ready:
      session?.status === 'success' &&
      session.fetchStatus === 'idle' &&
      !session.isInvalidated &&
      session.data?.user.id === actor &&
      permissions?.status === 'success' &&
      permissions.fetchStatus === 'idle' &&
      !permissions.isInvalidated &&
      permissions.data?.includes('calls.read_all') === true,
    providerReady: permissions?.data?.includes('providers.read') === true,
    stamp: JSON.stringify([
      session?.status,
      session?.fetchStatus,
      session?.isInvalidated,
      session?.dataUpdateCount,
      session?.data?.user.id,
      permissions?.status,
      permissions?.fetchStatus,
      permissions?.isInvalidated,
      permissions?.dataUpdateCount,
      permissions?.data?.includes('calls.read_all'),
      permissions?.data?.includes('providers.read'),
    ]),
  }
}

export default function InsightsPage() {
  const session = useSession()
  const generation = useSessionGeneration()
  const { t } = useTranslation('insights')
  if (session.isPending || session.isFetching) return <p role="status">{t('loading')}</p>
  if (session.isError || !session.data) return <p role="alert">{t('denied')}</p>
  return (
    <InsightsAuthority
      key={session.data.user.id}
      actor={session.data.user.id}
      generation={generation}
    />
  )
}
function InsightsAuthority({ actor, generation }: { actor: string; generation: number }) {
  const { t } = useTranslation('insights')
  const cache = useQueryClient()
  const key = useMemo<PermissionKey>(
    () => ['permissions', actor, 'insights', generation],
    [actor, generation],
  )
  const permissions = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getPermissions(signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchInterval: 30_000,
  })
  const subscribe = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated' && event.type !== 'removed') return
        if (
          JSON.stringify(event.query.queryKey) === JSON.stringify(key) ||
          JSON.stringify(event.query.queryKey) === JSON.stringify(sessionKey)
        )
          changed()
      }),
    [cache, key],
  )
  const snapshot = useCallback(() => authorityState(cache, actor, key).stamp, [cache, actor, key])
  const stamp = useSyncExternalStore(subscribe, snapshot, snapshot)
  if (permissions.isPending || permissions.isFetching) return <p role="status">{t('loading')}</p>
  if (permissions.isError || !authorityState(cache, actor, key).ready)
    return <p role="alert">{t('denied')}</p>
  return <InsightsContent key={stamp} actor={actor} permissionKey={key} authorityStamp={stamp} />
}

function InsightsContent({
  actor,
  permissionKey,
  authorityStamp,
}: {
  actor: string
  permissionKey: PermissionKey
  authorityStamp: string
}) {
  const { t } = useTranslation('insights')
  const cache = useQueryClient()
  const [draft, setDraft] = useState<InsightsRange>({ from: '', to: '', timezone: 'UTC' })
  const [input, setInput] = useState('')
  const [question, setQuestion] = useState<{ key: string } | { text: string }>({
    key: 'shortcutTokens',
  })
  const [providerReport, setProviderReport] = useState(false)
  const [supported, setSupported] = useState(true)
  const [message, setMessage] = useState('awaitingReview')
  const [request, setRequest] = useState<{ id: number; filters: UsageFilters } | null>(null)
  const sequence = useRef(0)
  const requestLock = useRef(false)
  const alive = useRef(true)
  const exportLock = useRef(false)
  const exportController = useRef<AbortController | null>(null)
  const [exporting, setExporting] = useState(false)
  const [exportMessage, setExportMessage] = useState('')
  const [denied, setDenied] = useState(false)
  const authorized = useCallback(() => {
    const state = authorityState(cache, actor, permissionKey)
    return alive.current && state.ready && state.stamp === authorityStamp
  }, [cache, actor, permissionKey, authorityStamp])
  const providerAuthorized = () =>
    authorized() && authorityState(cache, actor, permissionKey).providerReady
  const reportKey = useMemo(
    () => ['insights-report', actor, authorityStamp, request],
    [actor, authorityStamp, request],
  )
  const report = useQuery({
    queryKey: reportKey,
    enabled: request !== null,
    queryFn: async ({ signal }) => {
      if (!request) throw new DOMException('Aborted', 'AbortError')
      try {
        if (!authorized() || signal.aborted || sequence.current !== request.id)
          throw new DOMException('Aborted', 'AbortError')
        if (providerReport) {
          if (!providerAuthorized()) throw new DOMException('Aborted', 'AbortError')
          const filters = request.filters
          return {
            kind: 'provider' as const,
            value: await getProviderQualityReport(
              { from: filters.from!, to: filters.to!, timezone: filters.timezone! },
              signal,
            ),
          }
        }
        return {
          kind: 'tokens' as const,
          value: await getUsage({ admin: true }, request.filters, signal),
        }
      } finally {
        if (sequence.current === request.id) requestLock.current = false
      }
    },
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })
  const subscribeReport = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (event.type === 'updated' || event.type === 'removed') &&
          JSON.stringify(event.query.queryKey) === JSON.stringify(reportKey)
        )
          changed()
      }),
    [cache, reportKey],
  )
  const reportSnapshot = useCallback(() => {
    const state = cache.getQueryState(reportKey)
    return JSON.stringify([
      state?.status,
      state?.fetchStatus,
      state?.isInvalidated,
      state?.dataUpdateCount,
    ])
  }, [cache, reportKey])
  const reportStamp = useSyncExternalStore(subscribeReport, reportSnapshot, reportSnapshot)
  const currentReport =
    request &&
    report.isSuccess &&
    !report.isFetching &&
    !report.isError &&
    !cache.getQueryState(reportKey)?.isInvalidated &&
    !denied
      ? report.data
      : undefined
  const ai = useInsightsAI({
    actor,
    authorityStamp,
    authorized,
    range: draft,
    supported: supported && !providerReport,
  })
  useLayoutEffect(() => {
    alive.current = true
    const stop = cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const key = JSON.stringify(event.query.queryKey)
      if (key === JSON.stringify(sessionKey) || key === JSON.stringify(permissionKey)) {
        if (!authorized()) {
          alive.current = false
          exportController.current?.abort()
          void cache.cancelQueries({ queryKey: reportKey, exact: true })
        }
      }
      if (key === JSON.stringify(reportKey)) exportController.current?.abort()
    })
    return () => {
      alive.current = false
      exportController.current?.abort()
      stop()
    }
  }, [cache, permissionKey, reportKey, authorized])
  function clearReport() {
    ai.invalidate()
    sequence.current += 1
    exportController.current?.abort()
    void cache.cancelQueries({ queryKey: reportKey, exact: true })
    requestLock.current = false
    setRequest(null)
    setDenied(false)
    setExportMessage('')
  }
  function shortcut(key: string) {
    if (!authorized() || requestLock.current || exportLock.current) return
    clearReport()
    setQuestion({ key })
    setProviderReport(key === 'shortcutProvider')
    const permitted = key !== 'shortcutProvider' || providerAuthorized()
    setSupported((key === 'shortcutTokens' || key === 'shortcutProvider') && permitted)
    setMessage(
      !permitted
        ? 'providerReadDenied'
        : key === 'shortcutTokens' || key === 'shortcutProvider'
          ? 'awaitingReview'
          : 'unsupported',
    )
  }
  function run() {
    if (
      !authorized() ||
      requestLock.current ||
      exportLock.current ||
      (request && report.isFetching) ||
      !supported ||
      (providerReport && !providerAuthorized())
    )
      return
    const filters = readInsightsRange(draft)
    if (typeof filters === 'string') {
      setMessage(filters)
      return
    }
    clearReport()
    requestLock.current = true
    setMessage('')
    setRequest({ id: ++sequence.current, filters })
  }
  function currentExportAuthority() {
    const state = cache.getQueryState(reportKey)
    return (
      authorized() &&
      (!providerReport || providerAuthorized()) &&
      !!request &&
      sequence.current === request.id &&
      !denied &&
      !!currentReport &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      reportSnapshot() === reportStamp
    )
  }
  async function exportCSV() {
    if (exportLock.current || !request || !currentExportAuthority()) return
    const controller = new AbortController()
    exportController.current = controller
    exportLock.current = true
    setExporting(true)
    setExportMessage('')
    try {
      const range: ProviderQualityRange = {
        from: request.filters.from!,
        to: request.filters.to!,
        timezone: request.filters.timezone!,
      }
      const blob = providerReport
        ? await exportProviderQuality(range, controller.signal)
        : await exportUsage({ admin: true }, { ...request.filters }, controller.signal)
      if (controller.signal.aborted || !currentExportAuthority()) return
      if (providerReport) downloadReportCSV(blob, 'routex-provider-quality.csv')
      else downloadUsageCSV(blob, { admin: true })
      setExportMessage('exportReady')
    } catch (error) {
      if (controller.signal.aborted || !authorized()) return
      const status = isAxiosError(error) ? error.response?.status : undefined
      if ([401, 403, 404].includes(status ?? 0)) setDenied(true)
      setExportMessage(
        [401, 403, 404].includes(status ?? 0)
          ? providerReport
            ? 'providerReadDenied'
            : 'exportDenied'
          : status === 422
            ? 'exportOverflow'
            : 'exportFailed',
      )
    } finally {
      exportLock.current = false
      if (exportController.current === controller) exportController.current = null
      if (authorized()) setExporting(false)
    }
  }
  const busy = !!request && report.isFetching
  const status = isAxiosError(report.error) ? report.error.response?.status : undefined
  const errorKey = [401, 403, 404].includes(status ?? 0)
    ? providerReport
      ? 'providerReadDenied'
      : 'denied'
    : status === 422
      ? 'overflow'
      : status === 400
        ? 'invalidRange'
        : 'unavailable'
  return (
    <Page title={t('title')} description={t('description')}>
      <div className="space-y-4">
        <div className="space-y-2 rounded-lg border border-primary/20 bg-primary/5 p-4 text-sm">
          <p className="font-medium">
            <span className="mr-2 rounded border px-2 py-0.5 text-xs">{t('readOnly')}</span>
            {t('scopeTitle')}
          </p>
          <p className="text-muted-foreground">{t('scopeNotice')}</p>
        </div>
        <div className="grid items-start gap-4 xl:grid-cols-[9fr_15fr]">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-lg">
                <Bot className="size-5" aria-hidden />
                {t('assistantTitle')}
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <p className="text-sm text-muted-foreground">{t('introduction')}</p>
              <div className="flex flex-wrap gap-2">
                {['shortcutTeam', 'shortcutKey', 'shortcutProvider', 'shortcutTokens'].map(
                  (key) => (
                    <Button
                      key={key}
                      variant="outline"
                      size="sm"
                      disabled={busy || exporting}
                      onClick={() => shortcut(key)}
                    >
                      {t(key)}
                      <ArrowRight className="size-3" aria-hidden />
                    </Button>
                  ),
                )}
              </div>
              <div className="rounded-md border p-3 text-sm">
                {'key' in question ? t(question.key) : question.text}
              </div>
              {message && (
                <p role="status" className="rounded-md border p-3 text-sm">
                  {t(message)}
                </p>
              )}
              <form
                aria-label={t('reportTitle')}
                className="space-y-3"
                onSubmit={(event) => {
                  event.preventDefault()
                  run()
                }}
              >
                <FormField label={t('from')}>
                  <Input
                    name="from"
                    value={draft.from}
                    onChange={(event) => {
                      clearReport()
                      setDraft({ ...draft, from: event.target.value })
                      setMessage('awaitingReview')
                    }}
                    required
                  />
                </FormField>
                <FormField label={t('to')}>
                  <Input
                    name="to"
                    value={draft.to}
                    onChange={(event) => {
                      clearReport()
                      setDraft({ ...draft, to: event.target.value })
                      setMessage('awaitingReview')
                    }}
                    required
                  />
                </FormField>
                <FormField label={t('timezone')}>
                  <Input
                    name="timezone"
                    value={draft.timezone}
                    onChange={(event) => {
                      clearReport()
                      setDraft({ ...draft, timezone: event.target.value })
                      setMessage('awaitingReview')
                    }}
                    required
                  />
                </FormField>
                <p className="text-xs text-muted-foreground">{t('rangeHelp')}</p>
                <Button type="submit" disabled={busy || exporting || !supported}>
                  {t('reviewRun')}
                </Button>
                {busy && (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      clearReport()
                      setMessage('awaitingReview')
                    }}
                  >
                    {t('cancel')}
                  </Button>
                )}
              </form>
              {!providerReport && ai.controls}
              <form
                aria-label={t('questionLabel')}
                className="space-y-3"
                onSubmit={(event) => {
                  event.preventDefault()
                  if (!input.trim() || !authorized() || requestLock.current || exportLock.current)
                    return
                  clearReport()
                  setQuestion({ text: input.trim() })
                  setInput('')
                  setSupported(false)
                  setMessage('unsupported')
                }}
              >
                <FormField label={t('questionLabel')}>
                  <Textarea
                    name="question"
                    rows={3}
                    maxLength={2000}
                    placeholder={t('questionPlaceholder')}
                    value={input}
                    onChange={(event) => setInput(event.target.value)}
                    disabled={busy || exporting}
                  />
                </FormField>
                <p className="text-xs text-muted-foreground">{t('questionHelp')}</p>
                <div className="flex justify-end">
                  <Button type="submit" disabled={!input.trim() || busy || exporting}>
                    <Send className="size-4" aria-hidden />
                    {t('send')}
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">{t('resultLabel')}</p>
                <CardTitle>{t(providerReport ? 'providerReportTitle' : 'reportTitle')}</CardTitle>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  disabled={!currentReport || exporting}
                  onClick={() => void exportCSV()}
                >
                  <Download className="size-4" aria-hidden />
                  {t(exporting ? 'exporting' : 'exportCSV')}
                </Button>
                {exporting && (
                  <Button
                    variant="outline"
                    onClick={() => {
                      exportController.current?.abort()
                      setExportMessage('')
                    }}
                  >
                    {t('cancel')}
                  </Button>
                )}
                <Button variant="outline" disabled title={t('saveUnavailable')}>
                  {t('saveReport')}
                </Button>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <p className="text-xs text-muted-foreground">{t('exportHelp')}</p>
              {exportMessage && (
                <p role={exportMessage === 'exportReady' ? 'status' : 'alert'}>
                  {t(exportMessage)}
                </p>
              )}
              {busy && <p role="status">{t('loading')}</p>}
              {request && report.isError && <p role="alert">{t(errorKey)}</p>}
              {!request && (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  {t('awaitingReview')}
                </p>
              )}
              {currentReport?.kind === 'tokens' && <InsightsResult report={currentReport.value} />}
              {currentReport?.kind === 'provider' && (
                <ProviderQualityResult report={currentReport.value} />
              )}
              {!providerReport && ai.answer}
            </CardContent>
          </Card>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{t('savedTitle')}</CardTitle>
          </CardHeader>
          <Table aria-label={t('savedTitle')}>
            <thead>
              <tr>
                {['reportColumn', 'parametersColumn', 'rangeColumn', 'creatorColumn'].map((key) => (
                  <th key={key}>{t(key)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              <tr>
                <td colSpan={4} className="text-muted-foreground">
                  {t('savedUnavailable')}
                </td>
              </tr>
            </tbody>
          </Table>
        </Card>
      </div>
    </Page>
  )
}
