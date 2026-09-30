import { useTranslation } from 'react-i18next'
import type { QuotaUsage } from '@/types/resource-limits'

export function QuotaUsageSummary({ usage }: { usage: QuotaUsage | null }) {
  const { t, i18n } = useTranslation('limits')
  const count = (value: number) =>
    Number.isSafeInteger(value) && value >= 0
      ? value.toLocaleString(i18n.resolvedLanguage)
      : t('unknown')
  const date = (value: string) => {
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime())
      ? t('unknown')
      : parsed.toLocaleString(i18n.resolvedLanguage, { timeZone: usage?.time_zone || 'UTC' })
  }
  return (
    <section className="space-y-3 rounded-lg border p-4" aria-label={t('quotaUsage')}>
      <h4 className="font-medium">{t('quotaUsage')}</h4>
      {!usage ? (
        <p className="text-muted-foreground">{t('usageUnavailable')}</p>
      ) : !usage.activated ? (
        <p className="text-muted-foreground">{t('notActivated')}</p>
      ) : (
        <>
          <p className="text-xs text-muted-foreground">
            {t('usageSnapshot', {
              value: usage.as_of ? date(usage.as_of) : t('unknown'),
              zone: usage.time_zone,
            })}
          </p>
          {usage.coverage_start && (
            <p className="text-xs text-muted-foreground">
              {t('coverageStart', { value: date(usage.coverage_start) })}
            </p>
          )}
          <p className="text-xs text-muted-foreground">{t('usageHelp')}</p>
          <dl className="grid gap-4 sm:grid-cols-2">
            {(['active', 'minute', 'five_hours', 'seven_days', 'month'] as const).map((field) => {
              const window = usage[field]
              return (
                <div key={field} className="space-y-1" aria-label={t(`window_${field}`)}>
                  <dt className="font-medium">{t(`window_${field}`)}</dt>
                  {!window ? (
                    <dd>{t('usageUnavailable')}</dd>
                  ) : (
                    <>
                      <dd className={window.covered ? 'text-muted-foreground' : 'text-destructive'}>
                        {t(window.covered ? 'coverageComplete' : 'coverageIncomplete')}
                      </dd>
                      <dd>{t('tokensUsed', { value: count(window.tokens_used) })}</dd>
                      <dd>{t('tokensHeld', { value: count(window.tokens_held) })}</dd>
                      <dd>{t('tokensUnknown', { value: count(window.tokens_unknown) })}</dd>
                      <dd>
                        {t('moneyUsed')}: <MoneyValues amounts={window.money_used} />
                      </dd>
                      <dd>
                        {t('moneyHeld')}: <MoneyValues amounts={window.money_held} />
                      </dd>
                      <dd>{t('moneyUnknown', { value: count(window.money_unknown) })}</dd>
                    </>
                  )}
                </div>
              )
            })}
          </dl>
        </>
      )}
    </section>
  )
}
function MoneyValues({ amounts }: { amounts: Record<string, string> }) {
  const { t } = useTranslation('limits')
  const entries = Object.entries(amounts ?? {})
  return entries.length
    ? entries.map(([currency, amount]) => (
        <span key={currency} className="mr-2 break-all">
          {amount} {currency}
        </span>
      ))
    : t('noRecordedAmounts')
}
