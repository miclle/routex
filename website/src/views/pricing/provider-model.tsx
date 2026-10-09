import { useState } from 'react'
import type { ProviderModel } from '@/types/catalog'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { listAdminModels, listProviders } from '@/api/catalog'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { getPermissions } from '@/api/governance'
import { useConnectionQueryRevision } from '@/views/providers/connection-authority'
import type { Session } from '@/types/auth'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import { Badge } from '@/components/ui/badge'
import ModelPriceTable from './model-price-table'
import ProviderModelState from './provider-model-state'
import ProviderModelCapacity from './provider-model-capacity'

export default function ProviderModelPage() {
  const session = useSession(),
    { providerId, modelId } = useParams()
  return (
    <ProviderModelDetail
      key={`${session.data?.user.id ?? ''}:${providerId}:${modelId}`}
      session={session}
      providerId={providerId}
      modelId={modelId}
    />
  )
}
function ProviderModelDetail({
  session,
  providerId,
  modelId,
}: {
  session: ReturnType<typeof useSession>
  providerId?: string
  modelId?: string
}) {
  const { t } = useTranslation('pricing'),
    generation = useSessionGeneration()
  const cache = useQueryClient()
  const fresh =
    session.isSuccess && !session.isFetching && !session.error && !!session.data?.csrf_token
  const permissions = useQuery({
    queryKey: [
      'permissions',
      session.data?.user.id,
      'provider-price',
      providerId,
      modelId,
      session.data?.user.role,
      generation,
    ],
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: fresh,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const access = {
    ...permissions,
    can: (permission: string) =>
      fresh &&
      permissions.isSuccess &&
      !permissions.isFetching &&
      !permissions.error &&
      permissions.data.includes(permission),
  }
  const providers = useQuery({
    queryKey: [
      'admin',
      'providers',
      session.data?.user.id,
      providerId,
      modelId,
      session.data?.user.role,
      generation,
    ],
    queryFn: ({ signal }) => listProviders(signal),
    enabled: access.can('providers.read'),
    retry: false,
    gcTime: 0,
    staleTime: 0,
  })
  const models = useQuery({
    queryKey: [
      'admin',
      'models',
      session.data?.user.id,
      providerId,
      modelId,
      session.data?.user.role,
      generation,
    ],
    queryFn: listAdminModels,
    enabled: access.can('models.read_all'),
    retry: false,
    gcTime: 0,
    staleTime: 0,
  })
  const permissionsKey = [
    'permissions',
    session.data?.user.id,
    'provider-price',
    providerId,
    modelId,
    session.data?.user.role,
    generation,
  ]
  const catalogueKey = [
    'admin',
    'providers',
    session.data?.user.id,
    providerId,
    modelId,
    session.data?.user.role,
    generation,
  ]
  const revisions = useConnectionQueryRevision([sessionKey, permissionsKey, catalogueKey])
  const parentFresh = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey),
      allowed = cache.getQueryState<string[]>(permissionsKey),
      catalogue = cache.getQueryState<import('@/types/catalog').Provider[]>(catalogueKey)
    return (
      revisions.snapshot() === revisions.revision &&
      auth?.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.error &&
      !auth.isInvalidated &&
      auth.data?.user.id === session.data?.user.id &&
      allowed?.status === 'success' &&
      allowed.fetchStatus === 'idle' &&
      !allowed.error &&
      !allowed.isInvalidated &&
      allowed.data?.includes('providers.read') === true &&
      catalogue?.status === 'success' &&
      catalogue.fetchStatus === 'idle' &&
      !catalogue.error &&
      !catalogue.isInvalidated &&
      catalogue.data === providers.data
    )
  }
  const providerVisible =
    access.can('providers.read') && providers.isSuccess && !providers.isFetching && !providers.error
  const provider = providerVisible
    ? providers.data?.find((item) => item.id === providerId)
    : undefined
  const connection = provider?.connections.find((item) =>
    item.provider_models.some((model) => model.id === modelId),
  )
  const model = connection?.provider_models.find((item) => item.id === modelId)
  const [retainedModel, setRetainedModel] = useState<ProviderModel>()
  if (model && model !== retainedModel) setRetainedModel(model)
  const capturedModel = model ?? retainedModel
  const authority = {
    actor: session.data?.user.id ?? '',
    providerId: providerId ?? '',
    connectionId: connection?.id ?? '',
    generation: cache.getQueryState(sessionKey)?.dataUpdateCount ?? 0,
    permissionsKey,
    catalogueKey,
    readable: () =>
      parentFresh() &&
      !!providerId &&
      !!connection &&
      connection.provider_models.some((item) => item.id === modelId),
    writable: () =>
      parentFresh() &&
      cache.getQueryData<string[]>(permissionsKey)?.includes('providers.write') === true,
  }

  const bindings =
    (access.can('models.read_all') && models.isSuccess && !models.isFetching && !models.error
      ? models.data
      : undefined
    )?.flatMap((platform) =>
      platform.bindings
        .filter((binding) => binding.provider_model_id === modelId)
        .map((binding) => ({ platform, binding })),
    ) ?? []
  return (
    <Page title={model?.upstream_name ?? t('detail')} description={t('detail')}>
      <Link className="text-sm text-primary" to={`/admin/providers/${providerId}?tab=models`}>
        {t('back')}
      </Link>
      <QueryState
        pending={providers.isPending}
        error={providers.error}
        retry={() => void providers.refetch()}
      />
      {providers.isSuccess && !model && <p role="alert">{t('notFound')}</p>}
      {model && provider && connection && (
        <>
          <section className="space-y-5 rounded-lg border p-6">
            <div className="flex items-start justify-between gap-4">
              <h2 className="text-2xl font-semibold">{model.upstream_name}</h2>
              <Badge variant="outline">
                {t(model.enabled ? 'supplyEnabled' : 'supplyDisabled')}
              </Badge>
            </div>
            <dl className="grid gap-5 text-sm sm:grid-cols-3">
              {[
                ['provider', provider.name],
                ['connection', connection.name],
                ['protocol', connection.protocol],
                ['modelId', model.upstream_name],
              ].map(([label, value]) => (
                <div key={label}>
                  <dt className="text-muted-foreground">{t(label)}</dt>
                  <dd className="mt-1 break-all">{value}</dd>
                </div>
              ))}
            </dl>
          </section>
          {access.can('models.read_all') && (
            <section className="space-y-4 rounded-lg border p-6" aria-label={t('routing')}>
              <h3 className="font-semibold">{t('routing')}</h3>
              <QueryState
                pending={models.isPending}
                error={models.error}
                retry={() => void models.refetch()}
              />
              {models.isSuccess && !bindings.length && (
                <p className="text-sm text-muted-foreground">{t('noBindings')}</p>
              )}
              {bindings.map(({ platform, binding }) => (
                <div
                  key={binding.id}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-4"
                >
                  <div className="space-y-2">
                    <p>
                      {platform.name}{' '}
                      <Badge variant="outline">{t(binding.ready ? 'ready' : 'pending')}</Badge>
                    </p>
                    <p className="text-sm text-muted-foreground">{t('routingHelp')}</p>
                  </div>
                  <Link to={`/admin/models/${platform.id}`} className="text-sm text-primary">
                    {t('manageModel', { name: platform.name })}
                  </Link>
                </div>
              ))}
            </section>
          )}
        </>
      )}
      {capturedModel && (
        <ProviderModelState
          key={capturedModel.id}
          model={capturedModel}
          authority={authority}
          visible={!!model}
          reload={async () => {
            const result = await providers.refetch()
            if (result.isError) throw result.error
            return result.data
              ?.find((item) => item.id === providerId)
              ?.connections.find((item) => item.id === authority.connectionId)
              ?.provider_models.find((item) => item.id === modelId)
          }}
        />
      )}
      {modelId && (
        <ProviderModelCapacity modelId={modelId} authority={authority} visible={!!model} />
      )}
      {modelId && (
        <section className="space-y-4 rounded-lg border p-6">
          {access.can('prices.read') && <h3 className="font-semibold">{t('title')}</h3>}
          <ModelPriceTable modelId={modelId} modelName={model?.upstream_name ?? modelId} />
        </section>
      )}
    </Page>
  )
}
