import client from './client'
import type {
  ProviderModelCapacity,
  ProviderModelCapacityInput,
} from '@/types/provider-model-capacity'

export async function getProviderModelCapacity(modelId: string, signal?: AbortSignal) {
  return (
    await client.get<ProviderModelCapacity>(`/admin/provider-models/${modelId}/reservation-bound`, {
      signal,
    })
  ).data
}

export async function saveProviderModelCapacity(
  modelId: string,
  etag: string,
  input: ProviderModelCapacityInput,
  csrf: string,
) {
  return (
    await client.put<ProviderModelCapacity>(
      `/admin/provider-models/${modelId}/reservation-bound`,
      input,
      { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } },
    )
  ).data
}
