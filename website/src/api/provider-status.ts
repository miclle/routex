import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import { validProviderName, validProviderReason } from './provider-metadata'
import type {
  ProviderStatus,
  ProviderStatusInput,
  ProviderStatusResult,
} from '@/types/provider-status'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const fields = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const safeID = (value: unknown): value is string =>
  typeof value === 'string' && /^prv_[A-Za-z0-9_-]+$/.test(value) && value.length <= 30
const proof = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{64}\.[0-9a-f]{64}$/.test(value)
function invalid(): never {
  throw new Error('Provider status response unavailable')
}
export function decodeProviderStatus(value: unknown): ProviderStatus {
  if (
    !object(value) ||
    !fields(value, ['id', 'name', 'etag', 'can_edit', 'enabled']) ||
    !safeID(value.id) ||
    !validProviderName(value.name) ||
    !proof(value.etag) ||
    typeof value.can_edit !== 'boolean' ||
    typeof value.enabled !== 'boolean'
  )
    invalid()
  return value as unknown as ProviderStatus
}
function checked(response: AxiosResponse<unknown>, etag: string) {
  const headers = AxiosHeaders.from(response.headers as RawAxiosHeaders)
  const control = headers.get('Cache-Control')
  const parts =
    typeof control === 'string' ? control.split(',').map((part) => part.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store') ||
    headers.get('ETag') !== `"${etag}"`
  )
    invalid()
}
export async function getProviderStatus(id: string, signal?: AbortSignal): Promise<ProviderStatus> {
  if (!safeID(id)) invalid()
  const response = await client.get<unknown>(`/admin/providers/${id}/status`, { signal })
  const data = decodeProviderStatus(response.data)
  checked(response, data.etag)
  if (data.id !== id) invalid()
  return data
}
export async function saveProviderStatus(
  id: string,
  etag: string,
  input: ProviderStatusInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<ProviderStatusResult> {
  if (
    !safeID(id) ||
    !proof(etag) ||
    !object(input) ||
    !fields(input, ['enabled', 'reason']) ||
    typeof input.enabled !== 'boolean' ||
    !validProviderReason(input.reason) ||
    !csrf
  )
    invalid()
  const response = await client.put<unknown>(
    `/admin/providers/${id}/status`,
    { enabled: input.enabled, reason: input.reason },
    {
      signal,
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    },
  )
  const value = response.data
  if (
    !object(value) ||
    !fields(value, ['provider', 'runtime_applied', 'changed']) ||
    value.runtime_applied !== true ||
    typeof value.changed !== 'boolean'
  )
    invalid()
  const provider = decodeProviderStatus(value.provider)
  checked(response, provider.etag)
  if (
    provider.id !== id ||
    provider.enabled !== input.enabled ||
    !provider.can_edit ||
    provider.etag.slice(0, 64) !== etag.slice(0, 64)
  )
    invalid()
  return { provider, runtime_applied: true, changed: value.changed }
}
