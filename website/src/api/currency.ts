import client from './client'
import { writeCatalog } from './catalog'
import type { PricePage } from '@/types/pricing'
import type { CurrencyPage } from '@/types/currency'
export async function getCurrency(signal?: AbortSignal) {
  return (await client.get<CurrencyPage>('/admin/prices/currency', { signal })).data
}
export function saveCurrency(etag: string, currency: PricePage['currency'], csrf: string) {
  return writeCatalog<PricePage>('put', '/admin/prices/currency', { etag, currency }, csrf)
}
