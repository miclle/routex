import client from './client'
import type { InsightsAnalysis, InsightsAnalysisInput, InsightsModel } from '@/types/insights'

const modelID = /^mdl_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const requestID = /^req_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const bytes = (value: string) => new TextEncoder().encode(value).length
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const exact = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const string = (value: unknown, maximum: number): value is string =>
  typeof value === 'string' && bytes(value) <= maximum
const stamp = (value: unknown): value is string =>
  string(value, 64) &&
  /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.test(value) &&
  Number.isFinite(Date.parse(value))

export async function getInsightsModels(signal: AbortSignal): Promise<InsightsModel[]> {
  const response = await client.get('/admin/insights/models', { signal })
  if (response.status !== 200) throw new Error('Invalid Insights models status')
  const value: unknown = response.data
  if (
    !object(value) ||
    !exact(value, ['items']) ||
    !Array.isArray(value.items) ||
    value.items.length > 500 ||
    !value.items.every(
      (item) =>
        object(item) &&
        exact(item, ['model_id', 'name']) &&
        typeof item.model_id === 'string' &&
        modelID.test(item.model_id) &&
        string(item.name, 1024) &&
        item.name.length > 0,
    ) ||
    new Set(value.items.map((item) => item.model_id)).size !== value.items.length
  )
    throw new Error('Invalid Insights models response')
  return value.items as unknown as InsightsModel[]
}

export async function analyzeInsights(
  input: InsightsAnalysisInput,
  csrf: string,
  signal: AbortSignal,
): Promise<InsightsAnalysis> {
  if (
    !exact(input as unknown as Record<string, unknown>, [
      'report',
      'model_id',
      'from',
      'to',
      'timezone',
      'language',
    ]) ||
    input.report !== 'tokens_by_model' ||
    !modelID.test(input.model_id) ||
    !stamp(input.from) ||
    !stamp(input.to) ||
    !string(input.timezone, 100) ||
    !['en', 'zh'].includes(input.language) ||
    bytes(JSON.stringify(input)) > 2048 ||
    !csrf
  )
    throw new Error('Invalid Insights analysis request')
  const response = await client.post(
    '/admin/insights/analyze',
    { ...input },
    { signal, headers: { 'X-CSRF-Token': csrf } },
  )
  if (response.status !== 200) throw new Error('Invalid Insights analysis status')
  const value: unknown = response.data
  if (
    !object(value) ||
    bytes(JSON.stringify(value)) > 1024 * 1024 ||
    !exact(value, ['request_id', 'completion', 'text', 'capture', 'labels']) ||
    typeof value.request_id !== 'string' ||
    !requestID.test(value.request_id) ||
    !['unknown', 'completed', 'handoff', 'blocked', 'incomplete'].includes(
      value.completion as string,
    ) ||
    (value.completion === 'completed'
      ? !string(value.text, 16 * 1024) || !value.text.trim()
      : value.text !== null) ||
    !object(value.capture) ||
    !exact(value.capture, [
      'source',
      'from',
      'to',
      'timezone',
      'queried_at',
      'latest_completed_at',
      'may_lag',
    ]) ||
    value.capture.source !== 'persisted_call_records' ||
    value.capture.from !== input.from ||
    value.capture.to !== input.to ||
    value.capture.timezone !== input.timezone ||
    !stamp(value.capture.queried_at) ||
    !(value.capture.latest_completed_at === null || stamp(value.capture.latest_completed_at)) ||
    typeof value.capture.may_lag !== 'boolean' ||
    !Array.isArray(value.labels) ||
    value.labels.length > 500 ||
    !value.labels.every(
      (item) =>
        object(item) &&
        exact(item, ['label', 'model_id', 'name', 'unknown']) &&
        typeof item.label === 'string' &&
        /^M[1-9][0-9]{0,2}$/.test(item.label) &&
        Number(item.label.slice(1)) <= 500 &&
        string(item.model_id, 128) &&
        string(item.name, 1024) &&
        typeof item.unknown === 'boolean',
    ) ||
    new Set(value.labels.map((item) => item.label)).size !== value.labels.length ||
    new Set(value.labels.map((item) => item.model_id)).size !== value.labels.length
  )
    throw new Error('Invalid Insights analysis response')
  return value as unknown as InsightsAnalysis
}
