import axios from 'axios'
import client from './client'
import type {
  KeyCallerTemplate,
  KeyCallerTemplateInput,
  KeyDeliveryContext,
  KeyDeliveryDescriptor,
  KeyDeliveryIdentity,
  KeyDeliveryIdentityInput,
  KeyDeliveryMode,
  KeyDeliveryOwner,
  KeyDeliveryPolicy,
  KeyDeliveryPolicyInput,
  KeyDeliveryPrincipal,
  KeyDeliveryReceipt,
  KeyDeliveryResolveInput,
  KeyDeliverySecurityTuple,
  KeyDeliveryTarget,
  KeyDeliveryTargetPage,
  KeyDeliveryTargetReview,
  ManagedKeyInput,
  ManagedKeyResult,
} from '@/types/key-delivery'

export class KeyDeliveryError extends Error {
  constructor(public readonly status: number) {
    super('API-Key delivery request failed')
  }
}
const admin = '/admin/api-key-delivery'
const review = /^[a-f0-9]{64}$/
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/
const code = /^[a-zA-Z0-9_-]{1,64}$/
const modes = ['manual', 'managed_vault'] as const
const contextFields = [
  'allowed_modes',
  'default_mode',
  'managed_available',
  'failure_code',
  'review_etag',
]
const targetFields = [
  'id',
  'consumer_id',
  'application_name',
  'environment',
  'status',
  'control_verified',
  'isolation',
  'failure_code',
  'review_etag',
]
function fail(): never {
  throw new KeyDeliveryError(0)
}
function object(value: unknown, fields: readonly string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail()
  const row = value as Record<string, unknown>
  if (Object.keys(row).length !== fields.length || fields.some((key) => !Object.hasOwn(row, key)))
    return fail()
  return row
}
function text(value: unknown, maximum = 256, pattern?: RegExp, empty = false): string {
  if (
    typeof value !== 'string' ||
    (!empty && !value) ||
    new TextEncoder().encode(value).length > maximum ||
    /[\p{Cc}\p{Cs}]/u.test(value) ||
    (pattern && !pattern.test(value))
  )
    return fail()
  return value
}
function id(value: unknown, prefix: string, empty = false): string {
  if (empty && value === '') return ''
  return text(value, 30, new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`))
}
function bool(value: unknown): boolean {
  return typeof value === 'boolean' ? value : fail()
}
function choice<T extends string>(value: unknown, options: readonly T[]): T {
  return options.includes(value as T) ? (value as T) : fail()
}
function stamp(value: unknown): string {
  const result = text(
    value,
    40,
    /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/,
  )
  return Number.isFinite(Date.parse(result)) ? result : fail()
}
function nullableStamp(value: unknown): string | null {
  return value === null ? null : stamp(value)
}
function nullableCode(value: unknown): string | null {
  return value === null ? null : text(value, 64, /^[a-z][a-z0-9_]*$/)
}
function label(value: unknown): string {
  const result = text(value, 400)
  return [...result].length <= 100 && result.trim() === result ? result : fail()
}
function reason(value: unknown): string {
  const result = text(value, 4000)
  return [...result].length <= 1000 && result.trim() === result ? result : fail()
}
function principal(value: unknown): KeyDeliveryPrincipal {
  const row = object(value, ['method', 'subject'])
  return {
    method: choice(row.method, ['jwt']),
    subject: text(row.subject, 256),
  }
}
function strings(value: unknown, maximum: number, parse: (value: unknown) => string): string[] {
  if (!Array.isArray(value) || value.length > maximum) return fail()
  const result = value.map(parse)
  return new Set(result).size === result.length ? result : fail()
}
function context(row: Record<string, unknown>): KeyDeliveryContext {
  const allowed = strings(row.allowed_modes, 2, (value) =>
    choice(value, modes),
  ) as KeyDeliveryMode[]
  const selected = choice(row.default_mode, modes)
  if (!allowed.length || !allowed.includes(selected)) return fail()
  const result = {
    allowed_modes: allowed,
    default_mode: selected,
    managed_available: bool(row.managed_available),
    failure_code: nullableCode(row.failure_code),
    review_etag: text(row.review_etag, 64, review),
  }
  if (result.managed_available && result.failure_code !== null) return fail()
  return result
}
export function parseKeyDeliveryContext(value: unknown): KeyDeliveryContext {
  return context(object(value, contextFields))
}
export function parseKeyDeliveryPolicy(value: unknown): KeyDeliveryPolicy {
  const row = object(value, [
    ...contextFields,
    'integration_id',
    'writer_identity_id',
    'provisioner_identity_id',
    'template_configured',
  ])
  return {
    ...context(row),
    integration_id: id(row.integration_id, 'vlt', true),
    writer_identity_id: id(row.writer_identity_id, 'kda', true),
    provisioner_identity_id: id(row.provisioner_identity_id, 'kda', true),
    template_configured: bool(row.template_configured),
  }
}
export function parseKeyCallerTemplate(value: unknown): KeyCallerTemplate {
  const row = object(value, [
    'auth_mount',
    'issuer',
    'jwks_url',
    'audience',
    'allowed_subjects',
    'token_ttl_seconds',
    'token_max_ttl_seconds',
    'verified',
    'review_etag',
  ])
  const ttl = row.token_ttl_seconds
  const maximum = row.token_max_ttl_seconds
  if (
    typeof ttl !== 'number' ||
    !Number.isSafeInteger(ttl) ||
    typeof maximum !== 'number' ||
    !Number.isSafeInteger(maximum) ||
    ttl < 0 ||
    ttl > maximum ||
    maximum > 3600 ||
    (ttl === 0) !== (maximum === 0)
  )
    return fail()
  const result = {
    auth_mount: text(row.auth_mount, 128, undefined, true),
    issuer: text(row.issuer, 2048, undefined, true),
    jwks_url: text(row.jwks_url, 2048, undefined, true),
    audience: text(row.audience, 256, undefined, true),
    allowed_subjects: strings(row.allowed_subjects, 128, (value) => text(value, 256)),
    token_ttl_seconds: ttl,
    token_max_ttl_seconds: maximum,
    verified: bool(row.verified),
    review_etag: text(row.review_etag, 64, review),
  }
  if (
    ttl === 0 &&
    (result.verified ||
      result.auth_mount ||
      result.issuer ||
      result.jwks_url ||
      result.audience ||
      result.allowed_subjects.length)
  )
    return fail()
  return result
}
export function parseKeyDeliveryIdentity(value: unknown): KeyDeliveryIdentity {
  const row = object(value, ['id', 'purpose', 'configured', 'review_etag'])
  return {
    id: id(row.id, 'kda'),
    purpose: choice(row.purpose, ['writer', 'provisioner']),
    configured: bool(row.configured),
    review_etag: text(row.review_etag, 64, review),
  }
}
function target(row: Record<string, unknown>): KeyDeliveryTarget {
  const isolation = object(row.isolation, ['status', 'revision', 'recorded_at'])
  const result: KeyDeliveryTarget = {
    id: id(row.id, 'vap'),
    consumer_id: text(row.consumer_id, 64, code),
    application_name: label(row.application_name),
    environment: text(row.environment, 64, code),
    status: choice(row.status, ['pending', 'ready', 'error']),
    control_verified: bool(row.control_verified),
    isolation: {
      status: choice(isolation.status, ['missing', 'operator_attested', 'revoked', 'stale']),
      revision: isolation.revision === null ? null : text(isolation.revision, 64, review),
      recorded_at: nullableStamp(isolation.recorded_at),
    },
    failure_code: nullableCode(row.failure_code),
    review_etag: text(row.review_etag, 64, review),
  }
  if ((result.isolation.revision === null) !== (result.isolation.recorded_at === null))
    return fail()
  if (
    result.status === 'ready' &&
    (!result.control_verified ||
      result.isolation.status !== 'operator_attested' ||
      !result.isolation.revision ||
      result.failure_code !== null)
  )
    return fail()
  return result
}
export function parseKeyDeliveryTarget(value: unknown): KeyDeliveryTarget {
  return target(object(value, targetFields))
}
export function parseKeyDeliveryTargetPage(value: unknown): KeyDeliveryTargetPage {
  const row = object(value, ['items', 'next_cursor'])
  if (!Array.isArray(row.items) || row.items.length > 100) return fail()
  const items = row.items.map((value) => {
    const item = object(value, [...targetFields, 'owner_kind', 'owner_id'])
    const kind = choice(item.owner_kind, ['personal', 'project'])
    return {
      ...target(item),
      owner_kind: kind,
      owner_id: id(item.owner_id, kind === 'personal' ? 'usr' : 'prj'),
    }
  })
  if (new Set(items.map((item) => item.id)).size !== items.length) return fail()
  const cursor = row.next_cursor === null ? null : id(row.next_cursor, 'vap')
  if (cursor !== null && (!items.length || cursor !== items.at(-1)?.id)) return fail()
  return { items, next_cursor: cursor }
}
function securityTuple(value: unknown): KeyDeliverySecurityTuple {
  const identityFields = {
    profile_id: 'vap',
    integration_id: 'vlt',
    vault_revision_id: 'vlr',
    writer_identity_id: 'kda',
    provisioner_identity_id: 'kda',
    writer_generation: 'kdg',
    provisioner_generation: 'kdg',
    principal_id: 'vpb',
  }
  const birthFields = [
    'owner_birth',
    'integration_birth',
    'vault_revision_birth',
    'writer_identity_birth',
    'provisioner_identity_birth',
    'template_birth',
    'profile_birth',
    'principal_birth',
  ]
  const hashFields = [
    'owner_digest',
    'profile_config_revision',
    'profile_security_revision',
    'integration_revision',
    'infrastructure_security_revision',
    'template_security_revision',
    'trust_security_revision',
    'principal_security_revision',
  ]
  const row = object(value, [
    ...Object.keys(identityFields),
    ...birthFields,
    ...hashFields,
    'template_id',
    'owner_kind',
    'owner_id',
    'consumer_id',
    'environment',
    'auth_mount',
    'issuer',
    'jwks_url',
    'audience',
    'method',
    'subject',
  ])
  for (const [name, prefix] of Object.entries(identityFields)) id(row[name], prefix)
  for (const name of birthFields) stamp(row[name])
  for (const name of hashFields) text(row[name], 64, review)
  const kind = choice(row.owner_kind, ['personal', 'project'])
  id(row.owner_id, kind === 'personal' ? 'usr' : 'prj')
  text(row.consumer_id, 64, code)
  text(row.environment, 64, code)
  text(row.auth_mount, 128)
  text(row.issuer, 2048)
  text(row.jwks_url, 2048)
  text(row.audience, 256)
  choice(row.method, ['jwt'])
  text(row.subject, 256)
  choice(row.template_id, ['jwt'])
  return { ...row } as unknown as KeyDeliverySecurityTuple
}
export function parseKeyDeliveryTargetReview(value: unknown): KeyDeliveryTargetReview {
  const row = object(value, [...targetFields, 'security_tuple'])
  const result = {
    ...target(row),
    security_tuple: securityTuple(row.security_tuple),
  }
  if (
    result.id !== result.security_tuple.profile_id ||
    result.consumer_id !== result.security_tuple.consumer_id ||
    result.environment !== result.security_tuple.environment
  )
    return fail()
  return result
}
export function parseKeyDeliveryDescriptor(
  value: unknown,
  owner: KeyDeliveryOwner = { kind: 'personal' },
): KeyDeliveryDescriptor {
  const row = object(value, [
    'schema_version',
    'key_id',
    'profile_id',
    'principal_id',
    'slot_id',
    'descriptor_revision',
    'endpoint',
    'namespace',
    'mount',
    'path',
    'data_field',
    'version',
    'method',
    'auth_mount',
    'role',
    'coordinator',
    'application_status',
  ])
  if (row.schema_version !== 1 || row.version !== 1) return fail()
  const revision = text(row.descriptor_revision, 20, /^[1-9][0-9]*$/)
  if (BigInt(revision) > 18446744073709551615n) return fail()
  const principal = id(row.principal_id, 'vpb')
  if (row.role !== 'routex-api-key-' + principal) return fail()
  return {
    schema_version: 1,
    key_id: id(row.key_id, keyPrefix(owner)),
    profile_id: id(row.profile_id, 'vap'),
    principal_id: principal,
    slot_id: id(row.slot_id, 'kds'),
    descriptor_revision: revision,
    endpoint: text(row.endpoint, 2048),
    namespace: text(row.namespace, 256, undefined, true),
    mount: text(row.mount, 128),
    path: text(row.path, 512),
    data_field: text(row.data_field, 64),
    version: 1,
    method: choice(row.method, ['jwt']),
    auth_mount: text(row.auth_mount, 128),
    role: row.role,
    coordinator: choice(row.coordinator, ['not_configured']),
    application_status: choice(row.application_status, ['unknown']),
  }
}
export function parseKeyDeliveryReceipt(
  value: unknown,
  owner: KeyDeliveryOwner = { kind: 'personal' },
): KeyDeliveryReceipt {
  const row = object(value, [
    'request_id',
    'key_id',
    'mode',
    'delivery_status',
    'key_status',
    'runtime_status',
    'failure_code',
    'descriptor',
    'review_etag',
  ])
  const result = {
    request_id: text(row.request_id, 36, uuid),
    key_id: id(row.key_id, keyPrefix(owner)),
    mode: choice(row.mode, ['managed_vault']),
    delivery_status: choice(row.delivery_status, [
      'pending',
      'acknowledged',
      'unknown',
      'not_attempted',
    ]),
    key_status: choice(row.key_status, ['pending', 'active', 'disabled', 'revoked']),
    runtime_status: choice(row.runtime_status, ['pending', 'confirmed', 'unknown']),
    failure_code: nullableCode(row.failure_code),
    descriptor: row.descriptor === null ? null : parseKeyDeliveryDescriptor(row.descriptor, owner),
    review_etag: text(row.review_etag, 64, review),
  }
  if (
    result.descriptor &&
    (result.descriptor.key_id !== result.key_id ||
      result.delivery_status !== 'acknowledged' ||
      result.runtime_status !== 'confirmed' ||
      result.key_status === 'revoked')
  )
    return fail()
  return result
}
function keyPrefix(owner: KeyDeliveryOwner): 'key' | 'pky' {
  ownerPath(owner)
  return owner.kind === 'personal' ? 'key' : 'pky'
}
function ownerPath(owner: KeyDeliveryOwner): string {
  if (owner.kind === 'personal') {
    object(owner, ['kind'])
    return '/keys'
  }
  object(owner, ['kind', 'projectId'])
  if (owner.kind !== 'project') return fail()
  return '/projects/' + id(owner.projectId, 'prj') + '/keys'
}
async function request<T>(
  method: 'get' | 'post' | 'put',
  url: string,
  parse: (value: unknown) => T,
  signal?: AbortSignal,
  write?: { input: unknown; etag: string; csrf: string },
  statuses = [200],
  hasReview = true,
): Promise<T> {
  if (write) {
    text(write.etag, 64, review)
    text(write.csrf, 64, review)
  }
  let response
  try {
    response = await client.request({
      method,
      url,
      data: write?.input,
      signal,
      headers: write ? { 'If-Match': `"${write.etag}"`, 'X-CSRF-Token': write.csrf } : undefined,
    })
  } catch (error) {
    throw new KeyDeliveryError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
  if (!statuses.includes(response.status)) return fail()
  const result = parse(response.data)
  if (hasReview) {
    const row = result as {
      review_etag?: string
      delivery?: { review_etag: string }
    }
    const etag = row.review_etag ?? row.delivery?.review_etag
    if (!etag || response.headers.etag !== `"${etag}"`) return fail()
  }
  return result
}
export function getKeyDeliveryPolicy(signal?: AbortSignal) {
  return request('get', admin + '/policy', parseKeyDeliveryPolicy, signal)
}
export function saveKeyDeliveryPolicy(
  input: KeyDeliveryPolicyInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  object(input, [
    'allowed_modes',
    'default_mode',
    'integration_id',
    'writer_identity_id',
    'provisioner_identity_id',
    'reason',
  ])
  reason(input.reason)
  const allowed = strings(input.allowed_modes, 2, (value) => choice(value, modes))
  if (!allowed.length || !allowed.includes(choice(input.default_mode, modes))) return fail()
  id(input.integration_id, 'vlt', true)
  id(input.writer_identity_id, 'kda', true)
  id(input.provisioner_identity_id, 'kda', true)
  return request('put', admin + '/policy', parseKeyDeliveryPolicy, signal, {
    input,
    etag,
    csrf,
  })
}
export function saveKeyDeliveryIdentity(
  input: KeyDeliveryIdentityInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  object(input, ['purpose', 'integration_id', 'token', 'reason'])
  choice(input.purpose, ['writer', 'provisioner'])
  id(input.integration_id, 'vlt')
  text(input.token, 4096, /^[\x21-\x7e]+$/)
  reason(input.reason)
  return request(
    'post',
    admin + '/identities',
    parseKeyDeliveryIdentity,
    signal,
    { input, etag, csrf },
    [200, 201],
  )
}
export async function getKeyDeliveryIdentity(identity: string, signal?: AbortSignal) {
  const result = await request(
    'get',
    admin + '/identities/' + id(identity, 'kda'),
    parseKeyDeliveryIdentity,
    signal,
  )
  return result.id === identity ? result : fail()
}
export function getKeyCallerTemplate(signal?: AbortSignal) {
  return request('get', admin + '/caller-template', parseKeyCallerTemplate, signal)
}
export function saveKeyCallerTemplate(
  input: KeyCallerTemplateInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  object(input, [
    'auth_mount',
    'issuer',
    'jwks_url',
    'audience',
    'allowed_subjects',
    'token_ttl_seconds',
    'token_max_ttl_seconds',
    'reason',
  ])
  const { reason: explanation, ...template } = input
  reason(explanation)
  parseKeyCallerTemplate({ ...template, verified: false, review_etag: etag })
  if (!input.token_ttl_seconds || !input.allowed_subjects.length) return fail()
  return request('put', admin + '/caller-template', parseKeyCallerTemplate, signal, {
    input,
    etag,
    csrf,
  })
}
export function verifyKeyDelivery(
  reason: string,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  const explanation = text(reason, 4000)
  if ([...explanation].length > 1000 || explanation.trim() !== explanation) return fail()
  return request('post', admin + '/verify', parseKeyDeliveryPolicy, signal, {
    input: { reason },
    etag,
    csrf,
  })
}
export function getKeyDeliveryTargets(cursor = '', signal?: AbortSignal) {
  if (cursor) id(cursor, 'vap')
  return request(
    'get',
    admin + '/targets?limit=20' + (cursor ? '&cursor=' + cursor : ''),
    parseKeyDeliveryTargetPage,
    signal,
    undefined,
    [200],
    false,
  )
}
export async function reviewKeyDeliveryTarget(targetId: string, signal?: AbortSignal) {
  const result = await request(
    'get',
    admin + '/targets/' + id(targetId, 'vap'),
    parseKeyDeliveryTargetReview,
    signal,
  )
  return result.id === targetId ? result : fail()
}
export async function updateKeyDeliveryTarget(
  targetId: string,
  action: 'approval' | 'isolation' | 'reconcile',
  input:
    | { approved: boolean; control_verified: boolean; reason: string }
    | { isolated: boolean; reason: string }
    | { request_id: string; reason: string },
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  if (!['approval', 'isolation', 'reconcile'].includes(action)) return fail()
  const fields =
    action === 'approval'
      ? ['approved', 'control_verified', 'reason']
      : action === 'isolation'
        ? ['isolated', 'reason']
        : ['request_id', 'reason']
  const row = object(input, fields)
  reason(row.reason)
  if (action === 'approval') {
    if (bool(row.approved) !== bool(row.control_verified)) return fail()
  } else if (action === 'isolation') bool(row.isolated)
  else text(row.request_id, 36, uuid)
  const result = await request(
    action === 'reconcile' ? 'post' : 'put',
    admin + '/targets/' + id(targetId, 'vap') + '/' + action,
    parseKeyDeliveryTarget,
    signal,
    { input, etag, csrf },
  )
  return result.id === targetId ? result : fail()
}
export function getKeyDeliveryContext(owner: KeyDeliveryOwner, signal?: AbortSignal) {
  return request('get', ownerPath(owner) + '/delivery-context', parseKeyDeliveryContext, signal)
}
export function resolveKeyDeliveryTarget(
  owner: KeyDeliveryOwner,
  input: KeyDeliveryResolveInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  object(input, [
    'request_id',
    'consumer_id',
    'application_name',
    'environment',
    'principal',
    'reason',
  ])
  text(input.request_id, 36, uuid)
  text(input.consumer_id, 64, input.consumer_id ? code : undefined, true)
  label(input.application_name)
  text(input.environment, 64, code)
  principal(input.principal)
  reason(input.reason)
  return request(
    'post',
    ownerPath(owner) + '/delivery-targets/resolve',
    parseKeyDeliveryTarget,
    signal,
    { input, etag, csrf },
    [200, 201],
  )
}
export async function getKeyDeliveryTarget(
  owner: KeyDeliveryOwner,
  targetId: string,
  signal?: AbortSignal,
) {
  const result = await request(
    'get',
    ownerPath(owner) + '/delivery-targets/' + id(targetId, 'vap'),
    parseKeyDeliveryTarget,
    signal,
  )
  return result.id === targetId ? result : fail()
}
export async function getKeyDeliveryReceipt(
  owner: KeyDeliveryOwner,
  requestId: string,
  signal?: AbortSignal,
) {
  const scope = { ...owner }
  const result = await request(
    'get',
    ownerPath(scope) + '/delivery-receipts/' + text(requestId, 36, uuid),
    (value) => parseKeyDeliveryReceipt(value, scope),
    signal,
  )
  return result.request_id === requestId ? result : fail()
}
export async function getKeyDeliveryDescriptor(
  owner: KeyDeliveryOwner,
  keyId: string,
  signal?: AbortSignal,
) {
  const scope = { ...owner }
  const result = await request(
    'get',
    ownerPath(scope) + '/' + id(keyId, keyPrefix(scope)) + '/delivery-descriptor',
    (value) => parseKeyDeliveryDescriptor(value, scope),
    signal,
    undefined,
    [200],
    false,
  )
  return result.key_id === keyId ? result : fail()
}

// Tokens are accepted only by the identity write call. Managed-Key responses
// contain metadata and receipts; no function reconstructs or accepts a bearer.
export function keyDeliveryPrincipal(subject: string): KeyDeliveryPrincipal {
  return { method: 'jwt', subject: text(subject, 256) }
}

export function parseManagedKeyResult(value: unknown, owner: KeyDeliveryOwner): ManagedKeyResult {
  const row = object(value, ['key', 'delivery'])
  const fields = [
    'id',
    'name',
    'prefix',
    'status',
    'model_ids',
    'expires_at',
    'created_at',
    'replaces_key_id',
    'delivery_expires_at',
    'delivery_mode',
  ]
  const key = object(
    row.key,
    owner.kind === 'personal' ? fields : [...fields, 'project_id', 'creator_id'],
  )
  const result = {
    id: id(key.id, keyPrefix(owner)),
    name: label(key.name),
    prefix: text(key.prefix, 64),
    status: choice(key.status, ['pending', 'active', 'disabled', 'revoked']),
    model_ids: strings(key.model_ids, 128, (value) => id(value, 'mdl')),
    expires_at: nullableStamp(key.expires_at),
    created_at: stamp(key.created_at),
    replaces_key_id:
      key.replaces_key_id === null ? null : id(key.replaces_key_id, keyPrefix(owner)),
    delivery_expires_at: nullableStamp(key.delivery_expires_at),
    delivery_mode: choice(key.delivery_mode, ['managed_vault']),
  }
  const delivery = parseKeyDeliveryReceipt(row.delivery, owner)
  if (result.id !== delivery.key_id || result.status !== delivery.key_status) return fail()
  if (owner.kind === 'personal') return { key: result, delivery }
  const projectId = id(key.project_id, 'prj')
  if (projectId !== owner.projectId) return fail()
  return {
    key: {
      ...result,
      project_id: projectId,
      creator_id: id(key.creator_id, 'usr'),
    },
    delivery,
  }
}
function managedInput(input: ManagedKeyInput, rotation: boolean) {
  object(input, ['name', 'model_ids', 'expires_at', 'request_id', 'reason', 'delivery'])
  if (rotation) {
    if (input.name !== '' || input.model_ids.length !== 0 || input.expires_at !== null)
      return fail()
  } else label(input.name)
  strings(input.model_ids, 128, (value) => id(value, 'mdl'))
  nullableStamp(input.expires_at)
  text(input.request_id, 36, uuid)
  reason(input.reason)
  const selection = object(input.delivery, [
    'consumer_id',
    'environment',
    'principal',
    'target_review',
  ])
  text(selection.consumer_id, 64, code)
  text(selection.environment, 64, code)
  principal(selection.principal)
  text(selection.target_review, 64, review)
}
export async function issueManagedKey(
  owner: KeyDeliveryOwner,
  input: ManagedKeyInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
  predecessor?: string,
): Promise<ManagedKeyResult> {
  const scope = { ...owner }
  const path = ownerPath(scope)
  managedInput(input, predecessor !== undefined)
  const route =
    predecessor === undefined
      ? '/managed'
      : '/' + id(predecessor, keyPrefix(scope)) + '/managed-rotation'
  const result = await request(
    'post',
    path + route,
    (value) => parseManagedKeyResult(value, scope),
    signal,
    { input, etag, csrf },
    [200, 201, 202],
  )
  if (
    result.delivery.request_id !== input.request_id ||
    result.key.replaces_key_id !== (predecessor ?? null)
  )
    return fail()
  return result
}
export async function reconcileManagedKey(
  owner: KeyDeliveryOwner,
  keyId: string,
  input: { request_id: string; reason: string },
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  const scope = { ...owner }
  object(input, ['request_id', 'reason'])
  text(input.request_id, 36, uuid)
  reason(input.reason)
  const result = await request(
    'post',
    ownerPath(scope) + '/' + id(keyId, keyPrefix(scope)) + '/delivery-reconcile',
    (value) => parseManagedKeyResult(value, scope),
    signal,
    { input, etag, csrf },
    [200, 202],
  )
  return result.key.id === keyId && result.delivery.request_id === input.request_id
    ? result
    : fail()
}
