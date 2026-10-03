import { useLayoutEffect } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getOwnActiveTeams } from '@/api/playground-team'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'

export default function TeamPicker({
  actor,
  value,
  onChange,
  onConfirmed,
}: {
  actor: string
  value: string
  onChange: (value: string) => void
  onConfirmed: (confirmed: boolean) => void
}) {
  const { t } = useTranslation('playground')
  const query = useInfiniteQuery({
    queryKey: ['playground', 'teams', actor],
    enabled: !!actor,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnMount: 'always',
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => getOwnActiveTeams(pageParam, signal),
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
  })
  const available = query.isSuccess && !query.isFetching && !query.isError
  const items = available
    ? [
        ...new Map(
          query.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  const confirmed = !!actor && items.some((item) => item.id === value)
  useLayoutEffect(() => {
    onConfirmed(confirmed)
  }, [confirmed, onConfirmed, query.dataUpdatedAt])
  return (
    <div className="space-y-2">
      <FormField label={t('team')}>
        <select
          name="team"
          aria-label={t('team')}
          value={confirmed ? value : ''}
          onChange={(event) => onChange(event.target.value)}
          className="h-11 w-full rounded-md border bg-background px-3 text-sm"
          disabled={!available}
        >
          <option value="">{t('selectTeam')}</option>
          {items.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
            </option>
          ))}
        </select>
      </FormField>
      {query.isFetching && (
        <p role="status" className="text-xs">
          {t('teamLoading')}
        </p>
      )}
      {query.isError && (
        <p role="alert" className="text-xs">
          {t('teamUnavailable')}
        </p>
      )}
      {available && !items.length && (
        <p role="status" className="text-xs">
          {t('teamNone')}
        </p>
      )}
      {available && value && !confirmed && !query.hasNextPage && (
        <p role="status" className="text-xs">
          {t('teamUnavailable')}
        </p>
      )}
      <div className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={query.isFetching}
          onClick={() => {
            onConfirmed(false)
            void query.refetch()
          }}
        >
          {t('teamRefresh')}
        </Button>
        {query.hasNextPage && (
          <Button
            size="sm"
            variant="ghost"
            disabled={query.isFetching}
            onClick={() => {
              onConfirmed(false)
              void query.fetchNextPage()
            }}
          >
            {t('teamMore')}
          </Button>
        )}
      </div>
    </div>
  )
}
