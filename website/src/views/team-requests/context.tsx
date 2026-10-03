import { useTranslation } from 'react-i18next'
import type { TeamQuotaContext, TeamQuotaDimension } from '@/types/team-requests'
import type { QuotaUsage } from '@/types/resource-limits'
export function QuotaValue({
  value,
  dimension,
  currency,
  inherited = false,
}: {
  value: string | null
  dimension: TeamQuotaDimension
  currency: string | null
  inherited?: boolean
}) {
  const { t, i18n } = useTranslation('teamRequests')
  return (
    <>
      {value === null
        ? t(inherited ? 'inherited' : 'unlimited')
        : dimension === 'tokens'
          ? BigInt(value).toLocaleString(i18n.resolvedLanguage)
          : `${value} ${currency ?? t('unknown')}`}
    </>
  )
}
export function QuotaContext({ context, title }: { context: TeamQuotaContext; title: string }) {
  const { t, i18n } = useTranslation('teamRequests')
  const date = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage, { timeZone: context.time_zone })
  return (
    <section className="space-y-3 rounded-lg border p-4" aria-label={title}>
      <h3 className="font-medium">{title}</h3>
      <dl className="grid gap-3 text-sm sm:grid-cols-2">
        {(['member_stored', 'member_effective', 'team_stored', 'team_effective'] as const).map(
          (field, index) => (
            <div key={field}>
              <dt className="text-muted-foreground">
                {t(['storedMember', 'effectiveMember', 'storedTeam', 'effectiveTeam'][index])}
              </dt>
              <dd>
                <QuotaValue
                  value={context[field]}
                  dimension={context.dimension}
                  currency={context.currency}
                  inherited={field === 'member_stored'}
                />
              </dd>
            </div>
          ),
        )}
      </dl>
      <p className="text-xs text-muted-foreground">
        {t('quotaWindow', {
          start: date(context.month_start),
          end: date(context.month_end),
          zone: context.time_zone,
        })}
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <MonthUsage usage={context.member_usage} context={context} title={t('usageMember')} />
        <MonthUsage usage={context.team_usage} context={context} title={t('usageTeam')} />
      </div>
    </section>
  )
}
function MonthUsage({
  usage,
  context,
  title,
}: {
  usage: QuotaUsage | null
  context: TeamQuotaContext
  title: string
}) {
  const { t, i18n } = useTranslation('teamRequests')
  const window = usage?.activated ? usage.month : null
  const count = (value: number) =>
    Number.isSafeInteger(value) && value >= 0
      ? value.toLocaleString(i18n.resolvedLanguage)
      : t('unknown')
  const amount = (kind: 'used' | 'held') =>
    context.dimension === 'tokens'
      ? count(kind === 'used' ? window!.tokens_used : window!.tokens_held)
      : ((kind === 'used' ? window!.money_used : window!.money_held)[context.currency ?? ''] ??
        t('noAmounts'))
  return (
    <div className="space-y-1 text-xs">
      <h4 className="font-medium">{title}</h4>
      {!window ? (
        <p>{t('unknown')}</p>
      ) : (
        <>
          <p>
            {t('usageAsOf', {
              value: usage?.as_of
                ? new Date(usage.as_of).toLocaleString(i18n.resolvedLanguage, {
                    timeZone: usage.time_zone,
                  })
                : t('unknown'),
            })}
          </p>
          <p>{t(window.covered ? 'covered' : 'uncovered')}</p>
          <p>{t('settled', { value: amount('used') })}</p>
          <p>{t('held', { value: amount('held') })}</p>
          <p>
            {t('unknownCalls', {
              value: count(
                context.dimension === 'tokens' ? window.tokens_unknown : window.money_unknown,
              ),
            })}
          </p>
        </>
      )}
    </div>
  )
}
