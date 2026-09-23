import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { listAdminModels, listProviders } from '@/api/catalog'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { usePermissions } from '@/hooks/use-permissions'
import { Badge } from '@/components/ui/badge'
import ModelPriceTable from './model-price-table'

export default function ProviderModelPage() {
  return (
    <PermissionGate permission="providers.read">
      <ProviderModelDetail />
    </PermissionGate>
  )
}
function ProviderModelDetail() {
  const { t } = useTranslation('pricing')
  const { providerId, modelId } = useParams()
  const access = usePermissions()
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const models = useQuery({
    queryKey: ['admin', 'models'],
    queryFn: listAdminModels,
    enabled: access.can('models.read_all'),
  })
  const provider = providers.data?.find((item) => item.id === providerId)
  const connection = provider?.connections.find((item) =>
    item.provider_models.some((model) => model.id === modelId),
  )
  const model = connection?.provider_models.find((item) => item.id === modelId)
  const bindings =
    models.data?.flatMap((platform) =>
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
            <h2 className="text-2xl font-semibold">{model.upstream_name}</h2>
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
          {access.can('prices.read') && (
            <section className="space-y-4 rounded-lg border p-6">
              <h3 className="font-semibold">{t('title')}</h3>
              <ModelPriceTable key={model.id} modelId={model.id} modelName={model.upstream_name} />
            </section>
          )}
        </>
      )}
    </Page>
  )
}
