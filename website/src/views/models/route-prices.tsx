import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getRoutePrices } from '@/api/pricing'
import type { Model } from '@/types/catalog'
import type { PriceRate } from '@/types/pricing'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

function BaseRate({ rate }: { rate: PriceRate | undefined }) {
  const { t } = useTranslation('pricing')
  if (!rate) return <span className="text-muted-foreground">{t('routeNotConfigured')}</span>
  return (
    <div className="space-y-1">
      <p className="whitespace-nowrap">
        {t('routeAmount', { amount: rate.amount, currency: rate.currency, unit: t('1M_TOKEN') })}
      </p>
      {!rate.enabled && <Badge variant="outline">{t('disabled')}</Badge>}
    </div>
  )
}
export default function RoutePrices({
  actor,
  modelID,
  generation,
  binding,
  readable,
  refreshDetail,
}: {
  actor: string
  modelID: string
  generation: number
  binding: Model['bindings'][number]
  readable: boolean
  refreshDetail: () => void
}) {
  const { t } = useTranslation('pricing')
  const query = useQuery({
    queryKey: [
      'admin',
      'model-route-prices',
      actor,
      modelID,
      generation,
      binding.provider_model_id,
    ],
    queryFn: ({ signal }) => getRoutePrices(binding, signal),
    enabled: readable,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = readable && query.isSuccess && !query.isFetching
  const label = !readable
    ? 'routeReadPermission'
    : query.isFetching
      ? 'routeLoading'
      : 'routeUnavailable'
  return (
    <>
      <td>
        {fresh ? (
          <BaseRate
            rate={query.data.rates.find(
              (rate) => rate.metric === 'INPUT_TOKEN' && rate.tier === 'base',
            )}
          />
        ) : (
          <div className="space-y-1">
            <p
              className="text-muted-foreground"
              role={query.isError ? 'alert' : query.isFetching ? 'status' : undefined}
            >
              {t(label)}
            </p>
            {readable && query.isError && (
              <Button size="sm" variant="outline" onClick={refreshDetail}>
                {t('routeRetry', { name: binding.upstream_name })}
              </Button>
            )}
          </div>
        )}
      </td>
      <td>
        {fresh ? (
          <BaseRate
            rate={query.data.rates.find(
              (rate) => rate.metric === 'OUTPUT_TOKEN' && rate.tier === 'base',
            )}
          />
        ) : (
          <span className="text-muted-foreground">{t(label)}</span>
        )}
      </td>
    </>
  )
}
