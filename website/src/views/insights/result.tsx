import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { UsageCount, UsageReport } from '@/types/usage'
import { chartRatio, exactNumber, usageTime } from '@/views/usage/format'

export default function InsightsResult({ report }: { report: UsageReport }) {
  const { t, i18n } = useTranslation('insights')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const summary = report.current.summary
  const groups = [...report.current.models].sort((a, b) => {
    const left = BigInt(a.stats.tokens.total.known),
      right = BigInt(b.stats.tokens.total.known)
    return left > right ? -1 : left < right ? 1 : a.id.localeCompare(b.id)
  })
  const time = (value: string) => usageTime(value, locale, report.timezone)
  function count(value: UsageCount) {
    return (
      <>
        <strong className="text-2xl tabular-nums">{exactNumber(value.known, locale)}</strong>
        <p className="text-xs text-muted-foreground">
          {t(value.unknown_calls ? 'unknownCoverage' : 'completeCoverage', {
            count: value.unknown_calls,
          })}
        </p>
      </>
    )
  }
  return (
    <div className="space-y-4">
      <dl className="space-y-2 break-words text-xs text-muted-foreground">
        <div>
          <dt className="sr-only">{t('source')}</dt>
          <dd>{report.source === 'persisted_call_records' ? t('source') : t('unavailable')}</dd>
        </div>
        <div>
          <dt className="font-medium text-foreground">{t('rangeHelp')}</dt>
          <dd>
            {t('range', {
              from: time(report.current.from),
              to: time(report.current.to),
              timezone: report.timezone,
            })}
          </dd>
        </div>
        <div>
          <dt className="sr-only">
            {t('currentCapture', { timezone: report.timezone, granularity: report.granularity })}
          </dt>
          <dd>
            {t('currentCapture', { timezone: report.timezone, granularity: report.granularity })}
          </dd>
        </div>
        <div>
          <dt className="sr-only">{t('queriedAt', { time: time(report.queried_at) })}</dt>
          <dd>{t('queriedAt', { time: time(report.queried_at) })}</dd>
        </div>
        <div>
          <dt className="sr-only">
            {t('latestCompletedAt', {
              time: report.latest_completed_at
                ? time(report.latest_completed_at)
                : t('noCompletedCalls'),
            })}
          </dt>
          <dd>
            {report.latest_completed_at
              ? t('latestCompletedAt', { time: time(report.latest_completed_at) })
              : t('noCompletedCalls')}
          </dd>
        </div>
        <div>
          <dt className="font-medium text-foreground">{t('filters')}</dt>
          <dd>{t('tokenDefinition')}</dd>
        </div>
      </dl>
      {report.may_lag && <p className="text-sm text-muted-foreground">{t('mayLag')}</p>}
      {summary.requests === 0 && (
        <p role="status" className="rounded-md border border-dashed p-4 text-sm">
          {t('noCalls')}
        </p>
      )}
      <div className="grid gap-3 sm:grid-cols-3">
        <Card>
          <CardContent className="space-y-1">
            <p className="text-sm text-muted-foreground">{t('requests')}</p>
            <strong className="text-2xl tabular-nums">
              {exactNumber(String(summary.requests), locale)}
            </strong>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="space-y-1">
            <p className="text-sm text-muted-foreground">{t('knownInput')}</p>
            {count(summary.tokens.input)}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="space-y-1">
            <p className="text-sm text-muted-foreground">{t('knownOutput')}</p>
            {count(summary.tokens.output)}
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{t('distribution')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-xs text-muted-foreground">{t('distributionHelp')}</p>
          <div className="space-y-1">
            <p className="text-sm text-muted-foreground">{t('knownTotal')}</p>
            {count(summary.tokens.total)}
          </div>
          {BigInt(summary.tokens.total.known) === 0n && (
            <p className="text-sm text-muted-foreground">{t('noKnownTokens')}</p>
          )}
          <ul className="space-y-4">
            {groups.map((group) => (
              <li key={group.id} className="space-y-1">
                <div className="flex justify-between gap-3 text-sm">
                  <span className="min-w-0 break-all">
                    {group.unknown ? t('unknownModel') : group.name || group.id}
                  </span>
                  <strong className="shrink-0 tabular-nums">
                    {exactNumber(group.stats.tokens.total.known, locale)}
                  </strong>
                </div>
                <div aria-hidden className="h-2 overflow-hidden rounded bg-muted">
                  <div
                    className="h-full bg-primary"
                    style={{
                      width:
                        String(
                          Math.min(
                            1,
                            chartRatio(group.stats.tokens.total.known, summary.tokens.total.known),
                          ) * 100,
                        ) + '%',
                    }}
                  />
                </div>
                <p className="text-xs text-muted-foreground">
                  {t(
                    group.stats.tokens.total.unknown_calls ? 'unknownCoverage' : 'completeCoverage',
                    { count: group.stats.tokens.total.unknown_calls },
                  )}
                </p>
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="space-y-2">
          <h3 className="font-medium">{t('adviceTitle')}</h3>
          <p className="text-sm text-muted-foreground">{t('advice')}</p>
        </CardContent>
      </Card>
    </div>
  )
}
