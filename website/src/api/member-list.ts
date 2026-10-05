import client from './client'
import type { MemberFilters } from '@/types/governance'
import type { MemberListPage } from '@/types/member-list'
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const keys = (v: Record<string, unknown>, allowed: string[]) =>
  Object.keys(v).every((k) => allowed.includes(k))
const id = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const count = (v: unknown) => typeof v === 'string' && /^(0|[1-9]\d{0,127})$/.test(v)
const decimal = (v: unknown) =>
  typeof v === 'string' && v.length <= 256 && /^\d+(?:\.\d+)?$/.test(v)
const currency = (v: unknown) => typeof v === 'string' && /^[A-Z]{3}$/.test(v)
const stamp = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^\d{4}-\d\d-\d\dT.*(?:Z|[+-]\d\d:\d\d)$/.test(v) &&
  Number.isFinite(Date.parse(v))
const label = (v: unknown, max: number) =>
  typeof v === 'string' &&
  v.trim() === v &&
  [...v].length > 0 &&
  [...v].length <= max &&
  [...v].every((char) => {
    const code = char.codePointAt(0)!
    return code >= 32 && (code < 127 || code > 159)
  })
const amounts = (v: unknown) =>
  object(v) && Object.entries(v).every(([k, a]) => currency(k) && decimal(a))
