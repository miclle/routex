import axios from 'axios'
import client from './client'
import { validVaultReason, validVaultToken } from './vault-integrations'
import type { VaultObservation } from '@/types/vault-integrations'
import type {
  ProviderOrphan,
  ProviderOrphanPage,
  ProviderCleanupReceipt,
  ProviderCleanupIntent,
} from '@/types/provider-credential-cleanup'

export class ProviderCleanupError extends Error {
  constructor(public readonly status: number) {
    super('Provider cleanup request failed')
  }
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const review = /^[0-9a-f]{64}\.[0-9a-f]{64}$/
const ulid = '[0-7][0-9a-hjkmnp-tv-z]{25}'
const id = (value: unknown, prefix: string) =>
  typeof value === 'string' && new RegExp(`^${prefix}_${ulid}$`).test(value)
function fail(): never {
  throw new ProviderCleanupError(0)
}
function object(value: unknown, keys: string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail()
  const row = value as Record<string, unknown>
  if (Object.keys(row).length !== keys.length || keys.some((key) => !Object.hasOwn(row, key)))
    return fail()
  return row
}
function oneOf<T extends string>(value: unknown, values: readonly T[]): T {
  return values.includes(value as T) ? (value as T) : fail()
}
function boolean(value: unknown): boolean {
  return typeof value === 'boolean' ? value : fail()
}
function date(value: unknown): string {
  if (
    typeof value !== 'string' ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value) ||
    !Number.isFinite(Date.parse(value)) ||
    value.startsWith('0001-')
  )
    return fail()
  return value
}
function observation(value: unknown): VaultObservation {
  const row = object(value, ['attempted', 'succeeded', 'duration_ms', 'failure'])
  const failure =
    row.failure === null ? null : object(row.failure, ['stage', 'code', 'http_status'])
  if (
    typeof row.duration_ms !== 'string' ||
    row.duration_ms.length > 30 ||
    !/^(0|[1-9][0-9]*)(?:\.[0-9]+)?$/.test(row.duration_ms)
  )
    return fail()
  if (
    failure &&
    (typeof failure.code !== 'string' ||
      !/^[a-z][a-z0-9_]{0,63}$/.test(failure.code) ||
      typeof failure.http_status !== 'number' ||
      !Number.isInteger(failure.http_status) ||
      (failure.http_status !== 0 && (failure.http_status < 100 || failure.http_status > 599)))
  )
    return fail()
  const result: VaultObservation = {
    attempted: boolean(row.attempted),
    succeeded: boolean(row.succeeded),
    duration_ms: row.duration_ms,
    failure: failure
      ? {
          stage: oneOf(failure.stage, ['prepare', 'write', 'read', 'cleanup'] as const),
          code: failure.code as string,
          http_status: failure.http_status as number,
        }
      : null,
  }
  if (result.succeeded && (!result.attempted || result.failure)) return fail()
  return result
}
export function parseProviderOrphan(value: unknown): ProviderOrphan {
  const row = object(value, [
    'creation_request_id',
    'kind',
    'provider_id',
    'connection_id',
    'credential_id',
    'integration_id',
    'revision_id',
    'created_at',
    'state',
    'write',
    'read',
    'ownership_recorded',
    'eligible',
    'blocker_codes',
    'can_cleanup',
    'review_etag',
  ])
  if (
    typeof row.creation_request_id !== 'string' ||
    !uuid.test(row.creation_request_id) ||
    !id(row.provider_id, 'prv') ||
    !id(row.connection_id, 'con') ||
    !id(row.credential_id, 'crd') ||
    !id(row.integration_id, 'vlt') ||
    !id(row.revision_id, 'vlr') ||
    typeof row.review_etag !== 'string' ||
    !review.test(row.review_etag)
  )
    return fail()
  if (
    !Array.isArray(row.blocker_codes) ||
    row.blocker_codes.length > 32 ||
    row.blocker_codes.some(
      (code) => typeof code !== 'string' || !/^[a-z][a-z0-9_]{0,63}$/.test(code),
    ) ||
    new Set(row.blocker_codes).size !== row.blocker_codes.length
  )
    return fail()
  const result: ProviderOrphan = {
    creation_request_id: row.creation_request_id,
    kind: oneOf(row.kind, ['provider', 'connection', 'credential', 'replacement'] as const),
    provider_id: row.provider_id as string,
    connection_id: row.connection_id as string,
    credential_id: row.credential_id as string,
    integration_id: row.integration_id as string,
    revision_id: row.revision_id as string,
    created_at: date(row.created_at),
    state: oneOf(row.state, [
      'writing',
      'awaiting_read',
      'unknown',
      'owned',
      'orphan',
      'committed',
    ] as const),
    write: observation(row.write),
    read: observation(row.read),
    ownership_recorded: boolean(row.ownership_recorded),
    eligible: boolean(row.eligible),
    blocker_codes: row.blocker_codes as string[],
    can_cleanup: boolean(row.can_cleanup),
    review_etag: row.review_etag,
  }
  if (
    result.ownership_recorded !== (result.write.succeeded && result.read.succeeded) ||
    (result.eligible &&
      (!result.ownership_recorded || result.state !== 'orphan' || result.blocker_codes.length))
  )
    return fail()
  return result
}
export function parseProviderCleanupReceipt(value: unknown): ProviderCleanupReceipt {
  const row = object(value, [
    'creation_request_id',
    'request_id',
    'integration_id',
    'revision_id',
    'state',
    'ownership',
    'cleanup',
    'started_at',
    'finished_at',
  ])
  if (
    typeof row.creation_request_id !== 'string' ||
    !uuid.test(row.creation_request_id) ||
    typeof row.request_id !== 'string' ||
    !uuid.test(row.request_id) ||
    row.request_id === row.creation_request_id ||
    !id(row.integration_id, 'vlt') ||
    !id(row.revision_id, 'vlr')
  )
    return fail()
  const cleanup = object(row.cleanup, ['state', 'observation'])
  const result: ProviderCleanupReceipt = {
    creation_request_id: row.creation_request_id,
    request_id: row.request_id,
    integration_id: row.integration_id as string,
    revision_id: row.revision_id as string,
    state: oneOf(row.state, ['pending', 'unknown', 'failed', 'acknowledged'] as const),
    ownership: observation(row.ownership),
    cleanup: {
      state: oneOf(cleanup.state, ['not_attempted', 'unknown', 'failed', 'acknowledged'] as const),
      observation: observation(cleanup.observation),
    },
    started_at: date(row.started_at),
    finished_at: row.finished_at === null ? null : date(row.finished_at),
  }
  if (
    (result.finished_at && Date.parse(result.finished_at) < Date.parse(result.started_at)) ||
    (result.state === 'acknowledged' &&
      (!result.finished_at ||
        !result.ownership.succeeded ||
        result.cleanup.state !== 'acknowledged' ||
        !result.cleanup.observation.succeeded))
  )
    return fail()
  return result
}
function target(integration: string, creation?: string) {
  if (!id(integration, 'vlt') || (creation !== undefined && !uuid.test(creation))) return fail()
  return (
    `/admin/secrets/integrations/${integration}/provider-orphans` + (creation ? '/' + creation : '')
  )
}
async function request(
  method: 'get' | 'post',
  url: string,
  signal?: AbortSignal,
  data?: unknown,
  headers?: Record<string, string>,
) {
  try {
    return await client.request({ method, url, signal, data, headers })
  } catch (error) {
    throw new ProviderCleanupError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export async function getProviderOrphans(
  integration: string,
  cursor = '',
  signal?: AbortSignal,
): Promise<ProviderOrphanPage> {
  if (
    cursor &&
    (cursor.length !== 101 ||
      !new RegExp(`^${uuid.source.slice(1, -1)}\\.[0-9a-f]{64}$`).test(cursor))
  )
    return fail()
  const response = await request(
    'get',
    target(integration) + '?limit=20' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''),
    signal,
  )
  if (response.status !== 200) return fail()
  const page = object(response.data, ['items', 'next_cursor'])
  if (
    !Array.isArray(page.items) ||
    page.items.length > 20 ||
    (page.next_cursor !== null &&
      (typeof page.next_cursor !== 'string' ||
        page.next_cursor.length !== 101 ||
        !new RegExp(`^${uuid.source.slice(1, -1)}\\.[0-9a-f]{64}$`).test(page.next_cursor)))
  )
    return fail()
  const items = page.items.map(parseProviderOrphan)
  if (
    items.some((row) => row.integration_id !== integration) ||
    new Set(items.map((row) => row.creation_request_id)).size !== items.length
  )
    return fail()
  return { items, next_cursor: page.next_cursor as string | null }
}
export async function getProviderOrphan(
  integration: string,
  creation: string,
  signal?: AbortSignal,
) {
  const response = await request('get', target(integration, creation), signal)
  if (response.status !== 200) return fail()
  const row = parseProviderOrphan(response.data)
  if (
    row.integration_id !== integration ||
    row.creation_request_id !== creation ||
    response.headers.etag !== `"${row.review_etag}"`
  )
    return fail()
  return row
}
function receiptIdentity(receipt: ProviderCleanupReceipt, intent: ProviderCleanupIntent) {
  if (
    receipt.integration_id !== intent.integration_id ||
    receipt.creation_request_id !== intent.creation_request_id ||
    receipt.request_id !== intent.input.request_id ||
    receipt.revision_id !== intent.revision_id
  )
    return fail()
  return receipt
}
export async function getProviderCleanupReceipt(
  intent: ProviderCleanupIntent,
  signal?: AbortSignal,
) {
  if (!uuid.test(intent.input.request_id)) return fail()
  const response = await request(
    'get',
    target(intent.integration_id, intent.creation_request_id) +
      '/commands/' +
      intent.input.request_id,
    signal,
  )
  if (response.status !== 200) return fail()
  return receiptIdentity(parseProviderCleanupReceipt(response.data), intent)
}
export async function cleanupProviderOrphan(
  intent: ProviderCleanupIntent,
  csrf: string,
  signal?: AbortSignal,
  token?: string,
) {
  object(intent, ['integration_id', 'creation_request_id', 'revision_id', 'etag', 'input'])
  object(intent.input, ['request_id', 'reason'])
  if (
    !id(intent.revision_id, 'vlr') ||
    !review.test(intent.etag) ||
    !uuid.test(intent.input.request_id) ||
    intent.input.request_id === intent.creation_request_id ||
    !validVaultReason(intent.input.reason) ||
    !/^[0-9a-f]{64}$/.test(csrf) ||
    (token !== undefined && !validVaultToken(token))
  )
    return fail()
  const response = await request(
    'post',
    target(intent.integration_id, intent.creation_request_id) + '/cleanup',
    signal,
    { ...intent.input, ...(token !== undefined ? { cleanup_token: token } : {}) },
    { 'If-Match': `"${intent.etag}"`, 'X-CSRF-Token': csrf },
  )
  if (response.status !== 200 && response.status !== 202) return fail()
  const row = object(response.data, ['receipt', 'running'])
  const receipt = receiptIdentity(parseProviderCleanupReceipt(row.receipt), intent)
  const running = boolean(row.running)
  if (running !== (receipt.state === 'pending') || (response.status === 202) !== running)
    return fail()
  return { receipt, running }
}
