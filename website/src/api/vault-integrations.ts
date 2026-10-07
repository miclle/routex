import axios from 'axios'
import client from './client'
import type {
  VaultConfigIntent,
  VaultDescriptor,
  VaultIntegration,
  VaultObservation,
  VaultPage,
  VaultProbe,
  VaultProbeIntent,
  VaultSaved,
} from '@/types/vault-integrations'

export class VaultError extends Error {
  constructor(public readonly status: number) {
    super('Vault integration request failed')
  }
}
const identity = /^vlt_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const revision = /^vlr_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const probeIdentity = /^[0-9a-f]{32}$/
const review = /^[0-9a-f]{64}\.[0-9a-f]{64}$/
const csrfPattern = /^[0-9a-f]{64}$/
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
function fail(): never {
  throw new VaultError(0)
}
function object(
  value: unknown,
  fields: string[],
  optional: string[] = [],
): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail()
  const row = value as Record<string, unknown>
  if (
    fields.some((key) => !Object.hasOwn(row, key)) ||
    Object.keys(row).some((key) => !fields.includes(key) && !optional.includes(key))
  )
    return fail()
  return row
}
function text(value: unknown, maximum = 128, pattern?: RegExp, empty = false): string {
  if (
    typeof value !== 'string' ||
    (!empty && !value) ||
    [...value].length > maximum ||
    /[\p{Cc}\p{Cs}]/u.test(value) ||
    (pattern && !pattern.test(value))
  )
    return fail()
  return value
}
function bool(value: unknown): boolean {
  return typeof value === 'boolean' ? value : fail()
}
function oneOf<T extends string>(value: unknown, values: readonly T[]): T {
  return values.includes(value as T) ? (value as T) : fail()
}
function date(value: unknown): string {
  const result = text(
    value,
    40,
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/,
  )
  return Number.isFinite(Date.parse(result)) ? result : fail()
}
export function validVaultToken(value: string): boolean {
  return /^[\x21-\x7e]{1,4096}$/.test(value)
}
export function validVaultName(value: string): boolean {
  return (
    value.length > 0 &&
    !/^\p{White_Space}|\p{White_Space}$/u.test(value) &&
    [...value].length <= 100 &&
    !/[\p{Cc}\p{Cs}]/u.test(value)
  )
}
export function validVaultReason(value: string): boolean {
  return (
    value.length > 0 &&
    !/^\p{White_Space}|\p{White_Space}$/u.test(value) &&
    [...value].length <= 1000 &&
    !/[\p{Cc}\p{Cs}]/u.test(value)
  )
}
function segment(value: string): boolean {
  return (
    /^[A-Za-z0-9_.-]{1,128}$/.test(value) && value !== '.' && value !== '..' && !value.endsWith('.')
  )
}
function path(value: string, max: number, empty = false): boolean {
  return value === '' ? empty : value.length <= max && value.split('/').every(segment)
}
export function validVaultDescriptor(value: VaultDescriptor): boolean {
  try {
    const url = new URL(value.endpoint)
    const rawPath = value.endpoint.match(/^https?:\/\/[^/]+(\/.*)?$/)?.[1] ?? ''
    return (
      value.endpoint.length <= 2048 &&
      ['http:', 'https:'].includes(url.protocol) &&
      !!url.hostname &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      /^[\x21-\x7e]+$/.test(value.endpoint) &&
      !rawPath.includes('//') &&
      path(rawPath.replace(/^\/+|\/+$/g, ''), 256, true) &&
      path(value.namespace, 256, true) &&
      path(value.mount, 128) &&
      path(value.prefix, 256) &&
      value.data_field.length <= 64 &&
      segment(value.data_field)
    )
  } catch {
    return false
  }
}
function descriptor(value: unknown): VaultDescriptor {
  const row = object(value, ['endpoint', 'namespace', 'mount', 'prefix', 'data_field'])
  const result = {
    endpoint: text(row.endpoint, 2048),
    namespace: text(row.namespace, 256, undefined, true),
    mount: text(row.mount, 128),
    prefix: text(row.prefix, 256, undefined, true),
    data_field: text(row.data_field, 64),
  }
  return validVaultDescriptor(result) ? result : fail()
}
function observation(value: unknown): VaultObservation {
  const row = object(value, ['attempted', 'succeeded', 'duration_ms', 'failure'])
  const failure =
    row.failure === null ? null : object(row.failure, ['stage', 'code', 'http_status'])
  const status = failure?.http_status
  if (
    failure &&
    (typeof status !== 'number' ||
      !Number.isInteger(status) ||
      (status !== 0 && (status < 100 || status > 599)))
  )
    return fail()
  const result: VaultObservation = {
    attempted: bool(row.attempted),
    succeeded: bool(row.succeeded),
    duration_ms: text(row.duration_ms, 30, /^(0|[1-9][0-9]*)(?:\.[0-9]+)?$/),
    failure: failure
      ? {
          stage: oneOf(failure.stage, ['prepare', 'write', 'read', 'cleanup'] as const),
          code: text(failure.code, 64, /^[a-z][a-z0-9_]*$/),
          http_status: status as number,
        }
      : null,
  }
  if (result.succeeded && (!result.attempted || result.failure !== null)) return fail()
  return result
}
export function parseVaultProbe(value: unknown): VaultProbe {
  const row = object(value, [
    'id',
    'request_id',
    'integration_id',
    'revision_id',
    'review_etag',
    'state',
    'version',
    'write',
    'read',
    'cleanup',
    'created_at',
    'finished_at',
  ])
  const cleanup = object(row.cleanup, ['state', 'observation'])
  if (row.version !== null && row.version !== 1) return fail()
  const result: VaultProbe = {
    id: text(row.id, 32, probeIdentity),
    request_id: text(row.request_id, 36, uuid),
    integration_id: text(row.integration_id, 30, identity),
    revision_id: text(row.revision_id, 30, revision),
    review_etag: text(row.review_etag, 129, review),
    state: oneOf(row.state, [
      'planned',
      'writing',
      'awaiting_read',
      'reading',
      'cleanup_pending',
      'completed',
      'interrupted',
    ] as const),
    version: row.version,
    write: observation(row.write),
    read: observation(row.read),
    cleanup: {
      state: oneOf(cleanup.state, ['not_attempted', 'acknowledged', 'failed', 'unknown'] as const),
      observation: observation(cleanup.observation),
    },
    created_at: date(row.created_at),
    finished_at: row.finished_at === null ? null : date(row.finished_at),
  }
  if (result.cleanup.state === 'acknowledged' && !result.cleanup.observation.succeeded)
    return fail()
  return result
}
export function parseVaultIntegration(value: unknown): VaultIntegration {
  const row = object(value, [
    'id',
    'name',
    'revision_id',
    'descriptor',
    'writer_auth',
    'reader_auth',
    'review_etag',
    'can_write',
    'can_test',
    'last_probe',
  ])
  function auth(value: unknown) {
    const item = object(value, ['method', 'configured'])
    return { method: oneOf(item.method, ['token'] as const), configured: bool(item.configured) }
  }
  const result: VaultIntegration = {
    id: text(row.id, 30, identity),
    name: text(row.name, 100),
    revision_id: text(row.revision_id, 30, revision),
    descriptor: descriptor(row.descriptor),
    writer_auth: auth(row.writer_auth),
    reader_auth: auth(row.reader_auth),
    review_etag: text(row.review_etag, 129, review),
    can_write: bool(row.can_write),
    can_test: bool(row.can_test),
    last_probe: row.last_probe === null ? null : parseVaultProbe(row.last_probe),
  }
  if (!validVaultName(result.name)) return fail()
  if (result.last_probe && result.last_probe.integration_id !== result.id) return fail()
  return result
}
export function parseVaultPage(value: unknown): VaultPage {
  const row = object(value, ['items', 'next_cursor', 'review_etag', 'can_write', 'can_test'])
  if (!Array.isArray(row.items) || row.items.length > 50) return fail()
  const items = row.items.map(parseVaultIntegration)
  if (new Set(items.map((x) => x.id)).size !== items.length) return fail()
  return {
    items,
    review_etag: text(row.review_etag, 129, review),
    can_write: bool(row.can_write),
    can_test: bool(row.can_test),
    next_cursor: row.next_cursor === null ? null : text(row.next_cursor, 30, identity),
  }
}
async function request(
  method: 'get' | 'post' | 'put',
  path: string,
  input: unknown,
  etag?: string,
  csrf?: string,
  signal?: AbortSignal,
) {
  try {
    return await client.request({
      method,
      url: path,
      data: input,
      signal,
      headers: etag ? { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } : undefined,
    })
  } catch (error) {
    throw new VaultError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function responseReview(headers: Record<string, unknown>, etag: string) {
  if (headers.etag !== `"${etag}"`) return fail()
}
export async function getVaultIntegrations(cursor = '', signal?: AbortSignal) {
  if (cursor && !identity.test(cursor)) return fail()
  const response = await request(
    'get',
    '/admin/secrets/integrations?limit=50' +
      (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''),
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200) return fail()
  const result = parseVaultPage(response.data)
  responseReview(response.headers, result.review_etag)
  return result
}
export async function getVaultIntegration(id: string, signal?: AbortSignal) {
  if (!identity.test(id)) return fail()
  const response = await request(
    'get',
    '/admin/secrets/integrations/' + id,
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200) return fail()
  const result = parseVaultIntegration(response.data)
  if (result.id !== id) return fail()
  responseReview(response.headers, result.review_etag)
  return result
}
export async function getVaultProbe(id: string, probeID: string, signal?: AbortSignal) {
  if (!identity.test(id) || !probeIdentity.test(probeID)) return fail()
  const response = await request(
    'get',
    `/admin/secrets/integrations/${id}/probes/${probeID}`,
    undefined,
    undefined,
    undefined,
    signal,
  )
  if (response.status !== 200) return fail()
  const result = parseVaultProbe(response.data)
  if (result.id !== probeID || result.integration_id !== id) return fail()
  responseReview(response.headers, result.review_etag)
  return result
}
export async function saveVaultIntegration(
  intent: VaultConfigIntent,
  csrf: string,
  signal?: AbortSignal,
): Promise<VaultSaved> {
  const input = intent.input
  object(input, ['request_id', 'name', 'descriptor', 'writer_auth', 'reader_auth', 'reason'])
  descriptor(input.descriptor)
  if (
    (intent.id && !identity.test(intent.id)) ||
    !review.test(intent.etag) ||
    !csrfPattern.test(csrf) ||
    !uuid.test(input.request_id) ||
    !validVaultName(input.name) ||
    !validVaultReason(input.reason) ||
    !validVaultDescriptor(input.descriptor)
  )
    return fail()
  for (const auth of [input.writer_auth, input.reader_auth]) {
    object(auth, auth.action === 'replace' ? ['action', 'token'] : ['action'])
    if (
      auth.action === 'replace'
        ? !validVaultToken(auth.token)
        : !['keep', 'remove'].includes(auth.action) ||
          Object.hasOwn(auth, 'token') ||
          (!intent.id && auth.action === 'keep')
    )
      return fail()
  }
  if (
    input.writer_auth.action === 'replace' &&
    input.reader_auth.action === 'replace' &&
    input.writer_auth.token === input.reader_auth.token
  )
    return fail()
  const response = await request(
    intent.id ? 'put' : 'post',
    '/admin/secrets/integrations' + (intent.id ? '/' + intent.id : ''),
    input,
    intent.etag,
    csrf,
    signal,
  )
  if (!(intent.id ? response.status === 200 : [200, 201].includes(response.status))) return fail()
  const row = object(response.data, [
    'request_id',
    'integration_id',
    'revision_id',
    'committed',
    'changed',
  ])
  const result = {
    request_id: text(row.request_id, 36, uuid),
    integration_id: text(row.integration_id, 30, identity),
    revision_id: text(row.revision_id, 30, revision),
    committed: row.committed === true ? (true as const) : fail(),
    changed: bool(row.changed),
  }
  if (result.request_id !== input.request_id || (intent.id && result.integration_id !== intent.id))
    return fail()
  return result
}
export async function runVaultProbe(intent: VaultProbeIntent, csrf: string, signal?: AbortSignal) {
  object(intent.input, ['request_id', 'reason'])
  if (
    !['write', 'read', 'cleanup'].includes(intent.action) ||
    !identity.test(intent.id) ||
    !review.test(intent.etag) ||
    !csrfPattern.test(csrf) ||
    !uuid.test(intent.input.request_id) ||
    !validVaultReason(intent.input.reason) ||
    (intent.action !== 'write' && (!intent.probe_id || !probeIdentity.test(intent.probe_id)))
  )
    return fail()
  const path =
    `/admin/secrets/integrations/${intent.id}/probes/` +
    (intent.action === 'write' ? 'write' : `${intent.probe_id}/${intent.action}`)
  const response = await request('post', path, intent.input, intent.etag, csrf, signal)
  if (![200, 202].includes(response.status)) return fail()
  const result = parseVaultProbe(response.data)
  if (
    result.integration_id !== intent.id ||
    (intent.action === 'write' && result.request_id !== intent.input.request_id) ||
    (intent.probe_id && result.id !== intent.probe_id)
  )
    return fail()
  return result
}
