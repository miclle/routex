import { AxiosHeaders, type AxiosResponse, type RawAxiosHeaders } from 'axios'
import client from './client'
import { isModelRecordedDate } from './model-metadata'
import { routingProtocols } from '@/types/model-routing'
import type {
  ModelWeightDetail,
  ModelWeightPage,
  ModelWeightReview,
  ModelWeightVersion,
  ModelWeightRow,
  ModelWeightRollbackInput,
  ModelWeightRollbackResult,
} from '@/types/model-weight-history'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const fields = (v: Record<string, unknown>, keys: string[]) =>
  Object.keys(v).length === keys.length && keys.every((k) => Object.hasOwn(v, k))
const canonical = (v: unknown, prefix: string): v is string =>
  typeof v === 'string' && new RegExp('^' + prefix + '_[0-7][0-9a-hjkmnp-tv-z]{25}$').test(v)
// Retained identities may predate canonical public IDs; preserve exact bytes, never resolve aliases.
const retained = (v: unknown, prefix: string): v is string =>
  typeof v === 'string' && v.startsWith(prefix + '_') && /^[A-Za-z0-9_-]{5,30}$/.test(v)
const nullableDate = (v: unknown) => v === null || isModelRecordedDate(v)
const nullableVersion = (v: unknown) => v === null || canonical(v, 'mwv')
const plain = (v: unknown): v is string => typeof v === 'string' && !/[\p{Cc}\p{Cs}]/u.test(v)
export const trimModelWeightReason = (v: string) =>
  v.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
export const validModelWeightReason = (v: unknown): v is string =>
  plain(v) &&
  v.length > 0 &&
  trimModelWeightReason(v) === v &&
  new TextEncoder().encode(v).length <= 1024
