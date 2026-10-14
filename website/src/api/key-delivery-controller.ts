import axios from 'axios'
import client from './client'
import type {
  DeliveryControllerAssignmentInput,
  DeliveryControllerConfiguration,
  DeliveryControllerDetail,
  DeliveryControllerEnrollmentInput,
  DeliveryControllerEnrollmentReceipt,
  DeliveryControllerEnrollmentResult,
  DeliveryControllerPage,
  DeliveryControllerReport,
  DeliveryControllerReview,
  DeliveryControllerSummary,
  DeliverySlotSummary,
} from '@/types/key-delivery-controller'

const base = '/admin/api-key-delivery'
const digest = /^[a-f0-9]{64}$/
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/
const maxRevision = 9223372036854775807n
export class DeliveryControllerError extends Error {
  constructor(public readonly status: number) {
    super('Controller operation unavailable')
  }
}
function fail(): never {
  throw new DeliveryControllerError(0)
}
function object(value: unknown, fields: string[]) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail()
  const row = value as Record<string, unknown>
  if (
    Object.keys(row).length !== fields.length ||
    fields.some((field) => !Object.hasOwn(row, field))
  )
    return fail()
  return row
}
function text(value: unknown, maximum = 1000): string {
  if (
    typeof value !== 'string' ||
    !value ||
    new TextEncoder().encode(value).length > maximum ||
    /[\p{Cc}\p{Cs}]/u.test(value)
  )
    return fail()
  return value
}
function matches(value: unknown, pattern: RegExp, maximum = 1000) {
  const result = text(value, maximum)
  return pattern.test(result) ? result : fail()
}
function id(value: unknown, prefix: string) {
  return matches(value, new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`), 30)
}
function stamp(value: unknown): string {
  const result = matches(value, /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?Z$/, 40)
  return Number.isFinite(Date.parse(result)) ? result : fail()
}
function nullableStamp(value: unknown) {
  return value === null ? null : stamp(value)
}
function choice<T extends string>(value: unknown, values: readonly T[]): T {
  return values.includes(value as T) ? (value as T) : fail()
}
function revision(value: unknown) {
  const result = matches(value, /^[1-9][0-9]{0,18}$/, 19)
  return BigInt(result) <= maxRevision ? result : fail()
}
function review(value: unknown) {
  return matches(value, digest, 64)
}
function name(value: unknown) {
  const result = text(value, 100)
  return result.trim() === result ? result : fail()
}
function requestID(value: unknown) {
  return matches(value, uuid, 36)
}
function reason(value: unknown) {
  const result = text(value, 1000)
  return result.trim() === result ? result : fail()
}
export function validControllerReason(value: string) {
  try {
    reason(value)
    return true
  } catch {
    return false
  }
}
function configuration(value: unknown, slot: string): DeliveryControllerConfiguration {
  const row = object(value, [
    'schema_version',
    'slot_id',
    'descriptor_revision',
    'key_id',
    'configuration_digest',
    'agent_configuration',
    'jwt_file',
    'key_file',
  ])
  if (
    row.schema_version !== 1 ||
    id(row.slot_id, 'kds') !== slot ||
    row.jwt_file !== '/var/run/secrets/routex/workload.jwt' ||
    row.key_file !== `/var/run/secrets/routex/api-keys/${slot}` ||
    typeof row.agent_configuration !== 'string' ||
    !row.agent_configuration ||
    new TextEncoder().encode(row.agent_configuration).length > 16384 ||
    /\p{Cs}/u.test(row.agent_configuration) ||
    [...row.agent_configuration].some((character) => {
      const code = character.charCodeAt(0)
      return code === 127 || (code < 32 && code !== 9 && code !== 10 && code !== 13)
    })
  )
    return fail()
  return {
    schema_version: 1,
    slot_id: slot,
    descriptor_revision: revision(row.descriptor_revision),
    key_id: id(row.key_id, '(?:key|pky)'),
    configuration_digest: review(row.configuration_digest),
    agent_configuration: row.agent_configuration,
    jwt_file: row.jwt_file,
    key_file: row.key_file,
  } as DeliveryControllerConfiguration
}
function report(value: unknown): DeliveryControllerReport {
  const row = object(value, [
    'request_id',
    'descriptor_revision',
    'configuration_digest',
    'agent_state',
    'output_state',
    'recorded_at',
    'evidence_kind',
  ])
  return {
    request_id: requestID(row.request_id),
    descriptor_revision: revision(row.descriptor_revision),
    configuration_digest: review(row.configuration_digest),
    agent_state: choice(row.agent_state, ['unknown', 'running', 'failed']),
    output_state: choice(row.output_state, ['unknown', 'written', 'failed']),
    recorded_at: stamp(row.recorded_at),
    evidence_kind: choice(row.evidence_kind, ['controller_report']),
  }
}
export function parseControllerDetail(
  value: unknown,
  controller: string | undefined,
  slot: string,
): DeliveryControllerDetail {
  const row = object(value, [
    'id',
    'name',
    'slot_id',
    'owner_kind',
    'owner_id',
    'expires_at',
    'revoked_at',
    'assigned_descriptor_revision',
    'assigned_configuration_digest',
    'current_configuration',
    'configuration_status',
    'last_acknowledgement',
    'review_etag',
  ])
  const controllerID = id(row.id, 'kcg'),
    kind = choice(row.owner_kind, ['personal', 'project']),
    status = choice(row.configuration_status, [
      'assigned',
      'review_required',
      'unavailable',
      'revoked',
      'expired',
    ])
  if ((controller && controllerID !== controller) || id(row.slot_id, 'kds') !== slot) return fail()
  const current =
    row.current_configuration === null ? null : configuration(row.current_configuration, slot)
  const revoked = nullableStamp(row.revoked_at)
  if (
    (status === 'assigned' || status === 'review_required') !== !!current ||
    (status === 'revoked' && revoked === null) ||
    (status !== 'revoked' && revoked !== null)
  )
    return fail()
  const result: DeliveryControllerDetail = {
    id: controllerID,
    name: name(row.name),
    slot_id: slot,
    owner_kind: kind,
    owner_id: id(row.owner_id, kind === 'personal' ? 'usr' : 'prj'),
    expires_at: stamp(row.expires_at),
    revoked_at: revoked,
    assigned_descriptor_revision: revision(row.assigned_descriptor_revision),
    assigned_configuration_digest: review(row.assigned_configuration_digest),
    current_configuration: current,
    configuration_status: status,
    last_acknowledgement:
      row.last_acknowledgement === null ? null : report(row.last_acknowledgement),
    review_etag: review(row.review_etag),
  }
  if (
    status === 'assigned' &&
    (current!.descriptor_revision !== result.assigned_descriptor_revision ||
      current!.configuration_digest !== result.assigned_configuration_digest)
  )
    return fail()
  return result
}
function summary(value: unknown, slot: string): DeliveryControllerSummary {
  const row = object(value, [
    'id',
    'birth',
    'name',
    'slot_id',
    'expires_at',
    'revoked_at',
    'lifecycle_status',
    'configuration_status',
  ])
  const status = choice(row.lifecycle_status, ['active', 'expired', 'revoked']),
    revoked = nullableStamp(row.revoked_at)
  if (id(row.slot_id, 'kds') !== slot || (status === 'revoked') !== (revoked !== null))
    return fail()
  return {
    id: id(row.id, 'kcg'),
    birth: stamp(row.birth),
    name: name(row.name),
    slot_id: slot,
    expires_at: stamp(row.expires_at),
    revoked_at: revoked,
    lifecycle_status: status,
    configuration_status: choice(row.configuration_status, ['not_reviewed']),
  }
}
function page<T extends { id: string }>(
  value: unknown,
  parse: (value: unknown) => T,
): DeliveryControllerPage<T> {
  const row = object(value, ['items', 'next_cursor'])
  if (
    !Array.isArray(row.items) ||
    row.items.length > 50 ||
    typeof row.next_cursor !== 'string' ||
    row.next_cursor.length > 512 ||
    (row.next_cursor && !/^[A-Za-z0-9_-]+$/.test(row.next_cursor))
  )
    return fail()
  const items = row.items.map(parse)
  if (
    new Set(items.map((item) => item.id)).size !== items.length ||
    (row.next_cursor && !items.length)
  )
    return fail()
  return { items, next_cursor: row.next_cursor }
}
function pagination(cursor: string) {
  if (cursor.length > 512 || (cursor && !/^[A-Za-z0-9_-]+$/.test(cursor))) return fail()
  return cursor ? `?cursor=${encodeURIComponent(cursor)}&limit=20` : '?limit=20'
}
async function safe<T>(fn: () => Promise<T>) {
  try {
    return await fn()
  } catch (error) {
    if (error instanceof DeliveryControllerError) throw error
    // Do not retain Axios request bodies, response objects or machine bearers.
    throw new DeliveryControllerError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
function checkedETag(headers: unknown, token: string) {
  const value = (headers as { etag?: unknown })?.etag
  if (value !== `"${token}"`) return fail()
}
export async function getControllerSlots(target: string, cursor = '', signal?: AbortSignal) {
  id(target, 'vap')
  return safe(async () => {
    const r = await client.get(`${base}/targets/${target}/slots${pagination(cursor)}`, { signal })
    return page<DeliverySlotSummary>(r.data, (value) => {
      const row = object(value, [
        'id',
        'birth',
        'profile_id',
        'owner_kind',
        'owner_id',
        'descriptor_revision',
        'configuration_status',
      ])
      const kind = choice(row.owner_kind, ['personal', 'project'])
      if (id(row.profile_id, 'vap') !== target) return fail()
      return {
        id: id(row.id, 'kds'),
        birth: stamp(row.birth),
        profile_id: target,
        owner_kind: kind,
        owner_id: id(row.owner_id, kind === 'personal' ? 'usr' : 'prj'),
        descriptor_revision: revision(row.descriptor_revision),
        configuration_status: choice(row.configuration_status, ['not_reviewed']),
      }
    })
  })
}
export async function getControllers(slot: string, cursor = '', signal?: AbortSignal) {
  id(slot, 'kds')
  return safe(async () => {
    const r = await client.get(`${base}/slots/${slot}/controllers${pagination(cursor)}`, { signal })
    return page(r.data, (value) => summary(value, slot))
  })
}
export async function getControllerReview(
  slot: string,
  signal?: AbortSignal,
): Promise<DeliveryControllerReview> {
  id(slot, 'kds')
  return safe(async () => {
    const r = await client.get(`${base}/slots/${slot}/controller-configuration`, { signal })
    const row = object(r.data, ['configuration', 'review_etag'])
    const result = {
      configuration: configuration(row.configuration, slot),
      review_etag: review(row.review_etag),
    }
    checkedETag(r.headers, result.review_etag)
    return result
  })
}
export async function getController(controller: string, slot: string, signal?: AbortSignal) {
  id(controller, 'kcg')
  id(slot, 'kds')
  return safe(async () => {
    const r = await client.get(`${base}/controllers/${controller}`, { signal })
    const result = parseControllerDetail(r.data, controller, slot)
    checkedETag(r.headers, result.review_etag)
    return result
  })
}
function writeHeaders(etag: string, csrf: string) {
  review(etag)
  text(csrf, 256)
  return { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf }
}
export async function enrollController(
  slot: string,
  input: DeliveryControllerEnrollmentInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<DeliveryControllerEnrollmentResult> {
  id(slot, 'kds')
  requestID(input.request_id)
  name(input.name)
  stamp(input.expires_at)
  reason(input.reason)
  revision(input.descriptor_revision)
  review(input.configuration_digest)
  if (input.opt_in !== true) return fail()
  return safe(async () => {
    const r = await client.post(`${base}/slots/${slot}/controllers`, input, {
      headers: writeHeaders(etag, csrf),
      signal,
    })
    const row = object(r.data, ['controller', 'bearer', 'secret_status'])
    const controller = parseControllerDetail(row.controller, undefined, slot)
    checkedETag(r.headers, controller.review_etag)
    const status = choice(row.secret_status, ['display_once', 'not_recoverable'])
    let bearer: string | null = null
    if (r.status === 201 && status === 'display_once')
      bearer = matches(row.bearer, /^rxdctl_[A-Za-z0-9_-]{43}$/, 50)
    else if (r.status !== 200 || status !== 'not_recoverable' || row.bearer !== null) return fail()
    return { controller, bearer, secret_status: status }
  })
}
export async function assignController(
  controller: string,
  slot: string,
  input: DeliveryControllerAssignmentInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  id(controller, 'kcg')
  id(slot, 'kds')
  requestID(input.request_id)
  reason(input.reason)
  revision(input.descriptor_revision)
  review(input.configuration_digest)
  if (input.opt_in !== true) return fail()
  return safe(async () => {
    const r = await client.put(`${base}/controllers/${controller}/configuration`, input, {
      headers: writeHeaders(etag, csrf),
      signal,
    })
    const result = parseControllerDetail(r.data, controller, slot)
    checkedETag(r.headers, result.review_etag)
    return result
  })
}
export async function revokeController(
  controller: string,
  slot: string,
  reasonValue: string,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
) {
  id(controller, 'kcg')
  id(slot, 'kds')
  reason(reasonValue)
  return safe(async () => {
    const r = await client.post(
      `${base}/controllers/${controller}/revoke`,
      { reason: reasonValue },
      { headers: writeHeaders(etag, csrf), signal },
    )
    const result = parseControllerDetail(r.data, controller, slot)
    checkedETag(r.headers, result.review_etag)
    if (result.configuration_status !== 'revoked' || result.revoked_at === null) return fail()
    return result
  })
}
export async function getControllerEnrollmentReceipt(
  slot: string,
  request: string,
  signal?: AbortSignal,
): Promise<DeliveryControllerEnrollmentReceipt> {
  id(slot, 'kds')
  requestID(request)
  return safe(async () => {
    const r = await client.get(`${base}/slots/${slot}/controller-enrollments/${request}`, {
      signal,
    })
    const row = object(r.data, [
      'request_id',
      'intent_digest',
      'enrollment_review',
      'controller',
      'bearer',
      'secret_status',
    ])
    if (
      requestID(row.request_id) !== request ||
      row.bearer !== null ||
      row.secret_status !== 'not_recoverable'
    )
      return fail()
    return {
      request_id: request,
      intent_digest: review(row.intent_digest),
      enrollment_review: review(row.enrollment_review),
      controller: summary(row.controller, slot),
      bearer: null,
      secret_status: 'not_recoverable',
    }
  })
}
