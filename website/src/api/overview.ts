import axios from 'axios'
import client from './client'
import type {
  AdminOverview,
  OperationalAlert,
  UpdateOperationalAlertInput,
  OverviewAccountsPage,
} from '@/types/overview'

export const adminOverviewKey = ['admin', 'overview'] as const

export class OverviewError extends Error {
  constructor(public readonly status: number) {
    super('Overview request failed')
  }
}

export async function getAdminOverview(signal?: AbortSignal) {
  return (await client.get<AdminOverview>('/admin/overview', { signal })).data
}

export async function updateOperationalAlert(
  id: string,
  input: UpdateOperationalAlertInput,
  csrf: string,
) {
  try {
    return (
      await client.patch<OperationalAlert>(
        `/admin/alerts/${encodeURIComponent(id)}`,
        { state: input.state, etag: input.etag },
        { headers: { 'X-CSRF-Token': csrf, 'If-Match': input.etag } },
      )
    ).data
  } catch (error) {
    throw new OverviewError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown): value is string =>
  typeof value === 'string' && value.length > 0 && value.length <= 256
const identity = (value: unknown) =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const integer = (value: unknown) => typeof value === 'string' && /^(0|[1-9]\d{0,127})$/.test(value)
const decimal = (value: unknown) =>
  typeof value === 'string' && value.length <= 256 && /^\d+(?:\.\d+)?$/.test(value)
const currency = (value: unknown) => typeof value === 'string' && /^[A-Z]{3}$/.test(value)
const stamp = (value: unknown): value is string =>
  typeof value === 'string' &&
  /^\d{4}-\d\d-\d\dT.*(?:Z|[+-]\d\d:\d\d)$/.test(value) &&
  Number.isFinite(Date.parse(value))
function timezone(value: unknown) {
  if (!text(value) || value === 'Local') return false
  try {
    new Intl.DateTimeFormat('en', { timeZone: value }).format()
    return true
  } catch {
    return false
  }
}
function amounts(value: unknown) {
  return (
    object(value) &&
    Object.entries(value).every(([key, amount]) => currency(key) && decimal(amount))
  )
}
function usage(value: unknown) {
  if (!object(value)) return false
  return (
    stamp(value.as_of) &&
    stamp(value.month_start) &&
    stamp(value.month_end) &&
    Date.parse(value.month_start) < Date.parse(value.month_end) &&
    timezone(value.time_zone) &&
    typeof value.covered === 'boolean' &&
    ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].every((key) =>
      integer(value[key]),
    ) &&
    amounts(value.money_used) &&
    amounts(value.money_held)
  )
}
function account(value: unknown) {
  return (
    object(value) &&
    text(value.account_id) &&
    value.account_id.length <= 64 &&
    text(value.policy_etag) &&
    value.policy_etag.length <= 64 &&
    (value.tokens_month === null || integer(value.tokens_month)) &&
    (value.money_month === null || decimal(value.money_month)) &&
    (value.money_month === null ? value.currency === null : currency(value.currency)) &&
    typeof value.runtime_applied === 'boolean' &&
    ['active', 'inactive', 'unavailable'].includes(value.usage_status as string) &&
    (value.usage_status === 'active'
      ? usage(value.usage) &&
        object(value.active_reservations) &&
        integer(value.active_reservations.tokens_held) &&
        amounts(value.active_reservations.money_held)
      : value.usage === null && value.active_reservations === null)
  )
}
export const overviewAccountsKey = (actorId: string, generation: string, cursor: string | null) =>
  ['overview-accounts', actorId, generation, cursor] as const

export async function getOverviewAccounts(
  actorId: string,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const data = (
    await client.get<OverviewAccountsPage>('/overview/accounts', {
      params: { cursor: cursor || undefined, limit: 10 },
      signal,
    })
  ).data
  if (
    !object(data) ||
    !identity(actorId) ||
    data.actor_user_id !== actorId ||
    !stamp(data.observed_at) ||
    !currency(data.platform_currency) ||
    !account(data.personal) ||
    !Array.isArray(data.teams) ||
    data.teams.length > 10 ||
    !data.teams.every(
      (team) =>
        object(team) &&
        identity(team.id) &&
        identity(team.membership_id) &&
        text(team.name) &&
        account(team.aggregate) &&
        account(team.member),
    ) ||
    new Set(data.teams.map((team) => team.id)).size !== data.teams.length ||
    (data.next_cursor !== null && (!text(data.next_cursor) || data.next_cursor.length > 256)) ||
    (cursor !== null && data.next_cursor === cursor)
  )
    throw new Error('Invalid monthly overview response')
  return data as unknown as OverviewAccountsPage
}
