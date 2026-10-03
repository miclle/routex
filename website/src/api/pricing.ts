import client from './client'
import { writeCatalog } from './catalog'
import {
  currencies,
  metrics,
  metricUnits,
  type ModelPrice,
  type PricePage,
  type PriceWrite,
  type PriceMetric,
  type PriceCurrency,
} from '@/types/pricing'
import type { Model } from '@/types/catalog'

export async function getModelPrices(modelId: string, signal?: AbortSignal) {
  return (
    await client.get<PricePage>('/admin/prices', {
      params: { provider_model_id: modelId },
      signal,
    })
  ).data
}
export function writePrices(input: PriceWrite, csrf: string) {
  return writeCatalog<PricePage>('put', '/admin/prices', input, csrf)
}

export async function getRoutePrices(
  binding: Pick<
    Model['bindings'][number],
    'provider_model_id' | 'provider_id' | 'upstream_name' | 'protocol'
  >,
  signal?: AbortSignal,
): Promise<ModelPrice> {
  const value = (
    await client.get<unknown>(
      `/admin/provider-models/${encodeURIComponent(binding.provider_model_id)}/price`,
      { signal },
    )
  ).data
  const record = (item: unknown): item is Record<string, unknown> =>
    !!item && typeof item === 'object' && !Array.isArray(item)
  if (
    !record(value) ||
    typeof value.etag !== 'string' ||
    !value.etag ||
    !record(value.currency) ||
    typeof value.currency.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(value.currency.platform_currency) ||
    !record(value.currency.rates) ||
    !Array.isArray(value.items) ||
    value.items.length !== 1 ||
    !(value.next_cursor === undefined || value.next_cursor === '' || value.next_cursor === null)
  )
    throw new Error('Invalid route price response')
  const price = value.items[0]
  if (
    !record(price) ||
    price.provider_model_id !== binding.provider_model_id ||
    price.provider_id !== binding.provider_id ||
    price.protocol !== binding.protocol ||
    price.upstream_name !== binding.upstream_name ||
    typeof price.update_source !== 'string' ||
    typeof price.follow_repository !== 'boolean' ||
    typeof price.id !== 'string' ||
    !price.id ||
    typeof price.context_threshold !== 'number' ||
    ![0, 128000, 200000].includes(price.context_threshold) ||
    !Array.isArray(price.rates) ||
    price.rates.length > 10 ||
    price.rates.some(
      (rate) =>
        !record(rate) ||
        !metrics.includes(rate.metric as PriceMetric) ||
        !['base', 'long_context'].includes(String(rate.tier)) ||
        rate.unit !== metricUnits[rate.metric as PriceMetric] ||
        typeof rate.currency !== 'string' ||
        !currencies.includes(rate.currency as PriceCurrency) ||
        typeof rate.amount !== 'string' ||
        !/^[0-9]{1,18}(\.[0-9]{1,18})?$/.test(rate.amount) ||
        typeof rate.enabled !== 'boolean',
    ) ||
    new Set(price.rates.map((rate) => `${rate.metric}:${rate.tier}`)).size !== price.rates.length
  )
    throw new Error('Invalid route price target')
  return price as unknown as ModelPrice
}
