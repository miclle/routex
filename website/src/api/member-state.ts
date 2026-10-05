import { AxiosHeaders, type AxiosResponse } from 'axios'
import client from './client'
import type { MemberStateInput, MemberStateRecord, MemberStateResult } from '@/types/member-state'

const identity = (v: unknown) => typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const proof = (v: unknown) => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v)
const invalid = () => new Error('Member state unavailable')
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
function fields(v: Record<string, unknown>, keys: string[]) {
  return Object.keys(v).length === keys.length && keys.every((k) => Object.hasOwn(v, k))
}
function instant(v: unknown): v is string {
  if (typeof v !== 'string') return false
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?Z$/.exec(v)
  if (!match) return false
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number)
  if (year < 1 || month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return false
  const date = new Date(0)
  date.setUTCFullYear(year, month - 1, day)
  date.setUTCHours(hour, minute, second, 0)
  return (
    date.getUTCFullYear() === year &&
    date.getUTCMonth() === month - 1 &&
    date.getUTCDate() === day &&
    day >= 1 &&
    !(
      year === 1 &&
      month === 1 &&
      day === 1 &&
      hour === 0 &&
      minute === 0 &&
      second === 0 &&
      !/[1-9]/.test(match[7] ?? '')
    )
  )
}
function record(v: unknown, target: string, result: boolean): v is MemberStateRecord {
  if (
    !identity(target) ||
    !object(v) ||
    !fields(v, [
      'user_id',
      'name',
      'base_role',
      'disabled',
      'offboarded_at',
      'status',
      'can_change_base_role',
      'can_change_status',
      'activation_mode',
      'etag',
      'account_access_runtime_applied',
      ...(result ? ['confirmation', 'effect'] : []),
    ])
  )
    return false
  const status = v.offboarded_at !== null ? 'offboarded' : v.disabled ? 'disabled' : 'active'
  return (
    v.user_id === target &&
    typeof v.name === 'string' &&
    !/\p{Cs}/u.test(v.name) &&
    new TextEncoder().encode(v.name).length <= 65536 &&
    ['member', 'admin'].includes(v.base_role as string) &&
    typeof v.disabled === 'boolean' &&
    (v.offboarded_at === null || instant(v.offboarded_at)) &&
    (v.offboarded_at === null || v.disabled) &&
    v.status === status &&
    typeof v.can_change_base_role === 'boolean' &&
    typeof v.can_change_status === 'boolean' &&
    v.activation_mode ===
      (status === 'active' ? null : status === 'disabled' ? 'enable' : 'reactivate') &&
    proof(v.etag) &&
    typeof v.account_access_runtime_applied === 'boolean'
  )
}
export function validateMemberState(v: unknown, target: string): MemberStateRecord {
  if (!record(v, target, false)) throw invalid()
  return v
}
export function validMemberStateReason(reason: string) {
  return (
    typeof reason === 'string' &&
    !!reason &&
    reason === reason.trim() &&
    !/[\p{Cc}\p{Cs}]/u.test(reason) &&
    new TextEncoder().encode(reason).length <= 1024
  )
}
function validInput(input: unknown): input is MemberStateInput {
  return (
    object(input) &&
    validMemberStateReason(input.reason as string) &&
    ((fields(input, ['role', 'reason']) && ['member', 'admin'].includes(input.role as string)) ||
      (fields(input, ['disabled', 'reason']) && typeof input.disabled === 'boolean'))
  )
}
export function validateMemberStateResult(
  v: unknown,
  target: string,
  input: MemberStateInput,
): MemberStateResult {
  if (
    !validInput(input) ||
    !record(v, target, true) ||
    !object(v) ||
    v.confirmation !== 'current_member_state' ||
    ('role' in input
      ? v.effect !== 'current_base_identity' || v.base_role !== input.role
      : v.effect !== 'current_account_access' ||
        v.disabled !== input.disabled ||
        v.account_access_runtime_applied !== true ||
        (!input.disabled && v.offboarded_at !== null))
  )
    throw invalid()
  return v as unknown as MemberStateResult
}
function path(target: string) {
  if (!identity(target)) throw invalid()
  return `/admin/members/${encodeURIComponent(target)}`
}
function checkResponse(response: AxiosResponse, etag: string, signal?: AbortSignal) {
  const headers = response.headers
  const header = (key: string) =>
    headers instanceof AxiosHeaders ? headers.get(key) : headers[key.toLowerCase()]
  if (
    signal?.aborted ||
    response.status !== 200 ||
    header('ETag') !== `"${etag}"` ||
    header('Cache-Control') !== 'private, no-store'
  )
    throw invalid()
}
export async function getMemberState(target: string, signal?: AbortSignal) {
  const response = await client.get<unknown>(`${path(target)}/state`, { signal })
  const data = validateMemberState(response.data, target)
  checkResponse(response, data.etag, signal)
  return data
}
export async function setMemberState(
  target: string,
  etag: string,
  input: MemberStateInput,
  csrf: string,
  signal?: AbortSignal,
) {
  if (!proof(etag) || !validInput(input) || !csrf) throw invalid()
  const response = await client.patch<unknown>(path(target), input, {
    signal,
    headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
  })
  const data = validateMemberStateResult(response.data, target, input)
  checkResponse(response, data.etag, signal)
  return data
}
