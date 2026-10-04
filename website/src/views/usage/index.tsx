import { useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useInRouterContext, useSearchParams } from 'react-router'
import { isAxiosError } from 'axios'
import { Download, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getUsage, getUsageTeams } from '@/api/usage'
import TeamSelector from './team-selector'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { getPermissions } from '@/api/governance'
import { downloadUsageCSV, exportUsage } from '@/api/usage-export'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import type { UsageFilters, UsageScope } from '@/types/usage'
import UsageFiltersForm, { type UsageFilterDraft } from './filters'
import { defaultUsageFilters } from './filter-state'
import { SummaryCards, ChargeSummary } from './stats'
import UsageTrend from './trend'
import UsageDistribution from './distribution'
import UsageKeyTable from './key-table'
import { usageTime } from './format'

export default function UsagePage({ admin = false, projectId, teamId }: UsageScope) {
  return <UsageSession admin={admin && !projectId} projectId={projectId} teamId={teamId} />
}
export function ProjectUsagePanel({ projectId }: { projectId: string }) {
  return <UsagePage projectId={projectId} />
}
type UsageFilterState = {
  filters: UsageFilters
  onApply: (filters: UsageFilters) => void
  draft: UsageFilterDraft
  onDraft: (draft: UsageFilterDraft) => void
}
function UsageSession(scope: UsageScope) {
  const session = useSession()
  const routed = useInRouterContext()
  const gate =
    session.isFetching || session.isPending || session.isError || !session.data ? (
      <QueryState
        pending={session.isFetching || session.isPending}
        error={session.error}
        retry={() => void session.refetch()}
      />
    ) : undefined
  if (!session.data) return gate
  const actor = session.data.user.id
  if (!scope.admin && !scope.projectId)
    return routed && !scope.teamId ? (
      <RoutedMemberUsage key={actor} userId={actor} sessionGate={gate} />
    ) : (
      <LocalMemberUsage
        key={JSON.stringify([actor, scope.teamId ?? ''])}
        userId={actor}
        initialTeam={scope.teamId ?? ''}
        sessionGate={gate}
      />
    )
  return (
    <UsageScopeState
      key={JSON.stringify([actor, scope.admin ?? false, scope.projectId ?? ''])}
      scope={scope}
      userId={actor}
      sessionGate={gate}
    />
  )
}
// Keep only filter intent through renewed authority reads. Reports and exports
// remain inside the generation-bound subtree and cannot survive renewal.
function UsageScopeState({
  scope,
  userId,
  sessionGate,
  onTeamChange,
}: {
  scope: UsageScope
  userId: string
  sessionGate?: ReactNode
  onTeamChange?: (team: string) => void
}) {
  const generation = useSessionGeneration()
  const [filters, onApply] = useState<UsageFilters>({ ...defaultUsageFilters })
  const [draft, onDraft] = useState<UsageFilterDraft>({
    period: 'month',
    granularity: 'auto',
    timezone: 'UTC',
  })
  const state = { filters, onApply, draft, onDraft }
  if (sessionGate) return sessionGate
  if (onTeamChange)
    return (
      <MemberUsage
        key={generation}
        userId={userId}
        team={scope.teamId ?? ''}
        onChange={onTeamChange}
        filterState={state}
      />
    )
  return scope.admin ? (
    <AdminUsage key={generation} scope={scope} userId={userId} filterState={state} />
  ) : (
    <UsageContent key={generation} scope={scope} userId={userId} filterState={state} />
  )
}
function AdminUsage({
  scope,
  userId,
  filterState,
}: {
  scope: UsageScope
  userId: string
  filterState: UsageFilterState
}) {
  const { t } = useTranslation('usage')
  const generation = useSessionGeneration()
  const permissions = useQuery({
    queryKey: ['permissions', userId, 'usage', generation],
    queryFn: ({ signal }) => getPermissions(signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchInterval: 30_000,
  })
  if (permissions.isFetching || permissions.isPending || permissions.isError)
    return (
      <QueryState
        pending={permissions.isFetching || permissions.isPending}
        error={permissions.error}
        retry={() => void permissions.refetch()}
      />
    )
  if (!permissions.data.includes('calls.read_all'))
    return (
      <Page
        title={t('common:access_denied_cb8d4')}
        description={t('common:your_account_does_not_have_permission_to_access_ca6a8')}
      >
        <p role="alert">{t('common:your_account_does_not_have_permission_to_access_ca6a8')}</p>
      </Page>
    )
  return <UsageContent scope={scope} userId={userId} filterState={filterState} />
}
function RoutedMemberUsage({ userId, sessionGate }: { userId: string; sessionGate?: ReactNode }) {
  const [params, setParams] = useSearchParams()
  const team = params.get('team') ?? ''
  return (
    <UsageScopeState
      key={team || 'personal'}
      userId={userId}
      scope={{ teamId: team || undefined }}
      sessionGate={sessionGate}
      onTeamChange={(next) => {
        const updated = new URLSearchParams(params)
        if (next) updated.set('team', next)
        else updated.delete('team')
        setParams(updated)
      }}
    />
  )
}
function LocalMemberUsage({
  userId,
  initialTeam,
  sessionGate,
}: {
  userId: string
  initialTeam: string
  sessionGate?: ReactNode
}) {
  const [team, setTeam] = useState(initialTeam)
  return (
    <UsageScopeState
      key={team || 'personal'}
      userId={userId}
      scope={{ teamId: team || undefined }}
      sessionGate={sessionGate}
      onTeamChange={setTeam}
    />
  )
}
function MemberUsage({
  userId,
  team,
  onChange,
  filterState,
}: {
  userId: string
  team: string
  onChange: (team: string) => void
  filterState: UsageFilterState
}) {
  const generation = useSessionGeneration()
  const mounted = useRef(false)
  const [refreshing, setRefreshing] = useState(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const teams = useInfiniteQuery({
    queryKey: ['usage-teams', userId, generation],
    queryFn: ({ pageParam, signal }) => getUsageTeams(pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const ready = teams.isSuccess && !teams.isFetching && !teams.isError && !refreshing
  async function refreshTeams() {
    if (refreshing || teams.isFetching) return
    setRefreshing(true)
    try {
      await teams.refetch()
    } finally {
      if (mounted.current) setRefreshing(false)
    }
  }
  const items = ready
    ? [
        ...new Map(
          teams.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  const control = (
    <TeamSelector
      team={team}
      items={items}
      pending={teams.isFetching || refreshing}
      error={teams.isError}
      ready={ready}
      more={teams.hasNextPage}
      onChange={onChange}
      onRefresh={() => void refreshTeams()}
      onMore={() => void teams.fetchNextPage()}
    />
  )
  return (
    <UsageContent
      key={team || 'personal'}
      userId={userId}
      scope={{ teamId: team || undefined }}
      contextReady={ready}
      sourceControl={control}
      filterState={filterState}
    />
  )
}
function UsageContent({
  scope,
  userId,
  contextReady = true,
  sourceControl,
  filterState,
}: {
  scope: UsageScope
  userId: string
  contextReady?: boolean
  sourceControl?: ReactNode
  filterState: UsageFilterState
}) {
  const { t, i18n } = useTranslation('usage')
  const generation = useSessionGeneration()
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const { filters, onApply, draft, onDraft } = filterState
  const context = JSON.stringify([userId, scope, filters])
  const [deniedContext, setDeniedContext] = useState<string | null>(null)
  const teamAccount = !!scope.teamId || !!(scope.admin && filters.team_id)
  const [dimension, setDimension] = useState('keys')
  const [refreshing, setRefreshing] = useState(false)
  const mounted = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const reportKey = useMemo(
    () => [
      'usage',
      userId,
      generation,
      scope.teamId
        ? ['team', scope.teamId]
        : scope.projectId
          ? ['project', scope.projectId]
          : scope.admin
            ? 'admin'
            : 'personal',
      filters,
    ],
    [userId, generation, scope.teamId, scope.projectId, scope.admin, filters],
  )
  const reportHash = JSON.stringify(reportKey)
  const query = useQuery({
    queryKey: reportKey,
    queryFn: ({ signal }) => getUsage(scope, filters, signal),
    retry: false,
    enabled: !scope.teamId || contextReady,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const report =
    query.isSuccess &&
    (!scope.teamId || contextReady) &&
    !query.isFetching &&
    !query.isError &&
    !refreshing &&
    deniedContext !== context
      ? query.data
      : undefined
  const cache = useQueryClient()
  const exportController = useRef<AbortController | null>(null)
  const exportLock = useRef(false)
  const [exporting, setExporting] = useState(false)
  const [exportResult, setExportResult] = useState<{ context: string; key: string } | null>(null)
  const authority = useRef(false)
  const teamReady = !scope.teamId || contextReady
  const exportMessage = exportResult?.context === context ? exportResult.key : ''
  useLayoutEffect(() => {
    exportController.current?.abort()
    return () => {
      exportController.current?.abort()
    }
  }, [context, teamReady])
  useLayoutEffect(() => {
    authority.current = !!report
    if (!report) exportController.current?.abort()
  }, [report])
  useLayoutEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated') return
        const key = event.query.queryKey
        if (key[0] === 'auth' && key[1] === 'session') {
          const state = event.query.state
          const actor = (state.data as { user?: { id?: string } } | null)?.user?.id
          if (
            state.fetchStatus === 'fetching' ||
            state.status !== 'success' ||
            actor !== userId ||
            (event.action.type === 'success' && !event.action.manual)
          )
            exportController.current?.abort()
        }
        if (
          (key[0] === 'permissions' && scope.admin) ||
          (scope.teamId && key[0] === 'usage-teams' && key[1] === userId) ||
          JSON.stringify(key) === reportHash
        )
          exportController.current?.abort()
      }),
    [cache, userId, scope.admin, scope.teamId, reportHash],
  )
  function currentAuthority() {
    const session = cache.getQueryState<{ user: { id: string } } | null>(sessionKey)
    const state = cache.getQueryState(reportKey)
    if (
      !authority.current ||
      session?.fetchStatus === 'fetching' ||
      session?.status !== 'success' ||
      session.data?.user.id !== userId ||
      state?.status !== 'success' ||
      state.fetchStatus === 'fetching' ||
      state.isInvalidated
    )
      return false
    const teams = cache.getQueryState(['usage-teams', userId, generation])
    if (
      scope.teamId &&
      (!teams ||
        teams.status !== 'success' ||
        teams.fetchStatus === 'fetching' ||
        teams.isInvalidated)
    )
      return false
    if (scope.admin) {
      const permissions = cache.getQueryState<string[]>([
        'permissions',
        userId,
        'usage',
        generation,
      ])
      if (
        permissions?.status !== 'success' ||
        permissions.fetchStatus === 'fetching' ||
        !permissions.data?.includes('calls.read_all')
      )
        return false
    }
    return true
  }
  async function exportCSV() {
    if (exportLock.current || !currentAuthority()) return
    const controller = new AbortController()
    exportController.current = controller
    exportLock.current = true
    setExporting(true)
    setExportResult(null)
    const captured = { ...scope }
    try {
      const blob = await exportUsage(captured, { ...filters }, controller.signal)
      if (controller.signal.aborted || !currentAuthority()) return
      downloadUsageCSV(blob, captured)
      setExportResult({ context, key: 'exportReady' })
    } catch (error) {
      if (!controller.signal.aborted && mounted.current) {
        const denied = isAxiosError(error) && [401, 403, 404].includes(error.response?.status ?? 0)
        if (denied) setDeniedContext(context)
        setExportResult({
          context,
          key: denied
            ? 'exportDenied'
            : isAxiosError(error) && error.response?.status === 422
              ? 'exportOverflow'
              : 'exportFailed',
        })
      }
    } finally {
      if (exportController.current === controller) exportController.current = null
      exportLock.current = false
      if (mounted.current) setExporting(false)
    }
  }
  async function refreshReport() {
    if (query.isFetching || refreshing || (scope.teamId && !contextReady)) return
    setRefreshing(true)
    try {
      const result = await query.refetch()
      if (result.isSuccess && mounted.current) {
        setDeniedContext(null)
        setExportResult(null)
      }
    } finally {
      if (mounted.current) setRefreshing(false)
    }
  }
  const status = isAxiosError(query.error) ? query.error.response?.status : undefined
  const message =
    status === 422
      ? 'overflow'
      : status === 400
        ? 'invalidFilters'
        : status === 403 || status === 404
          ? 'denied'
          : 'unavailable'
  const dimensions = [
    ...(!teamAccount ? [{ id: 'keys', label: 'keys', unknownLabel: 'unknownIdentity' }] : []),
    ...(scope.admin && report?.available_dimensions.includes('provider')
      ? [{ id: 'providers', label: 'providers', unknownLabel: 'unknownProvider' }]
      : []),
    ...(scope.admin && report?.available_dimensions.includes('connection')
      ? [{ id: 'connections', label: 'connections', unknownLabel: 'unknownConnection' }]
      : []),
    ...(scope.admin && report?.available_dimensions.includes('provider_model')
      ? [
          {
            id: 'provider_models',
            label: 'providerModels',
            unknownLabel: 'unknownProviderModel',
          },
        ]
      : []),
  ]
  const selected = dimensions.find((value) => value.id === dimension) ?? dimensions[0]
  return (
    <Page
      title={t(
        scope.teamId
          ? 'teamTitle'
          : scope.projectId
            ? 'projectTitle'
            : scope.admin
              ? 'adminTitle'
              : 'title',
      )}
      description={t(
        scope.teamId
          ? 'teamDescription'
          : scope.projectId
            ? 'projectDescription'
            : scope.admin
              ? 'adminDescription'
              : 'description',
      )}
      action={
        <Button
          variant="outline"
          disabled={query.isFetching || refreshing || (!!scope.teamId && !contextReady)}
          onClick={() => void refreshReport()}
        >
          <RefreshCw className="size-4" aria-hidden />
          {t('refresh')}
        </Button>
      }
    >
      <UsageFiltersForm
        admin={scope.admin === true}
        team={teamAccount}
        sourceControl={sourceControl}
        models={report?.current.models ?? []}
        keys={report?.current.keys ?? []}
        onApply={onApply}
        draft={draft}
        onDraft={onDraft}
        action={
          <Button
            type="button"
            variant="outline"
            disabled={!report || exporting}
            onClick={() => void exportCSV()}
            className="ml-auto"
          >
            <Download className="size-4" aria-hidden />
            {t(exporting ? 'exporting' : 'exportCSV')}
          </Button>
        }
      />
      <p className="text-xs text-muted-foreground">{t('exportHelp')}</p>
      {exportMessage && (report || exportMessage === 'exportDenied') && (
        <p role={exportMessage === 'exportReady' ? 'status' : 'alert'} className="text-sm">
          {t(exportMessage)}
        </p>
      )}
      {(query.isPending || query.isFetching || refreshing) && (
        <QueryState pending error={null} retry={() => void refreshReport()} />
      )}
      {query.isError && (
        <div className="space-y-3 rounded-lg border p-4">
          <p role="alert" className="text-sm text-destructive">
            {t(message)}
          </p>
          <Button variant="outline" onClick={() => void refreshReport()}>
            {t('retry')}
          </Button>
        </div>
      )}
      {report && (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
            <span>
              {t('range', {
                from: usageTime(report.current.from, locale, report.timezone),
                to: usageTime(report.current.to, locale, report.timezone),
              })}
            </span>
            <span>
              {t('updated', { time: usageTime(report.queried_at, locale, report.timezone) })}
            </span>
          </div>
          {report.current.summary.requests === 0 && (
            <p
              role="status"
              className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground"
            >
              {t('noCalls')}
            </p>
          )}
          <SummaryCards current={report.current.summary} previous={report.previous?.summary} />
          <UsageTrend
            key={JSON.stringify([report.current.from, report.current.to, report.granularity])}
            report={report}
          />
          <div className={selected ? 'grid gap-4 lg:grid-cols-2' : 'grid gap-4'}>
            <UsageDistribution title={t('models')} groups={report.current.models} />
            {selected && (
              <div className="space-y-2">
                {dimensions.length > 1 && (
                  <label className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
                    {t('distribution')}
                    <select
                      value={selected.id}
                      onChange={(event) => setDimension(event.target.value)}
                      className="h-8 rounded-md border bg-background px-2"
                    >
                      {dimensions.map((value) => (
                        <option key={value.id} value={value.id}>
                          {t(value.label)}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                <UsageDistribution
                  title={t(selected.label)}
                  unknownLabel={t(selected.unknownLabel)}
                  groups={
                    report.current[
                      selected.id as 'keys' | 'providers' | 'connections' | 'provider_models'
                    ] ?? []
                  }
                />
              </div>
            )}
          </div>
          {!teamAccount && (
            <UsageKeyTable key={JSON.stringify(filters)} groups={report.current.keys} />
          )}
          <ChargeSummary stats={report.current.summary} />
          <footer className="space-y-1 text-xs text-muted-foreground">
            <p>{t('source')}</p>
            {report.latest_completed_at && (
              <p>
                {t('latest', {
                  time: usageTime(report.latest_completed_at, locale, report.timezone),
                })}
              </p>
            )}
          </footer>
        </>
      )}
    </Page>
  )
}
