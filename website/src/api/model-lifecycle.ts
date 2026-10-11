import client from './client'
import { validModelAliasReason } from './catalog'
import type {
  ModelLifecycleInput,
  ModelLifecycleReview,
  ModelLifecycleResult,
} from '@/types/model-lifecycle'

const targetPattern = /^mdl_[A-Za-z0-9_-]{1,26}$/
const etagPattern = /^[a-f0-9]{64}\.[a-f0-9]{64}$/
const namePattern = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/
const record = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const date = (value: unknown) =>
  typeof value === 'string' &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value) &&
  Number.isFinite(Date.parse(value))
export const validModelLifecycleReason = validModelAliasReason
function target(modelID: string) {
  if (!targetPattern.test(modelID)) throw new Error('Invalid Model lifecycle target')
}
function decode(value: unknown, modelID: string): ModelLifecycleReview {
  if (
    !record(value) ||
    Object.keys(value).sort().join(',') !==
      'can_change,created_at,current_name,etag,model_id,observed_at,runtime_applied,status' ||
    value.model_id !== modelID ||
    typeof value.current_name !== 'string' ||
    !namePattern.test(value.current_name) ||
    !date(value.created_at) ||
    !date(value.observed_at) ||
    typeof value.etag !== 'string' ||
    !etagPattern.test(value.etag) ||
    typeof value.status !== 'string' ||
    !['active', 'disabled', 'archived'].includes(value.status) ||
    typeof value.can_change !== 'boolean' ||
    (value.can_change && value.status === 'archived') ||
    typeof value.runtime_applied !== 'boolean'
  )
    throw new Error('Invalid Model lifecycle review response')
  return value as unknown as ModelLifecycleReview
}
export async function getModelLifecycle(modelID: string, signal?: AbortSignal) {
  target(modelID)
  const response = await client.get<unknown>(`/admin/models/${modelID}/lifecycle`, { signal })
  if (response.status !== 200) throw new Error('Invalid Model lifecycle response status')
  return decode(response.data, modelID)
}
export async function updateModelLifecycle(
  modelID: string,
  input: ModelLifecycleInput,
  etag: string,
  csrf: string,
  signal?: AbortSignal,
): Promise<ModelLifecycleResult> {
  target(modelID)
  if (
    Object.keys(input).sort().join(',') !== 'reason,status' ||
    !['active', 'disabled', 'archived'].includes(input.status) ||
    !validModelLifecycleReason(input.reason) ||
    !etagPattern.test(etag) ||
    !csrf
  )
    throw new Error('Invalid Model lifecycle intent')
  const response = await client.put<unknown>(`/admin/models/${modelID}/lifecycle`, input, {
    headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    signal,
  })
  const value = response.data
  if (
    response.status !== 200 ||
    !record(value) ||
    Object.keys(value).sort().join(',') !== 'changed,model,runtime_applied'
  )
    throw new Error('Invalid Model lifecycle response')
  const model = decode(value.model, modelID)
  if (
    model.status !== input.status ||
    model.etag.slice(0, 64) !== etag.slice(0, 64) ||
    typeof value.changed !== 'boolean' ||
    typeof value.runtime_applied !== 'boolean' ||
    model.runtime_applied !== value.runtime_applied
  )
    throw new Error('Invalid Model lifecycle response')
  return { model, changed: value.changed, runtime_applied: value.runtime_applied }
}
