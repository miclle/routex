import client from './client'
import { searchPublicModelNames } from '@/lib/public-model-references'
import type { ModelPublicNames } from '@/types/model-public-names'
export function validPublicNameQuery(query: string) {
  return (
    new TextEncoder().encode(query).length <= 128 &&
    new TextDecoder().decode(new TextEncoder().encode(query)) === query &&
    !/\p{Cc}/u.test(query)
  )
}
export async function getModelPublicNames(
  connectionId: string,
  query: string,
  signal?: AbortSignal,
): Promise<ModelPublicNames> {
  if (
    !/^con_[A-Za-z0-9_-]+$(?![\s\S])/.test(connectionId) ||
    connectionId.length > 30 ||
    !validPublicNameQuery(query)
  )
    throw new Error('Invalid public name query')
  const data: unknown = (
    await client.get(`/admin/connections/${connectionId}/model-creation/public-names`, {
      params: { q: query },
      signal,
    })
  ).data
  const object = (value: unknown): value is Record<string, unknown> =>
    !!value && typeof value === 'object' && !Array.isArray(value)
  const expected = searchPublicModelNames(query)
  if (
    !object(data) ||
    Object.keys(data).sort().join(',') !== 'connection_id,items,query' ||
    data.connection_id !== connectionId ||
    data.query !== query ||
    !Array.isArray(data.items) ||
    data.items.length !== expected.length
  )
    throw new Error('Invalid public name response')
  for (const [index, item] of data.items.entries()) {
    if (
      !object(item) ||
      Object.keys(item).sort().join(',') !== 'available,name' ||
      item.name !== expected[index] ||
      typeof item.available !== 'boolean'
    )
      throw new Error('Invalid public name response')
  }
  return data as unknown as ModelPublicNames
}
