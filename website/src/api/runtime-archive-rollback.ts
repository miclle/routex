import axios from 'axios'
import client from './client'
import type {
  RuntimeArchiveRollbackInput,
  RuntimeArchiveRollbackPreview,
  RuntimeArchiveRollbackReceipt,
} from '@/types/runtime-archive-rollback'

export class RuntimeArchiveRollbackError extends Error {
  constructor(public readonly status: number) {
    super('Configuration archive rollback failed')
  }
}

const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const keys = (v: Record<string, unknown>, expected: string[]) =>
  Object.keys(v).sort().join(',') === [...expected].sort().join(',')
const text = (v: unknown, max = 4000): v is string =>
  typeof v === 'string' && v.length > 0 && v.length <= max
const nullableText = (v: unknown) => v === null || text(v)
const archiveID = (v: unknown): v is string =>
  typeof v === 'string' && /^rcv_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(v)
const uuid = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$(?![\s\S])/.test(v)
const utc = (v: unknown) =>
  typeof v === 'string' &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$(?![\s\S])/.test(v) &&
  Number.isFinite(Date.parse(v)) &&
  new Date(v).toISOString().slice(0, 19) === v.slice(0, 19)
const protocol = (v: unknown) =>
  typeof v === 'string' &&
  ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'].includes(v)
const weight = (v: unknown) => typeof v === 'number' && Number.isInteger(v) && v >= 0 && v <= 100

// Keep the server's sub-millisecond ordering at this proof boundary.
function instant(value: string): bigint {
  const fraction = value.match(/\.(\d+)Z$/)?.[1] ?? ''
  return BigInt(Date.parse(value.slice(0, 19) + 'Z')) * 1_000_000n + BigInt(fraction.padEnd(9, '0'))
}

function acknowledgement(value: unknown, publication: Record<string, unknown>): boolean {
  if (value === null) return true
  if (
    !record(value) ||
    !keys(value, [
      'scope',
      'installation_id',
      'instance_id',
      'instance_started_at',
      'snapshot_id',
      'routes_published_at',
      'acknowledged_at',
    ]) ||
    value.scope !== 'single_process_gateway_admission' ||
    !text(value.installation_id, 100) ||
    !/^rin_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(value.installation_id) ||
    value.instance_id !== publication.claim_instance_id ||
    value.instance_started_at !== publication.claim_instance_started_at ||
    !text(value.snapshot_id, 100) ||
    !/^cfg_[0-7][0-9a-hjkmnp-tv-z]{25}$(?![\s\S])/.test(value.snapshot_id) ||
    !utc(value.routes_published_at) ||
    !utc(value.acknowledged_at)
  )
    return false
  const acknowledgedAt = instant(value.acknowledged_at as string)
  return (
    acknowledgedAt >= instant(publication.claimed_at as string) &&
    acknowledgedAt >= instant(publication.claim_instance_started_at as string) &&
    instant(value.routes_published_at as string) >=
      instant(publication.claim_instance_started_at as string) &&
    acknowledgedAt >= instant(value.routes_published_at as string)
  )
}

export function validRuntimeArchiveRollbackReason(value: string) {
  return (
    value.length > 0 &&
    !/^\p{White_Space}|\p{White_Space}$/u.test(value) &&
    Array.from(value).length <= 1000 &&
    !/[\p{Cc}\uD800-\uDFFF]/u.test(value)
  )
}

function invalid(): never {
  throw new RuntimeArchiveRollbackError(0)
}
function safeError(error: unknown): never {
  if (error instanceof RuntimeArchiveRollbackError) throw error
  throw new RuntimeArchiveRollbackError(
    axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0,
  )
}
function connection(v: unknown) {
  return (
    record(v) &&
    keys(v, ['base_url', 'protocol', 'adapter', 'api_version', 'egress_mode', 'egress_id']) &&
    text(v.base_url, 8192) &&
    protocol(v.protocol) &&
    (v.adapter === 'native' || v.adapter === 'azure_openai_classic') &&
    nullableText(v.api_version) &&
    typeof v.egress_mode === 'string' &&
    ['', 'default', 'direct', 'proxy'].includes(v.egress_mode) &&
    (v.egress_mode === 'proxy' ? text(v.egress_id, 100) : v.egress_id === null)
  )
}
function declaration(v: unknown) {
  return (
    record(v) &&
    keys(v, ['image_input', 'pdf_input']) &&
    typeof v.image_input === 'boolean' &&
    typeof v.pdf_input === 'boolean'
  )
}
function rows(v: unknown, identity: string, valid: (row: Record<string, unknown>) => boolean) {
  if (!Array.isArray(v) || v.length > 1000) return false
  const seen = new Set<string>()
  return v.every((row) => {
    if (!record(row) || !text(row[identity], 100) || seen.has(row[identity]) || !valid(row))
      return false
    seen.add(row[identity])
    return true
  })
}

