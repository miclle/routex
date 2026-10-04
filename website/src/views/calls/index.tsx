import { useTranslation } from 'react-i18next'
import { useLayoutEffect, useRef, useState, type FormEvent } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { Download, RefreshCw } from 'lucide-react'
import { downloadCallsCSV, exportCalls, getCall, listCalls } from '@/api/calls'
import type { AdminCallDetail, CallFilters, CallRecord } from '@/types/calls'
import { Page, QueryState, FormField, ErrorNotice } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Drawer } from '@/components/ui/drawer'
import { Table } from '@/components/ui/table'

export default function CallsPage({
  admin = false,
  projectId,
  teamId,
}: {
  admin?: boolean
  projectId?: string
  teamId?: string
}) {
  return admin ? (
    <PermissionGate permission="calls.read_all">
      <CallRecords admin />
    </PermissionGate>
  ) : teamId ? (
    <TeamCallRecords teamId={teamId} />
  ) : (
    <CallRecords admin={false} projectId={projectId} teamId={teamId} />
  )
}
function TeamCallRecords({ teamId }: { teamId: string }) {
  const session = useSession()
  const generation = useSessionGeneration()
  if (!session.data || session.isError)
    return (
      <QueryState
        pending={session.isPending}
        error={session.error}
        retry={() => void session.refetch()}
      />
    )
  return (
    <CallRecordContent
      key={`${session.data.user.id}:${teamId}`}
      admin={false}
      teamId={teamId}
      actor={session.data.user.id}
      generation={generation}
      sessionReady={!session.isFetching}
    />
  )
}
function CallRecords({
  admin,
  projectId,
}: {
  admin: boolean
  projectId?: string
  teamId?: string
}) {
  const session = useSession(false)
  const generation = useSessionGeneration()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  return (
    <CallRecordContent
      key={`${actor}:${admin}:${projectId ?? ''}`}
      admin={admin}
      projectId={projectId}
      actor={actor}
      generation={generation}
      sessionReady={!session.isError && !session.isFetching}
    />
  )
}
function CallRecordContent({
  admin,
  projectId,
  teamId,
  actor,
  generation,
  sessionReady,
}: {
  admin: boolean
  projectId?: string
  teamId?: string
  actor: string
  generation: number
  sessionReady: boolean
}) {
  const { t, i18n } = useTranslation('activity')
  const cache = useQueryClient()
  const formatTime = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  const scope = teamId
    ? ['team', actor, teamId, generation]
    : projectId
      ? ['project', projectId]
      : [admin ? 'admin' : 'self']
  const [filters, setFilters] = useState<CallFilters>({})
  const [selected, setSelected] = useState<string | null>(null)
  const [validation, setValidation] = useState('')
  const pendingExport = useRef<{ controller: AbortController; identity: string } | null>(null)
  const exportAuthority = useRef({ identity: '', ready: false })
  const [exportState, setExportState] = useState<{
    identity: string
    kind: 'preparing' | 'ready' | 'error'
    error?: unknown
  } | null>(null)
  const calls = useInfiniteQuery({
    queryKey: ['calls', ...scope, filters],
    queryFn: ({ pageParam, signal }) =>
      listCalls(admin, filters, pageParam, signal, projectId, teamId),
    enabled: !teamId || (!!actor && sessionReady),
    ...(teamId ? { retry: false, staleTime: 0, gcTime: 0, refetchOnMount: 'always' as const } : {}),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  })
  const detail = useQuery({
    queryKey: ['call', ...scope, selected],
    queryFn: ({ signal }) => getCall(admin, selected!, signal, projectId, teamId),
    enabled: selected !== null && (!teamId || (!!actor && sessionReady)),
    ...(teamId ? { retry: false, staleTime: 0, gcTime: 0, refetchOnMount: 'always' as const } : {}),
  })
  const queryIdentity = JSON.stringify(['calls', ...scope, filters])
  const exportIdentity = JSON.stringify([admin, projectId, teamId, actor, generation, filters])
  const exportAuthorized = sessionReady && calls.isSuccess && !calls.isFetching && !calls.isError
  const visibleExport =
    exportAuthorized && exportState?.identity === exportIdentity ? exportState : null
  const exporting = visibleExport?.kind === 'preparing'
  const exportReady = visibleExport?.kind === 'ready'
  const exportError = visibleExport?.kind === 'error' ? visibleExport.error : null
  useLayoutEffect(() => {
    exportAuthority.current = { identity: exportIdentity, ready: exportAuthorized }
    if (
      pendingExport.current &&
      (!exportAuthorized || pendingExport.current.identity !== exportIdentity)
    ) {
      pendingExport.current.controller.abort()
      pendingExport.current = null
    }
  }, [exportIdentity, exportAuthorized])
  useLayoutEffect(() => {
    const unsubscribe = cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const sessionEvent =
        JSON.stringify(event.query.queryKey) === JSON.stringify(['auth', 'session']) &&
        (event.type === 'removed' ||
          event.action.type === 'fetch' ||
          event.action.type === 'error' ||
          (event.action.type === 'success' && !event.action.manual))
      const listEvent =
        JSON.stringify(event.query.queryKey) === queryIdentity &&
        (event.type === 'removed' || ['fetch', 'error', 'success'].includes(event.action.type))
      if (!sessionEvent && !listEvent) return
      if (
        sessionEvent ||
        event.type === 'removed' ||
        event.action.type === 'fetch' ||
        event.action.type === 'error'
      )
        setSelected(null)
      pendingExport.current?.controller.abort()
      pendingExport.current = null
      setExportState(null)
    })
    return () => {
      unsubscribe()
      pendingExport.current?.controller.abort()
      pendingExport.current = null
    }
  }, [cache, queryIdentity])
  function filter(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const next: CallFilters = {}
    for (const name of ['status', 'model_id', 'key_id', ...(admin ? ['user_id'] : [])] as const) {
      const value = String(data.get(name) ?? '').trim()
      if (value) next[name as keyof CallFilters] = value
    }
    for (const name of ['from', 'to'] as const) {
      const value = String(data.get(name) ?? '')
      if (value) next[name] = new Date(value).toISOString()
    }
    if (next.from && next.to && next.from > next.to) {
      setValidation('calls.invalidRange')
      return
    }
    setValidation('')
    setSelected(null)
    setFilters(next)
    setExportState(null)
  }
  async function exportCSV() {
    if (pendingExport.current || !exportAuthority.current.ready) return
    const attempt = {
      controller: new AbortController(),
      identity: exportAuthority.current.identity,
    }
    const capturedFilters = { ...filters }
    const current = () => {
      const auth = cache.getQueryState<{ user: { id: string } } | null>(['auth', 'session'])
      const list = cache.getQueryState(['calls', ...scope, capturedFilters])
      return (
        pendingExport.current === attempt &&
        !attempt.controller.signal.aborted &&
        exportAuthority.current.ready &&
        exportAuthority.current.identity === attempt.identity &&
        list?.status === 'success' &&
        list.fetchStatus === 'idle' &&
        ((!teamId &&
          !actor &&
          (!auth || (auth.status === 'pending' && auth.fetchStatus === 'idle'))) ||
          (auth?.status === 'success' &&
            auth.fetchStatus === 'idle' &&
            auth.data?.user.id === actor))
      )
    }
    pendingExport.current = attempt
    if (!current()) {
      pendingExport.current = null
      return
    }
    setExportState({ identity: attempt.identity, kind: 'preparing' })
    try {
      const blob = await exportCalls(
        admin,
        capturedFilters,
        attempt.controller.signal,
        projectId,
        teamId,
      )
      if (!current()) return
      downloadCallsCSV(blob, admin, projectId, teamId)
      setExportState({ identity: attempt.identity, kind: 'ready' })
    } catch (error) {
      if (current()) setExportState({ identity: attempt.identity, kind: 'error', error })
    } finally {
      if (pendingExport.current === attempt) {
        pendingExport.current = null
        setExportState((state) =>
          state?.identity === attempt.identity && state.kind === 'preparing' ? null : state,
        )
      }
    }
  }

  const items =
    teamId && (calls.isFetching || calls.isError || !actor || !sessionReady)
      ? []
      : (calls.data?.pages.flatMap((page) => page.items) ?? [])
  return (
    <Page
      title={
        teamId
          ? t('calls.teamCalls')
          : projectId
            ? t('calls.projectCalls')
            : admin
              ? t('calls.allCalls')
              : t('calls.myCalls')
      }
      description={
        teamId
          ? t('calls.teamDescription')
          : projectId
            ? t('calls.projectDescription')
            : admin
              ? t('calls.adminDescription')
              : t('calls.description')
      }
      action={
        <Button variant="outline" disabled={calls.isFetching} onClick={() => void calls.refetch()}>
          <RefreshCw className="size-4" aria-hidden="true" />
          {t('calls.refresh')}
        </Button>
      }
    >
      <form
        onSubmit={filter}
        aria-label={t('calls.filters')}
        className="flex flex-wrap items-end gap-2 [&_label]:w-44 [&_label]:text-xs [&_input]:h-9 [&_select]:h-9"
      >
        <FormField label={t('calls.status')}>
          <select
            name="status"
            className="h-11 w-full rounded-md border bg-background px-3 text-sm"
          >
            <option value="">{t('calls.allStatuses')}</option>
            <option value="success">{t('calls.success')}</option>
            <option value="error">{t('calls.error')}</option>
            <option value="canceled">{t('calls.canceled')}</option>
          </select>
        </FormField>
        <FormField label={t('calls.modelID')}>
          <Input name="model_id" placeholder={t('calls.optional')} />
        </FormField>
        {!teamId && (
          <FormField label={t('calls.keyID')}>
            <Input name="key_id" placeholder={t('calls.optional')} />
          </FormField>
        )}
        {admin && (
          <FormField label={t('calls.userID')}>
            <Input name="user_id" placeholder={t('calls.optional')} />
          </FormField>
        )}
        <FormField label={t('calls.from')}>
          <Input name="from" type="datetime-local" />
        </FormField>
        <FormField label={t('calls.to')}>
          <Input name="to" type="datetime-local" />
        </FormField>
        <div className="flex items-end gap-2">
          <Button type="submit" disabled={calls.isFetching}>
            {t('calls.filter')}
          </Button>
          <Button
            type="reset"
            variant="ghost"
            onClick={() => {
              setFilters({})
              setSelected(null)
              setValidation('')
              setExportState(null)
            }}
          >
            {t('calls.reset')}
          </Button>
        </div>
        {validation && (
          <p role="alert" className="text-sm text-destructive sm:col-span-2">
            {t(validation)}
          </p>
        )}
        <div className="ml-auto">
          <Button
            type="button"
            variant="outline"
            disabled={exporting || !exportAuthorized}
            onClick={() => void exportCSV()}
          >
            <Download className="size-4" aria-hidden="true" />
            {t(exporting ? 'calls.exporting' : 'calls.exportCSV')}
          </Button>
        </div>
      </form>
      <ErrorNotice error={exportError} />
      {exportReady && (
        <p role="status" className="text-sm text-muted-foreground">
          {t('calls.exportReady')}
        </p>
      )}
      <QueryState
        pending={calls.isPending}
        error={calls.isFetchNextPageError ? null : calls.error}
        retry={() => void calls.refetch()}
        empty={calls.isSuccess && items.length === 0}
      />
      {items.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[800px] text-left text-sm [&_th]:whitespace-nowrap">
            <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">{t('calls.modelRequest')}</th>
                {admin && <th className="px-3 py-2 font-medium">{t('calls.owner')}</th>}
                <th className="px-3 py-2 font-medium">{t('calls.status')}</th>
                <th className="px-3 py-2 font-medium">{t('calls.from')}</th>
                <th className="px-3 py-2 font-medium">{t('calls.duration')}</th>
                <th className="px-3 py-2 font-medium">{t('calls.tokens')}</th>
                <th className="px-3 py-2 font-medium">
                  <span className="sr-only">{t('calls.details')}</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {items.map((call) => (
                <tr key={call.request_id}>
                  <td className="min-w-56 max-w-72 px-3 py-2">
                    <p className="break-all font-medium">{call.model_name}</p>
                    <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
                      {call.request_id}
                    </p>
                  </td>
                  {admin && (
                    <td className="px-3 py-2 font-mono text-xs">
                      {'project_id' in call && call.project_id
                        ? call.project_id
                        : 'user_id' in call
                          ? call.user_id || '—'
                          : '—'}
                    </td>
                  )}
                  <td className="whitespace-nowrap px-3 py-2">
                    <Badge variant="outline">{t(`calls.${call.status}`)}</Badge>
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs">
                    {formatTime(call.started_at)}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2">{call.duration_ms} ms</td>
                  <td className="whitespace-nowrap px-3 py-2">
                    {call.input_tokens ?? t('calls.missing')} /{' '}
                    {call.output_tokens ?? t('calls.missing')}
                  </td>
                  <td className="px-3 py-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label={t('calls.viewRequest', { id: call.request_id })}
                      onClick={() => setSelected(call.request_id)}
                    >
                      {t('calls.details')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {calls.isFetchNextPageError && <ErrorNotice error={calls.error} />}
      {calls.hasNextPage && (
        <div className="text-center">
          <Button
            variant="outline"
            disabled={calls.isFetchingNextPage}
            onClick={() => void calls.fetchNextPage()}
          >
            {calls.isFetchingNextPage
              ? t('calls.loading')
              : calls.isFetchNextPageError
                ? t('calls.retryMore')
                : t('calls.loadMore')}
          </Button>
        </div>
      )}
      <Drawer
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
        title={t('calls.detailTitle')}
        description={admin ? t('calls.adminDetailDescription') : t('calls.detailDescription')}
      >
        <QueryState
          pending={detail.isPending}
          error={detail.error}
          retry={() => void detail.refetch()}
        />
        {detail.data &&
          (!teamId ||
            (!!actor &&
              sessionReady &&
              !detail.isFetching &&
              !detail.isError &&
              !calls.isFetching &&
              !calls.isError)) && <CallDetail call={detail.data} admin={admin} team={!!teamId} />}
      </Drawer>
    </Page>
  )
}
function CallDetail({
  call,
  admin,
  team = false,
}: {
  call: CallRecord | AdminCallDetail
  admin: boolean
  team?: boolean
}) {
  const { t, i18n } = useTranslation('activity')
  const formatTime = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  return (
    <div className="space-y-5">
      <dl className="divide-y rounded-md border text-sm [&>div]:grid [&>div]:grid-cols-[140px_1fr] [&>div]:gap-3 [&>div]:p-3">
        {[
          [t('calls.requestID'), call.request_id],
          [t('calls.model'), call.model_name],
          [t('calls.modelID'), call.model_id],
          ...(!team ? [[t('calls.keyID'), call.key_id]] : []),
          [t('calls.protocol'), call.protocol],
          [t('calls.status'), t(`calls.${call.status}`)],
          [t('calls.responseMode'), call.stream ? t('calls.stream') : t('calls.ordinary')],
          [t('calls.duration'), `${call.duration_ms} ms`],
          [t('calls.from'), formatTime(call.started_at)],
          [t('calls.completedAt'), formatTime(call.completed_at)],
          [t('calls.inputTokens'), call.input_tokens ?? t('calls.missing')],
          [t('calls.outputTokens'), call.output_tokens ?? t('calls.missing')],
        ].map(([name, value]) => (
          <div key={name}>
            <dt className="text-xs text-muted-foreground">{name}</dt>
            <dd className="mt-1 break-all">{value}</dd>
          </div>
        ))}
      </dl>
      {admin && 'attempts' in call && (
        <section className="space-y-3 border-t pt-5">
          <h3 className="text-sm font-medium">{t('calls.upstreamDiagnostics')}</h3>
          <dl className="grid grid-cols-2 gap-3 text-xs">
            <div>
              <dt className="text-muted-foreground">
                {call.project_id ? t('calls.projectID') : t('calls.user')}
              </dt>
              <dd className="mt-1 break-all">{call.project_id || call.user_id || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.errorCode')}</dt>
              <dd className="mt-1 break-all">{call.error_code || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.providerModel')}</dt>
              <dd className="mt-1 break-all">{call.provider_model_id || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.connection')}</dt>
              <dd className="mt-1 break-all">{call.connection_id || '—'}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t('calls.routeStopReason')}</dt>
              <dd className="mt-1 break-all">
                {call.route_stop_reason
                  ? t(`calls.stopReasons.${call.route_stop_reason}`)
                  : t('calls.notRecorded')}
              </dd>
            </div>
          </dl>
          <h4 className="text-xs font-medium">{t('calls.attempts')}</h4>
          {call.attempts.length === 0 && (
            <p className="text-xs text-muted-foreground">{t('calls.noAttempts')}</p>
          )}
          {call.attempts.length > 0 && (
            <Table aria-label={t('calls.attemptDiagnosticsTable')} className="text-xs">
              <thead>
                <tr>
                  <th>{t('calls.attemptNumber')}</th>
                  <th>{t('calls.attemptOutcome')}</th>
                  <th>{t('calls.replayEvidence')}</th>
                  <th>{t('calls.upstreamRoute')}</th>
                  <th>{t('calls.timing')}</th>
                </tr>
              </thead>
              <tbody>
                {call.attempts.map((attempt) => (
                  <tr key={attempt.id}>
                    <td>
                      <p className="font-medium">
                        {attempt.attempt_number > 0 ? `#${attempt.attempt_number}` : '—'}
                      </p>
                      <p className="break-all font-mono text-muted-foreground">{attempt.id}</p>
                    </td>
                    <td>
                      <p>{t(`calls.failureClasses.${attempt.failure_class}`)}</p>
                      <p className="text-muted-foreground">
                        {t(`calls.${attempt.status}`, { defaultValue: attempt.status })} · HTTP{' '}
                        {attempt.http_status || '—'}
                      </p>
                      <p className="text-muted-foreground">
                        {attempt.error_code || t('calls.noErrorCode')}
                      </p>
                    </td>
                    <td>
                      <p>{t(`calls.workEvidence.${attempt.work_evidence}`)}</p>
                      <p className="text-muted-foreground">
                        {attempt.evidence_code
                          ? t(`calls.evidenceCodes.${attempt.evidence_code}`)
                          : t('calls.noEvidenceCode')}
                      </p>
                      <p className="text-muted-foreground">
                        {t('calls.outputStarted', {
                          value: t(attempt.output_started ? 'calls.yes' : 'calls.no'),
                        })}
                      </p>
                      <p className="text-muted-foreground">
                        {t('calls.finalUsageKnown', {
                          value: t(attempt.final_usage_known ? 'calls.yes' : 'calls.no'),
                        })}
                      </p>
                    </td>
                    <td className="break-all">
                      {attempt.connection_id} / {attempt.provider_model_id}
                    </td>
                    <td className="whitespace-nowrap text-muted-foreground">
                      {formatTime(attempt.started_at)} → {formatTime(attempt.completed_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </section>
      )}
    </div>
  )
}
