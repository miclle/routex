import { useId, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { Link } from 'react-router'
import { Activity, Hash, CircleCheck, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getHomeUsage, homeUsageKey } from '@/api/home-usage'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import type { UsageGroup, UsagePeriod } from '@/types/usage'
import { TokenValue } from '@/views/usage/stats'
import { exactNumber } from '@/views/usage/format'
import { overviewTrend, usageShare } from './usage-overview-values'

function UsageRows({ period, dimension }: { period: UsagePeriod; dimension: 'models' | 'keys' }) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const [page, setPage] = useState(0)
  const groups = period[dimension]
  const pages = Math.max(1, Math.ceil(groups.length / 20))
  const current = Math.min(page, pages - 1)
  const identity = (group: UsageGroup) =>
    group.unknown ? t('historyUnknown') : group.name || group.id
  return (
    <>
      <Table aria-label={t(dimension === 'models' ? 'modelDetails' : 'keyDetails')}>
        <thead>
          <tr>
            <th>{t('resource')}</th>
            <th>{t('requests30')}</th>
            <th>{t('tokens30')}</th>
            <th>{t('share')}</th>
          </tr>
        </thead>
        <tbody>
          {groups.slice(current * 20, (current + 1) * 20).map((group) => {
            const share = usageShare(group, period)
            return (
              <tr key={group.id}>
                <td className="min-w-40 [overflow-wrap:anywhere]">
                  {identity(group)}
                  {group.name && group.id ? (
                    <p className="text-xs text-muted-foreground">{group.id}</p>
                  ) : null}
                </td>
                <td className="tabular-nums">{group.stats.requests.toLocaleString(language)}</td>
                <td>
                  <TokenValue value={group.stats.tokens.total} compact />
                </td>
                <td className="min-w-32">
                  {share === null ? (
                    t('unknown')
                  ) : (
                    <div className="flex items-center gap-2">
                      <span
                        className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted"
                        aria-hidden
                      >
                        <span className="block h-full bg-primary" style={{ width: `${share}%` }} />
                      </span>
                      <span className="tabular-nums">
                        {t('percent', { value: exactNumber(share, language) })}
                      </span>
                    </div>
                  )}
                </td>
              </tr>
            )
          })}
          {!groups.length && (
            <tr>
              <td colSpan={4} className="py-8 text-center text-muted-foreground">
                {t('noUsage')}
              </td>
            </tr>
          )}
        </tbody>
      </Table>
      {pages > 1 && (
        <div className="flex items-center justify-end gap-3 p-3">
          <span className="text-xs text-muted-foreground">
            {t('usagePage', { page: current + 1, pages })}
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={current === 0}
            onClick={() => setPage(current - 1)}
          >
            {t('previousUsage')}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={current === pages - 1}
            onClick={() => setPage(current + 1)}
          >
            {t('nextUsage')}
          </Button>
        </div>
      )}
    </>
  )
}

