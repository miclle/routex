import client from './client'
import type { MemberAccessAuthority, MemberAccessSummary } from '@/types/member-access-summary'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const fields = (value: Record<string, unknown>, names: string[]) =>
  Object.keys(value).length === names.length && names.every((name) => Object.hasOwn(value, name))
const identity = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const stamp = (value: unknown): value is string => {
  if (
    typeof value !== 'string' ||
    !/^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) ||
    /^0001-01-01T00:00:00(?:\.0{1,9})?Z$/.test(value)
  )
    return false
  return (
    Number.isFinite(Date.parse(value)) &&
    new Date(value).toISOString().slice(0, 19) === value.slice(0, 19)
  )
}
const recordedName = (value: unknown): value is string =>
  typeof value === 'string' && [...value].length <= 100 && !/\p{Cs}/u.test(value)
function section(
  value: unknown,
  allowed: boolean,
  maximum: number,
  validItem: (item: Record<string, unknown>) => boolean,
) {
  if (!object(value) || !fields(value, ['status', 'items'])) return false
  if (!allowed) return value.status === 'not_authorized' && value.items === null
  if (value.status === 'overflow' || value.status === 'unavailable') return value.items === null
  if (value.status !== 'available' || !Array.isArray(value.items) || value.items.length > maximum)
    return false
  const items = value.items
  return items.every(
    (item, i) =>
      object(item) &&
      identity(item.id) &&
      validItem(item) &&
      (i === 0 || (items[i - 1].id as string) < item.id),
  )
}
export function validateMemberAccessSummary(
  value: unknown,
  target: string,
  authority: MemberAccessAuthority,
): MemberAccessSummary {
  if (
    !identity(target) ||
    !object(value) ||
    !fields(value, ['user_id', 'observed_at', 'updated_at', 'identity_role', 'roles', 'teams']) ||
    value.user_id !== target ||
    !stamp(value.observed_at) ||
    !(value.updated_at === null || stamp(value.updated_at)) ||
    !['admin', 'member'].includes(value.identity_role as string) ||
    !section(
      value.roles,
      authority.roles,
      10000,
      (item) =>
        fields(item, ['id', 'name', 'builtin']) &&
        recordedName(item.name) &&
        typeof item.builtin === 'boolean',
    ) ||
    !section(
      value.teams,
      authority.teams,
      1000,
      (item) =>
        fields(item, ['id', 'name', 'status', 'membership_status', 'membership_role']) &&
        recordedName(item.name) &&
        ['active', 'disabled', 'archived'].includes(item.status as string) &&
        ['active', 'disabled'].includes(item.membership_status as string) &&
        ['owner', 'member'].includes(item.membership_role as string),
    )
  )
    throw new Error('Invalid Member access summary response')
  return value as unknown as MemberAccessSummary
}
export async function getMemberAccessSummary(
  target: string,
  authority: MemberAccessAuthority,
  signal?: AbortSignal,
): Promise<MemberAccessSummary> {
  if (!identity(target)) throw new Error('Invalid Member identity')
  const { data, headers } = await client.get<unknown>(
    `/admin/members/${encodeURIComponent(target)}/access`,
    { signal },
  )
  if (String(headers['cache-control']).toLowerCase() !== 'private, no-store')
    throw new Error('Invalid private Member access response')
  return validateMemberAccessSummary(data, target, authority)
}
export const memberAccessSummaryKey = (
  actor: string,
  target: string,
  generation: number,
  revision: string,
) => ['admin', 'member-access-summary', actor, target, generation, revision] as const
