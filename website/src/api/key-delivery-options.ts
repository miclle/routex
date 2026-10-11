import { getVaultIntegrations } from './vault-integrations'

export interface VaultDeliveryOption {
  id: string
  name: string
  endpoint: string
  namespace: string
  mount: string
}
export class KeyDeliveryOptionsError extends Error {
  constructor(public readonly kind: 'unavailable' | 'changed' | 'overflow') {
    super('API-Key delivery options could not be confirmed')
  }
}

// Each page is an authorized normal catalogue read. Do not publish a partial or
// mixed-generation list as the complete set of reviewed destination choices.
export async function getVaultDeliveryOptions(
  signal?: AbortSignal,
): Promise<VaultDeliveryOption[]> {
  const items: VaultDeliveryOption[] = []
  const ids = new Set<string>()
  const cursors = new Set<string>()
  let cursor = ''
  let review: string | undefined
  for (let pageNumber = 0; pageNumber < 10; pageNumber++) {
    if (signal?.aborted) throw new KeyDeliveryOptionsError('unavailable')
    let page
    try {
      page = await getVaultIntegrations(cursor, signal)
    } catch {
      throw new KeyDeliveryOptionsError('unavailable')
    }
    if (signal?.aborted) throw new KeyDeliveryOptionsError('unavailable')
    if (review !== undefined && page.review_etag !== review)
      throw new KeyDeliveryOptionsError('changed')
    review = page.review_etag
    for (const item of page.items) {
      if (ids.has(item.id)) throw new KeyDeliveryOptionsError('changed')
      ids.add(item.id)
      items.push({
        id: item.id,
        name: item.name,
        endpoint: item.descriptor.endpoint,
        namespace: item.descriptor.namespace,
        mount: item.descriptor.mount,
      })
    }
    if (page.next_cursor === null) return items
    if (cursors.has(page.next_cursor)) throw new KeyDeliveryOptionsError('changed')
    cursors.add(page.next_cursor)
    cursor = page.next_cursor
  }
  throw new KeyDeliveryOptionsError('overflow')
}
