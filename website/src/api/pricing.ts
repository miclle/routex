import client from './client'
import { writeCatalog } from './catalog'
import type { PricePage, PriceWrite } from '@/types/pricing'

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
