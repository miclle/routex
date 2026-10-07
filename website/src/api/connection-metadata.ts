import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import type {
  ConnectionMetadata,
  ConnectionMetadataInput,
  ConnectionMetadataResult,
} from '@/types/connection-metadata'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const fields = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const safeID = (value: unknown, prefix: string): value is string =>
  typeof value === 'string' && value.startsWith(prefix) && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const proof = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{64}\.[0-9a-f]{64}$/.test(value)
const plain = (value: unknown): value is string =>
  typeof value === 'string' && !/[\p{Cc}\p{Cs}]/u.test(value)
// Go's strings.TrimSpace follows Unicode White_Space; JavaScript trim also removes U+FEFF.
export const trimConnectionMetadata = (value: string) =>
  value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
const exactBoundary = (value: string) => trimConnectionMetadata(value) === value
export const validConnectionName = (value: unknown): value is string =>
  plain(value) && exactBoundary(value) && value.length > 0 && [...value].length <= 100
export const validConnectionReason = (value: unknown): value is string =>
  plain(value) &&
  exactBoundary(value) &&
  value.length > 0 &&
  new TextEncoder().encode(value).length <= 1024
function invalid(): never {
  throw new Error('Connection metadata response unavailable')
}
export function decodeConnectionMetadata(value: unknown): ConnectionMetadata {
  if (
    !object(value) ||
    !fields(value, [
      'id',
      'provider_id',
      'name',
      'protocol',
      'base_url',
      'egress_mode',
      'egress_id',
      'etag',
      'can_edit',
    ])
  )
    invalid()
  if (
    !safeID(value.id, 'con_') ||
    !safeID(value.provider_id, 'prv_') ||
    !validConnectionName(value.name) ||
    !proof(value.etag) ||
    typeof value.can_edit !== 'boolean'
  )
    invalid()
  if (
    typeof value.protocol !== 'string' ||
    !['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'].includes(
      value.protocol,
    )
  )
    invalid()
  if (
    !plain(value.base_url) ||
    value.base_url.length > 2048 ||
    typeof value.egress_mode !== 'string' ||
    !['default', 'direct', 'proxy'].includes(value.egress_mode)
  )
    invalid()
  try {
    const parsed = new URL(value.base_url)
    if (
      !['http:', 'https:'].includes(parsed.protocol) ||
      !parsed.hostname ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash
    )
      invalid()
  } catch {
    invalid()
  }
  if (value.egress_id !== null && !safeID(value.egress_id, 'egr_')) invalid()
  if ((value.egress_mode === 'proxy') !== (value.egress_id !== null)) invalid()
  return value as unknown as ConnectionMetadata
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
export async function getConnectionMetadata(
  providerId: string,
  id: string,
  signal?: AbortSignal,
): Promise<ConnectionMetadata> {
  if (!safeID(providerId, 'prv_') || !safeID(id, 'con_')) invalid()
  const response = await client.get<unknown>(`/admin/connections/${id}/metadata`, { signal })
  const data = decodeConnectionMetadata(response.data)
  responseProof(response, data.etag)
  if (data.id !== id || data.provider_id !== providerId) invalid()
  return data
}
export async function saveConnectionMetadata(
  providerId: string,
  id: string,
  etag: string,
  input: ConnectionMetadataInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<ConnectionMetadataResult> {
  if (
    !safeID(providerId, 'prv_') ||
    !safeID(id, 'con_') ||
    !proof(etag) ||
    !object(input) ||
    !fields(input, ['name', 'reason']) ||
    !validConnectionName(input.name) ||
    !validConnectionReason(input.reason) ||
    !csrf
  )
    invalid()
  const response = await client.put<unknown>(
    `/admin/connections/${id}/metadata`,
    { name: input.name, reason: input.reason },
    { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf }, signal },
  )
  const data = response.data
  if (
    !object(data) ||
    !fields(data, ['connection', 'runtime_applied', 'changed']) ||
    data.runtime_applied !== true ||
    typeof data.changed !== 'boolean'
  )
    invalid()
  const connection = decodeConnectionMetadata(data.connection)
  responseProof(response, connection.etag)
  if (
    connection.id !== id ||
    connection.provider_id !== providerId ||
    connection.name !== input.name ||
    connection.etag.split('.')[0] !== etag.split('.')[0] ||
    !connection.can_edit
  )
    invalid()
  return { connection, runtime_applied: true, changed: data.changed }
}
