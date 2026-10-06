import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import type {
  ProviderMetadata,
  ProviderMetadataInput,
  ProviderMetadataResult,
} from '@/types/provider-metadata'
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const fields = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const safeID = (value: unknown): value is string =>
  typeof value === 'string' && /^prv_[A-Za-z0-9_-]+$/.test(value) && value.length <= 30
const proof = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{64}\.[0-9a-f]{64}$/.test(value)
const plain = (value: unknown): value is string =>
  typeof value === 'string' && !/[\p{Cc}\p{Cs}]/u.test(value)
// Go White_Space differs from JavaScript trim: U+FEFF is valid retained content.
export const trimProviderMetadata = (value: string) =>
  value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
export const validProviderName = (value: unknown): value is string =>
  plain(value) &&
  value.length > 0 &&
  trimProviderMetadata(value) === value &&
  [...value].length <= 100
export const validProviderReason = (value: unknown): value is string =>
  plain(value) &&
  value.length > 0 &&
  trimProviderMetadata(value) === value &&
  new TextEncoder().encode(value).length <= 1024
function invalid(): never {
  throw new Error('Provider metadata response unavailable')
}
function record(value: unknown): ProviderMetadata {
  if (
    !object(value) ||
    !fields(value, ['id', 'name', 'etag', 'can_edit']) ||
    !safeID(value.id) ||
    !validProviderName(value.name) ||
    !proof(value.etag) ||
    typeof value.can_edit !== 'boolean'
  )
    invalid()
  return value as unknown as ProviderMetadata
}
function responseProof(response: AxiosResponse<unknown>, token: string) {
  const headers = AxiosHeaders.from(response.headers as RawAxiosHeaders)
  const control = headers.get('Cache-Control')
  const parts =
    typeof control === 'string' ? control.split(',').map((part) => part.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store') ||
    headers.get('ETag') !== `"${token}"`
  )
    invalid()
}
export async function getProviderMetadata(
  id: string,
  signal?: AbortSignal,
): Promise<ProviderMetadata> {
  if (!safeID(id)) invalid()
  const response = await client.get<unknown>(`/admin/providers/${id}/metadata`, { signal })
  const data = record(response.data)
  responseProof(response, data.etag)
  if (data.id !== id) invalid()
  return data
}
export async function saveProviderMetadata(
  id: string,
  etag: string,
  input: ProviderMetadataInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<ProviderMetadataResult> {
  if (
    !safeID(id) ||
    !proof(etag) ||
    !object(input) ||
    !fields(input, ['name', 'reason']) ||
    !validProviderName(input.name) ||
    !validProviderReason(input.reason) ||
    !csrf
  )
    invalid()
  const response = await client.put<unknown>(
    `/admin/providers/${id}/metadata`,
    { name: input.name, reason: input.reason },
    { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf }, signal },
  )
  const data = response.data
  if (
    !object(data) ||
    !fields(data, ['provider', 'runtime_applied', 'changed']) ||
    data.runtime_applied !== true ||
    typeof data.changed !== 'boolean'
  )
    invalid()
  const provider = record(data.provider)
  responseProof(response, provider.etag)
  if (
    provider.id !== id ||
    provider.name !== input.name ||
    provider.etag.split('.')[0] !== etag.split('.')[0] ||
    !provider.can_edit
  )
    invalid()
  return { provider, runtime_applied: true, changed: data.changed }
}
