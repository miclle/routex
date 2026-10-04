import client from './client'
import type { MemberTeamsPage } from '@/types/member-teams'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const identity = (v: unknown, prefix: string) =>
  typeof v === 'string' && new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`).test(v)
const count = (v: unknown) => typeof v === 'string' && /^(0|[1-9]\d{0,127})$/.test(v)
const decimal = (v: unknown) =>
  typeof v === 'string' && v.length <= 256 && /^\d+(?:\.\d+)?$/.test(v)
const currency = (v: unknown) => typeof v === 'string' && /^[A-Z]{3}$/.test(v)
const stamp = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^\d{4}-\d\d-\d\dT.*(?:Z|[+-]\d\d:\d\d)$/.test(v) &&
  Number.isFinite(Date.parse(v))
const amounts = (v: unknown) =>
  object(v) && Object.entries(v).every(([k, a]) => currency(k) && decimal(a))
function policy(v: unknown) {
  return (
    object(v) &&
    ['tokens_month', 'rpm', 'tpm', 'concurrency'].every((k) => v[k] === null || count(v[k])) &&
    (v.money_month === null || decimal(v.money_month)) &&
    (v.money_month === null ? v.currency === null : currency(v.currency))
  )
}
function usage(v: unknown) {
  if (!object(v) || !stamp(v.month_start) || !stamp(v.month_end) || !stamp(v.as_of)) return false
  try {
    if (typeof v.time_zone !== 'string' || !v.time_zone || v.time_zone === 'Local') return false
    new Intl.DateTimeFormat('en', { timeZone: v.time_zone }).format()
  } catch {
    return false
  }
  return (
    Date.parse(v.month_start) < Date.parse(v.month_end) &&
    typeof v.covered === 'boolean' &&
    ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].every((k) => count(v[k])) &&
    amounts(v.money_used) &&
    amounts(v.money_held)
  )
}
function row(v: unknown, observed: string) {
  if (!object(v) || !object(v.limits)) return false
  const l = v.limits
  return (
    identity(v.id, 'tea') &&
    typeof v.name === 'string' &&
    v.name.length > 0 &&
    ['active', 'disabled', 'archived'].includes(v.status as string) &&
    identity(v.membership_id, 'tmm') &&
    ['owner', 'member'].includes(v.membership_role as string) &&
    ['active', 'disabled'].includes(v.membership_status as string) &&
    (v.joined_at === null ||
      (stamp(v.joined_at) &&
        v.joined_at.endsWith('Z') &&
        Date.parse(v.joined_at) <= Date.parse(observed))) &&
    typeof l.policy_recorded === 'boolean' &&
    typeof l.policy_etag === 'string' &&
    /^[A-Za-z0-9_-]{1,64}$/.test(l.policy_etag) &&
    policy(l.stored) &&
    policy(l.parent_stored) &&
    (l.policy_recorded ||
      (l.policy_etag === '0' && Object.values(l.stored as object).every((x) => x === null))) &&
    typeof l.runtime_applied === 'boolean' &&
    ['active', 'inactive', 'unavailable'].includes(l.usage_status as string) &&
    (!l.runtime_applied ||
      (v.status === 'active' && v.membership_status === 'active' && l.usage_status === 'active')) &&
    (l.usage_status === 'active'
      ? usage(l.usage) &&
        object(l.active_reservations) &&
        count(l.active_reservations.tokens_held) &&
        amounts(l.active_reservations.money_held)
      : l.usage === null && l.active_reservations === null)
  )
}
export const memberTeamsKey = (
  actor: string,
  target: string,
  generation: number,
  authority: string,
) => ['admin', 'member-teams', actor, target, generation, authority] as const
export function validMemberTeamsCursor(v: unknown): v is string {
  if (typeof v !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(v)) return false
  try {
    return (
      btoa(atob(v.replace(/-/g, '+').replace(/_/g, '/')))
        .replace(/=/g, '')
        .replace(/\+/g, '-')
        .replace(/\//g, '_') === v
    )
  } catch {
    return false
  }
}
export function validateMemberTeamsPage(
  data: unknown,
  target: string,
  previous: MemberTeamsPage[] = [],
): MemberTeamsPage {
  if (
    !object(data) ||
    data.user_id !== target ||
    !stamp(data.observed_at) ||
    !data.observed_at.endsWith('Z') ||
    !currency(data.platform_currency) ||
    !Array.isArray(data.items) ||
    data.items.length > 20 ||
    !data.items.every((v) => row(v, data.observed_at as string)) ||
    !(data.next_cursor === null || validMemberTeamsCursor(data.next_cursor))
  )
    throw new Error('Invalid member Teams response')
  const pages = [...previous, data as unknown as MemberTeamsPage]
  const rows = pages.flatMap((p) => p.items)
  const cursors = pages.flatMap((p) => (p.next_cursor ? [p.next_cursor] : []))
  if (
    new Set(rows.map((r) => r.id)).size !== rows.length ||
    new Set(rows.map((r) => r.membership_id)).size !== rows.length ||
    new Set(cursors).size !== cursors.length
  )
    throw new Error('Repeated member Teams page')
  return data as unknown as MemberTeamsPage
}
export async function getMemberTeams(target: string, cursor: string | null, signal?: AbortSignal) {
  if (!identity(target, 'usr') || (cursor !== null && !validMemberTeamsCursor(cursor)))
    throw new Error('Invalid member Teams identity or cursor')
  return validateMemberTeamsPage(
    (
      await client.get(`/admin/members/${encodeURIComponent(target)}/teams`, {
        params: { limit: 20, ...(cursor ? { cursor } : {}) },
        signal,
      })
    ).data,
    target,
  )
}
