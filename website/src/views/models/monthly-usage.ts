import { useEffect, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getUsage, isPersonalUsageReport } from '@/api/usage'
import type { UsageReport } from '@/types/usage'
import type { ModelAccessSource } from '@/types/model-catalog'
import { modelSourceKey } from './catalogue-metadata'

// The month is the server's UTC usage period, independent of quota reset settings.
export function monthlyUsageReport(value: UsageReport, source: ModelAccessSource): UsageReport {
  let personalShape = value
  if (source.type === 'team') {
    if (
      value.team_id !== source.team_id ||
      value.available_dimensions?.length !== 1 ||
      value.available_dimensions[0] !== 'model' ||
      !Array.isArray(value.current?.keys) ||
      value.current.keys.length !== 0
    )
      throw new Error('Invalid catalogue monthly usage')
    personalShape = { ...value, available_dimensions: ['model', 'key'] }
    delete personalShape.team_id
  }
  if (!isPersonalUsageReport(personalShape)) throw new Error('Invalid catalogue monthly usage')
  const end = new Date(value.current.to)
  const start = new Date(Date.UTC(end.getUTCFullYear(), end.getUTCMonth(), 1))
  const nextMonth = new Date(Date.UTC(end.getUTCFullYear(), end.getUTCMonth() + 1, 1))
  const groups = value.current.models
  if (
    value.timezone !== 'UTC' ||
    value.granularity !== 'month' ||
    value.may_lag !== true ||
    Date.parse(value.current.from) !== start.valueOf() ||
    Date.parse(value.queried_at) !== end.valueOf() ||
    value.current.trend.length !== 1 ||
    Date.parse(value.current.trend[0].start) !== start.valueOf() ||
    Date.parse(value.current.trend[0].end) !== nextMonth.valueOf() ||
    value.current.summary.requests > 10000 ||
    groups.some((group) => group.id !== '' && !/^mdl_[A-Za-z0-9_-]{1,26}$/.test(group.id)) ||
    groups.reduce((sum, group) => sum + group.stats.requests, 0) !== value.current.summary.requests
  )
    throw new Error('Invalid catalogue monthly usage')
  return value
}

export function useMonthlyModelUsage({
  actorID,
  generation,
  source,
  current,
  isCurrent,
}: {
  actorID: string | undefined
  generation: number
  source: ModelAccessSource | undefined
  current: boolean
  isCurrent: () => boolean
}) {
  const cache = useQueryClient()
  const scope = source ? modelSourceKey(source) : null
  const catalogue = cache.getQueryState(['model-catalog', 'list', actorID, generation])
  // Even an unchanged fast catalogue response must start one fresh scoped usage read.
  const catalogueGeneration = catalogue?.dataUpdateCount ?? 0
  const queryKey = useMemo(
    () => ['model-catalog-monthly-usage', actorID, generation, scope, catalogueGeneration] as const,
    [actorID, generation, scope, catalogueGeneration],
  )
  const enabled = !!actorID && !!source && current
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!source || !isCurrent()) throw new Error('Catalogue authority changed')
      const report = await getUsage(
        source.type === 'team' ? { teamId: source.team_id } : {},
        {
          period: 'month',
          timezone: 'UTC',
          granularity: 'month',
          compare: false,
        },
        signal,
      )
      return monthlyUsageReport(report, source)
    },
    enabled,
    retry: false,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  useEffect(() => {
    if (!enabled) void cache.cancelQueries({ queryKey, exact: true })
  }, [cache, enabled, queryKey])
  const usageState = cache.getQueryState(queryKey)
  const report =
    enabled &&
    isCurrent() &&
    catalogue?.status === 'success' &&
    catalogue.fetchStatus === 'idle' &&
    !catalogue.isInvalidated &&
    !usageState?.isInvalidated &&
    query.isSuccess &&
    !query.isFetching
      ? query.data
      : undefined
  return {
    report,
    members: (id: string) => {
      if (!report || report.member_count_basis !== 'distinct_recorded_actors') return undefined
      const group = report.current.models.find((item) => !item.unknown && item.id === id)
      return group ? group.members : { value: 0, known: 0, unknown_calls: 0 }
    },
    count: (id: string) =>
      report
        ? (report.current.models.find((group) => !group.unknown && group.id === id)?.stats
            .requests ?? 0)
        : undefined,
  }
}
