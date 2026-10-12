import { validCredentialInferenceAlert } from './credential-inference-notices'
import axios from 'axios'
import client from './client'
import { validRoleAssignment } from '@/lib/role-assignment'
import type {
  AdminOverview,
  OperationalAlert,
  UpdateOperationalAlertInput,
  OverviewAccountsPage,
  OverviewRolesPage,
} from '@/types/overview'

export const adminOverviewKey = ['admin', 'overview'] as const

export class OverviewError extends Error {
  constructor(public readonly status: number) {
    super('Overview request failed')
  }
}

export function validModelDiscoveryAlert(value: unknown): boolean {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const item = value as Record<string, unknown>
  const allowed = [
    'id',
    'kind',
    'detail_code',
    'severity',
    'state',
    'occurrence_count',
    'first_seen_at',
    'last_seen_at',
    'updated_at',
    'etag',
    'subject_type',
    'subject_id',
    'subject_name',
    'first_recorded_model_count',
  ]
  const id = (value: unknown, prefix: string) =>
    typeof value === 'string' && new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`).test(value)
  const stamp = (value: unknown): value is string =>
    typeof value === 'string' &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value) &&
    Number.isFinite(Date.parse(value))
  return (
    Object.keys(item).every((key) => allowed.includes(key)) &&
    id(item.id, 'alr') &&
    id(item.etag, 'rev') &&
    item.kind === 'models_first_recorded' &&
    item.severity === 'low' &&
    item.detail_code === 'first_recorded' &&
    ['open', 'handling', 'resolved'].includes(item.state as string) &&
    item.subject_type === 'provider' &&
    id(item.subject_id, 'prv') &&
    typeof item.subject_name === 'string' &&
    item.subject_name.length > 0 &&
    item.subject_name.trim() === item.subject_name &&
    [...item.subject_name].length <= 100 &&
    !/\p{Cc}/u.test(item.subject_name) &&
    Number.isSafeInteger(item.first_recorded_model_count) &&
    (item.first_recorded_model_count as number) >= 1 &&
    (item.first_recorded_model_count as number) <= 2000 &&
    Number.isSafeInteger(item.occurrence_count) &&
    (item.occurrence_count as number) >= 1 &&
    stamp(item.first_seen_at) &&
    stamp(item.last_seen_at) &&
    stamp(item.updated_at) &&
    Date.parse(item.first_seen_at) <= Date.parse(item.last_seen_at)
  )
}

export async function getAdminOverview(signal?: AbortSignal) {
  const data = (await client.get<AdminOverview>('/admin/overview', { signal })).data
  if (
    Array.isArray(data?.alerts) &&
    data.alerts.some(
      (item) =>
        ((item.kind === 'credential_inference_failure' || Object.hasOwn(item, 'credential_id')) &&
          !validCredentialInferenceAlert(item)) ||
        ((item.kind === 'models_first_recorded' || item.first_recorded_model_count != null) &&
          !validModelDiscoveryAlert(item)),
    )
  )
    throw new OverviewError(0)
  return data
}

export async function updateOperationalAlert(
  id: string,
  input: UpdateOperationalAlertInput,
  csrf: string,
) {
  try {
    const data = (
      await client.patch<OperationalAlert>(
        `/admin/alerts/${encodeURIComponent(id)}`,
        { state: input.state, etag: input.etag },
        { headers: { 'X-CSRF-Token': csrf, 'If-Match': input.etag } },
      )
    ).data
    if (
      ((data.kind === 'credential_inference_failure' || Object.hasOwn(data, 'credential_id')) &&
        !validCredentialInferenceAlert(data)) ||
      ((data.kind === 'models_first_recorded' || data.first_recorded_model_count != null) &&
        !validModelDiscoveryAlert(data))
    )
      throw new OverviewError(0)
    return data
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

const fields = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const roleID = (value: unknown): value is string =>
  identity(value) && typeof value === 'string' && value.startsWith('rol_')
export const validOverviewRoleName = (value: unknown): value is string =>
  typeof value === 'string' &&
  value.length > 0 &&
  [...value].length <= 100 &&
  !/[\p{Cc}\p{Cs}]/u.test(value) &&
  value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '') === value
function observedUTC(value: unknown): value is string {
  if (
    typeof value !== 'string' ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)
  )
    return false
  const date = new Date(value)
  return (
    Number.isFinite(date.getTime()) &&
    date.getTime() !== -62135596800000 &&
    Number(value.slice(0, 4)) > 0 &&
    date.toISOString().slice(0, 19) === value.slice(0, 19)
  )
}
function roleCursor(value: string, actorId: string): string | null {
  if (!/^[A-Za-z0-9_-]{1,128}$/.test(value)) return null
  try {
    const raw = atob(
      value.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (value.length % 4)) % 4),
    )
    const [owner, id, extra] = raw.split('|')
    const canonical = btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    return owner === actorId && roleID(id) && extra === undefined && canonical === value ? id : null
  } catch {
    return null
  }
}
export const overviewRolesKey = (actorId: string, generation: string, cursor: string | null) =>
  ['overview-roles', actorId, generation, cursor] as const
export async function getOverviewRoles(
  actorId: string,
  cursor: string | null,
  signal?: AbortSignal,
) {
  if (!identity(actorId) || (cursor !== null && !roleCursor(cursor, actorId)))
    throw new Error('Invalid self Role page request')
  const data: unknown = (
    await client.get('/overview/roles', {
      params: { cursor: cursor ?? undefined, limit: 10 },
      signal,
    })
  ).data
  if (
    !object(data) ||
    !fields(data, ['actor_user_id', 'observed_at', 'identity_role', 'roles', 'next_cursor']) ||
    data.actor_user_id !== actorId ||
    !observedUTC(data.observed_at) ||
    (data.identity_role !== 'admin' && data.identity_role !== 'member') ||
    !Array.isArray(data.roles) ||
    data.roles.length > 10
  )
    throw new Error('Invalid self Role labels response')
  let previous = cursor === null ? '' : roleCursor(cursor, actorId)!
  for (const row of data.roles) {
    if (
      !object(row) ||
      !fields(row, ['id', 'name', 'builtin', 'assignment_kind']) ||
      !roleID(row.id) ||
      row.id <= previous ||
      row.assignment_kind !== 'explicit' ||
      !validRoleAssignment(row) ||
      (row.name !== null && !validOverviewRoleName(row.name))
    )
      throw new Error('Invalid self Role label')
    previous = row.id
  }
  if (
    data.next_cursor !== null &&
    (typeof data.next_cursor !== 'string' ||
      data.roles.length === 0 ||
      data.next_cursor === cursor ||
      roleCursor(data.next_cursor, actorId) !== previous)
  )
    throw new Error('Invalid self Role cursor')
  return data as unknown as OverviewRolesPage
}