export async function previewRuntimeArchiveRollback(
  id: string,
  signal?: AbortSignal,
): Promise<RuntimeArchiveRollbackPreview> {
  if (!archiveID(id)) invalid()
  try {
    const response = await client.get<unknown>(`/admin/runtime/archives/${id}/rollback`, { signal })
    const v = response.data
    const etag = response.headers.etag
    if (
      response.status !== 200 ||
      typeof etag !== 'string' ||
      !/^"[a-f0-9]{64}"$(?![\s\S])/.test(etag) ||
      !record(v) ||
      !keys(v, [
        'scope',
        'archive_id',
        'archive_created_at',
        'observed_at',
        'can_apply',
        'blocker_codes',
        'connections',
        'declarations',
        'weights',
      ]) ||
      v.scope !== 'gateway_routing_configuration' ||
      v.archive_id !== id ||
      !utc(v.archive_created_at) ||
      !utc(v.observed_at) ||
      typeof v.can_apply !== 'boolean' ||
      !Array.isArray(v.blocker_codes) ||
      v.blocker_codes.length > 100 ||
      !v.blocker_codes.every(
        (code) => typeof code === 'string' && /^[a-z][a-z0-9_]{0,99}$(?![\s\S])/.test(code),
      ) ||
      new Set(v.blocker_codes).size !== v.blocker_codes.length ||
      v.can_apply !== (v.blocker_codes.length === 0) ||
      !rows(
        v.connections,
        'connection_id',
        (row) =>
          keys(row, [
            'connection_id',
            'name',
            'enabled',
            'before',
            'after',
            'requires_verification',
          ]) &&
          text(row.name) &&
          typeof row.enabled === 'boolean' &&
          row.requires_verification === true &&
          connection(row.before) &&
          connection(row.after) &&
          record(row.before) &&
          record(row.after) &&
          row.before.protocol === row.after.protocol,
      ) ||
      !rows(
        v.declarations,
        'provider_model_id',
        (row) =>
          keys(row, ['provider_model_id', 'before', 'after']) &&
          declaration(row.before) &&
          declaration(row.after),
      ) ||
      !rows(
        v.weights,
        'binding_id',
        (row) =>
          keys(row, [
            'binding_id',
            'model_id',
            'provider_model_id',
            'protocol',
            'before',
            'after',
          ]) &&
          text(row.model_id, 100) &&
          text(row.provider_model_id, 100) &&
          protocol(row.protocol) &&
          weight(row.before) &&
          weight(row.after),
      )
    )
      invalid()
    return { ...v, etag: etag.slice(1, -1) } as unknown as RuntimeArchiveRollbackPreview
  } catch (error) {
    return safeError(error)
  }
}

function receipt(v: unknown, id: string, requestID: string): RuntimeArchiveRollbackReceipt {
  if (
    !record(v) ||
    !keys(v, [
      'scope',
      'request_id',
      'archive_id',
      'archive_created_at',
      'applied_at',
      'changed',
      'reason',
      'publication',
      'runtime_applied',
      'superseded',
    ]) ||
    v.scope !== 'gateway_routing_configuration' ||
    v.request_id !== requestID ||
    v.archive_id !== id ||
    !utc(v.archive_created_at) ||
    !utc(v.applied_at) ||
    typeof v.changed !== 'boolean' ||
    typeof v.reason !== 'string' ||
    !validRuntimeArchiveRollbackReason(v.reason) ||
    typeof v.runtime_applied !== 'boolean' ||
    typeof v.superseded !== 'boolean' ||
    (v.runtime_applied && v.superseded)
  )
    invalid()
  if (v.publication === null) invalid()
  {
    const p = v.publication
    if (
      !record(p) ||
      !keys(p, [
        'request_id',
        'actor_id',
        'reason',
        'claim_instance_id',
        'claim_instance_started_at',
        'claimed_at',
        'finalized_at',
        'status',
        'failure_code',
        'acknowledgement',
      ]) ||
      p.request_id !== requestID ||
      p.reason !== v.reason ||
      !text(p.actor_id, 100) ||
      !text(p.claim_instance_id, 100) ||
      !utc(p.claim_instance_started_at) ||
      !utc(p.claimed_at) ||
      !(p.finalized_at === null || utc(p.finalized_at)) ||
      typeof p.status !== 'string' ||
      !['unknown', 'pending', 'refresh_returned_nil', 'refresh_returned_error'].includes(
        p.status,
      ) ||
      !(p.failure_code === null || p.failure_code === 'runtime_refresh_failed') ||
      !acknowledgement(p.acknowledgement, p) ||
      ((p.status === 'unknown' || p.status === 'pending') &&
        (p.finalized_at !== null || p.failure_code !== null)) ||
      (p.status === 'refresh_returned_nil' &&
        (p.finalized_at === null || p.failure_code !== null)) ||
      (p.status === 'refresh_returned_error' &&
        (p.finalized_at === null || p.failure_code !== 'runtime_refresh_failed'))
    )
      invalid()
  }
  return v as unknown as RuntimeArchiveRollbackReceipt
}

export async function applyRuntimeArchiveRollback(
  id: string,
  input: RuntimeArchiveRollbackInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<RuntimeArchiveRollbackReceipt> {
  if (
    !archiveID(id) ||
    !uuid(input.request_id) ||
    !validRuntimeArchiveRollbackReason(input.reason) ||
    !/^[a-f0-9]{64}$(?![\s\S])/.test(etag) ||
    !csrf
  )
    invalid()
  try {
    const response = await client.post<unknown>(`/admin/runtime/archives/${id}/rollback`, input, {
      signal,
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    })
    if (response.status !== 200) invalid()
    const result = receipt(response.data, id, input.request_id)
    if (result.reason !== input.reason) invalid()
    return result
  } catch (error) {
    return safeError(error)
  }
}

export async function getRuntimeArchiveRollbackReceipt(
  id: string,
  requestID: string,
  signal?: AbortSignal,
): Promise<RuntimeArchiveRollbackReceipt> {
  if (!archiveID(id) || !uuid(requestID)) invalid()
  try {
    const response = await client.get<unknown>(`/admin/runtime/archive-rollbacks/${requestID}`, {
      signal,
    })
    if (response.status !== 200) invalid()
    return receipt(response.data, id, requestID)
  } catch (error) {
    return safeError(error)
  }
}
