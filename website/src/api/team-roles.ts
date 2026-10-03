import client from './client'
import type { TeamAction, TeamRole, TeamRoles } from '@/types/team-roles'
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const text = (value: unknown): value is string => typeof value === 'string' && value.length > 0
const actions = ['teams.write', 'teams.models.write']
function validActions(value: unknown): value is TeamAction[] {
  return (
    Array.isArray(value) &&
    new Set(value).size === value.length &&
    value.every((action) => actions.includes(action))
  )
}
function validRole(value: unknown): value is TeamRole {
  return (
    object(value) &&
    text(value.id) &&
    typeof value.name === 'string' &&
    typeof value.builtin === 'boolean' &&
    validActions(value.team_actions)
  )
}
function validRoles(value: unknown, team: string): value is TeamRoles {
  if (
    !object(value) ||
    value.team_id !== team ||
    typeof value.can_assign_roles !== 'boolean' ||
    typeof value.etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(value.etag) ||
    !Array.isArray(value.role_ids) ||
    value.role_ids.length > 100 ||
    !value.role_ids.every(text) ||
    new Set(value.role_ids).size !== value.role_ids.length ||
    !Array.isArray(value.roles) ||
    !value.roles.every(validRole) ||
    value.roles.length !== value.role_ids.length ||
    new Set(value.roles.map((role) => role.id)).size !== value.roles.length ||
    !value.roles.every((role) => (value.role_ids as string[]).includes(role.id)) ||
    !validActions(value.effective_team_actions) ||
    !validActions(value.actor_team_actions)
  )
    return false
  const union = new Set(value.roles.flatMap((role) => role.team_actions))
  return value.effective_team_actions.every((action) => union.has(action))
}
export async function getTeamRoles(team: string, signal?: AbortSignal) {
  const value: unknown = (await client.get(`/teams/${encodeURIComponent(team)}/roles`, { signal }))
    .data
  if (!validRoles(value, team)) throw new Error('Invalid Team role projection')
  return value
}
export async function getTeamRoleCandidates(
  team: string,
  query: string,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const value: unknown = (
    await client.get(`/teams/${encodeURIComponent(team)}/role-candidates`, {
      params: { query: query || undefined, cursor: cursor ?? undefined, limit: 50 },
      signal,
    })
  ).data
  if (
    !object(value) ||
    !Array.isArray(value.items) ||
    value.items.length > 50 ||
    typeof value.etag !== 'string' ||
    !/^[a-f0-9]{64}$/.test(value.etag) ||
    !(value.next_cursor === null || text(value.next_cursor)) ||
    (cursor && cursor === value.next_cursor) ||
    !value.items.every(validRole) ||
    new Set(value.items.map((role) => role.id)).size !== value.items.length
  )
    throw new Error('Invalid Team role candidates')
  return {
    items: value.items as TeamRole[],
    next_cursor: value.next_cursor as string | null,
    etag: value.etag,
  }
}
export async function saveTeamRoles(
  team: string,
  roleIds: string[],
  reason: string,
  etag: string,
  csrf: string,
) {
  const value: unknown = (
    await client.put(
      `/teams/${encodeURIComponent(team)}/roles`,
      { role_ids: roleIds, reason },
      { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf } },
    )
  ).data
  if (
    !validRoles(value, team) ||
    value.role_ids.length !== roleIds.length ||
    !value.role_ids.every((role) => roleIds.includes(role))
  )
    throw new Error('Invalid Team role save receipt')
  return value
}
