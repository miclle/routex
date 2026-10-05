import client from './client'
import type {
  MemberRoleCandidatePage,
  MemberRoleDetail,
  MemberRoleSummary,
  MemberRolesInput,
  MemberRolesResult,
  MemberRolesWorkspace,
} from '@/types/member-roles'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const fields = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length && names.every((n) => Object.hasOwn(v, n))
const identity = (v: unknown): v is string =>
  typeof v === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(v)
const proof = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{64}$/.test(v)
const name = (v: unknown): v is string =>
  typeof v === 'string' && [...v].length <= 100 && !/\p{Cs}/u.test(v)
const code = (v: unknown): v is string =>
  typeof v === 'string' && v.length > 0 && v.length <= 80 && /^[A-Za-z0-9._-]+$/.test(v)
const cursor = (v: unknown): v is string =>
  typeof v === 'string' && v.length > 0 && v.length <= 200 && /^[A-Za-z0-9_-]+$/.test(v)
// This surface mirrors the implemented platform permission catalogue. Retained
// unknown codes remain readable in definition details, never in the active union.
const codes = [
  'secrets.read',
  'secrets.rotate',
  'members.read',
  'members.write',
  'members.keys.disable',
  'members.models.write',
  'roles.read',
  'roles.write',
  'registration.write',
  'providers.read',
  'providers.write',
  'models.read_all',
  'models.write',
  'calls.read_all',
  'audit.read',
  'system.read',
  'system.write',
  'teams.read_all',
  'teams.write',
  'teams.models.write',
  'teams.tokens.write',
  'teams.money.write',
  'teams.rates.write',
  'teams.quota_requests.read_all',
  'projects.read_all',
  'projects.write',
  'projects.models.write',
  'prices.read',
  'prices.write',
  'limits.users.write',
  'limits.settings.write',
  'projects.limits.write',
  'site.write',
  'announcements.write',
  'egress.read',
  'egress.write',
  'egress.test',
  'smtp.read',
  'smtp.write',
  'smtp.test',
  'storage.read',
  'storage.write',
  'storage.test',
]
const blockers = [
  'not_platform_admin',
  'offboarded',
  'assignment_audit_bound',
  'candidate_catalogue_bound',
  'definition_unavailable',
]
function invalid(): never {
  throw new Error('Invalid Member Roles response')
}
function stamp(v: unknown): v is string {
  return (
    typeof v === 'string' &&
    /^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(v) &&
    !/^0001-01-01T00:00:00(?:\.0{1,9})?Z$/.test(v) &&
    Number.isFinite(Date.parse(v)) &&
    new Date(v).toISOString().slice(0, 19) === v.slice(0, 19)
  )
}
function sorted(v: unknown, max: number, check: (item: unknown) => boolean): v is string[] {
  return (
    Array.isArray(v) &&
    v.length <= max &&
    v.every((item, i) => check(item) && (i === 0 || v[i - 1] < item))
  )
}
function summary(v: unknown): v is MemberRoleSummary {
  return (
    object(v) &&
    fields(v, ['id', 'name', 'builtin', 'permission_count', 'definition_etag']) &&
    identity(v.id) &&
    name(v.name) &&
    typeof v.builtin === 'boolean' &&
    Number.isSafeInteger(v.permission_count) &&
    (v.permission_count as number) >= 0 &&
    (v.permission_count as number) <= 100 &&
    proof(v.definition_etag)
  )
}
function customRows(v: unknown, max: number): v is MemberRoleSummary[] {
  return (
    Array.isArray(v) &&
    v.length <= max &&
    v.every(
      (r, i) =>
        summary(r) &&
        !r.builtin &&
        !['rol_admin', 'rol_member'].includes(r.id) &&
        (i === 0 || v[i - 1].id < r.id),
    )
  )
}
export function validateMemberRoles(v: unknown, target: string): MemberRolesWorkspace {
  if (
    !identity(target) ||
    !object(v) ||
    !fields(v, [
      'user_id',
      'observed_at',
      'identity_role',
      'subject_status',
      'builtin_role',
      'assigned_roles',
      'effective_permissions',
      'permission_use',
      'etag',
      'can_edit',
      'edit_blockers',
      'candidate_status',
    ]) ||
    v.user_id !== target ||
    !stamp(v.observed_at) ||
    !['admin', 'member'].includes(v.identity_role as string) ||
    !['active', 'disabled', 'offboarded'].includes(v.subject_status as string) ||
    !summary(v.builtin_role) ||
    !v.builtin_role.builtin ||
    v.builtin_role.id !== `rol_${v.identity_role}` ||
    !customRows(v.assigned_roles, 10000) ||
    !sorted(
      v.effective_permissions,
      codes.length,
      (x) => typeof x === 'string' && codes.includes(x),
    ) ||
    v.permission_use !== (v.subject_status === 'active' ? 'active' : 'inactive') ||
    !proof(v.etag) ||
    typeof v.can_edit !== 'boolean' ||
    !Array.isArray(v.edit_blockers) ||
    v.edit_blockers.length > blockers.length ||
    !v.edit_blockers.every((b) => blockers.includes(b)) ||
    new Set(v.edit_blockers).size !== v.edit_blockers.length ||
    !['available', 'not_authorized', 'overflow', 'unavailable'].includes(
      v.candidate_status as string,
    ) ||
    (v.can_edit &&
      (v.subject_status === 'offboarded' ||
        v.assigned_roles.length > 1000 ||
        v.edit_blockers.length > 0 ||
        v.candidate_status !== 'available')) ||
    (!v.can_edit && v.edit_blockers.length === 0)
  )
    invalid()
  return v as unknown as MemberRolesWorkspace
}
export function validateMemberRoleCandidates(v: unknown, review: string): MemberRoleCandidatePage {
  if (
    !proof(review) ||
    !object(v) ||
    !fields(v, ['items', 'next_cursor', 'etag']) ||
    v.etag !== review ||
    !customRows(v.items, 50) ||
    !(v.next_cursor === null || cursor(v.next_cursor))
  )
    invalid()
  return v as unknown as MemberRoleCandidatePage
}
export function validateMemberRoleDetail(
  v: unknown,
  target: string,
  review: string,
  expected: MemberRoleSummary,
): MemberRoleDetail {
  if (
    !identity(target) ||
    !proof(review) ||
    !summary(expected) ||
    !object(v) ||
    !fields(v, ['user_id', 'role', 'permissions', 'etag']) ||
    v.user_id !== target ||
    v.etag !== review ||
    !summary(v.role) ||
    Object.keys(expected).some(
      (k) =>
        v.role &&
        (v.role as unknown as Record<string, unknown>)[k] !==
          (expected as unknown as Record<string, unknown>)[k],
    ) ||
    !sorted(v.permissions, 100, code) ||
    v.permissions.length !== expected.permission_count
  )
    invalid()
  return v as unknown as MemberRoleDetail
}
export function validateMemberRolesConfirmation(
  v: unknown,
  target: string,
  ids: string[],
): MemberRolesResult {
  if (
    !identity(target) ||
    !object(v) ||
    !fields(v, ['user_id', 'role_ids', 'etag', 'confirmation', 'effect']) ||
    v.user_id !== target ||
    !proof(v.etag) ||
    !sorted(v.role_ids, 100, identity) ||
    JSON.stringify(v.role_ids) !== JSON.stringify(ids) ||
    v.confirmation !== 'current_member_roles' ||
    v.effect !== 'current_database'
  )
    invalid()
  return v as unknown as MemberRolesResult
}
export function validMemberRolesReason(v: string) {
  return (
    !!v &&
    v === v.trim() &&
    new TextEncoder().encode(v).length <= 1024 &&
    !/[\p{Cc}\p{Cs}]/u.test(v)
  )
}
function inputValid(v: MemberRolesInput) {
  return (
    object(v) &&
    fields(v, ['role_ids', 'role_definitions', 'builtin_definition_etag', 'reason']) &&
    sorted(v.role_ids, 100, identity) &&
    v.role_ids.every((id) => !['rol_admin', 'rol_member'].includes(id)) &&
    Array.isArray(v.role_definitions) &&
    v.role_definitions.length === v.role_ids.length &&
    v.role_definitions.every(
      (d, i) => object(d) && fields(d, ['id', 'etag']) && d.id === v.role_ids[i] && proof(d.etag),
    ) &&
    proof(v.builtin_definition_etag) &&
    typeof v.reason === 'string' &&
    validMemberRolesReason(v.reason)
  )
}
function headersValid(headers: Record<string, unknown>, etag: string) {
  if (
    headers.etag !== `"${etag}"` ||
    String(headers['cache-control']).toLowerCase() !== 'private, no-store'
  )
    invalid()
}
const path = (target: string) => {
  if (!identity(target)) invalid()
  return `/admin/members/${encodeURIComponent(target)}/roles`
}
export async function getMemberRoles(target: string, signal?: AbortSignal) {
  const r = await client.get<unknown>(path(target), { signal })
  const data = validateMemberRoles(r.data, target)
  headersValid(r.headers, data.etag)
  return data
}
export function validMemberRoleSearch(query: string) {
  return (
    typeof query === 'string' &&
    !/\p{Cs}/u.test(query) &&
    new TextEncoder().encode(query).length <= 200
  )
}
export async function getMemberRoleCandidates(
  target: string,
  review: string,
  filter: { q: string; cursor: string | null; limit?: number },
  signal?: AbortSignal,
) {
  if (
    !proof(review) ||
    typeof filter.q !== 'string' ||
    !validMemberRoleSearch(filter.q) ||
    (filter.cursor !== null && !cursor(filter.cursor)) ||
    (filter.limit !== undefined &&
      (!Number.isInteger(filter.limit) || filter.limit < 1 || filter.limit > 50))
  )
    invalid()
  const r = await client.get<unknown>(`${path(target)}/candidates`, {
    signal,
    params: {
      q: filter.q,
      ...(filter.cursor ? { cursor: filter.cursor } : {}),
      limit: filter.limit ?? 25,
    },
    headers: { 'If-Match': `"${review}"` },
  })
  const data = validateMemberRoleCandidates(r.data, review)
  headersValid(r.headers, data.etag)
  return data
}
export async function getMemberRoleDetail(
  target: string,
  review: string,
  role: MemberRoleSummary,
  signal?: AbortSignal,
) {
  if (!proof(review) || !summary(role)) invalid()
  const r = await client.get<unknown>(`${path(target)}/${encodeURIComponent(role.id)}`, {
    signal,
    headers: { 'If-Match': `"${review}"` },
  })
  const data = validateMemberRoleDetail(r.data, target, review, role)
  headersValid(r.headers, data.etag)
  return data
}
export async function setMemberRoles(
  target: string,
  review: string,
  input: MemberRolesInput,
  csrf: string,
  signal?: AbortSignal,
) {
  if (!proof(review) || !inputValid(input) || !csrf) invalid()
  const r = await client.put<unknown>(path(target), input, {
    signal,
    headers: { 'If-Match': `"${review}"`, 'X-CSRF-Token': csrf },
  })
  const data = validateMemberRolesConfirmation(r.data, target, input.role_ids)
  headersValid(r.headers, data.etag)
  return data
}
