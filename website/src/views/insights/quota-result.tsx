import { useTranslation } from 'react-i18next'
import type { QuotaInsightsReport, QuotaInsightsRow } from '@/types/insights-quota'
import { exactNumber, usageTime } from '@/views/usage/format'

const statusLabels = {
  active: 'quotaStatus_active',
  disabled: 'quotaStatus_disabled',
  archived: 'quotaStatus_archived',
  pending: 'quotaStatus_pending',
  revoked: 'quotaStatus_revoked',
} as const satisfies Record<QuotaInsightsRow['status'], string>
const levelLabels = {
  normal: 'quotaLevel_normal',
  near: 'quotaLevel_near',
  critical: 'quotaLevel_critical',
  reached: 'quotaLevel_reached',
  unknown: 'quotaLevel_unknown',
} as const satisfies Record<QuotaInsightsRow['level'], string>
const estimateLabels = {
  estimated: 'quotaEstimate_estimated',
  already_reached: 'quotaEstimate_already_reached',
  no_local_cap: 'quotaEstimate_no_local_cap',
  zero_cap: 'quotaEstimate_zero_cap',
  inactive: 'quotaEstimate_inactive',
  unavailable: 'quotaEstimate_unavailable',
  incomplete_coverage: 'quotaEstimate_incomplete_coverage',
  unknown_usage: 'quotaEstimate_unknown_usage',
  zero_rate: 'quotaEstimate_zero_rate',
  after_reset: 'quotaEstimate_after_reset',
} as const satisfies Record<QuotaInsightsRow['estimate_status'], string>
const behaviorLabels = {
  stop: 'quotaBehavior_stop',
  alert_only: 'quotaBehavior_alert_only',
} as const satisfies Record<QuotaInsightsRow['behavior'], string>

