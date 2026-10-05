import { useCallback, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getMemberEffectiveModels, memberEffectiveModelsKey } from '@/api/member-effective-models'
import { QueryState } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Tooltip } from '@/components/ui/tooltip'
import type { Session } from '@/types/auth'
import type { Member } from '@/types/governance'
import type { MemberModelPriceCell } from '@/types/member-models'

export default function MemberEffectiveModels({
  actor,
  target,
  generation,
  ready,
  targetQueryKey,
}: {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}) {
  const { t, i18n } = useTranslation('governance')
  const cache = useQueryClient()
  const keys = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((key: unknown[]) => {
          const state = cache.getQueryState(key)
          return `${state?.status}:${state?.fetchStatus}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
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
        (state) => state?.status === 'success' && state.fetchStatus === 'idle' && !state.error,
      ) &&
      session?.data?.user.id === actor &&
      subject?.data?.id === target &&
      permission?.data?.includes('members.read') === true
    )
  }
  const grants = cache.getQueryData<string[]>(['permissions', actor])
  const flags = {
    teams: grants?.includes('teams.read_all') === true,
    providers: grants?.includes('providers.read') === true,
    prices: grants?.includes('prices.read') === true,
  }
  const ownKey = JSON.stringify([
    'admin',
    'member-effective-models',
    actor,
    target,
    generation,
    version,
  ])
  const queryKey = memberEffectiveModelsKey(actor, target, generation, version)
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error(t('memberEffectiveModels.denied'))
      const data = await getMemberEffectiveModels(target, flags, signal)
      if (signal.aborted || !authority()) throw new Error(t('memberEffectiveModels.denied'))
      return data
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const readSnapshot = useCallback(() => {
    const state = cache.getQueryState(JSON.parse(ownKey))
    return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
  }, [cache, ownKey])
  const readSubscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === ownKey) notify()
      }),
    [cache, ownKey],
  )
  const readVersion = useSyncExternalStore(readSubscribe, readSnapshot, readSnapshot)
  const ownState = cache.getQueryState(queryKey)
  const fresh =
    authority() &&
    query.isSuccess &&
    !query.isFetching &&
    readSnapshot() === readVersion &&
    ownState?.status === 'success' &&
    ownState.fetchStatus === 'idle' &&
    !ownState.isInvalidated &&
    !ownState.error &&
    ownState.data === query.data
  function current() {
    const state = cache.getQueryState(queryKey)
    return (
      fresh &&
      authority() &&
      readSnapshot() === readVersion &&
      !state?.isInvalidated &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      state.data === query.data
    )
  }
  const price = (cell: MemberModelPriceCell) =>
    !flags.prices ? (
      t('memberModels.unknown')
    ) : cell.rate ? (
      <>
        <span className="block break-all whitespace-normal">
          {t('memberModels.amount', { amount: cell.rate.amount, currency: cell.rate.currency })}
        </span>
        {cell.state === 'disabled' && <Badge variant="outline">{t('memberModels.disabled')}</Badge>}
      </>
    ) : (
      t(`memberModels.price_${cell.state}`)
    )
  return (
    <section className="rounded-lg border" aria-label={t('memberEffectiveModels.title')}>
      <h3 className="border-b p-4 font-medium">{t('memberEffectiveModels.title')}</h3>
      {!authority() ? (
        <p role="status" className="p-4 text-sm text-muted-foreground">
          {t('memberEffectiveModels.denied')}
        </p>
      ) : (
        <QueryState
          pending={query.isPending || query.isFetching}
          error={query.error}
          retry={() => {
            if (authority()) void query.refetch()
          }}
          empty={fresh && query.data!.items.length === 0}
        />
      )}
      {fresh && (
        <>
          <Table
            className="min-w-[1890px] table-fixed"
            aria-label={t('memberEffectiveModels.table')}
          >
            <thead>
              <tr>
                {(
                  [
                    'model',
                    'source',
                    'type',
                    'provider',
                    'protocol',
                    'availability',
                    'input',
                    'output',
                    'created',
                    'updated',
                  ] as const
                ).map((field, index) => (
                  <th
                    key={field}
                    style={{ width: [220, 260, 140, 240, 200, 110, 180, 180, 180, 180][index] }}
                  >
                    {field === 'source'
                      ? t('memberEffectiveModels.source')
                      : t(`memberModels.${field}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {query.data!.items.map((row) => (
                <tr key={row.id}>
                  <td className="break-all">
                    {row.name}
                    <p className="font-mono text-xs text-muted-foreground">{row.id}</p>
                  </td>
                  <td>
                    <div className="flex flex-wrap items-center gap-1.5">
                      {row.sources.map((source) => {
                        const label =
                          source.kind === 'personal'
                            ? t('memberEffectiveModels.personal')
                            : source.team_name!
                        return (
                          <span
                            key={source.kind === 'personal' ? 'personal' : source.team_id!}
                            className="inline-flex items-center gap-1"
                          >
                            <Badge variant="outline">{label}</Badge>
                            <Tooltip
                              label={t('memberEffectiveModels.sourceDetails', { source: label })}
                              canOpen={current}
                            >
                              <p>{label}</p>
                              <p>{t(`memberModels.availability_${source.availability}`)}</p>
                              <p>
                                {source.protocols.join(t('common.listSeparator')) ||
                                  t('memberModels.unknown')}
                              </p>
                            </Tooltip>
                          </span>
                        )
                      })}
                    </div>
                  </td>
                  <td>{t('memberModels.unknown')}</td>
                  <td>
                    {!flags.providers || row.providers === null
                      ? t('memberModels.unknown')
                      : row.providers.join(t('common.listSeparator')) || t('memberModels.none')}
                  </td>
                  <td>
                    {row.protocols.join(t('common.listSeparator')) || t('memberModels.unknown')}
                  </td>
                  <td>
                    <Badge variant="outline">
                      {t(`memberModels.availability_${row.availability}`)}
                    </Badge>
                  </td>
                  <td>{price(row.input_price)}</td>
                  <td>{price(row.output_price)}</td>
                  <td>
                    {new Date(row.created_at).toLocaleString(
                      i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
                    )}
                  </td>
                  <td>{t('memberModels.unknown')}</td>
                </tr>
              ))}
            </tbody>
          </Table>
          <div className="space-y-1 p-4 text-xs text-muted-foreground">
            <p>{t('memberEffectiveModels.help')}</p>
            {query.data!.union_completeness === 'unknown' && (
              <p role="status">{t('memberEffectiveModels.incomplete')}</p>
            )}
            {query.data!.subject_status !== 'active' && (
              <p>{t('memberEffectiveModels.inactive')}</p>
            )}
          </div>
        </>
      )}
    </section>
  )
}
