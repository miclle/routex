import { useCallback, useId, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Autocomplete } from '@/components/ui/autocomplete'
import { getModelPublicNames, validPublicNameQuery } from '@/api/model-public-names'
import { searchPublicModelNames } from '@/lib/public-model-references'
import type { ModelPublicNames } from '@/types/model-public-names'

function usePublicNameSnapshot(key: readonly unknown[]) {
  const cache = useQueryClient(),
    serialized = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === serialized) notify()
      }),
    [cache, serialized],
  )
  const snapshot = useCallback(() => {
    const state = cache.getQueryState(JSON.parse(serialized))
    return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}`
  }, [cache, serialized])
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}

export function PublicModelName({
  label,
  value,
  excludedNames,
  disabled,
  onValueChange,
  actor,
  connectionId,
  authority,
  fresh,
}: {
  label: string
  value: string
  excludedNames: readonly string[]
  disabled: boolean
  onValueChange: (value: string) => void
  actor: string
  connectionId: string
  authority: string
  fresh: () => boolean
}) {
  const { t } = useTranslation('modelCreation'),
    cache = useQueryClient(),
    descriptionId = useId()
  const key = ['model-creation-public-names', actor, connectionId, authority, value] as const
  const enabled =
    !disabled && fresh() && validPublicNameQuery(value) && searchPublicModelNames(value).length > 0
  const names = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getModelPublicNames(connectionId, value, signal),
    enabled,
    retry: false,
    refetchOnMount: 'always',
    gcTime: 0,
  })
  usePublicNameSnapshot(key)
  const current = () => {
    const state = cache.getQueryState<ModelPublicNames>(key)
    return enabled &&
      fresh() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      state.data?.connection_id === connectionId &&
      state.data.query === value
      ? state.data
      : null
  }
  const suggestions =
    current()
      ?.items.filter((item) => item.available && !excludedNames.includes(item.name))
      .map((item) => item.name) ?? []
  return (
    <div className="min-w-0 flex-1 space-y-1">
      <Autocomplete
        label={label}
        descriptionId={descriptionId}
        value={value}
        suggestions={suggestions}
        disabled={disabled}
        canSelectSuggestion={(name) =>
          current()?.items.some(
            (item) => item.name === name && item.available && !excludedNames.includes(name),
          ) === true
        }
        onValueChange={(next) => {
          if (!disabled && fresh()) onValueChange(next)
        }}
      />
      <p id={descriptionId} className="text-xs text-muted-foreground">
        {t('publicNameSuggestions')}
      </p>
      {enabled && names.isFetching && (
        <p role="status" className="text-xs text-muted-foreground">
          {t('publicNameChecking')}
        </p>
      )}
      {enabled && names.isError && (
        <p role="status" className="text-xs text-muted-foreground">
          {t('publicNameUnknown')}
        </p>
      )}
    </div>
  )
}
