import { AxiosHeaders, type RawAxiosHeaders } from 'axios'
import client from './client'
import type { ConnectionTestResult } from '@/types/connection-test'

const id = (value: unknown, prefix: string): value is string =>
  typeof value === 'string' && value.startsWith(prefix) && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const invalid = (): never => {
  throw new Error('Connection test response unavailable')
}
function timestamp(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$/.exec(value)
  if (!parts) return false
  const date = new Date(value)
  return (
    Number.isFinite(date.getTime()) &&
    [
      date.getUTCFullYear(),
      date.getUTCMonth() + 1,
      date.getUTCDate(),
      date.getUTCHours(),
      date.getUTCMinutes(),
      date.getUTCSeconds(),
    ].every((part, index) => part === Number(parts[index + 1]))
  )
}
export function decodeConnectionTest(value: unknown): ConnectionTestResult {
  if (!value || typeof value !== 'object' || Array.isArray(value)) invalid()
  const row = value as Record<string, unknown>
  const keys = [
    'connection_id',
    'credential_id',
    'outcome',
    'scope',
    'discovered_model_count',
    'checked_at',
  ]
  if (
    Object.keys(row).length !== keys.length ||
    !keys.every((key) => Object.hasOwn(row, key)) ||
    !id(row.connection_id, 'con_') ||
    !id(row.credential_id, 'crd_') ||
    (row.outcome !== 'passed' && row.outcome !== 'failed') ||
    (row.scope !== 'model_discovery' && row.scope !== 'authentication_only') ||
    !timestamp(row.checked_at)
  )
    invalid()
  const counted = row.scope === 'model_discovery' && row.outcome === 'passed'
  if (
    counted
      ? typeof row.discovered_model_count !== 'number' ||
        !Number.isInteger(row.discovered_model_count) ||
        row.discovered_model_count < 0 ||
        row.discovered_model_count > 2000
      : row.discovered_model_count !== null
  )
    invalid()
  return row as unknown as ConnectionTestResult
}
export async function testConnection(
  connectionId: string,
  credentialId: string,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<ConnectionTestResult> {
  if (
    !id(connectionId, 'con_') ||
    !id(credentialId, 'crd_') ||
    !/^[0-9a-f]{64}\.[0-9a-f]{64}$/.test(etag) ||
    !csrf ||
    signal?.aborted
  )
    invalid()
  const response = await client.post<unknown>(
    `/admin/connections/${connectionId}/test`,
    { credential_id: credentialId },
    { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf }, signal, timeout: 15_000 },
  )
  const headers = AxiosHeaders.from(response.headers as RawAxiosHeaders)
  const control = headers.get('Cache-Control')
  const parts =
    typeof control === 'string' ? control.split(',').map((part) => part.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store') ||
    signal?.aborted
  )
    invalid()
  const result = decodeConnectionTest(response.data)
  if (result.connection_id !== connectionId || result.credential_id !== credentialId) invalid()
  return result
}
