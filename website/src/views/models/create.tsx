import { useTranslation } from 'react-i18next'
import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import type { Model } from '@/types/catalog'
import { Page, QueryState, FormField, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Input } from '@/components/ui/input'
import { buttonVariants } from '@/components/ui/button'

export default function CreateModelPage() {
  return (
    <PermissionGate permission="models.write">
      <PermissionGate permission="providers.read">
        <CreateModel />
      </PermissionGate>
    </PermissionGate>
  )
}
function CreateModel() {
  const { t } = useTranslation('catalog')
  const { data: session } = useSession()
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const cache = useQueryClient()
  const navigate = useNavigate()
  const [connectionId, setConnectionId] = useState('')
  const connection = providers.data
    ?.flatMap((p) => p.connections)
    .find((c) => c.id === connectionId)
  const provider = providers.data?.find((p) => p.connections.some((c) => c.id === connectionId))
  const mutation = useMutation({
    mutationFn: (data: { name: string; provider_model_id: string }) =>
      writeCatalog<Model>('post', '/admin/models', data, session!.csrf_token),
    onSuccess: (model) => {
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
      void cache.invalidateQueries({ queryKey: ['models'] })
      navigate(`/admin/models/${model.id}`)
    },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (mutation.isPending) return
    const data = new FormData(event.currentTarget)
    mutation.mutate({
      name: String(data.get('name')).trim(),
      provider_model_id: String(data.get('provider_model_id')),
    })
  }
  return (
    <Page title={t('createModel.title')} description={t('createModel.description')}>
      <QueryState
        pending={providers.isPending}
        error={providers.error}
        retry={() => void providers.refetch()}
      />
      <section className="rounded-lg border p-6">
        <form
          aria-label={t('createModel.title')}
          onSubmit={submit}
          className="space-y-6 [&_label]:grid [&_label]:grid-cols-1 sm:[&_label]:grid-cols-[180px_1fr] [&_label]:items-center [&_label]:gap-4"
        >
          <fieldset disabled={mutation.isPending} className="space-y-6">
            <FormField label={t('common.connectionSettings')}>
              <span className="text-sm">{t('createModel.existingConnection')}</span>
            </FormField>
            <FormField label={t('createModel.providerConnection')}>
              <select
                className="h-10 rounded-md border bg-background px-3"
                value={connectionId}
                onChange={(event) => setConnectionId(event.target.value)}
                required
              >
                <option value="">{t('createModel.chooseConnection')}</option>
                {providers.data?.flatMap((p) =>
                  p.connections.map((c) => (
                    <option key={c.id} value={c.id}>
                      {p.name} / {c.name}
                    </option>
                  )),
                )}
              </select>
            </FormField>
            {connection && (
              <>
                <FormField label={t('common.provider')}>
                  <Input value={provider?.name ?? ''} disabled />
                </FormField>
                <FormField label={t('common.protocolType')}>
                  <Input value={t('common.openAIChat')} disabled />
                </FormField>
                <FormField label={t('createModel.upstreamBaseURL')}>
                  <Input value={connection.base_url} disabled />
                </FormField>
                <FormField label={t('createModel.availableModels')}>
                  <select
                    name="provider_model_id"
                    required
                    className="h-10 rounded-md border bg-background px-3"
                  >
                    <option value="">{t('createModel.chooseModel')}</option>
                    {connection.provider_models.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.upstream_name}
                      </option>
                    ))}
                  </select>
                </FormField>
              </>
            )}
            <FormField label={t('createModel.publicName')}>
              <Input
                name="name"
                required
                maxLength={200}
                placeholder={t('createModel.namePlaceholder')}
              />
            </FormField>
          </fieldset>
          <ErrorNotice error={mutation.error} />
          <div className="flex justify-center gap-3">
            <Link className={buttonVariants({ variant: 'outline' })} to="/admin/models">
              {t('common.cancel')}
            </Link>
            <SaveButton pending={mutation.isPending} disabled={!connection}>
              {t('createModel.title')}
            </SaveButton>
          </div>
        </form>
      </section>
    </Page>
  )
}
