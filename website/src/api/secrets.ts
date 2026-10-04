import axios from 'axios'
import client from './client'
import {
  secretDomains,
  type SecretIntent,
  type SecretResult,
  type SecretRotation,
  type SecretStore,
} from '@/types/secrets'

export class SecretError extends Error {
  constructor(public readonly status: number) {
    super('Secret store request failed')
  }
}
const rootID = /^[A-Za-z0-9_-]{1,64}$/
const rotationID = /^srt_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const etag = /^[0-9a-f]{64}$/
const counter = /^(0|[1-9][0-9]*)$/
function fail(): never {
  throw new SecretError(0)
}
function object(value: unknown, fields: string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail()
  const result = value as Record<string, unknown>
  if (
    Object.keys(result).length !== fields.length ||
    fields.some((field) => !Object.hasOwn(result, field))
  )
    return fail()
  return result
}
function string(value: unknown, pattern?: RegExp, maximum = 128): string {
  if (
    typeof value !== 'string' ||
    !value ||
    value.length > maximum ||
    (pattern && !pattern.test(value))
  )
    return fail()
  return value
}
function count(value: unknown): string {
  const result = string(value, counter, 20)
  if (BigInt(result) > 18446744073709551615n) return fail()
  return result
}
function boolean(value: unknown): boolean {
  return typeof value === 'boolean' ? value : fail()
}
function nullable<T>(value: unknown, parse: (value: unknown) => T): T | null {
  return value === null ? null : parse(value)
}
function date(value: unknown): string {
  const result = string(
    value,
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/,
    40,
  )
  if (!Number.isFinite(Date.parse(result))) return fail()
  return result
}
function oneOf<T extends string>(value: unknown, values: readonly T[]): T {
  return values.includes(value as T) ? (value as T) : fail()
}
function array<T>(value: unknown, parse: (value: unknown, index: number) => T, max = 64): T[] {
  if (!Array.isArray(value) || value.length > max) return fail()
  return value.map(parse)
}
function unique<T>(values: T[]): T[] {
  if (new Set(values).size !== values.length) return fail()
  return values
}
function rotation(value: unknown): SecretRotation {
  const row = object(value, [
    'id',
    'status',
    'phase',
    'source_key_id',
    'target_key_id',
    'domains',
    'blocker_codes',
    'observation_started_at',
    'observation_eligible_at',
    'allowed_actions',
  ])
  const domains = array(
    row.domains,
    (value, index) => {
      const domain = object(value, [
        'code',
        'scanned',
        'rewrapped',
        'already_target',
        'deleted',
        'changed',
        'blocked',
      ])
      if (domain.code !== secretDomains[index]) return fail()
      return {
        code: secretDomains[index],
        scanned: count(domain.scanned),
        rewrapped: count(domain.rewrapped),
        already_target: count(domain.already_target),
        deleted: count(domain.deleted),
        changed: count(domain.changed),
        blocked: count(domain.blocked),
      }
    },
    5,
  )
  if (domains.length !== 5) return fail()
  return {
    id: string(row.id, rotationID),
    status: oneOf(row.status, [
      'migrating',
      'blocked',
      'observing',
      'ready',
      'completed',
      'rolled_back',
    ]),
    phase: oneOf(row.phase, ['migration', 'verification', 'observation', 'completed']),
    source_key_id: string(row.source_key_id, rootID),
    target_key_id: string(row.target_key_id, rootID),
    domains,
    blocker_codes: unique(
      array(row.blocker_codes, (value) => string(value, /^[a-z][a-z0-9_]{0,63}$/)),
    ),
    observation_started_at: nullable(row.observation_started_at, date),
    observation_eligible_at: nullable(row.observation_eligible_at, date),
    allowed_actions: unique(
      array(
        row.allowed_actions,
        (value) => oneOf(value, ['resume', 'retire', 'rollback'] as const),
        3,
      ),
    ),
  }
}
export function parseSecretStore(value: unknown): SecretStore {
  const row = object(value, [
    'mode',
    'observed_at',
    'review_etag',
    'can_read',
    'can_rotate',
    'policy',
    'keys',
    'process',
    'rotation',
  ])
  const policy = object(row.policy, ['write_key_id', 'epoch'])
  const process = object(row.process, [
    'id',
    'verified',
    'policy_epoch',
    'snapshot_id',
    'verified_at',
  ])
  const keys = array(row.keys, (value) => {
    const key = object(value, ['id', 'state', 'configured', 'verified'])
    return {
      id: string(key.id, rootID),
      state: oneOf(key.state, ['write', 'decrypt_only', 'retired'] as const),
      configured: boolean(key.configured),
      verified: boolean(key.verified),
    }
  })
  unique(keys.map((key) => key.id))
  if (
    process.verified === true &&
    (process.id === null ||
      process.policy_epoch === null ||
      process.snapshot_id === null ||
      process.verified_at === null ||
      process.policy_epoch !== policy.epoch ||
      policy.write_key_id === null)
  )
    return fail()
  if (row.mode !== 'internal' || row.can_read !== true) return fail()
  return {
    mode: 'internal',
    observed_at: date(row.observed_at),
    review_etag: string(row.review_etag, etag),
    can_read: true,
    can_rotate: boolean(row.can_rotate),
    policy: {
      write_key_id: nullable(policy.write_key_id, (value) => string(value, rootID)),
      epoch: count(policy.epoch),
    },
    keys,
    process: {
      id: nullable(process.id, (value) => string(value, /^[A-Za-z0-9_-]{1,64}$/)),
      verified: boolean(process.verified),
      policy_epoch: nullable(process.policy_epoch, count),
      snapshot_id: nullable(process.snapshot_id, (value) =>
        string(value, /^[A-Za-z0-9_-]{1,128}$/),
      ),
      verified_at: nullable(process.verified_at, date),
    },
    rotation: nullable(row.rotation, rotation),
  }
}
export function parseSecretResult(value: unknown, intent: SecretIntent): SecretResult {
  const row = object(value, [
    'receipt',
    'committed',
    'write_policy_applied',
    'publication_applied',
    'application_status',
    'rotation',
  ])
  const receipt = object(row.receipt, ['request_id', 'rotation_id', 'action', 'created_at'])
  if (
    row.committed !== true ||
    receipt.request_id !== intent.input.request_id ||
    receipt.action !== intent.action ||
    (intent.rotationId && receipt.rotation_id !== intent.rotationId)
  )
    return fail()
  if (
    row.application_status === 'applied' &&
    (row.write_policy_applied !== true || row.publication_applied !== true)
  )
    return fail()
  const job = nullable(row.rotation, rotation)
  const id = string(receipt.rotation_id, rotationID)
  if (job && job.id !== id) return fail()
  return {
    receipt: {
      request_id: string(receipt.request_id, uuid),
      rotation_id: id,
      action: oneOf(receipt.action, ['start', 'resume', 'retire', 'rollback']),
      created_at: date(receipt.created_at),
    },
    committed: true,
    write_policy_applied: boolean(row.write_policy_applied),
    publication_applied: boolean(row.publication_applied),
    application_status: oneOf(row.application_status, [
      'applied',
      'pending',
      'superseded',
      'unavailable',
    ]),
    rotation: job,
  }
}
export function validSecretReason(value: unknown) {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value === value.trim() &&
    [...value].length <= 1000 &&
    !/[\p{Cc}\p{Cs}]/u.test(value)
  )
}
export async function getSecretStore(rotationId?: string, signal?: AbortSignal) {
  if (rotationId && !rotationID.test(rotationId)) return fail()
  try {
    const response = await client.get(
      rotationId ? `/admin/secrets/rotations/${rotationId}` : '/admin/secrets',
      { signal },
    )
    if (response.status !== 200) return fail()
    const result = parseSecretStore(response.data)
    if (rotationId && result.rotation?.id !== rotationId) return fail()
    return result
  } catch (error) {
    throw error instanceof SecretError
      ? error
      : new SecretError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export async function applySecretIntent(intent: SecretIntent, csrf: string, signal?: AbortSignal) {
  const input = intent.input
  if (
    !etag.test(intent.etag) ||
    !etag.test(csrf) ||
    !uuid.test(input.request_id) ||
    !validSecretReason(input.reason)
  )
    return fail()
  if (intent.action === 'start') {
    if (
      intent.rotationId ||
      !input.target_key_id ||
      !rootID.test(input.target_key_id) ||
      Object.keys(input).length !== 3
    )
      return fail()
  } else if (
    !rotationID.test(intent.rotationId ?? '') ||
    !['resume', 'retire', 'rollback'].includes(intent.action) ||
    Object.keys(input).length !== 2
  )
    return fail()
  try {
    const path =
      intent.action === 'start'
        ? '/admin/secrets/rotations'
        : `/admin/secrets/rotations/${intent.rotationId}/${intent.action}`
    const response = await client.post(path, input, {
      signal,
      headers: { 'If-Match': `"${intent.etag}"`, 'X-CSRF-Token': csrf },
    })
    if (!(response.status === 200 || (intent.action === 'start' && response.status === 201)))
      return fail()
    return parseSecretResult(response.data, intent)
  } catch (error) {
    throw error instanceof SecretError
      ? error
      : new SecretError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
