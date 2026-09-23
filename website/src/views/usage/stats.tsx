import { useTranslation } from 'react-i18next'
import { Activity, Clock3, Hash, CircleCheck } from 'lucide-react'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import type { UsageCount, UsageStats, UsageAmount } from '@/types/usage'
import { exactNumber } from './format'

export function TokenValue({ value, compact = false }: { value: UsageCount; compact?: boolean }) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  return (
    <span className="inline-flex max-w-full min-w-0 flex-col gap-1">
      <span className="break-all tabular-nums">
        {value.value === null ? t('unknown') : exactNumber(value.value, locale)}
      </span>
      {value.unknown_calls > 0 && (
        <span className="text-xs font-normal text-muted-foreground">
          {t('known', { value: exactNumber(value.known, locale) })}
          {!compact && <> · {t('missing', { count: value.unknown_calls })}</>}
        </span>
      )}
    </span>
  )
}
export function AmountValues({ amounts }: { amounts: UsageAmount[] }) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  return amounts.length ? (
    <span className="inline-flex max-w-full flex-col gap-1 tabular-nums">
      {amounts.map((amount) => (
        <span key={amount.currency} className="break-all">
          {amount.currency} {exactNumber(amount.amount, locale)}
        </span>
      ))}
    </span>
  ) : (
    <span className="text-muted-foreground">{t('noCharges')}</span>
  )
}
export function SummaryCards({
  current,
  previous,
}: {
  current: UsageStats
  previous?: UsageStats
}) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const rate = (value: number | null) =>
    value === null
      ? t('unknown')
      : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(value)
  const duration = (value: number | null) =>
    value === null
      ? t('unknown')
      : t('milliseconds', {
          value: new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(value),
        })
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Card>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            {t('totalTokens')}
            <Hash className="size-4" aria-hidden />
          </div>
          <div className="text-2xl font-semibold tracking-tight">
            <TokenValue value={current.tokens.total} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t('input')}:{' '}
            {current.tokens.input.value === null
              ? t('unknown')
              : exactNumber(current.tokens.input.value, locale)}{' '}
            · {t('output')}:{' '}
            {current.tokens.output.value === null
              ? t('unknown')
              : exactNumber(current.tokens.output.value, locale)}
          </p>
          {previous && (
            <p className="text-xs text-muted-foreground">
              {t('previousValue', {
                value:
                  previous.tokens.total.value === null
                    ? t('unknown')
                    : exactNumber(previous.tokens.total.value, locale),
              })}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            {t('requests')}
            <Activity className="size-4" aria-hidden />
          </div>
          <p className="text-2xl font-semibold tabular-nums">
            {current.requests.toLocaleString(locale)}
          </p>
          <p className="text-xs text-muted-foreground">
            {t('failures', { errors: current.errors, canceled: current.canceled })}
          </p>
          {previous && (
            <p className="text-xs text-muted-foreground">
              {t('previousValue', { value: previous.requests.toLocaleString(locale) })}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            {t('successRate')}
            <CircleCheck className="size-4" aria-hidden />
          </div>
          <p className="text-2xl font-semibold tabular-nums">{rate(current.success_rate)}</p>
          {previous && (
            <p className="text-xs text-muted-foreground">
              {t('previousValue', { value: rate(previous.success_rate) })}
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            {t('duration')}
            <Clock3 className="size-4" aria-hidden />
          </div>
          <p className="text-2xl font-semibold tabular-nums">
            {duration(current.average_duration_ms)}
          </p>
          <p className="text-xs text-muted-foreground">{t('durationHelp')}</p>
          {previous && (
            <p className="text-xs text-muted-foreground">
              {t('previousValue', { value: duration(previous.average_duration_ms) })}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
export function ChargeSummary({ stats }: { stats: UsageStats }) {
  const { t } = useTranslation('usage')
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('charges')}</CardTitle>
        <p className="text-xs text-muted-foreground">{t('chargeHelp')}</p>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap gap-x-8 gap-y-3">
          {stats.amounts.length ? (
            stats.amounts.map((amount) => (
              <div key={amount.currency}>
                <div className="text-xl font-semibold">
                  <AmountValues amounts={[amount]} />
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t('chargedCalls', { count: amount.calls })}
                </p>
              </div>
            ))
          ) : (
            <p className="text-sm text-muted-foreground">{t('noCharges')}</p>
          )}
        </div>
        <p className="mt-3 text-sm text-muted-foreground">
          {t('unknownCharges', { count: stats.unknown_amount_calls })}
        </p>
        <details className="mt-3 text-xs text-muted-foreground">
          <summary className="cursor-pointer">{t('priceCoverage')}</summary>
          <ul className="mt-2 space-y-1">
            {Object.entries(stats.pricing_statuses).map(([status, count]) => (
              <li key={status}>
                {t(`pricingStatuses.${status}`, { defaultValue: t('unknown') })}: {count}
              </li>
            ))}
          </ul>
        </details>
      </CardContent>
    </Card>
  )
}
