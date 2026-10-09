import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import type {
  ProviderModelCapacity,
  ProviderModelCapacityInput,
} from '@/types/provider-model-capacity'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const proof = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{64}$/.test(value)
const positive = (value: unknown): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value > 0
export const trimCapacityText = (value: string) =>
  value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
export function validCapacityText(value: string) {
  return (
    value.length > 0 &&
    trimCapacityText(value) === value &&
    new TextEncoder().encode(value).length <= 2000
  )
}
function invalid(): never {
  throw new Error('Provider Model capacity response unavailable')
}
export function decodeProviderModelCapacity(value: unknown, id: string): ProviderModelCapacity {
  const keys = [
    'provider_model_id',
    'protocol',
    'etag',
    'revision',
    'transport_current',
    'configured',
    'max_input_tokens',
    'max_output_tokens',
    'evidence',
    'updated_at',
  ]
  if (
    !object(value) ||
    Object.keys(value).length !== keys.length ||
    !keys.every((key) => Object.hasOwn(value, key)) ||
    value.provider_model_id !== id ||
    typeof value.protocol !== 'string' ||
    !['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'].includes(
      value.protocol,
    ) ||
    !proof(value.etag) ||
    typeof value.revision !== 'string' ||
    !/^(0|bnd_[0-9a-hjkmnp-tv-z]{26})$/.test(value.revision) ||
    typeof value.transport_current !== 'boolean' ||
    typeof value.configured !== 'boolean' ||
    value.configured !== (value.revision !== '0') ||
    typeof value.evidence !== 'string' ||
    typeof value.updated_at !== 'string' ||
    !Number.isFinite(Date.parse(value.updated_at)) ||
    (value.configured
      ? !positive(value.max_input_tokens) ||
        !positive(value.max_output_tokens) ||
        !validCapacityText(value.evidence)
      : value.max_input_tokens !== 0 || value.max_output_tokens !== 0 || value.evidence !== '')
  )
    invalid()
  return value as unknown as ProviderModelCapacity
}
function checked(response: AxiosResponse<unknown>, id: string) {
  const value = decodeProviderModelCapacity(response.data, id)
  const headers = AxiosHeaders.from(response.headers as RawAxiosHeaders)
  const raw = headers.get('Cache-Control')
  const parts =
    typeof raw === 'string' ? raw.split(',').map((part) => part.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store') ||
    headers.get('ETag') !== `"${value.etag}"`
  )
    invalid()
  return value
}
export async function getProviderModelCapacity(modelId: string, signal?: AbortSignal) {
  if (!/^[A-Za-z0-9_-]{1,30}$/.test(modelId)) invalid()
  return checked(
    await client.get<unknown>(
      `/admin/provider-models/${encodeURIComponent(modelId)}/reservation-bound`,
      { signal },
    ),
    modelId,
  )
}
export async function saveProviderModelCapacity(
  modelId: string,
  etag: string,
  input: ProviderModelCapacityInput,
  csrf: string,
  signal?: AbortSignal,
) {
  if (
    !/^[A-Za-z0-9_-]{1,30}$/.test(modelId) ||
    !proof(etag) ||
    !csrf ||
    !object(input) ||
    Object.keys(input).length !== 4 ||
    !positive(input.max_input_tokens) ||
    !positive(input.max_output_tokens) ||
    typeof input.evidence !== 'string' ||
    typeof input.reason !== 'string' ||
    !validCapacityText(input.evidence) ||
    !validCapacityText(input.reason)
  )
    invalid()
  const result = checked(
    await client.put<unknown>(
      `/admin/provider-models/${encodeURIComponent(modelId)}/reservation-bound`,
      { ...input },
      { signal, headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } },
    ),
    modelId,
  )
  if (
    !result.configured ||
    !result.transport_current ||
    result.max_input_tokens !== input.max_input_tokens ||
    result.max_output_tokens !== input.max_output_tokens ||
    result.evidence !== input.evidence
  )
    invalid()
  return result
}
