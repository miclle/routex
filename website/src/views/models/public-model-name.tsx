import { useCallback, useId, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { protocolLabels } from '@/lib/protocols'
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
  const { t, i18n } = useTranslation('modelCreation'),
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
  const selected = current()?.items.find(
    (item) => item.name === value && item.available && !excludedNames.includes(item.name),
  )
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
      {selected && (
        <div
          className="space-y-1 text-xs text-muted-foreground"
          role="note"
          aria-label={t('publicNameReference')}
        >
          <p>{t('publicNameAdvisory')}</p>
          {selected.reference.protocols.length > 0 && (
            <p>
              {t('publicNameProtocols', {
                protocols: protocolLabels([...selected.reference.protocols]),
              })}
            </p>
          )}
          {selected.reference.capabilities.length > 0 && (
            <p>
              {t('publicNameCapabilities', {
                capabilities: selected.reference.capabilities
                  .map((capability) =>
                    t(capability === 'image_input' ? 'publicNameImageInput' : 'publicNamePDFInput'),
                  )
                  .join(', '),
              })}
            </p>
          )}
          <p>
            {t('publicNameReviewed', {
              date: new Intl.DateTimeFormat(i18n.language === 'zh' ? 'zh-CN' : 'en', {
                dateStyle: 'medium',
                timeZone: 'UTC',
              }).format(new Date(selected.reference.reviewed_at)),
            })}{' '}
            <a
              href={selected.reference.source_url}
              target="_blank"
              rel="noopener noreferrer"
              className="underline"
            >
              {t('publicNameSource')}
            </a>
          </p>
        </div>
      )}
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
