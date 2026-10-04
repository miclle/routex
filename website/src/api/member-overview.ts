import client from './client'
import type { MemberOverviewRecord } from '@/types/member-overview'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const count = (value: unknown) => typeof value === 'string' && /^(0|[1-9]\d{0,127})$/.test(value)
const decimal = (value: unknown) =>
  typeof value === 'string' && value.length <= 256 && /^\d+(?:\.\d+)?$/.test(value)
const currency = (value: unknown) => typeof value === 'string' && /^[A-Z]{3}$/.test(value)
const stamp = (value: unknown): value is string =>
  typeof value === 'string' &&
  /^\d{4}-\d\d-\d\dT.*(?:Z|[+-]\d\d:\d\d)$/.test(value) &&
  Number.isFinite(Date.parse(value))
const amounts = (value: unknown) =>
  object(value) && Object.entries(value).every(([key, amount]) => currency(key) && decimal(amount))
const identifier = (value: unknown, max: number) =>
  typeof value === 'string' && value.length > 0 && value.length <= max
function usage(value: unknown) {
  if (!object(value) || !stamp(value.month_start) || !stamp(value.month_end)) return false
  try {
    if (typeof value.time_zone !== 'string' || value.time_zone === 'Local') return false
    new Intl.DateTimeFormat('en', { timeZone: value.time_zone }).format()
  } catch {
    return false
  }
  return (
    stamp(value.as_of) &&
    Date.parse(value.month_start) < Date.parse(value.month_end) &&
    typeof value.covered === 'boolean' &&
    ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].every((key) =>
      count(value[key]),
    ) &&
    amounts(value.money_used) &&
    amounts(value.money_held)
  )
}
function account(value: unknown) {
  return (
    object(value) &&
    identifier(value.account_id, 64) &&
    identifier(value.policy_etag, 64) &&
    (value.tokens_month === null || count(value.tokens_month)) &&
    (value.money_month === null || decimal(value.money_month)) &&
    (value.money_month === null ? value.currency === null : currency(value.currency)) &&
    typeof value.runtime_applied === 'boolean' &&
    ['active', 'inactive', 'unavailable'].includes(value.usage_status as string) &&
    (value.usage_status === 'active'
      ? usage(value.usage) &&
        object(value.active_reservations) &&
        count(value.active_reservations.tokens_held) &&
        amounts(value.active_reservations.money_held)
      : value.usage === null && value.active_reservations === null)
  )
}
export const memberOverviewKey = (actor: string, target: string, generation: number) =>
  ['admin', 'member-overview', actor, target, generation] as const

export async function getMemberOverview(target: string, signal?: AbortSignal) {
  if (!/^[A-Za-z0-9_-]{1,30}$/.test(target)) throw new Error('Invalid member identity')
  const data: unknown = (
    await client.get(`/admin/members/${encodeURIComponent(target)}/overview`, { signal })
  ).data
  if (
    !object(data) ||
    data.user_id !== target ||
    !stamp(data.observed_at) ||
    !data.observed_at.endsWith('Z') ||
    !currency(data.platform_currency) ||
    !account(data.personal) ||
    !count(data.total_personal_keys)
  )
    throw new Error('Invalid member overview response')
  return data as unknown as MemberOverviewRecord
}