const uuid = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(v)
const etag = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{64}$/.test(v)
function invalid(): never {
  throw new Error('Model weight history response unavailable')
}
function bounded(v: unknown, limit = 512 * 1024) {
  if (new TextEncoder().encode(JSON.stringify(v)).length > limit) invalid()
}
function rows(v: unknown): ModelWeightRow[] {
  bounded(v)
  if (!Array.isArray(v) || v.length > 1000) invalid()
  for (let i = 0; i < v.length; i++) {
    const row = v[i]
    if (
      !object(row) ||
      !fields(row, [
        'binding_id',
        'binding_created_at',
        'provider_model_id',
        'provider_model_created_at',
        'connection_id',
        'connection_created_at',
        'provider_id',
        'provider_created_at',
        'protocol',
        'weight',
      ]) ||
      !retained(row.binding_id, 'bnd') ||
      !retained(row.provider_model_id, 'pmd') ||
      !retained(row.connection_id, 'con') ||
      !retained(row.provider_id, 'prv') ||
      ![
        'binding_created_at',
        'provider_model_created_at',
        'connection_created_at',
        'provider_created_at',
      ].every((k) => nullableDate(row[k])) ||
      !routingProtocols.includes(row.protocol as (typeof routingProtocols)[number]) ||
      !Number.isInteger(row.weight) ||
      Number(row.weight) < 0 ||
      Number(row.weight) > 100 ||
      (i > 0 && v[i - 1].binding_id >= row.binding_id)
    )
      invalid()
  }
  return v as ModelWeightRow[]
}
function version(v: unknown, modelID: string): ModelWeightVersion {
  if (
    !object(v) ||
    !fields(v, [
      'version_id',
      'model_id',
      'model_created_at',
      'captured_at',
      'source',
      'parent_version_id',
      'rollback_version_id',
      'actor_id',
      'reason',
      'binding_count',
      'valid_weight_set',
    ]) ||
    !canonical(v.version_id, 'mwv') ||
    v.model_id !== modelID ||
    !nullableDate(v.model_created_at) ||
    !isModelRecordedDate(v.captured_at) ||
    !['observed_baseline', 'legacy_editor', 'rollback'].includes(String(v.source)) ||
    !nullableVersion(v.parent_version_id) ||
    !nullableVersion(v.rollback_version_id) ||
    !retained(v.actor_id, 'usr') ||
    (v.reason !== null && !validModelWeightReason(v.reason)) ||
    !Number.isInteger(v.binding_count) ||
    Number(v.binding_count) < 0 ||
    Number(v.binding_count) > 1000 ||
    typeof v.valid_weight_set !== 'boolean'
  )
    invalid()
  return v as unknown as ModelWeightVersion
}
function target(modelID: string, versionID?: string) {
  if (!retained(modelID, 'mdl') || (versionID !== undefined && !canonical(versionID, 'mwv')))
    invalid()
  return '/admin/models/' + modelID
}
function response(response: AxiosResponse<unknown>, reviewETag?: string, limit?: number) {
  const headers = AxiosHeaders.from(response.headers as RawAxiosHeaders)
  const control = headers.get('Cache-Control')
  if (
    response.status !== 200 ||
    typeof control !== 'string' ||
    !control
      .split(',')
      .map((x) => x.trim().toLowerCase())
      .includes('private') ||
    !control
      .split(',')
      .map((x) => x.trim().toLowerCase())
      .includes('no-store')
  )
    invalid()
  if (reviewETag !== undefined && headers.get('ETag') !== '"' + reviewETag + '"') invalid()
  bounded(response.data, limit)
}
export async function listModelWeightVersions(
  modelID: string,
  cursor?: string,
  signal?: AbortSignal,
): Promise<ModelWeightPage> {
  const base = target(modelID)
  if (cursor !== undefined && (!plain(cursor) || !cursor.length)) invalid()
  const query = new URLSearchParams({ limit: '20' })
  if (cursor !== undefined) query.set('cursor', cursor)
  const res = await client.get<unknown>(base + '/weight-versions?' + query, { signal })
  response(res)
  const v = res.data
  if (
    !object(v) ||
    !fields(v, ['model_id', 'items', 'next_cursor']) ||
    v.model_id !== modelID ||
    !Array.isArray(v.items) ||
    v.items.length > 20 ||
    (v.next_cursor !== null && (!plain(v.next_cursor) || !v.next_cursor.length))
  )
    invalid()
  const items = v.items.map((x) => version(x, modelID))
  if (new Set(items.map((x) => x.version_id)).size !== items.length) invalid()
  return { ...v, items } as ModelWeightPage
}
export async function getModelWeightVersion(
  modelID: string,
  versionID: string,
  signal?: AbortSignal,
): Promise<ModelWeightDetail> {
  const res = await client.get<unknown>(
    target(modelID, versionID) + '/weight-versions/' + versionID,
    { signal },
  )
  response(res)
  const v = res.data
  if (!object(v) || !fields(v, ['version', 'weights'])) invalid()
  const summary = version(v.version, modelID),
    weights = rows(v.weights)
  if (summary.version_id !== versionID || summary.binding_count !== weights.length) invalid()
  if (summary.valid_weight_set) {
    const totals = new Map<string, number>()
    for (const row of weights) {
      if (
        !row.binding_created_at ||
        !row.provider_model_created_at ||
        !row.connection_created_at ||
        !row.provider_created_at
      )
        invalid()
      totals.set(row.protocol, (totals.get(row.protocol) ?? 0) + row.weight)
    }
    if (!summary.model_created_at || !totals.size || [...totals.values()].some((n) => n !== 100))
      invalid()
  }
  return { version: summary, weights }
}
export async function reviewModelWeightRollback(
  modelID: string,
  versionID: string,
  signal?: AbortSignal,
): Promise<ModelWeightReview> {
  const res = await client.get<unknown>(
    target(modelID, versionID) +
      '/weights/rollback-review?' +
      new URLSearchParams({ version_id: versionID }),
    { signal },
  )
  const v = res.data
  if (
    !object(v) ||
    !fields(v, [
      'model_id',
      'version_id',
      'current_version_id',
      'current_weights',
      'proposed_weights',
      'eligible',
      'can_rollback',
      'blocker_codes',
      'review_etag',
      'observed_at',
    ]) ||
    v.model_id !== modelID ||
    v.version_id !== versionID ||
    !nullableVersion(v.current_version_id) ||
    typeof v.eligible !== 'boolean' ||
    typeof v.can_rollback !== 'boolean' ||
    !Array.isArray(v.blocker_codes) ||
    v.blocker_codes.some((x) => typeof x !== 'string' || !/^[a-z0-9_]{1,100}$/.test(x)) ||
    !etag(v.review_etag) ||
    !isModelRecordedDate(v.observed_at) ||
    (v.eligible && (!v.can_rollback || v.blocker_codes.length))
  )
    invalid()
  // A review contains two independently bounded complete weight sets plus fixed metadata.
  response(res, v.review_etag, 2 * 512 * 1024 + 4096)
  return {
    ...v,
    current_weights: rows(v.current_weights),
    proposed_weights: rows(v.proposed_weights),
  } as unknown as ModelWeightReview
}
function result(
  res: AxiosResponse<unknown>,
  modelID: string,
  input: ModelWeightRollbackInput,
): ModelWeightRollbackResult {
  response(res)
  const v = res.data
  if (
    !object(v) ||
    !fields(v, ['receipt', 'application_status', 'runtime_applied']) ||
    !object(v.receipt)
  )
    invalid()
  const r = v.receipt
  if (
    !fields(r, [
      'request_id',
      'model_id',
      'version_id',
      'source_version_id',
      'saved_version_id',
      'effect',
      'reason',
      'created_at',
    ]) ||
    r.model_id !== modelID ||
    r.version_id !== input.version_id ||
    r.request_id !== input.request_id ||
    r.reason !== input.reason ||
    !nullableVersion(r.source_version_id) ||
    !nullableVersion(r.saved_version_id) ||
    !['changed', 'noop'].includes(String(r.effect)) ||
    !isModelRecordedDate(r.created_at) ||
    !['applied', 'pending', 'superseded', 'unknown'].includes(String(v.application_status)) ||
    typeof v.runtime_applied !== 'boolean' ||
    v.runtime_applied !== (v.application_status === 'applied')
  )
    invalid()
  return v as unknown as ModelWeightRollbackResult
}
function command(input: ModelWeightRollbackInput) {
  if (
    !canonical(input.version_id, 'mwv') ||
    !uuid(input.request_id) ||
    !validModelWeightReason(input.reason)
  )
    invalid()
}
export async function rollbackModelWeights(
  modelID: string,
  reviewETag: string,
  input: ModelWeightRollbackInput,
  csrf: string,
  signal?: AbortSignal,
) {
  command(input)
  if (!etag(reviewETag) || !csrf) invalid()
  return result(
    await client.post<unknown>(target(modelID, input.version_id) + '/weights/rollback', input, {
      signal,
      headers: { 'If-Match': '"' + reviewETag + '"', 'X-CSRF-Token': csrf },
    }),
    modelID,
    input,
  )
}
export async function getModelWeightRollbackCommand(
  modelID: string,
  input: ModelWeightRollbackInput,
  signal?: AbortSignal,
) {
  command(input)
  return result(
    await client.get<unknown>(
      target(modelID, input.version_id) + '/weights/rollback-commands/' + input.request_id,
      { signal },
    ),
    modelID,
    input,
  )
}
