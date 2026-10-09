import client from './client'
import type { ProviderModel } from '@/types/catalog'
import type { ProviderModelStateInput } from '@/types/provider-model-state'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const review = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{64}$/.test(value)
const revision = (value: unknown): value is string =>
  typeof value === 'string' &&
  value.length > 0 &&
  value.length <= 64 &&
  !/[\p{Cc}\p{Cs}]/u.test(value)
export function validProviderModelStateInput(value: unknown): value is ProviderModelStateInput {
  if (!object(value) || !revision(value.etag)) return false
  const capabilities = Object.hasOwn(value, 'capability_review_etag')
  const allowed = capabilities
    ? [
        'etag',
        'capability_review_etag',
        'supports_image_input',
        'supports_pdf_input',
        ...(Object.hasOwn(value, 'enabled') ? ['enabled'] : []),
      ]
    : ['etag', 'enabled']
  return (
    Object.keys(value).length === allowed.length &&
    allowed.every((key) => Object.hasOwn(value, key)) &&
    (!Object.hasOwn(value, 'enabled') || typeof value.enabled === 'boolean') &&
    (!capabilities ||
      (review(value.capability_review_etag) &&
        typeof value.supports_image_input === 'boolean' &&
        typeof value.supports_pdf_input === 'boolean'))
  )
}
export async function saveProviderModelState(
  id: string,
  input: ProviderModelStateInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<ProviderModel> {
  if (!/^[A-Za-z0-9_-]{1,30}$/.test(id) || !csrf || !validProviderModelStateInput(input))
    throw new Error('Provider Model state request unavailable')
  // Availability alone never copies or reattests either capability declaration.
  const response = await client.patch<unknown>(
    `/admin/provider-models/${encodeURIComponent(id)}`,
    { ...input },
    { signal, headers: { 'X-CSRF-Token': csrf } },
  )
  const value = response.data
  if (
    response.status !== 200 ||
    !object(value) ||
    value.id !== id ||
    !revision(value.etag) ||
    typeof value.upstream_name !== 'string' ||
    typeof value.enabled !== 'boolean' ||
    typeof value.supports_image_input !== 'boolean' ||
    typeof value.supports_pdf_input !== 'boolean' ||
    typeof value.capabilities_transport_current !== 'boolean' ||
    !review(value.capability_review_etag) ||
    (input.enabled !== undefined && value.enabled !== input.enabled) ||
    (input.capability_review_etag !== undefined &&
      (value.capabilities_transport_current !== true ||
        value.supports_image_input !== input.supports_image_input ||
        value.supports_pdf_input !== input.supports_pdf_input))
  )
    throw new Error('Provider Model state response unavailable')
  return value as unknown as ProviderModel
}