function usage(v: unknown) {
  if (
    !object(v) ||
    !stamp(v.as_of) ||
    !stamp(v.month_start) ||
    !stamp(v.month_end) ||
    Date.parse(v.month_start) >= Date.parse(v.month_end)
  )
    return false
  try {
    if (typeof v.time_zone !== 'string' || !v.time_zone || v.time_zone === 'Local') return false
    new Intl.DateTimeFormat('en', { timeZone: v.time_zone }).format()
  } catch {
    return false
  }
  return (
    keys(v, [
      'as_of',
      'time_zone',
      'month_start',
      'month_end',
      'covered',
      'tokens_used',
      'tokens_held',
      'tokens_unknown',
      'money_used',
      'money_held',
      'money_unknown',
    ]) &&
    typeof v.covered === 'boolean' &&
    ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].every((k) => count(v[k])) &&
    amounts(v.money_used) &&
    amounts(v.money_held)
  )
}
function account(v: unknown, user: Record<string, unknown>) {
  return (
    object(v) &&
    keys(v, [
      'account_id',
      'policy_etag',
      'tokens_month',
      'money_month',
      'currency',
      'runtime_applied',
      'usage_status',
      'usage',
      'active_reservations',
    ]) &&
    v.account_id === `user_${user.id}` &&
    typeof v.policy_etag === 'string' &&
    /^[A-Za-z0-9_-]{1,64}$/.test(v.policy_etag) &&
    (v.tokens_month === null || count(v.tokens_month)) &&
    (v.money_month === null || decimal(v.money_month)) &&
    (v.money_month === null ? v.currency === null : currency(v.currency)) &&
    typeof v.runtime_applied === 'boolean' &&
    (!v.runtime_applied ||
      (!user.disabled && user.offboarded_at === null && v.usage_status === 'active')) &&
    ['active', 'inactive', 'unavailable'].includes(v.usage_status as string) &&
    (user.personal_policy_stored ||
      (v.policy_etag === '0' &&
        v.tokens_month === null &&
        v.money_month === null &&
        v.currency === null)) &&
    (v.usage_status === 'active'
      ? usage(v.usage) &&
        object(v.active_reservations) &&
        keys(v.active_reservations, ['tokens_held', 'money_held']) &&
        count(v.active_reservations.tokens_held) &&
        amounts(v.active_reservations.money_held)
      : v.usage === null && v.active_reservations === null)
  )
}
function teams(v: unknown) {
  if (!object(v) || !keys(v, ['status', 'items'])) return false
  if (v.status !== 'available')
    return (
      ['not_authorized', 'overflow', 'unavailable'].includes(v.status as string) && v.items === null
    )
  return (
    Array.isArray(v.items) &&
    v.items.length <= 1000 &&
    v.items.every(
      (t) =>
        object(t) &&
        keys(t, ['id', 'name', 'status', 'membership_status', 'membership_role']) &&
        id(t.id) &&
        label(t.name, 100) &&
        ['active', 'disabled', 'archived'].includes(t.status as string) &&
        ['active', 'disabled'].includes(t.membership_status as string) &&
        ['owner', 'member'].includes(t.membership_role as string),
    ) &&
    new Set(v.items.map((t) => t.id)).size === v.items.length
  )
}
function row(v: unknown) {
  return (
    object(v) &&
    keys(v, [
      'id',
      'name',
      'email',
      'role',
      'disabled',
      'offboarded_at',
      'created_at',
      'role_ids',
      'updated_at',
      'last_login_at',
      'last_login_status',
      'total_personal_keys',
      'personal_policy_stored',
      'personal',
      'teams',
    ]) &&
    id(v.id) &&
    typeof v.name === 'string' &&
    [...v.name].length <= 100 &&
    label(v.email, 254) &&
    ['admin', 'member'].includes(v.role as string) &&
    typeof v.disabled === 'boolean' &&
    (v.offboarded_at === null || stamp(v.offboarded_at)) &&
    stamp(v.created_at) &&
    stamp(v.updated_at) &&
    v.last_login_at === null &&
    v.last_login_status === 'historical_unavailable' &&
    Array.isArray(v.role_ids) &&
    v.role_ids.length <= 10000 &&
    v.role_ids.every(id) &&
    new Set(v.role_ids).size === v.role_ids.length &&
    count(v.total_personal_keys) &&
    typeof v.personal_policy_stored === 'boolean' &&
    account(v.personal, v) &&
    teams(v.teams)
  )
}
export function validateMemberListPage(
  data: unknown,
  actor: string,
  cursor: string | null = null,
): MemberListPage {
  if (
    !id(actor) ||
    (cursor !== null && !id(cursor)) ||
    !object(data) ||
    !keys(data, ['actor_user_id', 'observed_at', 'platform_currency', 'items', 'next_cursor']) ||
    data.actor_user_id !== actor ||
    !stamp(data.observed_at) ||
    !currency(data.platform_currency) ||
    !Array.isArray(data.items) ||
    data.items.length > 100 ||
    !data.items.every(row) ||
    !(data.next_cursor === null || id(data.next_cursor))
  )
    throw new Error('Invalid member list response')
  const page = data as unknown as MemberListPage
  if (
    new Set(page.items.map((r) => r.id)).size !== page.items.length ||
    page.items.some(
      (r, i) => (cursor !== null && r.id <= cursor) || (i > 0 && page.items[i - 1].id >= r.id),
    ) ||
    (page.next_cursor !== null &&
      (!page.items.length || page.next_cursor !== page.items.at(-1)?.id)) ||
    page.items.reduce((n, r) => n + r.role_ids.length, 0) > 10000 ||
    page.items.reduce((n, r) => n + (r.teams.items?.length ?? 0), 0) > 1000
  )
    throw new Error('Invalid member list page identity')
  return page
}
export function validateMemberListChain(pages: MemberListPage[], actor: string): MemberListPage[] {
  let cursor: string | null = null
  for (const [i, page] of pages.entries()) {
    validateMemberListPage(page, actor, cursor)
    if (i < pages.length - 1 && page.next_cursor === null)
      throw new Error('Invalid member list cursor chain')
    cursor = page.next_cursor
  }
  return pages
}

export async function getMemberList(
  filters: MemberFilters,
  actor: string,
  cursor: string | null,
  signal?: AbortSignal,
) {
  if (
    !/^[A-Za-z0-9_-]{1,30}$/.test(actor) ||
    (cursor !== null && !/^[A-Za-z0-9_-]{1,30}$/.test(cursor))
  )
    throw new Error('Invalid member list identity or cursor')
  const data: unknown = (
    await client.get('/admin/members', {
      params: { ...filters, cursor: cursor || undefined },
      signal,
    })
  ).data
  if (signal?.aborted) throw new Error('Member list read was cancelled')
  return validateMemberListPage(data, actor, cursor)
}
