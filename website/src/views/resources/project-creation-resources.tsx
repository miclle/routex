import { useState } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getProjectCreationModels } from '@/api/resources'
import type { ProjectCreationContext } from '@/types/resources'
import { creationLimitFields, type CreationResourceDraft } from './project-creation-values'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

export default function ProjectCreationResources({
  actor,
  context,
  request,
  draft,
  onChange,
  disabled,
  visible,
}: {
  actor: string
  context: ProjectCreationContext
  request: boolean
  draft: CreationResourceDraft
  onChange: (draft: CreationResourceDraft) => void
  disabled: boolean
  visible: boolean
}) {
  const { t } = useTranslation('resources')
  const [q, setQ] = useState('')
  const modelsAllowed = request || context.can_set_models
  const limitsAllowed = request || context.can_set_limits
  const query = useInfiniteQuery({
    queryKey: ['project-creation-models', actor, context.review_etag, request, q],
    queryFn: ({ pageParam, signal }) => getProjectCreationModels(q, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page, pages) =>
      page.next_cursor && !pages.slice(0, -1).some((old) => old.next_cursor === page.next_cursor)
        ? page.next_cursor
        : undefined,
    enabled: visible && modelsAllowed,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = visible && query.isSuccess && !query.isFetching
  const items = fresh
    ? [
        ...new Map(
          query.data.pages.flatMap((page) => page.items).map((item) => [item.id, item]),
        ).values(),
      ]
    : []
  if (!visible) return null
  return (
    <section
      className="space-y-4"
      aria-label={t(request ? 'creationRequestedResources' : 'creationInitialResources')}
    >
      <h3 className="font-medium">
        {t(request ? 'creationRequestedResources' : 'creationInitialResources')}
      </h3>
      <p className="text-sm text-muted-foreground">
        {t(request ? 'creationRequestHelp' : 'creationInitialHelp')}
      </p>
      {modelsAllowed && (
        <fieldset className="space-y-3" disabled={disabled}>
          <legend className="text-sm font-medium">
            {t(request ? 'creationRequestModels' : 'creationModels')}
          </legend>
          <Input
            aria-label={t('creationModelSearch')}
            type="search"
            value={q}
            onValueChange={setQ}
          />
          <QueryState
            pending={query.isFetching}
            error={query.error}
            retry={() => void query.refetch()}
          />
          {fresh && (
            <div className="max-h-48 space-y-2 overflow-y-auto rounded-md border p-3">
              {items.map((item) => (
                <label key={item.id} className="flex items-center gap-3 text-sm">
                  <input
                    type="checkbox"
                    checked={draft.models.some((model) => model.id === item.id)}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        models: event.target.checked
                          ? [...draft.models.filter((model) => model.id !== item.id), item]
                          : draft.models.filter((model) => model.id !== item.id),
                      })
                    }
                  />
                  <span>{item.name}</span>
                </label>
              ))}
              {!items.length && <p>{t('emptyCandidates')}</p>}
            </div>
          )}
          {fresh && (
            <div className="flex flex-wrap gap-2" aria-label={t('creationSelectedModels')}>
              {draft.models.map((item) => (
                <Button
                  key={item.id}
                  size="sm"
                  variant="secondary"
                  disabled={disabled}
                  aria-label={t('creationRemoveModel', { name: item.name })}
                  onClick={() =>
                    onChange({
                      ...draft,
                      models: draft.models.filter((model) => model.id !== item.id),
                    })
                  }
                >
                  {item.name} ×
                </Button>
              ))}
            </div>
          )}
          {query.hasNextPage && (
            <Button
              variant="outline"
              disabled={disabled || query.isFetching}
              onClick={() => void query.fetchNextPage()}
            >
              {t('creationLoadMoreModels')}
            </Button>
          )}
        </fieldset>
      )}
      {limitsAllowed && (
        <div className="flex flex-wrap gap-4">
          {creationLimitFields.map((field) => (
            <FormField
              key={field}
              label={t(field === 'money_month' ? 'creationMoney' : `creation_${field}`, {
                currency: context.platform_currency,
              })}
            >
              <Input
                aria-label={t(field === 'money_month' ? 'creationMoney' : `creation_${field}`, {
                  currency: context.platform_currency,
                })}
                type="text"
                inputMode={field === 'money_month' ? 'decimal' : 'numeric'}
                value={draft[field]}
                onValueChange={(value) => onChange({ ...draft, [field]: value })}
                disabled={disabled}
                placeholder={t('creationOmit')}
                className={field === 'tokens_month' || field === 'money_month' ? 'w-48' : 'w-36'}
              />
            </FormField>
          ))}
        </div>
      )}
      <FormField label={t('creationResourceReason')}>
        <Textarea
          aria-label={t('creationResourceReason')}
          value={draft.reason}
          onChange={(event) => onChange({ ...draft, reason: event.target.value })}
          disabled={disabled}
          rows={3}
        />
        <p className="text-xs text-muted-foreground">{t('creationReasonHelp')}</p>
      </FormField>
    </section>
  )
}
