import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getMemberOverview, memberOverviewKey } from '@/api/member-overview'
import { QueryState } from '@/components/app/CatalogUI'
import { exactDecimal, exactInteger } from '@/views/home/monthly-account-values'

export default function MemberOverview({
  actor,
  target,
  generation,
  authorized,
}: {
  actor: string
  target: string
  generation: number
  authorized: boolean
}) {
  const { t, i18n } = useTranslation('governance')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const query = useQuery({
    queryKey: memberOverviewKey(actor, target, generation),
    queryFn: ({ signal }) => getMemberOverview(target, signal),
    enabled: authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  const data = authorized && query.isSuccess && !query.isFetching ? query.data : undefined
  const usage = data?.personal.usage
  const active = data?.personal.active_reservations
  const money = (values: Record<string, string>) =>
    Object.entries(values)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([code, amount]) => `${exactDecimal(amount, locale)} ${code}`)
      .join('; ') || t('memberOverview.noAmounts')
  const date = (value: string, timeZone?: string) =>
    new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short', timeZone }).format(
      new Date(value),
    )
  return (
    <section aria-label={t('memberOverview.title')} className="space-y-4">
      {authorized && (
        <QueryState
          pending={query.isPending || query.isFetching}
          error={query.error}
          retry={() => {
            if (authorized) void query.refetch()
          }}
        />
      )}
      {data && (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            <section
              className="space-y-2 rounded-lg border p-4"
              aria-label={t('memberOverview.tokens')}
            >
              <h3 className="text-sm text-muted-foreground">{t('memberOverview.tokens')}</h3>
              <p className="text-xl font-semibold tabular-nums break-all">
                {usage ? exactInteger(usage.tokens_used, locale) : t('memberOverview.unknown')} /{' '}
                {data.personal.tokens_month === null
                  ? t('memberOverview.notSet')
                  : exactInteger(data.personal.tokens_month, locale)}
              </p>
              {usage && (
                <>
                  <p className="text-xs text-muted-foreground">
                    {t('memberOverview.retainedTokens', {
                      amount: exactInteger(usage.tokens_held, locale),
                    })}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t('memberOverview.liveTokens', {
                      amount: active
                        ? exactInteger(active.tokens_held, locale)
                        : t('memberOverview.unknown'),
                    })}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t('memberOverview.unknownTokens', {
                      count: exactInteger(usage.tokens_unknown, locale),
                    })}
                  </p>
                </>
              )}
            </section>
            <section
              className="space-y-2 rounded-lg border p-4"
              aria-label={t('memberOverview.money')}
            >
              <h3 className="text-sm text-muted-foreground">{t('memberOverview.money')}</h3>
              <p className="text-xl font-semibold tabular-nums break-all">
                {usage ? money(usage.money_used) : t('memberOverview.unknown')} /{' '}
                {data.personal.money_month === null
                  ? t('memberOverview.notSet')
                  : `${exactDecimal(data.personal.money_month, locale)} ${data.personal.currency}`}
              </p>
              {usage && (
                <>
                  <p className="text-xs text-muted-foreground">
                    {t('memberOverview.retainedMoney', { amount: money(usage.money_held) })}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t('memberOverview.liveMoney', {
                      amount: active ? money(active.money_held) : t('memberOverview.unknown'),
                    })}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t('memberOverview.unknownMoney', {
                      count: exactInteger(usage.money_unknown, locale),
                    })}
                  </p>
                </>
              )}
            </section>
            <section
              className="space-y-2 rounded-lg border p-4"
              aria-label={t('memberOverview.keys')}
            >
              <h3 className="text-sm text-muted-foreground">{t('memberOverview.keys')}</h3>
              <p className="text-xl font-semibold tabular-nums">
                {exactInteger(data.total_personal_keys, locale)}
              </p>
              <p className="text-xs text-muted-foreground">{t('memberOverview.keyHelp')}</p>
            </section>
          </div>
          <div className="space-y-1 text-xs text-muted-foreground">
            <p>
              {t(
                data.personal.runtime_applied
                  ? 'memberOverview.applied'
                  : 'memberOverview.notApplied',
              )}
            </p>
            <p>
              {usage
                ? t(usage.covered ? 'memberOverview.covered' : 'memberOverview.uncovered')
                : t(
                    data.personal.usage_status === 'inactive'
                      ? 'memberOverview.inactive'
                      : 'memberOverview.unavailable',
                  )}
            </p>
            {usage && (
              <>
                <p>
                  {t('memberOverview.window', {
                    from: date(usage.month_start, usage.time_zone),
                    to: date(usage.month_end, usage.time_zone),
                    zone: usage.time_zone,
                  })}
                </p>
                <p>{t('memberOverview.asOf', { date: date(usage.as_of) })}</p>
              </>
            )}
            <p>
              {t('memberOverview.observed', {
                date: date(data.observed_at),
                currency: data.platform_currency,
              })}
            </p>
          </div>
        </>
      )}
    </section>
  )
}
