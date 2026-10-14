import client from './client'
import { getMembers } from './governance'
import { getResource, getResources } from './resources'
import { exportReportCSV } from './usage-export'
import type {
  QuotaInsightsInput,
  QuotaInsightsReport,
  QuotaInsightsRow,
  QuotaScopePage,
} from '@/types/insights-quota'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const exact = (value: Record<string, unknown>, fields: readonly string[]) =>
  Object.keys(value).length === fields.length &&
  fields.every((field) => Object.hasOwn(value, field))
const id = (value: unknown, prefix: string): value is string =>
  typeof value === 'string' && new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`).test(value)
const integer = (value: unknown): value is string =>
  typeof value === 'string' &&
  /^(0|[1-9]\d{0,18})$/.test(value) &&
  BigInt(value) <= 9223372036854775807n
const decimal = (value: unknown): value is string =>
  typeof value === 'string' && /^(0|[1-9]\d{0,59})(?:\.\d{1,18})?$/.test(value)
const nullable = (value: unknown, validator: (value: unknown) => boolean) =>
  value === null || validator(value)
const cursor = (value: unknown): value is string | null =>
  value === null ||
  (typeof value === 'string' &&
    !!value &&
    new TextEncoder().encode(value).length <= 160 &&
    !Array.from(value).some(
      (character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127,
    ))
function instant(value: unknown): bigint | null {
  if (typeof value !== 'string') return null
  const match = value.match(
    /^(\d{4})-(\d\d)-(\d\d)T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.(\d{1,9}))?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/,
  )
  if (!match || !Number.isFinite(Date.parse(value))) return null
  const year = Number(match[1]),
    month = Number(match[2]),
    day = Number(match[3])
  const days = [
    31,
    year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ]
  if (month < 1 || month > 12 || day < 1 || day > days[month - 1]) return null
  return (
    BigInt(Date.parse(value.replace(/\.\d{1,9}/, ''))) * 1000000n +
    BigInt((match[5] ?? '').padEnd(9, '0'))
  )
}
function timezone(value: unknown) {
  if (
    typeof value !== 'string' ||
    !value ||
    value === 'Local' ||
    /^[+-]/.test(value) ||
    new TextEncoder().encode(value).length > 100
  )
    return false
  try {
    new Intl.DateTimeFormat('en', { timeZone: value })
    return true
  } catch {
    return false
  }
}
function validateInput(input: QuotaInsightsInput) {
  if (
    !object(input) ||
    !exact(input, ['kind', 'dimension', 'scope', 'cursor']) ||
    !['team_quota', 'key_quota'].includes(input.kind) ||
    !['tokens', 'money'].includes(input.dimension) ||
    !object(input.scope) ||
    !exact(input.scope, ['kind', 'id']) ||
    !cursor(input.cursor)
  )
    throw new Error('Invalid quota analysis query')
  if (
    input.kind === 'team_quota'
      ? input.scope.kind !== 'readable_teams' || input.scope.id !== ''
      : !['member', 'project'].includes(input.scope.kind) ||
        !id(input.scope.id, input.scope.kind === 'member' ? 'usr' : 'prj')
  )
    throw new Error('Invalid quota analysis scope')
}
function parameters(input: QuotaInsightsInput) {
  validateInput(input)
  return {
    dimension: input.dimension,
    ...(input.kind === 'key_quota'
      ? { scope_kind: input.scope.kind, scope_id: input.scope.id }
      : {}),
    limit: 20,
    ...(input.cursor ? { cursor: input.cursor } : {}),
  }
}
function row(value: unknown, input: QuotaInsightsInput): value is QuotaInsightsRow {
  if (
    !object(value) ||
    !exact(value, [
      'id',
      'name',
      'status',
      'shared_key_count',
      'local_cap',
      'behavior',
      'month_settled',
      'baseline_settled',
      'baseline_held',
      'month_held',
      'active_held',
      'month_unknown',
      'baseline_unknown',
      'month_covered',
      'baseline_covered',
      'utilization_percent',
      'level',
      'estimate_at',
      'estimate_status',
      'estimate_within_seven_days',
    ]) ||
    !id(
      value.id,
      input.kind === 'team_quota' ? 'tea' : input.scope.kind === 'project' ? 'pky' : 'key',
    ) ||
    typeof value.name !== 'string' ||
    new TextEncoder().encode(value.name).length > 1024 ||
    !(
      input.kind === 'team_quota'
        ? ['active', 'disabled', 'archived']
        : ['pending', 'active', 'disabled', 'revoked']
    ).includes(value.status as string) ||
    !Number.isSafeInteger(value.shared_key_count) ||
    (value.shared_key_count as number) < 0 ||
    (value.shared_key_count as number) > 10000 ||
    (input.kind === 'team_quota'
      ? value.shared_key_count !== 0
      : (value.shared_key_count as number) < 1) ||
    !['stop', 'alert_only'].includes(value.behavior as string) ||
    typeof value.month_covered !== 'boolean' ||
    typeof value.baseline_covered !== 'boolean' ||
    !['normal', 'near', 'critical', 'reached', 'unknown'].includes(value.level as string) ||
    ![
      'estimated',
      'already_reached',
      'no_local_cap',
      'zero_cap',
      'inactive',
      'unavailable',
      'incomplete_coverage',
      'unknown_usage',
      'zero_rate',
      'after_reset',
    ].includes(value.estimate_status as string) ||
    typeof value.estimate_within_seven_days !== 'boolean' ||
    (value.estimate_within_seven_days && value.estimate_status !== 'estimated') ||
    !nullable(value.estimate_at, (v) => instant(v) !== null) ||
    !nullable(
      value.utilization_percent,
      (v) => typeof v === 'string' && /^(0|[1-9]\d{0,81})\.\d{2}$/.test(v),
    )
  )
    return false
  if (
    value.estimate_status === 'estimated' &&
    (value.status !== 'active' ||
      value.local_cap === null ||
      /^0(?:\.0+)?$/.test(String(value.local_cap)) ||
      value.month_covered !== true ||
      value.baseline_covered !== true ||
      value.month_unknown !== '0' ||
      value.baseline_unknown !== '0' ||
      !/^0(?:\.0+)?$/.test(String(value.month_held)) ||
      !/^0(?:\.0+)?$/.test(String(value.baseline_held)) ||
      value.baseline_settled === null ||
      /^0(?:\.0+)?$/.test(String(value.baseline_settled)) ||
      value.month_settled === null ||
      value.utilization_percent === null ||
      value.level === 'unknown' ||
      value.level === 'reached')
  )
    return false
  const quantity = input.dimension === 'tokens' ? integer : decimal
  if (
    value.local_cap !== null &&
    !(input.dimension === 'tokens'
      ? integer(value.local_cap) && BigInt(value.local_cap) <= 9007199254740991n
      : typeof value.local_cap === 'string' &&
        /^(0|[1-9]\d{0,17})(?:\.\d{1,18})?$/.test(value.local_cap))
  )
    return false
  return (
    [
      'local_cap',
      'month_settled',
      'baseline_settled',
      'baseline_held',
      'month_held',
      'active_held',
    ].every((field) => nullable(value[field], quantity)) &&
    ['month_unknown', 'baseline_unknown'].every((field) => nullable(value[field], integer)) &&
    (value.estimate_status === 'estimated'
      ? value.estimate_at !== null
      : value.estimate_at === null)
  )
}
export function parseQuotaInsights(value: unknown, input: QuotaInsightsInput): QuotaInsightsReport {
  validateInput(input)
  if (
    !object(value) ||
    !exact(value, [
      'kind',
      'dimension',
      'currency',
      'scope',
      'as_of',
      'coverage_start',
      'time_zone',
      'baseline_from',
      'baseline_to',
      'month_from',
      'month_reset',
      'baseline_basis',
      'items',
      'next_cursor',
    ]) ||
    new TextEncoder().encode(JSON.stringify(value)).length > 1024 * 1024 ||
    value.kind !== input.kind ||
    value.dimension !== input.dimension ||
    !object(value.scope) ||
    !exact(value.scope, ['kind', 'id']) ||
    value.scope.kind !== input.scope.kind ||
    value.scope.id !== input.scope.id ||
    (input.dimension === 'tokens'
      ? value.currency !== ''
      : typeof value.currency !== 'string' || !/^[A-Z]{3}$/.test(value.currency)) ||
    !timezone(value.time_zone) ||
    value.baseline_basis !== 'settled_admissions_168h' ||
    !['as_of', 'coverage_start', 'baseline_from', 'baseline_to', 'month_from', 'month_reset'].every(
      (field) => nullable(value[field], (v) => instant(v) !== null),
    ) ||
    !cursor(value.next_cursor) ||
    (value.next_cursor !== null && value.next_cursor === input.cursor) ||
    !Array.isArray(value.items) ||
    value.items.length > 20 ||
    !value.items.every((item) => row(item, input)) ||
    new Set(value.items.map((item) => item.id)).size !== value.items.length ||
    (value.next_cursor !== null && value.items.length === 0)
  )
    throw new Error('Invalid quota analysis response')
  const asOf = instant(value.as_of),
    from = instant(value.baseline_from),
    to = instant(value.baseline_to),
    month = instant(value.month_from),
    reset = instant(value.month_reset)
  if (
    asOf === null
      ? [
          value.coverage_start,
          value.baseline_from,
          value.baseline_to,
          value.month_from,
          value.month_reset,
        ].some((v) => v !== null)
      : instant(value.coverage_start) === null ||
        instant(value.coverage_start)! > asOf ||
        from === null ||
        to !== asOf ||
        to - from !== 604800000000000n ||
        month === null ||
        reset === null ||
        month > asOf ||
        reset <= asOf
  )
    throw new Error('Invalid quota analysis observation frame')
  const result = value as unknown as QuotaInsightsReport
  for (const item of result.items)
    if (
      item.estimate_status === 'estimated' &&
      (asOf === null ||
        reset === null ||
        instant(item.estimate_at)! <= asOf ||
        instant(item.estimate_at)! >= reset)
    )
      throw new Error('Invalid quota analysis estimate')
  return result
}
const active = (signal: AbortSignal) => {
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
}
export async function getQuotaInsights(input: QuotaInsightsInput, signal: AbortSignal) {
  const params = parameters(input)
  active(signal)
  const response = await client.get<unknown>(
    `/admin/insights/quota/${input.kind === 'team_quota' ? 'teams' : 'keys'}`,
    { params, signal },
  )
  active(signal)
  if (response.status !== 200) throw new Error('Invalid quota analysis response')
  return parseQuotaInsights(response.data, input)
}
export function exportQuotaInsights(input: QuotaInsightsInput, signal: AbortSignal) {
  return exportReportCSV(
    `/admin/insights/quota/${input.kind === 'team_quota' ? 'teams' : 'keys'}/export.csv`,
    parameters(input),
    signal,
  )
}
export async function getQuotaScopeOptions(
  kind: 'member' | 'project',
  adminProjects: boolean,
  q: string,
  after: string | null,
  signal: AbortSignal,
): Promise<QuotaScopePage> {
  if (q.length > 200 || !cursor(after)) throw new Error('Invalid quota scope query')
  active(signal)
  const page =
    kind === 'member'
      ? await getMembers({ ...(q ? { q } : {}) }, after, signal)
      : await getResources('projects', adminProjects, { q, status: '' }, after, signal)
  active(signal)
  if (
    !Array.isArray(page.items) ||
    page.items.length > 100 ||
    !cursor(page.next_cursor) ||
    (page.next_cursor !== null && page.next_cursor === after) ||
    page.items.some(
      (item) => !id(item.id, kind === 'member' ? 'usr' : 'prj') || typeof item.name !== 'string',
    ) ||
    new Set(page.items.map((item) => item.id)).size !== page.items.length
  )
    throw new Error('Invalid quota scope options')
  return {
    items: page.items.map((item) => ({ id: item.id, name: item.name })),
    next_cursor: page.next_cursor,
  }
}
export async function reviewQuotaScope(
  kind: 'member' | 'project',
  scopeID: string,
  actor: string,
  canWriteProjects: boolean,
  signal: AbortSignal,
) {
  if (!id(scopeID, kind === 'member' ? 'usr' : 'prj'))
    throw new Error('Invalid quota scope identity')
  active(signal)
  if (kind === 'member') {
    const response = await client.get<{ id: string }>(`/admin/members/${scopeID}`, { signal })
    if (response.status !== 200 || response.data.id !== scopeID)
      throw new Error('Invalid quota member identity')
  } else {
    const project = await getResource('projects', false, scopeID, signal)
    if (
      project.id !== scopeID ||
      !(canWriteProjects || project.managers?.some((manager) => manager.user_id === actor))
    )
      throw new Error('Quota Project Key authority unavailable')
  }
  active(signal)
  return { id: scopeID }
}
