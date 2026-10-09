import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import { decodeConnectionMetadata, validConnectionReason } from './connection-metadata'
import type {
  ConnectionStatus,
  ConnectionStatusInput,
  ConnectionStatusResult,
} from '@/types/connection-status'
const safe = (value: string, prefix: string) =>
  value.startsWith(prefix) && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const proof = (value: string) => /^[0-9a-f]{64}\.[0-9a-f]{64}$/.test(value)
function invalid(): never {
  throw new Error('Connection status response unavailable')
}
export function decodeConnectionStatus(value: unknown): ConnectionStatus {
  if (
    !value ||
    typeof value !== 'object' ||
    Array.isArray(value) ||
    Object.keys(value).length !== 15 ||
    !Object.hasOwn(value, 'enabled')
  )
    invalid()
  const { enabled, ...metadata } = value as Record<string, unknown>
  if (typeof enabled !== 'boolean') invalid()
  return { ...decodeConnectionMetadata(metadata), enabled }
}
function checked(response: AxiosResponse<unknown>, data: ConnectionStatus) {
  const headers = AxiosHeaders.from(response.headers as RawAxiosHeaders)
  const raw = headers.get('Cache-Control')
  const parts = typeof raw === 'string' ? raw.split(',').map((x) => x.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store') ||
    headers.get('ETag') !== `"${data.etag}"`
  )
    invalid()
}
export async function getConnectionStatus(
  provider: string,
  id: string,
  signal?: AbortSignal,
): Promise<ConnectionStatus> {
  if (!safe(provider, 'prv_') || !safe(id, 'con_')) invalid()
  const response = await client.get<unknown>(`/admin/connections/${id}/status`, { signal })
  const data = decodeConnectionStatus(response.data)
  checked(response, data)
  if (data.id !== id || data.provider_id !== provider) invalid()
  return data
}
export async function saveConnectionStatus(
  provider: string,
  id: string,
  etag: string,
  input: ConnectionStatusInput,
  csrf: string,
  signal?: AbortSignal,
): Promise<ConnectionStatusResult> {
  if (
    !safe(provider, 'prv_') ||
    !safe(id, 'con_') ||
    !proof(etag) ||
    !input ||
    Object.keys(input).length !== 2 ||
    !Object.hasOwn(input, 'enabled') ||
    !Object.hasOwn(input, 'reason') ||
    typeof input.enabled !== 'boolean' ||
    !validConnectionReason(input.reason)
  )
    invalid()
  const response = await client.put<unknown>(`/admin/connections/${id}/status`, input, {
    signal,
    headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
  })
  const value = response.data
  if (
    !value ||
    typeof value !== 'object' ||
    Array.isArray(value) ||
    Object.keys(value).length !== 3 ||
    !Object.hasOwn(value, 'connection') ||
    !Object.hasOwn(value, 'runtime_applied') ||
    !Object.hasOwn(value, 'changed')
  )
    invalid()
  const result = value as Record<string, unknown>
  const data = decodeConnectionStatus(result.connection)
  checked(response, data)
  if (
    data.id !== id ||
    data.provider_id !== provider ||
    data.etag.slice(0, 64) !== etag.slice(0, 64) ||
    data.enabled !== input.enabled ||
    !data.can_edit ||
    result.runtime_applied !== true ||
    typeof result.changed !== 'boolean'
  )
    invalid()
  return { connection: data, runtime_applied: true, changed: result.changed }
}
