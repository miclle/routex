import { useCallback, useSyncExternalStore } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { getMemberTeams, memberTeamsKey, validateMemberTeamsPage } from '@/api/member-teams'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tooltip } from '@/components/ui/tooltip'
import { QueryState } from '@/components/app/CatalogUI'
import { exactInteger, exactDecimal } from '@/views/home/monthly-account-values'
import type { Session } from '@/types/auth'
import type { Member } from '@/types/governance'
import type { MemberTeamPolicy } from '@/types/member-teams'

type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}
export default function MemberTeams(props: Props) {
  return <Teams key={`${props.actor}:${props.target}:${props.generation}`} {...props} />
}
function Teams({ actor, target, generation, ready, targetQueryKey }: Props) {
  const { t, i18n } = useTranslation('governance')
  const cache = useQueryClient()
  const keys = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((key: unknown[]) => {
          const s = cache.getQueryState(key)
          return `${s?.status}:${s?.fetchStatus}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
        })
        .join('|'),
    [cache, keys],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.parse(keys).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, keys],
  )
  const version = useSyncExternalStore(subscribe, snapshot, snapshot)
  function authority() {
    const session = cache.getQueryState<Session>(['auth', 'session'])
    const permission = cache.getQueryState<string[]>(['permissions', actor])
    const subject = cache.getQueryState<Member>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      snapshot() === version &&
      [session, permission, subject].every(
        (s) => s?.status === 'success' && s.fetchStatus === 'idle' && !s.error,
      ) &&
      session?.data?.user.id === actor &&
      !!session.data.csrf_token &&
      subject?.data?.id === target &&
      permission?.data?.includes('members.read') === true &&
      permission.data.includes('teams.read_all')
    )
  }
  const queryKey = memberTeamsKey(actor, target, generation, version)
  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => {
      if (!authority()) throw new Error('Member Teams authority unavailable')
      const page = await getMemberTeams(target, pageParam, signal)
      if (signal.aborted || !authority()) throw new Error('Member Teams authority changed')
      return page
    },
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
  })
  let valid = true
  try {
    query.data?.pages.forEach((p, i, pages) =>
      validateMemberTeamsPage(p, target, pages.slice(0, i)),
    )
  } catch {
    valid = false
  }
  const fresh = authority() && query.isSuccess && !query.isFetching && valid
  function current() {
    const s = cache.getQueryState(queryKey)
    return (
      fresh &&
      authority() &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.error &&
      s.data === query.data
    )
  }
  const language = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const integer = (v: string | null) =>
    v === null ? t('memberTeams.notSet') : exactInteger(v, language)
  const money = (v: string | null, c: string | null) =>
    v === null ? t('memberTeams.notSet') : `${exactDecimal(v, language)} ${c}`
  const amounts = (map: Record<string, string>, empty = t('memberTeams.noCharges')) =>
    Object.entries(map)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([c, value]) => `${exactDecimal(value, language)} ${c}`)
      .join(t('common.listSeparator')) || empty
  const policy = (p: MemberTeamPolicy) =>
    t('memberTeams.parentValues', {
      tokens: integer(p.tokens_month),
      money: money(p.money_month, p.currency),
      rpm: integer(p.rpm),
      tpm: integer(p.tpm),
      concurrency: integer(p.concurrency),
    })
  return (
    <section className="rounded-lg border" aria-label={t('memberTeams.title')}>
      <header className="flex items-center justify-between gap-4 border-b p-4">
        <h3 className="font-medium">{t('memberTeams.title')}</h3>
        {authority() && (
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => {
              if (authority()) void query.refetch()
            }}
          >
            {t('memberTeams.refresh')}
          </Button>
        )}
      </header>
      {!authority() ? (
        <p role="status" className="p-4 text-sm text-muted-foreground">
          {t('memberTeams.denied')}
        </p>
      ) : (
        <QueryState
          pending={query.isPending || query.isFetching}
          error={query.error || (!valid ? new Error(t('memberTeams.invalid')) : null)}
          retry={() => {
            if (authority()) void query.refetch()
          }}
          empty={fresh && !query.data?.pages.some((p) => p.items.length)}
        />
      )}
      {fresh && (
        <Table className="min-w-[1066px]" aria-label={t('memberTeams.table')}>
          <thead>
            <tr>
              <th className="w-[180px]">{t('memberTeams.team')}</th>
              <th className="w-[86px]">{t('common.status')}</th>
              <th className="w-[190px]">
                <span className="inline-flex items-center gap-1.5">
                  {t('memberTeams.tokens')}
                  <Tooltip label={t('memberTeams.tokensHelpLabel')} canOpen={current}>
                    {t('memberTeams.tokensHelp')}
                  </Tooltip>
                </span>
              </th>
              <th className="w-[220px]">
                <span className="inline-flex items-center gap-1.5">
                  {t('memberTeams.budget')}
                  <Tooltip label={t('memberTeams.budgetHelpLabel')} canOpen={current}>
                    {t('memberTeams.budgetHelp')}
                  </Tooltip>
                </span>
              </th>
              <th className="w-[300px]">{t('memberTeams.rates')}</th>
              <th className="w-[150px]">{t('members.joined')}</th>
            </tr>
          </thead>
          <tbody>
            {query.data!.pages.flatMap((page) =>
              page.items.map((row) => {
                const l = row.limits,
                  u = l.usage,
                  holds = l.active_reservations
                const knownTokens = !!u && (!u.covered || u.tokens_unknown !== '0')
                const knownMoney = !!u && (!u.covered || u.money_unknown !== '0')
                return (
                  <tr key={row.id}>
                    <td>
                      <div className="flex items-center gap-2">
                        <Link
                          className="font-medium hover:underline"
                          to={`/admin/teams/${encodeURIComponent(row.id)}`}
                          onClick={(e) => {
                            if (!current()) e.preventDefault()
                          }}
                        >
                          {row.name}
                        </Link>
                        <Tooltip
                          label={t('memberTeams.contextLabel', { name: row.name })}
                          canOpen={current}
                        >
                          <dl className="space-y-2">
                            <div>
                              <dt>{t('memberTeams.parent')}</dt>
                              <dd>{policy(l.parent_stored)}</dd>
                            </div>
                            <div>
                              <dt>{t('memberTeams.policy')}</dt>
                              <dd>
                                {l.policy_recorded ? l.policy_etag : t('memberTeams.noPolicy')}
                              </dd>
                            </div>
                            <div>
                              <dt>{t('memberTeams.application')}</dt>
                              <dd>
                                {t(
                                  l.runtime_applied
                                    ? 'memberTeams.applied'
                                    : 'memberTeams.unconfirmed',
                                )}
                              </dd>
                            </div>
                            <div>
                              <dt>{t('memberTeams.platformCurrency')}</dt>
                              <dd>{page.platform_currency}</dd>
                            </div>
                            <div>
                              <dt>{t('memberTeams.observed')}</dt>
                              <dd>{new Date(page.observed_at).toLocaleString(language)}</dd>
                            </div>
                            {u && (
                              <>
                                <div>
                                  <dt>{t('memberTeams.journal')}</dt>
                                  <dd>{new Date(u.as_of).toLocaleString(language)}</dd>
                                </div>
                                <div>
                                  <dt>{t('memberTeams.window')}</dt>
                                  <dd>
                                    {new Date(u.month_start).toLocaleString(language)} –{' '}
                                    {new Date(u.month_end).toLocaleString(language)} ({u.time_zone})
                                  </dd>
                                </div>
                                <div>
                                  <dt>{t('memberTeams.coverage')}</dt>
                                  <dd>
                                    {t(u.covered ? 'memberTeams.covered' : 'memberTeams.partial')}
                                  </dd>
                                </div>
                              </>
                            )}
                          </dl>
                        </Tooltip>
                      </div>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {t(`memberTeams.role_${row.membership_role}`)} ·{' '}
                        {t(`memberTeams.membership_${row.membership_status}`)}
                      </p>
                    </td>
                    <td>
                      <Badge variant="outline">{t(`memberTeams.status_${row.status}`)}</Badge>
                    </td>
                    <td>
                      <p>
                        {u ? exactInteger(u.tokens_used, language) : t('memberTeams.unknown')} /{' '}
                        {integer(l.stored.tokens_month)}
                      </p>
                      {!l.policy_recorded && (
                        <p className="text-xs text-muted-foreground">{t('memberTeams.noPolicy')}</p>
                      )}
                      {knownTokens && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberTeams.knownSubtotal')}
                        </p>
                      )}
                      {u && (
                        <>
                          <p className="text-xs text-muted-foreground">
                            {t('memberTeams.unknownTokens', {
                              count: exactInteger(u.tokens_unknown, language),
                            })}
                          </p>
                          <p className="text-xs text-muted-foreground">
                            {t('memberTeams.monthHeld', {
                              value: exactInteger(u.tokens_held, language),
                            })}
                          </p>
                        </>
                      )}
                      <p className="text-xs text-muted-foreground">
                        {t('memberTeams.liveHeld', {
                          value: holds
                            ? exactInteger(holds.tokens_held, language)
                            : t('memberTeams.unknown'),
                        })}
                      </p>
                    </td>
                    <td>
                      <p>
                        {u ? amounts(u.money_used) : t('memberTeams.unknown')} /{' '}
                        {money(l.stored.money_month, l.stored.currency)}
                      </p>
                      {knownMoney && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberTeams.knownSubtotal')}
                        </p>
                      )}
                      {u && (
                        <>
                          <p className="text-xs text-muted-foreground">
                            {t('memberTeams.unknownMoney', {
                              count: exactInteger(u.money_unknown, language),
                            })}
                          </p>
                          <p className="text-xs text-muted-foreground">
                            {t('memberTeams.monthHeld', {
                              value: amounts(u.money_held, t('memberTeams.noReservations')),
                            })}
                          </p>
                        </>
                      )}
                      <p className="text-xs text-muted-foreground">
                        {t('memberTeams.liveHeld', {
                          value: holds
                            ? amounts(holds.money_held, t('memberTeams.noReservations'))
                            : t('memberTeams.unknown'),
                        })}
                      </p>
                    </td>
                    <td>
                      <p>
                        {t('memberTeams.rateValues', {
                          rpm: integer(l.stored.rpm),
                          tpm: integer(l.stored.tpm),
                          concurrency: integer(l.stored.concurrency),
                        })}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {t(`memberTeams.usage_${l.usage_status}`)} ·{' '}
                        {t(l.runtime_applied ? 'memberTeams.applied' : 'memberTeams.unconfirmed')}
                      </p>
                    </td>
                    <td>
                      {row.joined_at === null
                        ? t('memberTeams.unknown')
                        : new Date(row.joined_at).toLocaleString(language)}
                    </td>
                  </tr>
                )
              }),
            )}
          </tbody>
        </Table>
      )}
      {fresh && query.hasNextPage && (
        <div className="p-4 text-center">
          <Button
            variant="outline"
            onClick={() => {
              if (current()) void query.fetchNextPage()
            }}
          >
            {t('memberTeams.loadMore')}
          </Button>
        </div>
      )}
    </section>
  )
}