export default function QuotaInsightsResult({ report }: { report: QuotaInsightsReport }) {
  const { t, i18n } = useTranslation('insights')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const number = (value: string | null) =>
    value === null ? t('quotaUnknown') : exactNumber(value, locale)
  const amount = (value: string | null) =>
    value === null
      ? t('quotaUnknown')
      : report.dimension === 'tokens'
        ? number(value)
        : `${value} ${report.currency}`
  const time = (value: string | null) =>
    value === null ? t('quotaUnknown') : usageTime(value, locale, report.time_zone)
  const width = (value: string | null) => {
    if (value === null) return '0%'
    const [whole] = value.split('.')
    return BigInt(whole) >= 100n ? '100%' : `${value}%`
  }
  const high = report.items.filter((row) =>
    ['near', 'critical', 'reached'].includes(row.level),
  ).length
  const estimates = report.items.filter((row) => row.estimate_within_seven_days).length
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t('quotaPageScope')}</p>
      <dl className="space-y-1 text-xs text-muted-foreground">
        <div>
          <dt>{t('quotaSource')}</dt>
          <dd>{t('quotaObserved', { time: time(report.as_of), timezone: report.time_zone })}</dd>
        </div>
        <div>
          <dt>{t('quotaBaseline')}</dt>
          <dd>
            {t('quotaRange', { from: time(report.baseline_from), to: time(report.baseline_to) })}
          </dd>
        </div>
        <div>
          <dt>{t('quotaMonth')}</dt>
          <dd>
            {t('quotaRange', { from: time(report.month_from), to: time(report.month_reset) })}
          </dd>
        </div>
        <div>
          <dt>{t('quotaCoverageStart')}</dt>
          <dd>{time(report.coverage_start)}</dd>
        </div>
      </dl>
      <div className="grid gap-3 sm:grid-cols-3">
        {[
          ['quotaPageAccounts', report.items.length],
          ['quotaPageHigh', high],
          ['quotaPageEstimates', estimates],
        ].map(([key, value]) => (
          <div key={key} className="rounded-lg border p-4">
            <p className="text-sm text-muted-foreground">{t(String(key))}</p>
            <strong className="text-2xl tabular-nums">{number(String(value))}</strong>
          </div>
        ))}
      </div>
      <section
        className="space-y-4 rounded-lg border p-4"
        aria-label={t(report.kind === 'team_quota' ? 'quotaTeamRows' : 'quotaKeyRows')}
      >
        <h3 className="font-medium">
          {t(report.kind === 'team_quota' ? 'quotaTeamRows' : 'quotaKeyRows')}
        </h3>
        {!report.items.length && <p role="status">{t('quotaEmpty')}</p>}
        {report.items.map((row) => (
          <div className="space-y-2 border-b pb-4 last:border-0 last:pb-0" key={row.id}>
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div>
                <strong>{row.name || row.id}</strong>
                <span className="block break-all text-xs text-muted-foreground">{row.id}</span>
                <span className="block text-xs text-muted-foreground">
                  {t(statusLabels[row.status])}
                </span>
              </div>
              <div className="text-right">
                <strong>
                  {row.utilization_percent === null
                    ? t('quotaUnknown')
                    : t('quotaPercent', { value: row.utilization_percent })}
                </strong>
                <span className="block text-xs text-muted-foreground">
                  {t(levelLabels[row.level])}
                </span>
              </div>
            </div>
            <div
              role="img"
              aria-label={t('quotaProgress', {
                name: row.name || row.id,
                value:
                  row.utilization_percent === null
                    ? t('quotaUnknown')
                    : t('quotaPercent', { value: row.utilization_percent }),
              })}
              className="h-2 overflow-hidden rounded-full bg-muted"
            >
              <div
                className={`h-full rounded-full ${['critical', 'reached'].includes(row.level) ? 'bg-destructive' : 'bg-primary'}`}
                style={{ width: width(row.utilization_percent) }}
              />
            </div>
            <dl className="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-muted-foreground">{t('quotaOwnCap')}</dt>
                <dd>{row.local_cap === null ? t('quotaNoLocalCap') : amount(row.local_cap)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaSettledMonth')}</dt>
                <dd>{amount(row.month_settled)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaSettledBaseline')}</dt>
                <dd>{amount(row.baseline_settled)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaHolds')}</dt>
                <dd>
                  {t('quotaHeldValues', {
                    month: amount(row.month_held),
                    baseline: amount(row.baseline_held),
                    active: amount(row.active_held),
                  })}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaCoverage')}</dt>
                <dd>
                  {t('quotaCoverageValues', {
                    month: t(row.month_covered ? 'quotaCovered' : 'quotaIncomplete'),
                    baseline: t(row.baseline_covered ? 'quotaCovered' : 'quotaIncomplete'),
                  })}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaUnknownUsage')}</dt>
                <dd>
                  {t('quotaUnknownValues', {
                    month: number(row.month_unknown),
                    baseline: number(row.baseline_unknown),
                  })}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaBehavior')}</dt>
                <dd>{t(behaviorLabels[row.behavior])}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('quotaEstimate')}</dt>
                <dd>
                  {t(estimateLabels[row.estimate_status])}
                  {row.estimate_status === 'estimated' && (
                    <span className="block">{time(row.estimate_at)}</span>
                  )}
                </dd>
              </div>
            </dl>
            {report.kind === 'key_quota' && (
              <p className="text-xs text-muted-foreground">
                {t('quotaSharedKeys', { count: row.shared_key_count })}
              </p>
            )}
          </div>
        ))}
      </section>
      <section className="space-y-2 rounded-lg border p-4" aria-label={t('quotaAdviceTitle')}>
        <h3 className="font-medium">{t('quotaAdviceTitle')}</h3>
        <p className="text-sm">{t('quotaAdvice')}</p>
        <p className="text-xs text-muted-foreground">{t('quotaForecastDefinition')}</p>
      </section>
    </div>
  )
}