function TokenTrend({ period }: { period: UsagePeriod }) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const id = useId()
  const { maximum, segments } = overviewTrend(period)
  const dates = new Intl.DateTimeFormat(language, {
    timeZone: 'UTC',
    month: 'short',
    day: 'numeric',
  })
  const labels = [0, Math.floor((period.trend.length - 1) / 2), period.trend.length - 1]
  return (
    <Card>
      <CardHeader className="sm:flex-row sm:items-center sm:justify-between">
        <CardTitle>{t('tokenTrend')}</CardTitle>
        <span className="text-sm text-muted-foreground">{t('last30')}</span>
      </CardHeader>
      <CardContent className="space-y-4">
        <svg
          viewBox="0 0 800 240"
          className="w-full min-w-0"
          role="img"
          aria-labelledby={`${id}-title`}
          aria-describedby={`${id}-help`}
        >
          <title id={`${id}-title`}>{t('trend30Label')}</title>
          <desc id={`${id}-help`}>{t('trendHelp')}</desc>
          {[28, 116, 204].map((y) => (
            <line
              key={y}
              x1="36"
              x2="764"
              y1={y}
              y2={y}
              className="stroke-border"
              strokeDasharray="4 4"
            />
          ))}
          <text x="36" y="16" className="fill-muted-foreground text-[11px]">
            {exactNumber(maximum, language)}
          </text>
          <text x="24" y="208" className="fill-muted-foreground text-[11px]">
            0
          </text>
          {segments.map((segment, i) => (
            <g key={i} data-known-segment={i}>
              <path d={segment.area} className="fill-primary/10" />
              <path
                d={segment.line}
                fill="none"
                className="stroke-primary"
                strokeWidth="2.5"
                vectorEffect="non-scaling-stroke"
              />
              {segment.points.length === 1 && (
                <circle
                  cx={segment.points[0].x}
                  cy={segment.points[0].y}
                  r="2.5"
                  className="fill-primary"
                />
              )}
            </g>
          ))}
          {[...new Set(labels)].map((index) => (
            <text
              key={index}
              x={36 + (index / Math.max(1, period.trend.length - 1)) * 728}
              y="230"
              textAnchor={
                index === 0 ? 'start' : index === period.trend.length - 1 ? 'end' : 'middle'
              }
              className="fill-muted-foreground text-[11px]"
            >
              {dates.format(new Date(period.trend[index].start))}
            </text>
          ))}
        </svg>
        <p className="text-xs text-muted-foreground">{t('trendHelp')}</p>
        <details>
          <summary className="w-fit cursor-pointer text-sm text-muted-foreground">
            {t('dailyValues')}
          </summary>
          <Table aria-label={t('dailyValues')}>
            <thead>
              <tr>
                <th>{t('day')}</th>
                <th>{t('requests30')}</th>
                <th>{t('tokens30')}</th>
              </tr>
            </thead>
            <tbody>
              {period.trend.map((bucket) => (
                <tr key={bucket.start}>
                  <td>{dates.format(new Date(bucket.start))}</td>
                  <td>{bucket.stats.requests.toLocaleString(language)}</td>
                  <td>
                    <TokenValue value={bucket.stats.tokens.total} />
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </details>
      </CardContent>
    </Card>
  )
}

export default function UsageOverview({
  actorId,
  generation,
}: {
  actorId: string
  generation: number
}) {
  const { t, i18n } = useTranslation('overview')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const query = useQuery({
    queryKey: homeUsageKey(actorId, generation),
    queryFn: ({ signal }) => getHomeUsage(signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const report = query.isSuccess && !query.isFetching && !query.isError ? query.data : undefined
  const status = isAxiosError(query.error) ? query.error.response?.status : undefined
  const date = (value: string) =>
    new Intl.DateTimeFormat(locale, {
      dateStyle: 'medium',
      timeStyle: 'long',
      timeZone: 'UTC',
    }).format(new Date(value))
  return (
    <section aria-label={t('personal30')} className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">{t('personal30')}</h2>
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <RefreshCw className="size-4" aria-hidden />
          {t('refresh30')}
        </Button>
      </div>
      {query.isFetching || query.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('loading30')}
        </p>
      ) : query.isError ? (
        <div className="space-y-2">
          <p role="alert">
            {t(
              status === 422
                ? 'overflow30'
                : status === 401 || status === 403 || status === 404
                  ? 'denied30'
                  : 'error30',
            )}
          </p>
          <Link to="/usage" className="text-sm text-primary underline">
            {t('usageLink')}
          </Link>
        </div>
      ) : null}
      {report && (
        <>
          <p className="text-xs text-muted-foreground">
            {t('range30', { from: date(report.current.from), to: date(report.current.to) })}
          </p>
          <div className="grid gap-4 md:grid-cols-3" data-summary-cards>
            <Card>
              <CardContent className="space-y-2">
                <div className="flex items-center justify-between text-sm text-muted-foreground">
                  {t('requests30')}
                  <Activity className="size-4" aria-hidden />
                </div>
                <p className="text-2xl font-semibold tabular-nums">
                  {report.current.summary.requests.toLocaleString(locale)}
                </p>
                <p className="text-xs text-muted-foreground">{t('last30')}</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="space-y-2">
                <div className="flex items-center justify-between text-sm text-muted-foreground">
                  {t('tokens30')}
                  <Hash className="size-4" aria-hidden />
                </div>
                <div className="text-2xl font-semibold">
                  <TokenValue value={report.current.summary.tokens.total} />
                </div>
                <p className="text-xs text-muted-foreground">{t('inputOutput30')}</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="space-y-2">
                <div className="flex items-center justify-between text-sm text-muted-foreground">
                  {t('success30')}
                  <CircleCheck className="size-4" aria-hidden />
                </div>
                <p className="text-2xl font-semibold tabular-nums">
                  {report.current.summary.success_rate === null
                    ? t('unknown')
                    : new Intl.NumberFormat(locale, {
                        style: 'percent',
                        maximumFractionDigits: 1,
                      }).format(report.current.summary.success_rate)}
                </p>
                <p className="text-xs text-muted-foreground">{t('success30Help')}</p>
              </CardContent>
            </Card>
          </div>
          <TokenTrend period={report.current} />
          <Card>
            <CardHeader className="sm:flex-row sm:items-center sm:justify-between">
              <CardTitle>{t('usageDetails')}</CardTitle>
              <span className="text-sm text-muted-foreground">{t('last30')}</span>
            </CardHeader>
            <CardContent>
              <Tabs defaultValue="models">
                <TabsList aria-label={t('usageDetails')}>
                  <TabsTrigger value="models">{t('models30')}</TabsTrigger>
                  <TabsTrigger value="keys">{t('keys30')}</TabsTrigger>
                </TabsList>
                <TabsContent value="models">
                  <UsageRows period={report.current} dimension="models" />
                </TabsContent>
                <TabsContent value="keys">
                  <UsageRows period={report.current} dimension="keys" />
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>
          <div className="space-y-1 text-xs text-muted-foreground">
            <p>{t('queried30', { date: date(report.queried_at) })}</p>
            <p>
              {t('latest30', {
                date: report.latest_completed_at ? date(report.latest_completed_at) : t('unknown'),
              })}
            </p>
            <p>{t(report.may_lag ? 'lag30' : 'source30')}</p>
          </div>
        </>
      )}
    </section>
  )
}
