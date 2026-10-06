import {
  AxiosHeaders,
  type RawAxiosHeaders,
  type AxiosResponseHeaders,
  type RawAxiosResponseHeaders,
} from 'axios'
import client from './client'
import { validRoleAssignment } from '@/lib/role-assignment'
import type {
  RoleDefinition,
  RoleDefinitionInput,
  RoleDefinitionResult,
} from '@/types/role-definition'

const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const fields = (v: Record<string, unknown>, names: string[]) =>
  Object.keys(v).length === names.length && names.every((name) => Object.hasOwn(v, name))
const id = (v: unknown): v is string =>
  typeof v === 'string' && /^rol_[A-Za-z0-9_-]*$/.test(v) && v.length <= 30
const proof = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{64}$/.test(v)
const text = (v: unknown): v is string => typeof v === 'string' && !/\p{Cs}/u.test(v)
const recordedName = (v: unknown): v is string => text(v) && [...v].length <= 100
const code = (v: unknown): v is string => typeof v === 'string' && /^[A-Za-z0-9._-]{1,80}$/.test(v)
const implemented = [
  'secrets.read',
  'secrets.rotate',
  'members.read',
  'members.write',
  'members.approvals.write',
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
const reserved = ['roles.write', 'registration.write', 'members.approvals.write']
const catalogue = implemented.filter((value) => !reserved.includes(value)).sort()
const assignableCode = (v: unknown) => code(v) && implemented.includes(v) && !reserved.includes(v)
const sorted = (v: unknown, check: (item: unknown) => boolean): v is string[] =>
  Array.isArray(v) &&
  v.length <= 100 &&
  v.every((item, i) => check(item) && (i === 0 || v[i - 1] < item))
function invalid(): never {
  throw new Error('Invalid Role definition resource')
}

export function validRoleDefinitionName(v: unknown): v is string {
  return recordedName(v) && v.length > 0 && v.trim() === v && !/\p{Cc}/u.test(v)
}
// Match strings.TrimSpace for descriptions; JavaScript trim also removes U+FEFF.
export function trimRoleDefinitionDescription(value: string): string {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
}
export function validRoleDefinitionDescription(v: unknown): v is string {
  return (
    text(v) &&
    v.length > 0 &&
    trimRoleDefinitionDescription(v) === v &&
    !/\p{Cc}/u.test(v.replace(/\n/g, '')) &&
    new TextEncoder().encode(v).length <= 2000
  )
}
export function validRecordedRoleDescription(v: unknown): v is string {
  return v === '' || validRoleDefinitionDescription(v)
}
export function validRoleDefinitionReason(v: unknown): v is string {
  return (
    text(v) &&
    v.length > 0 &&
    v.trim() === v &&
    !/\p{Cc}/u.test(v) &&
    new TextEncoder().encode(v).length <= 1024
  )
}
export function validateRoleDefinition(v: unknown, target: string): RoleDefinition {
  if (
    !id(target) ||
    !object(v) ||
    !fields(v, [
      'id',
      'name',
      'description',
      'builtin',
      'assignment_kind',
      'permissions',
      'available_permissions',
      'definition_etag',
      'identity_etag',
      'review_etag',
      'can_edit',
    ]) ||
    v.id !== target ||
    !recordedName(v.name) ||
    !validRecordedRoleDescription(v.description) ||
    !validRoleAssignment(v) ||
    !sorted(v.permissions, code) ||
    !sorted(v.available_permissions, assignableCode) ||
    JSON.stringify(v.available_permissions) !== JSON.stringify(catalogue) ||
    !proof(v.definition_etag) ||
    !(v.identity_etag === null || proof(v.identity_etag)) ||
    !proof(v.review_etag) ||
    typeof v.can_edit !== 'boolean' ||
    (v.can_edit &&
      (v.builtin || v.identity_etag === null || ['rol_admin', 'rol_member'].includes(target)))
  )
    invalid()
  return v as unknown as RoleDefinition
}
export function validateRoleDefinitionInput(v: unknown): RoleDefinitionInput {
  if (
    !object(v) ||
    !fields(v, ['name', 'description', 'permissions', 'identity_etag', 'reason']) ||
    !validRoleDefinitionName(v.name) ||
    !validRoleDefinitionDescription(v.description) ||
    !sorted(v.permissions, assignableCode) ||
    !proof(v.identity_etag) ||
    !validRoleDefinitionReason(v.reason)
  )
    invalid()
  return v as unknown as RoleDefinitionInput
}
export function validateRoleDefinitionResult(
  v: unknown,
  target: string,
  input: RoleDefinitionInput,
): RoleDefinitionResult {
  validateRoleDefinitionInput(input)
  if (
    !id(target) ||
    !object(v) ||
    !fields(v, [
      'id',
      'name',
      'description',
      'permissions',
      'identity_etag',
      'etag',
      'confirmation',
      'effect',
    ]) ||
    v.id !== target ||
    v.name !== input.name ||
    v.description !== input.description ||
    !sorted(v.permissions, assignableCode) ||
    JSON.stringify(v.permissions) !== JSON.stringify(input.permissions) ||
    v.identity_etag !== input.identity_etag ||
    !proof(v.etag) ||
    v.confirmation !== 'current_role_definition' ||
    v.effect !== 'current_database'
  )
    invalid()
  return v as unknown as RoleDefinitionResult
}
function headers(value: AxiosResponseHeaders | RawAxiosResponseHeaders, etag: string) {
  const received = AxiosHeaders.from(value as RawAxiosHeaders)
  if (received.get('etag') !== `"${etag}"` || received.get('cache-control') !== 'private, no-store')
    invalid()
}
export async function getRoleDefinition(target: string, signal?: AbortSignal) {
  if (!id(target)) invalid()
  const response = await client.get<unknown>(`/admin/roles/${encodeURIComponent(target)}`, {
    signal,
  })
  if (response.status !== 200) invalid()
  const result = validateRoleDefinition(response.data, target)
  headers(response.headers, result.review_etag)
  return result
}
export async function setRoleDefinition(
  target: string,
  etag: string,
  input: RoleDefinitionInput,
  csrf: string,
  signal?: AbortSignal,
) {
  if (!id(target) || !proof(etag) || !csrf) invalid()
  validateRoleDefinitionInput(input)
  const captured = { ...input, permissions: [...input.permissions] }
  const response = await client.put<unknown>(
    `/admin/roles/${encodeURIComponent(target)}`,
    captured,
    {
      signal,
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    },
  )
  if (response.status !== 200) invalid()
  const result = validateRoleDefinitionResult(response.data, target, captured)
  headers(response.headers, result.etag)
  return result
}
