import client from './client'
import type { ModelMonthlyRequests } from '@/types/model-metadata'
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)
const modelID = (v: unknown): v is string =>
  typeof v === 'string' && /^mdl_[A-Za-z0-9_-]{1,26}$/.test(v)
// Recorded catalog fields are nullable; absence from a legacy response remains unknown.
export function isModelRecordedDate(v: unknown): v is string {
  if (
    typeof v !== 'string' ||
    !/^(?!0000)\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(v) ||
    /^0001-01-01T00:00:00(?:\.0{1,9})?Z$/.test(v)
  )
    return false
  const instant = Date.parse(v)
  return Number.isFinite(instant) && new Date(instant).toISOString().slice(0, 19) === v.slice(0, 19)
}
export function validateModelRecordedMetadata(v: unknown) {
  if (
    !object(v) ||
    ['created_at', 'config_updated_at'].some(
      (key) => key in v && v[key] !== null && !isModelRecordedDate(v[key]),
    )
  )
    throw new Error('Invalid recorded Model metadata')
}
export function validateModelMonthlyRequests(v: unknown, ids: string[]): ModelMonthlyRequests {
  if (
    ids.length < 1 ||
    ids.length > 500 ||
    ids.some((id) => !modelID(id)) ||
    new Set(ids).size !== ids.length
  )
    throw new Error('Invalid Model monthly target batch')
  if (
    !object(v) ||
    Object.keys(v).length !== 7 ||
    !['items', 'period_from', 'period_to', 'as_of', 'timezone', 'source', 'may_lag'].every(
      (key) => key in v,
    ) ||
    !Array.isArray(v.items) ||
    v.items.length !== ids.length ||
    v.items.some(
      (item, index) =>
        !object(item) ||
        Object.keys(item).length !== 2 ||
        item.model_id !== ids[index] ||
        typeof item.requests !== 'string' ||
        !/^(0|[1-9]\d{0,4})$/.test(item.requests) ||
        BigInt(item.requests) > 10000n,
    ) ||
    !isModelRecordedDate(v.period_from) ||
    !isModelRecordedDate(v.period_to) ||
    !isModelRecordedDate(v.as_of) ||
    v.period_to !== v.as_of ||
    v.timezone !== 'UTC' ||
    v.source !== 'persisted_call_records' ||
    v.may_lag !== true
  )
    throw new Error('Invalid Model monthly request response')
  const from = v.period_from as string,
    until = v.as_of as string
  if (
    from !== until.slice(0, 7) + '-01T00:00:00Z' ||
    Date.parse(from) > Date.parse(until) ||
    v.items.reduce((sum, item) => sum + BigInt((item as { requests: string }).requests), 0n) >
      10000n
  )
    throw new Error('Invalid Model monthly period or complete count')
  return v as unknown as ModelMonthlyRequests
}
export async function getModelMonthlyRequests(ids: string[], signal?: AbortSignal) {
  // Validate locally before sending: no empty batches, arbitrary names or truncated requests.
  if (
    ids.length < 1 ||
    ids.length > 500 ||
    ids.some((id) => !modelID(id)) ||
    new Set(ids).size !== ids.length
  )
    throw new Error('Invalid Model monthly target batch')
  const query = new URLSearchParams(ids.map((id) => ['model_id', id]))
  const value = (
    await client.get<unknown>('/admin/model-monthly-requests?' + query.toString(), { signal })
  ).data
  return validateModelMonthlyRequests(value, ids)
}
