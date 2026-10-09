import client from './client'
import type { ProviderModel } from '@/types/catalog'

// The existing state endpoint consumes an opaque body revision, not If-Match.
// Omit capabilities so a status action cannot overwrite another editor's declarations.
export async function saveProviderModelStatus(
  id: string,
  input: { etag: string; enabled: boolean },
  csrf: string,
  signal?: AbortSignal,
): Promise<ProviderModel> {
  const response = await client.patch<ProviderModel>(
    `/admin/provider-models/${encodeURIComponent(id)}`,
    { etag: input.etag, enabled: input.enabled },
    { signal, headers: { 'X-CSRF-Token': csrf } },
  )
  const value = response.data
  if (
    response.status !== 200 ||
    !value ||
    value.id !== id ||
    value.enabled !== input.enabled ||
    typeof value.etag !== 'string' ||
    !value.etag ||
    typeof value.upstream_name !== 'string' ||
    typeof value.supports_image_input !== 'boolean' ||
    typeof value.supports_pdf_input !== 'boolean'
  )
    throw new Error('Provider Model status response unavailable')
  return value
}
