import { AxiosHeaders, type RawAxiosHeaders } from 'axios'
import client from './client'
import type {
  CredentialAttemptStatistics,
  CredentialAttemptTarget,
} from '@/types/credential-attempt-statistics'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const keys = (value: Record<string, unknown>, expected: string[]) =>
  Object.keys(value).length === expected.length &&
  expected.every((key) => Object.hasOwn(value, key))
const id = (value: unknown, prefix: string): value is string =>
  typeof value === 'string' &&
  value.startsWith(prefix) &&
  value.length > prefix.length &&
  /^[A-Za-z0-9_-]{1,30}$(?![\s\S])/.test(value)
const bounded = (value: unknown): value is number =>
  typeof value === 'number' && Number.isInteger(value) && value >= 0 && value <= 100
function invalid(): never {
  throw new Error('Credential attempt statistics unavailable')
}
function timestamp(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?Z$(?![\s\S])/.exec(
    value,
  )
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
function timestampOrder(value: string) {
  // UTC/calendar validation precedes this comparison; retain RFC3339Nano precision.
  const [whole, fraction = ''] = value.slice(0, -1).split('.')
  return `${whole}.${fraction.padEnd(9, '0')}Z`
}
// Machine-owned codes only; raw stored/upstream messages never reach this view.
const errorCodes = new Set([
  'invalid_session',
  'session_access_denied',
  'unsupported_input',
  'quota_exceeded',
  'quota_history_incomplete',
  'quota_usage_unknown',
  'quota_currency_mismatch',
  'quota_bound_unavailable',
  'quota_price_unavailable',
  'quota_request_unsupported',
  'process_interrupted',
  'event_buffer_unavailable',
  'invalid_request',
  'invalid_request_error',
  'invalid_api_key',
  'rate_limit_exceeded',
  'concurrency_limit_exceeded',
  'ip_not_allowed',
  'service_unavailable',
  'unauthorized',
  'forbidden',
  'model_not_found',
  'no_route',
  'upstream_error',
  'upstream_timeout',
  'upstream_unavailable',
  'invalid_upstream_response',
  'canceled',
  'internal_error',
  'invalid_attachment_reference',
  'unsupported_attachment_reference',
  'attachment_limit_exceeded',
  'project_attachment_unsupported',
  'attachment_type_unsupported',
  'attachment_not_found',
  'attachment_storage_unavailable',
  'attachment_storage_timeout',
])
export function decodeCredentialAttemptStatistics(value: unknown): CredentialAttemptStatistics {
  if (
    !object(value) ||
    !keys(value, ['provider_id', 'observed_at', 'attempt_limit', 'recorded_only', 'items']) ||
    !id(value.provider_id, 'prv_') ||
    !timestamp(value.observed_at) ||
    value.attempt_limit !== 100 ||
    value.recorded_only !== true ||
    !Array.isArray(value.items) ||
    value.items.length < 1 ||
    value.items.length > 20
  )
    invalid()
  const seen = new Set<string>()
  for (const item of value.items) {
    if (
      !object(item) ||
      !keys(item, [
        'credential_id',
        'connection_id',
        'inspected_attempts',
        'has_more',
        'failure_streak',
        'recent_error',
      ]) ||
      !id(item.credential_id, 'crd_') ||
      seen.has(item.credential_id) ||
      !id(item.connection_id, 'con_') ||
      !bounded(item.inspected_attempts) ||
      typeof item.has_more !== 'boolean' ||
      (item.has_more && item.inspected_attempts !== 100) ||
      !object(item.failure_streak) ||
      !object(item.recent_error)
    )
      invalid()
    seen.add(item.credential_id)
    const streak = item.failure_streak,
      recent = item.recent_error
    if (
      !keys(streak, ['state', 'count', 'lower_bound']) ||
      !bounded(streak.lower_bound) ||
      streak.lower_bound > item.inspected_attempts ||
      !['no_records', 'exact', 'lower_bound', 'unknown'].includes(String(streak.state)) ||
      !keys(recent, ['state', 'code', 'completed_at']) ||
      !['no_records', 'recorded', 'none', 'unknown'].includes(String(recent.state))
    )
      invalid()
    if (item.inspected_attempts === 0) {
      if (
        item.has_more ||
        streak.state !== 'no_records' ||
        streak.count !== null ||
        streak.lower_bound !== 0 ||
        recent.state !== 'no_records'
      )
        invalid()
    } else if (streak.state === 'no_records' || recent.state === 'no_records') invalid()
    if (streak.state === 'exact') {
      if (
        !bounded(streak.count) ||
        streak.count !== streak.lower_bound ||
        (item.has_more && streak.count === 100)
      )
        invalid()
    } else if (streak.count !== null) invalid()
    if (streak.state === 'lower_bound' && (!item.has_more || streak.lower_bound !== 100)) invalid()
    if (recent.state === 'recorded') {
      if (
        !timestamp(recent.completed_at) ||
        timestampOrder(recent.completed_at) > timestampOrder(value.observed_at) ||
        (recent.code !== null && (typeof recent.code !== 'string' || !errorCodes.has(recent.code)))
      )
        invalid()
    } else if (
      recent.code !== null ||
      recent.completed_at !== null ||
      (recent.state === 'none' && item.has_more)
    )
      invalid()
  }
  return value as unknown as CredentialAttemptStatistics
}
export async function getCredentialAttemptStatistics(
  providerId: string,
  targets: readonly CredentialAttemptTarget[],
  signal?: AbortSignal,
): Promise<CredentialAttemptStatistics> {
  if (
    !id(providerId, 'prv_') ||
    targets.length < 1 ||
    targets.length > 20 ||
    signal?.aborted ||
    targets.some(
      (target) => !id(target.credentialId, 'crd_') || !id(target.connectionId, 'con_'),
    ) ||
    new Set(targets.map((target) => target.credentialId)).size !== targets.length
  )
    invalid()
  const params = new URLSearchParams()
  targets.forEach((target) => params.append('credential_id', target.credentialId))
  const response = await client.get<unknown>(
    `/admin/providers/${encodeURIComponent(providerId)}/credential-attempt-statistics`,
    { params, signal },
  )
  const control = AxiosHeaders.from(response.headers as RawAxiosHeaders).get('Cache-Control')
  const parts =
    typeof control === 'string' ? control.split(',').map((part) => part.trim().toLowerCase()) : []
  if (
    response.status !== 200 ||
    signal?.aborted ||
    parts.length !== 2 ||
    !parts.includes('private') ||
    !parts.includes('no-store')
  )
    invalid()
  const result = decodeCredentialAttemptStatistics(response.data)
  if (
    result.provider_id !== providerId ||
    result.items.length !== targets.length ||
    result.items.some(
      (item, index) =>
        item.credential_id !== targets[index].credentialId ||
        item.connection_id !== targets[index].connectionId,
    )
  )
    invalid()
  return result
}
