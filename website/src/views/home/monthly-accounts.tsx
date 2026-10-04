import { useId, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { ArrowRight, RefreshCw, Users, User } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getOverviewAccounts, overviewAccountsKey } from '@/api/overview'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import type { MonthlyAccount } from '@/types/overview'
import { exactDecimal, exactInteger, tokenPercentage } from './monthly-account-values'

function AccountFacts({
  account,
  kind,
}: {
  account: MonthlyAccount
  kind: 'tokens' | 'money' | 'usage'
}) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const usage = account.usage
  const active = account.active_reservations
  const money = (values: Record<string, string>) =>
    Object.entries(values)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([code, amount]) => `${exactDecimal(amount, language)} ${code}`)
      .join('; ')
  const status = account.usage_status === 'inactive' ? t('inactive') : t('unavailable')
  if (kind === 'tokens')
    return (
      <div className="space-y-1">
        <p className="whitespace-nowrap tabular-nums">
          {usage ? exactInteger(usage.tokens_used, language) : t('unknown')} /{' '}
          {account.tokens_month === null
            ? t('notSet')
            : exactInteger(account.tokens_month, language)}
        </p>
        {usage ? (
          <>
            <p className="text-xs text-muted-foreground">
              {t(usage.covered ? 'covered' : 'uncovered')}
              {!usage.covered ? ` · ${t('knownSubtotal')}` : ''}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('heldTokens', { amount: exactInteger(usage.tokens_held, language) })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('activeTokens', {
                amount: active ? exactInteger(active.tokens_held, language) : t('unknown'),
              })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('unknownTokens', { count: exactInteger(usage.tokens_unknown, language) })}
            </p>
          </>
        ) : (
          <p className="text-xs text-muted-foreground">{status}</p>
        )}
      </div>
    )
  if (kind === 'money')
    return (
      <div className="space-y-1">
        <p className="tabular-nums">
          {usage
            ? Object.keys(usage.money_used).length
              ? money(usage.money_used)
              : t('noRecordedMoney')
            : t('unknown')}{' '}
          /{' '}
          {account.money_month === null
            ? t('notSet')
            : `${exactDecimal(account.money_month, language)} ${account.currency}`}
        </p>
        {usage ? (
          <>
            <p className="text-xs text-muted-foreground">
              {t('heldMoney', {
                amount: Object.keys(usage.money_held).length
                  ? money(usage.money_held)
                  : t('noRecordedMoney'),
              })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('activeMoney', {
                amount: active
                  ? Object.keys(active.money_held).length
                    ? money(active.money_held)
                    : t('noActiveMoney')
                  : t('unknown'),
              })}
            </p>
            <p className="text-xs text-muted-foreground">
              {t('unknownMoney', { count: exactInteger(usage.money_unknown, language) })}
            </p>
            {!usage.covered && (
              <p className="text-xs text-muted-foreground">{t('knownSubtotal')}</p>
            )}
          </>
        ) : (
          <p className="text-xs text-muted-foreground">{status}</p>
        )}
      </div>
    )
  const percent = tokenPercentage(account)
  const label = percent
    ? t('percent', {
        value:
          exactInteger(percent.whole.toString(), language) +
          (percent.fraction === 0n ? '' : '.' + percent.fraction.toString().padStart(2, '0')),
      })
    : t('percentUnavailable')
  const date = (value: string) =>
    new Intl.DateTimeFormat(language, {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone: usage?.time_zone,
    }).format(new Date(value))
  return (
    <div className="min-w-40 space-y-1">
      {percent ? (
        <div className="flex items-center gap-2">
          <div
            role="progressbar"
            aria-label={t('tokens')}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent.bar}
            aria-valuetext={label}
            className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted"
          >
            <div className="h-full bg-primary" style={{ width: `${percent.bar}%` }} />
          </div>
          <span className="whitespace-nowrap tabular-nums">{label}</span>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{label}</p>
      )}
      {usage && (
        <>
          <p className="text-xs text-muted-foreground">
            {t('month', {
              start: date(usage.month_start),
              end: date(usage.month_end),
              zone: usage.time_zone,
            })}
          </p>
          <p className="text-xs text-muted-foreground">{t('asOf', { date: date(usage.as_of) })}</p>
        </>
      )}
      {!account.runtime_applied && (
        <p className="text-xs text-muted-foreground">{t('notApplied')}</p>
      )}
    </div>
  )
}

