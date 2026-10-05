import client from './client'
import type { MemberDetail } from '@/types/member-recent-login'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const identity = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)

// Calendar validation avoids Date.parse silently normalizing impossible dates.
export function isRecordedLoginTimestamp(value: unknown): value is string {
  if (
    typeof value !== 'string' ||
    !/^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,6})?Z$/.test(value) ||
    /^0001-01-01T00:00:00(?:\.0{1,6})?Z$/.test(value)
  )
    return false
  const milliseconds = Date.parse(value)
  return (
    Number.isFinite(milliseconds) &&
    new Date(milliseconds).toISOString().slice(0, 19) === value.slice(0, 19)
  )
}
// Retained entity timestamps may include their recorded zone and nanosecond precision.
function isRetainedMemberTimestamp(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const parts =
    /^(?!0000)(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-](\d{2}):(\d{2}))$/.exec(
      value,
    )
  if (!parts) return false
  const year = Number(parts[1]),
    month = Number(parts[2]),
    day = Number(parts[3])
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (
    month < 1 ||
    month > 12 ||
    day < 1 ||
    day > days[month - 1] ||
    Number(parts[4]) > 23 ||
    Number(parts[5]) > 59 ||
    Number(parts[6]) > 59 ||
    (parts[8] !== 'Z' && (Number(parts[9]) > 23 || Number(parts[10]) > 59))
  )
    return false
  const instant = Date.parse(value)
  return (
    Number.isFinite(instant) &&
    (instant !== Date.parse('0001-01-01T00:00:00Z') || /[1-9]/.test(parts[7] ?? ''))
  )
}
export function isMemberRecentLogin(value: unknown): boolean {
  return (
    object(value) &&
    ((value.last_login_status === 'historical_unavailable' && value.last_login_at === null) ||
      (value.last_login_status === 'recorded' && isRecordedLoginTimestamp(value.last_login_at)))
  )
}
export function validateMemberDetail(data: unknown, target: string): MemberDetail {
  const allowed = [
    'id',
    'email',
    'name',
    'role',
    'disabled',
    'offboarded_at',
    'created_at',
    'role_ids',
    'last_login_at',
    'last_login_status',
  ]
  if (
    !identity(target) ||
    !object(data) ||
    Object.keys(data).some((key) => !allowed.includes(key)) ||
    data.id !== target ||
    typeof data.email !== 'string' ||
    data.email.length > 254 ||
    !data.email ||
    typeof data.name !== 'string' ||
    [...data.name].length > 100 ||
    !['admin', 'member'].includes(data.role as string) ||
    typeof data.disabled !== 'boolean' ||
    !(data.offboarded_at === null || isRetainedMemberTimestamp(data.offboarded_at)) ||
    !isRetainedMemberTimestamp(data.created_at) ||
    !Array.isArray(data.role_ids) ||
    data.role_ids.length > 10000 ||
    !data.role_ids.every(identity) ||
    new Set(data.role_ids).size !== data.role_ids.length ||
    !isMemberRecentLogin(data)
  )
    throw new Error('Invalid member detail response')
  return data as unknown as MemberDetail
}
export async function getMemberDetail(target: string, signal?: AbortSignal): Promise<MemberDetail> {
  if (!identity(target)) throw new Error('Invalid member identity')
  const { data } = await client.get<unknown>(`/admin/members/${encodeURIComponent(target)}`, {
    signal,
  })
  return validateMemberDetail(data, target)
}
