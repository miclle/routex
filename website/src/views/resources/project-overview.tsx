import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { ArrowUpRight } from 'lucide-react'
import { getProjectOverview } from '@/api/resources'
import { useSession } from '@/hooks/use-auth'
import { QueryState } from '@/components/app/CatalogUI'
import { buttonVariants } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { ResourceSection } from './shared'
import type { ProjectMonthlyQuota, ResourceRecord } from '@/types/resources'

export default function ProjectOverview({
  resource,
  isManager,
  canKeys,
}: {
  resource: ResourceRecord
  isManager: boolean
  canKeys: boolean
}) {
  const { t, i18n } = useTranslation('resources')
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const query = useQuery({
    queryKey: ['project-overview', actor, resource.id],
    queryFn: ({ signal }) => getProjectOverview(resource.id, signal),
    enabled: !!actor,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const data = query.isSuccess && !query.isFetching && actor ? query.data : undefined
  const date = (value: string | null) =>
    value ? new Date(value).toLocaleString(i18n.resolvedLanguage) : t('projectOverviewUnknown')
  const count = (value: number | null) =>
    value === null ? t('projectOverviewUnavailable') : value.toLocaleString(i18n.resolvedLanguage)
  return (
    <div className="space-y-6">
      <QueryState
        pending={query.isFetching}
        error={query.error}
        retry={() => void query.refetch()}
      />
      {data && (
        <>
          {data.counts.pending_requests !== null && data.counts.pending_requests > 0 && (
            <p role="status" className="rounded-lg border bg-muted/40 p-4 text-sm">
              {t('projectOverviewPending', { count: data.counts.pending_requests })}
            </p>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">{t('projectOverviewHelp')}</p>
            <div className="flex gap-2">
              {isManager && resource.status === 'active' && (
                <Link
                  className={buttonVariants({ variant: 'outline' })}
                  to={`/playground?project=${encodeURIComponent(resource.id)}`}
                >
                  {t('openInPlayground')}
                  <ArrowUpRight className="size-4" aria-hidden="true" />
                </Link>
              )}
              {canKeys && data.counts.active_keys !== null && resource.status === 'active' && (
                <Link className={buttonVariants()} to="?tab=keys">
                  {t('projectOverviewManageKeys')}
                </Link>
              )}
            </div>
          </div>
          <div className="grid gap-6 lg:grid-cols-2">
            <ResourceSection title={t('projectOverviewMonthly')}>
              <MonthlyQuota quota={data.monthly_quota} />
            </ResourceSection>
            <ResourceSection title={t('projectOverviewRuntime')}>
              <dl className="grid grid-cols-2 gap-5 text-sm">
                {[
                  [t('status'), t(resource.status)],
                  [
                    t('projectOverviewLastCall'),
                    data.calls_available
                      ? date(data.last_call_at)
                      : t('projectOverviewUnavailable'),
                  ],
                  [t('projectOverviewActiveKeys'), count(data.counts.active_keys)],
                  [t('modelCount'), count(data.counts.models)],
                  [t('managerCount'), count(data.counts.managers)],
                  [t('projectOverviewRequests'), count(data.counts.pending_requests)],
                ].map(([label, value]) => (
                  <div key={label}>
                    <dt className="text-muted-foreground">{label}</dt>
                    <dd className="mt-1 font-medium">{value}</dd>
                  </div>
                ))}
              </dl>
            </ResourceSection>
          </div>
          <ResourceSection title={t('projectOverviewActivity')} padded={false}>
            <Table aria-label={t('projectOverviewActivity')}>
              <thead>
                <tr>
                  <th>{t('projectOverviewAction')}</th>
                  <th>{t('projectOverviewActor')}</th>
                  <th>{t('projectOverviewTime')}</th>
                  <th>{t('status')}</th>
                </tr>
              </thead>
              <tbody>
                {data.activities.map((activity) => (
                  <tr key={activity.id}>
                    <td>{t(`projectOverviewActivity_${activity.kind}`)}</td>
                    <td>{activity.actor_name ?? t('projectOverviewUnknown')}</td>
                    <td>{date(activity.created_at)}</td>
                    <td>{t('projectOverviewCommitted')}</td>
                  </tr>
                ))}
                {!data.activities.length && (
                  <tr>
                    <td colSpan={4} className="py-8 text-center text-muted-foreground">
                      {t('projectOverviewNoActivity')}
                    </td>
                  </tr>
                )}
              </tbody>
            </Table>
          </ResourceSection>
          <ResourceSection title={t('basic')}>
            <dl className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3 text-sm">
              <div>
                <dt className="text-muted-foreground">{t('identity', { kind: t('project') })}</dt>
                <dd className="mt-1 font-mono">{resource.id}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('created')}</dt>
                <dd className="mt-1">{date(resource.created_at)}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">{t('businessDescription')}</dt>
                <dd className="mt-1">{resource.description || t('noDescription')}</dd>
              </div>
            </dl>
          </ResourceSection>
          <ResourceSection title={t('managers')}>
            <div className="space-y-3">
              {resource.managers?.map((person) => (
                <div key={person.user_id} className="flex items-center gap-3">
                  <span className="flex size-8 items-center justify-center rounded-full bg-muted text-xs">
                    {person.name.slice(0, 2).toUpperCase()}
                  </span>
                  <div>
                    <p className="text-sm">{person.name}</p>
                    <p className="text-xs text-muted-foreground">{person.email}</p>
                  </div>
                </div>
              ))}
            </div>
          </ResourceSection>
        </>
      )}
    </div>
  )
}
function percentage(used: bigint, limit: bigint) {
  return limit <= 0n ? undefined : used >= limit ? 100 : Number((used * 10000n) / limit) / 100
}
function decimal(value: string) {
  const [integer, fraction = ''] = value.split('.')
  return BigInt(integer + fraction.padEnd(18, '0'))
}
function Meter({ value, label }: { value: number | undefined; label: string }) {
  return value === undefined ? null : (
    <div
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={value}
      className="h-2 overflow-hidden rounded-full bg-muted"
    >
      <div className="h-full bg-primary" style={{ width: `${value}%` }} />
    </div>
  )
}
function MonthlyQuota({ quota }: { quota: ProjectMonthlyQuota | null }) {
  const { t, i18n } = useTranslation('resources')
  if (!quota)
    return <p className="text-sm text-muted-foreground">{t('projectOverviewUnavailable')}</p>
  const usage = quota.usage
  const money = usage?.money_used[quota.currency]
  const knownTokens = !!usage?.covered && usage.tokens_unknown === 0
  const knownMoney =
    !!usage?.covered &&
    usage.money_unknown === 0 &&
    quota.currency === quota.platform_currency &&
    money !== undefined
  const tokensPercent =
    knownTokens && quota.tokens_month !== null
      ? percentage(BigInt(usage!.tokens_used), BigInt(quota.tokens_month))
      : undefined
  const moneyPercent =
    knownMoney && quota.money_month !== null
      ? percentage(decimal(money!), decimal(quota.money_month))
      : undefined
  const number = (value: string) => BigInt(value).toLocaleString(i18n.resolvedLanguage)
  const date = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage, { timeZone: usage!.time_zone })
  return (
    <div className="space-y-5 text-sm">
      <div className="space-y-2">
        <div className="flex justify-between gap-4">
          <span>{t('projectOverviewTokens')}</span>
          <span>
            {usage ? number(usage.tokens_used) : t('projectOverviewUnknown')} /{' '}
            {quota.tokens_month === null
              ? t('projectOverviewUnlimited')
              : quota.tokens_month.toLocaleString(i18n.resolvedLanguage)}
          </span>
        </div>
        <Meter value={tokensPercent} label={t('projectOverviewTokens')} />
      </div>
      <div className="space-y-2">
        <div className="flex justify-between gap-4">
          <span>{t('projectOverviewBudget')}</span>
          <span>
            {quota.money_month === null
              ? t('projectOverviewUnlimited')
              : `${quota.money_month} ${quota.currency}`}
          </span>
        </div>
        {usage &&
          Object.entries(usage.money_used).map(([currency, amount]) => (
            <p key={currency}>{t('projectOverviewMoneyUsed', { amount, currency })}</p>
          ))}
        {usage && !Object.keys(usage.money_used).length && <p>{t('projectOverviewNoMoney')}</p>}
        {!usage && <p>{t('projectOverviewUnknown')}</p>}
        <Meter value={moneyPercent} label={t('projectOverviewBudget')} />
      </div>
      {usage ? (
        <div className="space-y-1 text-xs text-muted-foreground">
          <p>{t(usage.covered ? 'projectOverviewCovered' : 'projectOverviewUncovered')}</p>
          <p>{t('projectOverviewHeldTokens', { value: number(usage.tokens_held) })}</p>
          <p>{t('projectOverviewUnknownTokens', { count: usage.tokens_unknown })}</p>
          {Object.entries(usage.money_held).map(([currency, amount]) => (
            <p key={currency}>{t('projectOverviewMoneyHeld', { amount, currency })}</p>
          ))}
          <p>{t('projectOverviewUnknownMoney', { count: usage.money_unknown })}</p>
          <p>
            {t('projectOverviewWindow', {
              start: date(usage.month_start),
              end: date(usage.month_end),
              zone: usage.time_zone,
            })}
          </p>
          <p>{t('projectOverviewAsOf', { value: date(usage.as_of) })}</p>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t('projectOverviewUsageUnavailable')}</p>
      )}
    </div>
  )
}
