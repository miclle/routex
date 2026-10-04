import axios from 'axios'
import client from './client'
import { metrics, currencies, metricUnits, type PriceRate } from '@/types/pricing'
import type {
  RateSource,
  RepositoryConfig,
  RepositorySelection,
  RepositoryPreview,
  RepositoryResult,
  RepositoryIntent,
  RepositoryCandidates,
} from '@/types/repository-prices'
export class RepositoryPriceError extends Error {
  constructor(public readonly status: number) {
    super('Repository pricing request failed')
  }
}
const hash = /^[a-f0-9]{64}$/
const id = /^[A-Za-z0-9_-]{1,30}$/
const sourceKey = /^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$/
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const
function fail(): never {
  throw new RepositoryPriceError(0)
}
function object(
  value: unknown,
  fields: string[],
  optional: string[] = [],
): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return fail()
  const row = value as Record<string, unknown>
  if (
    fields.some((field) => !Object.hasOwn(row, field)) ||
    Object.keys(row).some((field) => !fields.includes(field) && !optional.includes(field))
  )
    return fail()
  return row
}
function string(value: unknown, pattern?: RegExp, max = 256): string {
  if (
    typeof value !== 'string' ||
    !value ||
    new TextEncoder().encode(value).length > max ||
    /[\p{Cc}\p{Cs}]/u.test(value) ||
    (pattern && !pattern.test(value))
  )
    return fail()
  return value
}
function bool(value: unknown): boolean {
  return typeof value === 'boolean' ? value : fail()
}
function member<T extends string>(value: unknown, list: readonly T[]): T {
  return list.includes(value as T) ? (value as T) : fail()
}
function array<T>(value: unknown, parse: (v: unknown) => T, max: number): T[] {
  if (!Array.isArray(value) || value.length > max) return fail()
  return value.map(parse)
}
function unique<T>(values: T[]): T[] {
  if (new Set(values).size !== values.length) return fail()
  return values
}
function nullable<T>(value: unknown, parse: (v: unknown) => T): T | null {
  return value === null ? null : parse(value)
}
function threshold(value: unknown): number {
  return typeof value === 'number' && [0, 128000, 200000].includes(value) ? value : fail()
}
function date(value: unknown): string {
  const result = string(
    value,
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/,
    40,
  )
  return Number.isFinite(Date.parse(result)) ? result : fail()
}
function rate(value: unknown): PriceRate {
  const row = object(value, ['metric', 'tier', 'unit', 'currency', 'amount', 'enabled'], ['id'])
  const metric = member(row.metric, metrics)
  const tier = member(row.tier, ['base', 'long_context'])
  if (
    row.unit !== metricUnits[metric] ||
    (['IMAGE_INPUT', 'PDF_INPUT'].includes(metric) && tier !== 'base')
  )
    return fail()
  return {
    ...(row.id === undefined ? {} : { id: string(row.id, id) }),
    metric,
    tier,
    unit: metricUnits[metric],
    currency: member(row.currency, currencies),
    amount: string(row.amount, /^[0-9]{1,18}(\.[0-9]{1,18})?$/, 37),
    enabled: bool(row.enabled),
  }
}
function ownership(value: unknown): RateSource {
  const row = object(value, ['kind', 'source_model_key', 'source_rate_key'])
  const kind = member(row.kind, ['custom', 'repository'])
  const model = nullable(row.source_model_key, (v) => string(v, sourceKey))
  const rate = nullable(row.source_rate_key, (v) => string(v, sourceKey))
  if (
    (kind === 'custom' && (model !== null || rate !== null)) ||
    (kind === 'repository' && (model === null || rate === null))
  )
    return fail()
  return { kind, source_model_key: model, source_rate_key: rate }
}
export function validRepositoryReason(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value === value.trim() &&
    [...value].length <= 1000 &&
    !/[\p{Cc}\p{Cs}]/u.test(value)
  )
}
export function parseRepositoryConfig(value: unknown): RepositoryConfig {
  const row = object(value, [
    'review_etag',
    'enabled',
    'can_write',
    'source',
    'mappings',
    'last_attempt_at',
    'last_success_at',
    'last_result',
  ])
  const source = object(row.source, ['id', 'digest', 'model_count', 'models'])
  if (source.id !== 'routex-repository') return fail()
  const models = array(
    source.models,
    (value) => {
      const m = object(value, ['key', 'provider_key', 'model', 'protocol', 'context_threshold'])
      return {
        key: string(m.key, sourceKey),
        provider_key: string(m.provider_key, sourceKey),
        model: string(m.model),
        protocol: member(m.protocol, protocols),
        context_threshold: threshold(m.context_threshold),
      }
    },
    1000,
  )
  unique(models.map((m) => m.key))
  if (source.model_count !== models.length) return fail()
  const mappings = array(
    row.mappings,
    (value) => {
      const m = object(value, ['provider_model_id', 'source_model_key'])
      return {
        provider_model_id: string(m.provider_model_id, id),
        source_model_key: string(m.source_model_key, sourceKey),
      }
    },
    100,
  )
  unique(mappings.map((m) => m.provider_model_id))
  return {
    review_etag: string(row.review_etag, hash),
    enabled: bool(row.enabled),
    can_write: bool(row.can_write),
    source: {
      id: 'routex-repository',
      digest: string(source.digest, hash),
      model_count: models.length,
      models,
    },
    mappings,
    last_attempt_at: nullable(row.last_attempt_at, date),
    last_success_at: nullable(row.last_success_at, date),
    last_result: nullable(row.last_result, (v) => string(v, /^[a-z][a-z0-9_]{0,63}$/)),
  }
}
export function validateRepositorySelection(value: unknown): RepositorySelection {
  const row = object(value, ['mode', 'provider_model_ids', 'rate_ids'])
  const mode = member(row.mode, ['sync', 'restore'])
  const models = unique(array(row.provider_model_ids, (v) => string(v, id), 20))
  const rates = unique(array(row.rate_ids, (v) => string(v, id), 200))
  if (!models.length || (mode === 'sync' && rates.length) || (mode === 'restore' && !rates.length))
    return fail()
  return { mode, provider_model_ids: models, rate_ids: rates }
}
export function parseRepositoryPreview(
  value: unknown,
  selection: RepositorySelection,
): RepositoryPreview {
  const row = object(value, [
    'review_etag',
    'source_digest',
    'preview_digest',
    'mode',
    'valid',
    'changes',
    'errors',
    'warnings',
  ])
  const valid = bool(row.valid)
  if (
    row.mode !== selection.mode ||
    (valid && !hash.test(String(row.preview_digest))) ||
    (!valid && row.preview_digest !== '')
  )
    return fail()
  const changes = array(
    row.changes,
    (value) => {
      const c = object(value, [
        'provider_model_id',
        'rate_id',
        'source_model_key',
        'source_rate_key',
        'action',
        'before',
        'after',
        'before_source',
        'after_source',
        'threshold_before',
        'threshold_after',
      ])
      const model = string(c.provider_model_id, id)
      const rateID = nullable(c.rate_id, (v) => string(v, id))
      if (
        !selection.provider_model_ids.includes(model) ||
        (selection.mode === 'restore' && (rateID === null || !selection.rate_ids.includes(rateID)))
      )
        return fail()
      const before = nullable(c.before, rate),
        after = nullable(c.after, rate)
      if (
        rateID !== null &&
        ((before?.id && before.id !== rateID) || (after?.id && after.id !== rateID))
      )
        return fail()
      return {
        provider_model_id: model,
        rate_id: rateID,
        source_model_key: string(c.source_model_key, sourceKey),
        source_rate_key: string(c.source_rate_key, sourceKey),
        action: member(c.action, ['added', 'updated', 'unchanged', 'protected_custom'] as const),
        before,
        after,
        before_source: ownership(c.before_source),
        after_source: ownership(c.after_source),
        threshold_before: threshold(c.threshold_before),
        threshold_after: threshold(c.threshold_after),
      }
    },
    200,
  )
  const issue = (value: unknown) => {
    const i = object(value, ['provider_model_id', 'rate_id', 'code', 'message'])
    const model = string(i.provider_model_id, id)
    if (!selection.provider_model_ids.includes(model)) return fail()
    return {
      provider_model_id: model,
      rate_id: nullable(i.rate_id, (v) => string(v, id)),
      code: string(i.code, /^[a-z][a-z0-9_]{0,63}$/),
      message: string(i.message, undefined, 1000),
    }
  }
  const errors = array(row.errors, issue, 1000),
    warnings = array(row.warnings, issue, 1000)
  if (valid && errors.length) return fail()
  return {
    review_etag: string(row.review_etag, hash),
    source_digest: string(row.source_digest, hash),
    preview_digest: valid ? string(row.preview_digest, hash) : '',
    mode: selection.mode,
    valid,
    changes,
    errors,
    warnings,
  }
}
export function parseRepositoryResult(value: unknown, intent: RepositoryIntent): RepositoryResult {
  const row = object(value, [
    'receipt',
    'committed',
    'runtime_applied',
    'configuration_applied',
    'configuration',
    'application_status',
  ])
  const receipt = object(row.receipt, ['request_id', 'source_digest', 'mode', 'created_at'])
  const mode = intent.kind === 'configure' ? 'configure' : intent.input.selection.mode
  if (
    row.committed !== true ||
    receipt.request_id !== intent.input.request_id ||
    receipt.source_digest !== intent.sourceDigest ||
    receipt.mode !== mode ||
    (mode === 'configure' && row.runtime_applied !== false)
  )
    return fail()
  const status = member(row.application_status, ['applied', 'pending', 'superseded', 'unavailable'])
  if (
    status === 'applied' &&
    (mode === 'configure' ? row.configuration_applied !== true : row.runtime_applied !== true)
  )
    return fail()
  return {
    receipt: {
      request_id: string(receipt.request_id, uuid),
      source_digest: string(receipt.source_digest, hash),
      mode,
      created_at: date(receipt.created_at),
    },
    committed: true,
    runtime_applied: bool(row.runtime_applied),
    configuration_applied: bool(row.configuration_applied),
    configuration: nullable(row.configuration, parseRepositoryConfig),
    application_status: status,
  }
}
async function request<T>(run: () => Promise<T>): Promise<T> {
  try {
    return await run()
  } catch (error) {
    throw error instanceof RepositoryPriceError
      ? error
      : new RepositoryPriceError(axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0)
  }
}
export function getRepositoryConfig(signal?: AbortSignal) {
  return request(async () => {
    const result = await client.get('/admin/prices/repository', { signal })
    if (result.status !== 200) return fail()
    return parseRepositoryConfig(result.data)
  })
}
export function getRepositoryCandidates(
  query: string,
  cursor: string | null,
  signal?: AbortSignal,
): Promise<RepositoryCandidates> {
  if (
    new TextEncoder().encode(query).length > 200 ||
    /[\p{Cc}\p{Cs}]/u.test(query) ||
    (cursor && !id.test(cursor))
  )
    return Promise.reject(new RepositoryPriceError(0))
  return request(async () => {
    const result = await client.get('/admin/prices/repository/candidates', {
      signal,
      params: { q: query || undefined, cursor: cursor || undefined, limit: 20 },
    })
    if (result.status !== 200) return fail()
    const row = object(result.data, ['items', 'next_cursor'])
    const items = array(
      row.items,
      (v) => {
        const i = object(v, ['provider_model_id', 'upstream_name', 'protocol'])
        return {
          provider_model_id: string(i.provider_model_id, id),
          upstream_name: string(i.upstream_name),
          protocol: member(i.protocol, protocols),
        }
      },
      20,
    )
    unique(items.map((i) => i.provider_model_id))
    return { items, next_cursor: row.next_cursor === '' ? null : string(row.next_cursor, id) }
  })
}
export function previewRepositoryPrices(
  selection: RepositorySelection,
  csrf: string,
  signal?: AbortSignal,
) {
  if (!csrf || /[\p{Cc}\p{Cs}]/u.test(csrf)) return Promise.reject(new RepositoryPriceError(0))
  const captured = validateRepositorySelection(selection)
  return request(async () => {
    const result = await client.post('/admin/prices/repository/preview', captured, {
      signal,
      headers: { 'X-CSRF-Token': csrf },
    })
    if (result.status !== 200) return fail()
    return parseRepositoryPreview(result.data, captured)
  })
}
export function applyRepositoryIntent(
  intent: RepositoryIntent,
  csrf: string,
  signal?: AbortSignal,
) {
  return request(async () => {
    if (
      !hash.test(intent.etag) ||
      !hash.test(intent.sourceDigest) ||
      !uuid.test(intent.input.request_id) ||
      !validRepositoryReason(intent.input.reason) ||
      !csrf ||
      /[\p{Cc}\p{Cs}]/u.test(csrf)
    )
      return fail()
    if (intent.kind === 'configure') {
      const input = object(intent.input, ['request_id', 'enabled', 'mappings', 'reason'])
      bool(input.enabled)
      const mappings = array(
        input.mappings,
        (value) => {
          const m = object(value, ['provider_model_id', 'source_model_key'])
          string(m.source_model_key, sourceKey)
          return string(m.provider_model_id, id)
        },
        100,
      )
      unique(mappings)
    } else {
      object(intent.input, ['request_id', 'preview_digest', 'selection', 'reason'])
      if (!hash.test(intent.input.preview_digest)) return fail()
      validateRepositorySelection(intent.input.selection)
    }
    const response = await client.request({
      method: intent.kind === 'configure' ? 'put' : 'post',
      url:
        intent.kind === 'configure' ? '/admin/prices/repository' : '/admin/prices/repository/apply',
      data: intent.input,
      signal,
      headers: { 'If-Match': `"${intent.etag}"`, 'X-CSRF-Token': csrf },
    })
    if (![200, 201].includes(response.status)) return fail()
    return parseRepositoryResult(response.data, intent)
  })
}
export function getRepositoryReceipt(intent: RepositoryIntent, signal?: AbortSignal) {
  if (!uuid.test(intent.input.request_id)) return Promise.reject(new RepositoryPriceError(0))
  return request(async () => {
    const response = await client.get(
      `/admin/prices/repository/receipts/${intent.input.request_id}`,
      { signal },
    )
    if (response.status !== 200) return fail()
    return parseRepositoryResult(response.data, intent)
  })
}
