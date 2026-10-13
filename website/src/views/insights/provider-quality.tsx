import { useTranslation } from 'react-i18next'
import { Table } from '@/components/ui/table'
import type { ProviderQualityReport } from '@/types/insights'
import { exactNumber, usageTime } from '@/views/usage/format'

export default function ProviderQualityResult({ report }: { report: ProviderQualityReport }) {
  const { t, i18n } = useTranslation('insights')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const number = (value: number) => exactNumber(String(value), locale)
  const duration = (value: number | null) =>
    value === null
      ? t('qualityUnknown')
      : t('qualityMilliseconds', {
          value: new Intl.NumberFormat(locale, { maximumFractionDigits: 3 }).format(value),
        })
  const time = (value: string) => usageTime(value, locale, report.timezone)
  const rate = (value: number | null) =>
    value === null
      ? t('qualityUnknown')
      : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(
          value / 10000,
        )
  const observations = [
    ['qualityObservationRateLimited', report.summary.rate_limited],
    ['qualityObservationServerErrors', report.summary.server_errors],
    ['qualityObservationAttribution', report.unknown_attribution],
    ['qualityObservationDuration', report.summary.unknown_duration],
  ] as const
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t('qualityDefinition')}</p>
      <p className="text-xs text-muted-foreground">{t('qualityMeanDefinition')}</p>
      <dl className="space-y-1 break-words text-xs text-muted-foreground">
        <div>
          <dt>{t('qualityAttempts')}</dt>
          <dd>{number(report.total_attempts)}</dd>
        </div>
        <div>
          <dt>{t('qualitySource')}</dt>
          <dd>
            {t('range', {
              from: time(report.from),
              to: time(report.to),
              timezone: report.timezone,
            })}
          </dd>
        </div>
        <div>
          <dt className="sr-only">{t('queriedAt', { time: time(report.queried_at) })}</dt>
          <dd>{t('queriedAt', { time: time(report.queried_at) })}</dd>
        </div>
        <div>
          <dt>{t('qualityLatest')}</dt>
          <dd>
            {report.latest_completed_at ? time(report.latest_completed_at) : t('qualityUnknown')}
          </dd>
        </div>
      </dl>
      {report.may_lag && <p className="text-xs text-muted-foreground">{t('mayLag')}</p>}
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          ['qualitySuccessRate', rate(report.summary.success_rate_bps)],
          ['qualityMean', duration(report.summary.average_duration_ms)],
          ['qualityUnknownAttribution', number(report.unknown_attribution)],
        ].map(([key, value]) => (
          <div key={key} className="rounded-lg border p-4">
            <p className="text-sm text-muted-foreground">{t(key)}</p>
            <strong className="text-2xl tabular-nums">{value}</strong>
            {key === 'qualityMean' && (
              <p className="text-xs text-muted-foreground">
                {t('qualityDurationCounts', {
                  known: number(report.summary.known_duration),
                  unknown: number(report.summary.unknown_duration),
                })}
              </p>
            )}
          </div>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">{t('qualityUnknownHelp')}</p>
      {!report.providers.length && <p role="status">{t('qualityEmpty')}</p>}
      <div className="overflow-x-auto">
        <Table aria-label={t('providerReportTitle')}>
          <thead>
            <tr>
              {[
                'qualityProvider',
                'qualityAttempts',
                'qualityEligible',
                'qualityExcluded',
                'qualityRejected',
                'qualitySuccessRate',
                'quality429',
                'quality5xx',
                'qualityMean',
                'qualityP95',
                'qualityDurationCoverage',
              ].map((key) => (
                <th key={key}>{t(key)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {report.providers.map((row) => (
              <tr key={row.provider_id}>
                <td className="break-all">
                  {row.name || row.provider_id}
                  <span className="block text-xs text-muted-foreground">{row.provider_id}</span>
                </td>
                <td>{number(row.attempts)}</td>
                <td>{number(row.eligible)}</td>
                <td>{number(row.excluded)}</td>
                <td>{number(row.credential_rejected)}</td>
                <td>{rate(row.success_rate_bps)}</td>
                <td>{number(row.rate_limited)}</td>
                <td>{number(row.server_errors)}</td>
                <td>{duration(row.average_duration_ms)}</td>
                <td>
                  {row.p95_duration_ms === null
                    ? t('qualityUnknown')
                    : t('qualityMilliseconds', { value: number(row.p95_duration_ms) })}
                </td>
                <td>
                  {t('qualityDurationCounts', {
                    known: number(row.known_duration),
                    unknown: number(row.unknown_duration),
                  })}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      </div>
      <section
        className="space-y-2 rounded-lg border p-4"
        aria-label={t('qualityObservationsTitle')}
      >
        <h3 className="font-medium">{t('qualityObservationsTitle')}</h3>
        <p className="text-xs text-muted-foreground">{t('qualityObservationsDefinition')}</p>
        {report.total_attempts === 0 ? (
          <p className="text-sm">{t('qualityObservationsEmpty')}</p>
        ) : observations.some(([, count]) => count > 0) ? (
          <ul className="list-disc space-y-2 pl-5 text-sm">
            {observations.map(([key, count]) =>
              count > 0 ? <li key={key}>{t(key, { value: number(count) })}</li> : null,
            )}
          </ul>
        ) : (
          <p className="text-sm">{t('qualityObservationsNone')}</p>
        )}
      </section>
    </div>
  )
}
