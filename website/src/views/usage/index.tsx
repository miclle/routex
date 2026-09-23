import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getUsage } from '@/api/usage'
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

export default function UsagePage({ admin = false, projectId }: UsageScope) {
  return admin && !projectId ? (
    <PermissionGate permission="calls.read_all">
      <UsageSession admin />
    </PermissionGate>
  ) : (
    <UsageSession projectId={projectId} />
  )
}
export function ProjectUsagePanel({ projectId }: { projectId: string }) {
  return <UsagePage projectId={projectId} />
}
function UsageSession(scope: UsageScope) {
  const session = useSession()
  if (session.isPending || session.isError || !session.data)
    return (
      <QueryState
        pending={session.isPending}
        error={session.error}
        retry={() => void session.refetch()}
      />
    )
  const key = JSON.stringify([session.data.user.id, scope.admin ?? false, scope.projectId ?? ''])
  return <UsageContent key={key} scope={scope} userId={session.data.user.id} />
}
function UsageContent({ scope, userId }: { scope: UsageScope; userId: string }) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const [filters, setFilters] = useState<UsageFilters>({ ...defaultUsageFilters })
  const [dimension, setDimension] = useState('keys')
  const query = useQuery({
    queryKey: [
      'usage',
      userId,
      scope.projectId ? ['project', scope.projectId] : scope.admin ? 'admin' : 'personal',
      filters,
    ],
    queryFn: ({ signal }) => getUsage(scope, filters, signal),
    retry: false,
  })
  const report = query.isSuccess ? query.data : undefined
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
    { id: 'keys', label: 'keys' },
    ...(scope.admin && report?.available_dimensions.includes('connection')
      ? [{ id: 'connections', label: 'connections' }]
      : []),
    ...(scope.admin && report?.available_dimensions.includes('provider_model')
      ? [{ id: 'provider_models', label: 'providerModels' }]
      : []),
  ]
  const selected = dimensions.find((value) => value.id === dimension) ?? dimensions[0]
  return (
    <Page
      title={t(scope.projectId ? 'projectTitle' : scope.admin ? 'adminTitle' : 'title')}
      description={t(
        scope.projectId ? 'projectDescription' : scope.admin ? 'adminDescription' : 'description',
      )}
      action={
        <Button variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>
          <RefreshCw className="size-4" aria-hidden />
          {t('refresh')}
        </Button>
      }
    >
      <UsageFiltersForm
        admin={scope.admin === true}
        models={report?.current.models ?? []}
        keys={report?.current.keys ?? []}
        onApply={setFilters}
      />
      {query.isPending && <QueryState pending error={null} retry={() => void query.refetch()} />}
      {query.isError && (
        <div className="space-y-3 rounded-lg border p-4">
          <p role="alert" className="text-sm text-destructive">
            {t(message)}
          </p>
          <Button variant="outline" onClick={() => void query.refetch()}>
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
          <div className="grid gap-4 lg:grid-cols-2">
            <UsageDistribution title={t('models')} groups={report.current.models} />
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
                groups={
                  report.current[selected.id as 'keys' | 'connections' | 'provider_models'] ?? []
                }
              />
            </div>
          </div>
          <UsageKeyTable key={JSON.stringify(filters)} groups={report.current.keys} />
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
