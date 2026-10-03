import { useLayoutEffect, useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { listTeamModelRequestTeams } from '@/api/team-model-requests'
import { QueryState, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { TeamModelRequestTeam } from '@/types/team-model-requests'

export default function TeamRequestPicker({
  actor,
  selected,
  onChange,
  disabled,
  visible,
  onFresh,
}: {
  actor: string
  selected: TeamModelRequestTeam | null
  onChange: (team: TeamModelRequestTeam) => void
  disabled: boolean
  visible: boolean
  onFresh: (value: boolean) => void
}) {
  const { t } = useTranslation('teamModelRequests')
  const cache = useQueryClient()
  const [search, setSearch] = useState('')
  const query = useInfiniteQuery({
    queryKey: ['team-model-request-teams', actor, search],
    queryFn: ({ pageParam, signal }) => listTeamModelRequestTeams(search, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
    enabled: !!actor && visible,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const refresh = () => {
    if (selected)
      void cache.invalidateQueries({ queryKey: ['team-model-candidate', actor, selected.id] })
    void query.refetch()
  }
  const fresh = visible && query.isSuccess && !query.isFetching
  useLayoutEffect(() => onFresh(fresh), [fresh, onFresh])
  const items = fresh
    ? [
        ...new Map(
          query.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  const options = !fresh
    ? []
    : selected && !items.some((item) => item.id === selected.id)
      ? [selected, ...items]
      : items
  const changed =
    selected &&
    items.some((item) => item.id === selected.id && item.membership_id !== selected.membership_id)
  return (
    <div className="space-y-3">
      <FormField label={t('team')}>
        <select
          aria-label={t('team')}
          value={selected?.id ?? ''}
          disabled={disabled || !fresh}
          onChange={(event) => {
            const item = items.find((item) => item.id === event.target.value)
            if (item) onChange(item)
          }}
          className="h-10 w-full rounded-md border bg-background px-3 text-sm"
        >
          <option value="">{t('selectTeam')}</option>
          {options.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
        <p className="text-xs text-muted-foreground">{t('selectTeamHelp')}</p>
      </FormField>
      {visible && (
        <>
          <Input
            aria-label={t('selectTeam')}
            type="search"
            value={search}
            onValueChange={setSearch}
            disabled={disabled}
          />
          <QueryState pending={query.isFetching} error={query.error} retry={refresh} />
          {fresh && !items.length && !selected && <p role="status">{t('noTeams')}</p>}
          {changed && <p role="alert">{t('conflict')}</p>}
          <div className="flex gap-2">
            <Button variant="outline" disabled={disabled || query.isFetching} onClick={refresh}>
              {t('teamRefresh')}
            </Button>
            {query.hasNextPage && (
              <Button
                variant="outline"
                disabled={disabled || query.isFetching}
                onClick={() => void query.fetchNextPage()}
              >
                {t('loadMore')}
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  )
}
