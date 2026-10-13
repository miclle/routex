import client from './client'
import { insightsInstant, isInsightsRange } from '@/lib/insights-range'
import type {
  InsightsReportDefinition,
  InsightsReportInput,
  InsightsReportPage,
  InsightsReportParameters,
} from '@/types/insights'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const exact = (value: Record<string, unknown>, keys: readonly string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const bytes = (value: string) => new TextEncoder().encode(value).length
const reportID = (value: unknown): value is string =>
  typeof value === 'string' && /^isr_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(value)
const kind = (value: unknown) => value === 'tokens_by_model' || value === 'provider_quality'
const cursor = (value: unknown): value is string =>
  typeof value === 'string' &&
  bytes(value) <= 160 &&
  [...value].every((character) => character.charCodeAt(0) > 32 && character.charCodeAt(0) !== 127)

const timestamp = (value: unknown): value is string => insightsInstant(value) !== null
function validParameters(value: Record<string, unknown>) {
  return (
    kind(value.kind) &&
    isInsightsRange({ from: value.from, to: value.to, timezone: value.timezone })
  )
}

function canonicalUTC(value: unknown): value is string {
  return timestamp(value) && value.endsWith('Z') && !/\.\d*0Z$/.test(value)
}
export function parseInsightsReport(value: unknown): InsightsReportDefinition {
  if (
    !object(value) ||
    !exact(value, ['id', 'kind', 'from', 'to', 'timezone', 'created_at']) ||
    !reportID(value.id) ||
    !validParameters(value) ||
    !timestamp(value.created_at) ||
    !canonicalUTC(value.from) ||
    !canonicalUTC(value.to) ||
    !value.created_at.endsWith('Z')
  )
    throw new Error('Invalid saved report definition')
  return value as unknown as InsightsReportDefinition
}
export function parseInsightsReportPage(value: unknown, limit: number): InsightsReportPage {
  if (
    !object(value) ||
    bytes(JSON.stringify(value)) > 128 * 1024 ||
    !exact(value, ['items', 'next_cursor']) ||
    !Array.isArray(value.items) ||
    value.items.length > limit ||
    !cursor(value.next_cursor)
  )
    throw new Error('Invalid saved report page')
  const items = value.items.map(parseInsightsReport)
  if (
    items.some((item, index) => index > 0 && items[index - 1].id <= item.id) ||
    (value.next_cursor !== '' && items.length === 0)
  )
    throw new Error('Invalid saved report order')
  return { items, next_cursor: value.next_cursor }
}
function active(signal: AbortSignal) {
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
}
export async function getInsightsReports(signal: AbortSignal, after = '', limit = 20) {
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > 100 || !cursor(after))
    throw new Error('Invalid saved report query')
  active(signal)
  const response = await client.get<unknown>('/insights/reports', {
    params: { limit, ...(after ? { cursor: after } : {}) },
    signal,
  })
  active(signal)
  if (response.status !== 200) throw new Error('Invalid saved report response')
  return parseInsightsReportPage(response.data, limit)
}
export async function saveInsightsReport(
  input: InsightsReportInput,
  csrf: string,
  signal: AbortSignal,
) {
  if (
    !object(input) ||
    !exact(input, ['request_id', 'kind', 'from', 'to', 'timezone']) ||
    typeof input.request_id !== 'string' ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      input.request_id,
    ) ||
    !validParameters(input) ||
    bytes(JSON.stringify(input)) > 2048 ||
    !csrf
  )
    throw new Error('Invalid saved report request')
  active(signal)
  const response = await client.post<unknown>('/insights/reports', input, {
    signal,
    headers: { 'X-CSRF-Token': csrf },
  })
  active(signal)
  if (![200, 201].includes(response.status)) throw new Error('Invalid saved report response')
  const definition = parseInsightsReport(response.data)
  if (
    definition.kind !== input.kind ||
    definition.timezone !== input.timezone ||
    insightsInstant(definition.from) !== insightsInstant(input.from) ||
    insightsInstant(definition.to) !== insightsInstant(input.to)
  )
    throw new Error('Saved report does not match reviewed intent')
  return definition
}
export async function deleteInsightsReport(id: string, csrf: string, signal: AbortSignal) {
  if (!reportID(id) || !csrf) throw new Error('Invalid saved report deletion')
  active(signal)
  const response = await client.delete(`/insights/reports/${id}`, {
    signal,
    headers: { 'X-CSRF-Token': csrf },
  })
  active(signal)
  if (response.status !== 204) throw new Error('Invalid saved report deletion response')
}
export function reportParameters(value: InsightsReportDefinition): InsightsReportParameters {
  return { kind: value.kind, from: value.from, to: value.to, timezone: value.timezone }
}