export default function MonthlyAccounts({ actorId }: { actorId: string }) {
  const { t, i18n } = useTranslation('overview')
  // This component mounts only under a fresh Session and has no Session observer.
  // A new mount gets a new generation; obsolete actor/session reads cannot reuse it.
  const generation = useId()
  const [cursors, setCursors] = useState<(string | null)[]>([null])
  const cursor = cursors[cursors.length - 1]
  const query = useQuery({
    queryKey: overviewAccountsKey(actorId, generation, cursor),
    queryFn: ({ signal }) => getOverviewAccounts(actorId, cursor, signal),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const data = !query.isFetching && !query.isError && query.isSuccess ? query.data : null
  const language = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  return (
    <section aria-label={t('title')} className="rounded-lg border">
      <header className="flex flex-wrap items-center justify-between gap-3 p-4">
        <h2 className="font-semibold">{t('title')}</h2>
        <div className="flex items-center gap-3">
          <Link to="/usage" className="inline-flex items-center gap-1 text-sm text-primary">
            {t('usageLink')}
            <ArrowRight className="size-4" aria-hidden />
          </Link>
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <RefreshCw className="size-4" aria-hidden />
            {t('refresh')}
          </Button>
        </div>
      </header>
      <p className="px-4 pb-4 text-sm text-muted-foreground">{t('description')}</p>
      {query.isFetching || query.isPending ? (
        <p role="status" className="p-4 text-sm">
          {t('loading')}
        </p>
      ) : query.isError ? (
        <p role="alert" className="p-4 text-sm">
          {t('error')}
        </p>
      ) : null}
      {data && (
        <>
          <Table aria-label={t('title')} className="min-w-[900px]">
            <thead>
              <tr>
                <th>{t('account')}</th>
                <th>{t('tokens')}</th>
                <th>{t('money')}</th>
                <th>{t('usage')}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>
                  <div className="flex items-center gap-2">
                    <User className="size-4 shrink-0" aria-hidden />
                    <span>{t('personal')}</span>
                  </div>
                  <Link className="mt-1 inline-block text-xs text-primary" to="/usage">
                    {t('accountUsage', { name: t('personal') })}
                  </Link>
                </td>
                {(['tokens', 'money', 'usage'] as const).map((kind) => (
                  <td key={kind}>
                    <AccountFacts account={data.personal} kind={kind} />
                  </td>
                ))}
              </tr>
              {data.teams.map((team) => (
                <tr key={team.id}>
                  <td>
                    <div className="flex items-center gap-2">
                      <Users className="size-4 shrink-0" aria-hidden />
                      <span>{team.name}</span>
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">{t('team')}</p>
                    <Link
                      className="mt-1 inline-block text-xs text-primary"
                      to={`/usage?team=${encodeURIComponent(team.id)}`}
                    >
                      {t('accountUsage', { name: team.name })}
                    </Link>
                  </td>
                  {(['tokens', 'money', 'usage'] as const).map((kind) => (
                    <td key={kind}>
                      <div className="space-y-4">
                        {(
                          [
                            ['aggregate', team.aggregate],
                            ['member', team.member],
                          ] as const
                        ).map(([scope, account]) => (
                          <div key={scope} className="space-y-1">
                            <p className="text-xs font-medium">{t(scope)}</p>
                            <AccountFacts account={account} kind={kind} />
                          </div>
                        ))}
                      </div>
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </Table>
          {!data.teams.length && (
            <p className="p-4 text-sm text-muted-foreground">{t('noTeams')}</p>
          )}
          <footer className="flex flex-wrap items-center justify-between gap-3 p-4">
            <p className="text-xs text-muted-foreground">
              {t('observed', {
                date: new Intl.DateTimeFormat(language, {
                  dateStyle: 'medium',
                  timeStyle: 'short',
                }).format(new Date(data.observed_at)),
              })}
            </p>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={cursors.length === 1}
                onClick={() => setCursors((values) => values.slice(0, -1))}
              >
                {t('previous')}
              </Button>
              <span className="text-xs text-muted-foreground">
                {t('page', { number: cursors.length })}
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={!data.next_cursor}
                onClick={() => {
                  if (data.next_cursor) setCursors((values) => [...values, data.next_cursor])
                }}
              >
                {t('next')}
              </Button>
            </div>
          </footer>
        </>
      )}
    </section>
  )
}
