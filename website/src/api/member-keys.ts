import { AxiosHeaders } from 'axios'
import client from './client'
import type { MemberKeyDisableResult, MemberKeyPage, MemberKeyRecord } from '@/types/member-keys'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const fields = (v: Record<string, unknown>, keys: string[]) =>
  Object.keys(v).length === keys.length && keys.every((k) => Object.hasOwn(v, k))
const identity = (v: unknown, prefix: string) =>
  typeof v === 'string' && new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`).test(v)
const etag = (v: unknown) => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
const integer = (v: unknown) => typeof v === 'string' && /^(0|[1-9]\d{0,127})$/.test(v)
const decimal = (v: unknown) =>
  typeof v === 'string' && /^(0|[1-9]\d{0,127})(?:\.\d{1,18})?$/.test(v)
const currency = (v: unknown) => typeof v === 'string' && /^[A-Z]{3}$/.test(v)
const stamp = (v: unknown) =>
  typeof v === 'string' &&
  /^\d{4}-\d\d-\d\dT.*(?:Z|[+-]\d\d:\d\d)$/.test(v) &&
  Number.isFinite(Date.parse(v))
const nullableStamp = (v: unknown) => v === null || stamp(v)
function amounts(v: unknown) {
  return object(v) && Object.entries(v).every(([c, n]) => currency(c) && decimal(n))
}
function window(v: unknown) {
  return (
    object(v) &&
    fields(v, [
      'covered',
      'tokens_used',
      'tokens_held',
      'tokens_unknown',
      'money_used',
      'money_held',
      'money_unknown',
    ]) &&
    typeof v.covered === 'boolean' &&
    ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].every((k) => integer(v[k])) &&
    amounts(v.money_used) &&
    amounts(v.money_held)
  )
}
function usage(v: unknown) {
  if (
    !object(v) ||
    !fields(v, [
      'as_of',
      'activated',
      'coverage_start',
      'time_zone',
      'active',
      'minute',
      'five_hours',
      'seven_days',
      'month',
    ]) ||
    !nullableStamp(v.as_of) ||
    !nullableStamp(v.coverage_start) ||
    typeof v.activated !== 'boolean' ||
    typeof v.time_zone !== 'string' ||
    v.time_zone.length > 100
  )
    return false
  try {
    new Intl.DateTimeFormat('en', { timeZone: v.time_zone }).format()
  } catch {
    return false
  }
  return ['active', 'minute', 'five_hours', 'seven_days', 'month'].every(
    (k) => v[k] === null || window(v[k]),
  )
}
function policy(v: unknown) {
  const maxima = ['tokens_5h', 'tokens_7d', 'tokens_month', 'tpm', 'rpm', 'concurrency']
  return (
    object(v) &&
    fields(v, [...maxima, 'money_month', 'currency']) &&
    maxima.every(
      (k) => v[k] === null || (typeof v[k] === 'number' && Number.isSafeInteger(v[k]) && v[k] >= 0),
    ) &&
    (v.money_month === null ? v.currency === '' : decimal(v.money_month) && currency(v.currency))
  )
}
function limits(v: unknown) {
  return (
    object(v) &&
    fields(v, [
      'platform_currency',
      'quota_root_id',
      'shared_rotation_quota',
      'stored',
      'effective',
      'quota_usage',
      'rpm_used',
      'active',
      'enforced',
    ]) &&
    currency(v.platform_currency) &&
    identity(v.quota_root_id, 'key') &&
    typeof v.shared_rotation_quota === 'boolean' &&
    policy(v.stored) &&
    policy(v.effective) &&
    (v.quota_usage === null || usage(v.quota_usage)) &&
    (v.rpm_used === null || integer(v.rpm_used)) &&
    (v.active === null || integer(v.active)) &&
    typeof v.enforced === 'boolean'
  )
}
function record(v: unknown): v is MemberKeyRecord {
  return (
    object(v) &&
    fields(v, [
      'id',
      'name',
      'status',
      'expired',
      'model_ids',
      'expires_at',
      'created_at',
      'updated_at',
      'etag',
      'disable_eligible',
      'last_used_at',
      'last_use_coverage',
      'limits',
    ]) &&
    identity(v.id, 'key') &&
    typeof v.name === 'string' &&
    v.name.length <= 256 &&
    ['pending', 'active', 'disabled', 'revoked'].includes(v.status as string) &&
    typeof v.expired === 'boolean' &&
    Array.isArray(v.model_ids) &&
    v.model_ids.length <= 8192 &&
    v.model_ids.every((id) => typeof id === 'string' && /^mdl_[A-Za-z0-9_-]{1,26}$/.test(id)) &&
    new Set(v.model_ids).size === v.model_ids.length &&
    nullableStamp(v.expires_at) &&
    stamp(v.created_at) &&
    stamp(v.updated_at) &&
    etag(v.etag) &&
    typeof v.disable_eligible === 'boolean' &&
    (!v.disable_eligible || (v.status === 'active' && !v.expired)) &&
    ['recorded', 'no_recorded_use', 'unknown'].includes(v.last_use_coverage as string) &&
    (v.last_use_coverage === 'recorded' ? stamp(v.last_used_at) : v.last_used_at === null) &&
    limits(v.limits)
  )
}
const responseETag = (headers: unknown) =>
  headers instanceof AxiosHeaders
    ? headers.get('etag')
    : object(headers)
      ? (headers.etag ?? headers.ETag)
      : undefined
const invalid = () => new Error('Member Key response unavailable')
const path = (userId: string) => {
  if (!identity(userId, 'usr')) throw invalid()
  return `/admin/members/${encodeURIComponent(userId)}/keys`
}
export async function listMemberKeys(
  userId: string,
  cursor: string | null,
  signal?: AbortSignal,
): Promise<MemberKeyPage> {
  if (cursor !== null && !identity(cursor, 'key')) throw invalid()
  const data: unknown = (
    await client.get(path(userId), { params: { limit: 40, cursor: cursor ?? undefined }, signal })
  ).data
  if (!object(data) || !fields(data, ['items', 'next_cursor']) || !Array.isArray(data.items))
    throw invalid()
  const items = data.items
  if (items.length > 40 || !items.every(record)) throw invalid()
  if (
    new Set(items.map((r) => r.id)).size !== items.length ||
    items.some(
      (r, i) => (cursor !== null && r.id >= cursor) || (i > 0 && r.id >= items[i - 1].id),
    ) ||
    (data.next_cursor !== null &&
      (!identity(data.next_cursor, 'key') || data.next_cursor !== items.at(-1)?.id))
  )
    throw invalid()
  return data as unknown as MemberKeyPage
}
export async function getMemberKey(
  userId: string,
  keyId: string,
  signal?: AbortSignal,
): Promise<MemberKeyRecord> {
  if (!identity(keyId, 'key')) throw invalid()
  const response = await client.get(`${path(userId)}/${encodeURIComponent(keyId)}`, { signal })
  if (
    !record(response.data) ||
    response.data.id !== keyId ||
    responseETag(response.headers) !== `"${response.data.etag}"`
  )
    throw invalid()
  return response.data
}
export function validMemberKeyReason(reason: string) {
  return (
    !!reason.trim() &&
    new TextEncoder().encode(reason.trim()).length <= 1024 &&
    !/[\p{Cc}\p{Cs}]/u.test(reason.trim())
  )
}
export async function disableMemberKey(
  userId: string,
  keyId: string,
  revision: string,
  reason: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<MemberKeyDisableResult> {
  if (!identity(keyId, 'key') || !etag(revision) || !validMemberKeyReason(reason) || !csrf)
    throw invalid()
  const response = await client.post(
    `${path(userId)}/${encodeURIComponent(keyId)}/disable`,
    { reason },
    { signal, headers: { 'If-Match': `"${revision}"`, 'X-CSRF-Token': csrf } },
  )
  const v: unknown = response.data
  if (
    !object(v) ||
    !fields(v, ['user_id', 'id', 'status', 'etag', 'runtime_applied', 'confirmation']) ||
    v.user_id !== userId ||
    v.id !== keyId ||
    v.status !== 'disabled' ||
    !etag(v.etag) ||
    v.runtime_applied !== true ||
    v.confirmation !== 'current_disabled_state' ||
    responseETag(response.headers) !== `"${v.etag}"`
  )
    throw invalid()
  return v as unknown as MemberKeyDisableResult
}
