import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useInRouterContext, useSearchParams } from 'react-router'
import { isAxiosError } from 'axios'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getUsage, getUsageTeams } from '@/api/usage'
import TeamSelector from './team-selector'
import { useSession } from '@/hooks/use-auth'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Button } from '@/components/ui/button'
import type { UsageFilters, UsageScope } from '@/types/usage'
import UsageFiltersForm from './filters'
import { defaultUsageFilters } from './filter-state'
import { SummaryCards, ChargeSummary } from './stats'
import UsageTrend from './trend'
import UsageDistribution from './distribution'
import UsageKeyTable from './key-table'
import { usageTime } from './format'

export default function UsagePage({ admin = false, projectId, teamId }: UsageScope) {
  return admin && !projectId ? (
    <PermissionGate permission="calls.read_all">
      <UsageSession admin />
    </PermissionGate>
  ) : (
    <UsageSession projectId={projectId} teamId={teamId} />
  )
}
export function ProjectUsagePanel({ projectId }: { projectId: string }) {
  return <UsagePage projectId={projectId} />
}
function UsageSession(scope: UsageScope) {
  const session = useSession()
  const routed = useInRouterContext()
  if (session.isPending || session.isError || !session.data)
    return (
      <QueryState
        pending={session.isPending}
        error={session.error}
        retry={() => void session.refetch()}
      />
    )
  const actor = session.data.user.id
  if (!scope.admin && !scope.projectId)
    return routed && !scope.teamId ? (
      <RoutedMemberUsage key={actor} userId={actor} />
    ) : (
      <LocalMemberUsage
        key={`${actor}:${scope.teamId ?? ''}`}
        userId={actor}
        initialTeam={scope.teamId ?? ''}
      />
    )
  const key = JSON.stringify([actor, scope.admin ?? false, scope.projectId ?? ''])
  return <UsageContent key={key} scope={scope} userId={actor} />
}
function RoutedMemberUsage({ userId }: { userId: string }) {
  const [params, setParams] = useSearchParams()
  const team = params.get('team') ?? ''
  return (
    <MemberUsage
      userId={userId}
      team={team}
      onChange={(next) => {
        const updated = new URLSearchParams(params)
        if (next) updated.set('team', next)
        else updated.delete('team')
        setParams(updated)
      }}
    />
  )
}
function LocalMemberUsage({ userId, initialTeam }: { userId: string; initialTeam: string }) {
  const [team, setTeam] = useState(initialTeam)
  return <MemberUsage userId={userId} team={team} onChange={setTeam} />
}
function MemberUsage({
  userId,
  team,
  onChange,
}: {
  userId: string
  team: string
  onChange: (team: string) => void
}) {
  const mounted = useRef(false)
  const [refreshing, setRefreshing] = useState(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const teams = useInfiniteQuery({
    queryKey: ['usage-teams', userId],
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
    />
  )
}
function UsageContent({
  scope,
  userId,
  contextReady = true,
  sourceControl,
}: {
  scope: UsageScope
  userId: string
  contextReady?: boolean
  sourceControl?: ReactNode
}) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const [filters, setFilters] = useState<UsageFilters>({ ...defaultUsageFilters })
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
  const query = useQuery({
    queryKey: [
      'usage',
      userId,
      scope.teamId
        ? ['team', scope.teamId]
        : scope.projectId
          ? ['project', scope.projectId]
          : scope.admin
            ? 'admin'
            : 'personal',
      filters,
    ],
    queryFn: ({ signal }) => getUsage(scope, filters, signal),
    retry: false,
    enabled: !scope.teamId || contextReady,
    ...(scope.teamId ? { staleTime: 0, gcTime: 0, refetchOnMount: 'always' as const } : {}),
  })
  const report =
    query.isSuccess &&
    (!scope.teamId || (contextReady && !query.isFetching && !query.isError && !refreshing))
      ? query.data
      : undefined
  async function refreshReport() {
    if (query.isFetching || refreshing || (scope.teamId && !contextReady)) return
    setRefreshing(true)
    try {
      await query.refetch()
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
        onApply={setFilters}
      />
      {(query.isPending || refreshing) && (
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
