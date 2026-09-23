import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getResourceCandidates } from '@/api/resources'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { QueryState, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { ResourceSection } from './shared'
import type { ResourceKind, ResourceRecord } from '@/types/resources'

export function ResourceModels({
  resource,
  kind,
  canEdit,
}: {
  resource: ResourceRecord
  kind: ResourceKind
  canEdit: boolean
}) {
  const { t } = useTranslation('resources')
  const session = useSession()
  const cache = useQueryClient()
  const [draft, setDraft] = useState<string[] | null>(null)
  const [search, setSearch] = useState('')
  const [saved, setSaved] = useState(false)
  const selected = draft ?? resource.model_ids
  const candidates = useQuery({
    queryKey: ['resource-model-candidates', kind, search],
    queryFn: ({ signal }) =>
      getResourceCandidates(`/admin/resource-model-candidates?kind=${kind}`, search, signal),
    enabled: canEdit,
  })
  const update = useMutation({
    mutationFn: () =>
      writeCatalog<ResourceRecord>(
        'put',
        `${kind === 'teams' ? '/admin/teams' : '/projects'}/${resource.id}/models`,
        { model_ids: selected },
        session.data!.csrf_token,
      ),
    onSuccess: () => {
      setDraft(null)
      setSaved(true)
      void cache.invalidateQueries({ queryKey: ['resources'] })
    },
  })
  const names = new Map(candidates.data?.map((item) => [item.id, item.name]))
  function change(ids: string[]) {
    setSaved(false)
    setDraft(ids)
    update.reset()
  }
  return (
    <div className="space-y-4" role="group" aria-label={t('modelScope', { name: resource.name })}>
      <p className="text-sm text-muted-foreground">{t('scopeHelp')}</p>
      <ResourceSection title={t('allowedModels')} padded={false}>
        <Table aria-label={t('allowedModels')}>
          <thead>
            <tr>
              <th>{t('modelName')}</th>
              <th>{t('modelId')}</th>
              {canEdit && <th>{t('actions')}</th>}
            </tr>
          </thead>
          <tbody>
            {selected.map((id) => (
              <tr key={id}>
                <td>
                  {names.get(id) ?? (
                    <span className="text-muted-foreground">{t('unknownModel')}</span>
                  )}
                </td>
                <td className="font-mono">{id}</td>
                {canEdit && (
                  <td>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={update.isPending}
                      aria-label={t('removeModel', { name: names.get(id) ?? id })}
                      onClick={() => change(selected.filter((item) => item !== id))}
                    >
                      {t('remove')}
                    </Button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
        {selected.length === 0 && (
          <p className="p-4 text-sm text-muted-foreground">{t('noModels')}</p>
        )}
      </ResourceSection>
      {canEdit && (
        <>
          <Input
            aria-label={t('searchModels')}
            placeholder={t('searchModels')}
            value={search}
            onValueChange={setSearch}
          />
          <QueryState
            pending={candidates.isPending}
            error={candidates.error}
            retry={() => void candidates.refetch()}
          />
          <ResourceSection title={t('availableModels')} padded={false}>
            <Table aria-label={t('availableModels')}>
              <thead>
                <tr>
                  <th>{t('modelName')}</th>
                  <th>{t('modelId')}</th>
                  <th>{t('actions')}</th>
                </tr>
              </thead>
              <tbody>
                {candidates.data
                  ?.filter((item) => !selected.includes(item.id))
                  .map((item) => (
                    <tr key={item.id}>
                      <td>{item.name}</td>
                      <td className="font-mono">{item.id}</td>
                      <td>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={update.isPending}
                          aria-label={t('addModel', { name: item.name })}
                          onClick={() => change([...selected, item.id])}
                        >
                          {t('add')}
                        </Button>
                      </td>
                    </tr>
                  ))}
              </tbody>
            </Table>
            {candidates.data?.every((item) => selected.includes(item.id)) && (
              <p className="p-4 text-sm text-muted-foreground">{t('emptyCandidates')}</p>
            )}
          </ResourceSection>
          <ErrorNotice error={update.error} />
          {draft && (
            <form
              className="flex justify-end"
              aria-label={t('saveModels')}
              onSubmit={(event) => {
                event.preventDefault()
                if (!update.isPending) update.mutate()
              }}
            >
              <SaveButton pending={update.isPending}>{t('saveModels')}</SaveButton>
            </form>
          )}
        </>
      )}
      {saved && (
        <p role="status" className="text-sm">
          {t('saved')}
        </p>
      )}
    </div>
  )
}
