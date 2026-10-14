import client from './client'
import type {
  InsightsAnalysis,
  InsightsAnalysisInput,
  InsightsModel,
  InsightsTeamQuotaInput,
  InsightsTeamQuotaAnalysis,
  InsightsKeyQuotaInput,
  InsightsKeyQuotaAnalysis,
} from '@/types/insights'

const modelID = /^mdl_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const requestID = /^req_[0-7][0-9a-hjkmnp-tv-z]{25}$/
const bytes = (value: string) => new TextEncoder().encode(value).length
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const exact = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const string = (value: unknown, maximum: number): value is string =>
  typeof value === 'string' && bytes(value) <= maximum && !/[\uD800-\uDFFF]/u.test(value)
const stamp = (value: unknown): value is string =>
  string(value, 64) &&
  /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.test(value) &&
  Number.isFinite(Date.parse(value))

export function isInsightsQuestion(value: unknown): value is string {
  return (
    string(value, 1024) &&
    !!value.trim() &&
    !/[\p{Cc}]/u.test(value.replace(/[\n\t]/g, '')) &&
    !/[\uD800-\uDFFF]/u.test(value)
  )
}

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
      ...(Object.hasOwn(input, 'question') ? ['question'] : []),
    ]) ||
    (Object.hasOwn(input, 'question') && !isInsightsQuestion(input.question)) ||
    !['tokens_by_model', 'provider_quality'].includes(input.report) ||
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
    value.capture.source !==
      (input.report === 'provider_quality'
        ? 'persisted_call_attempts'
        : 'persisted_call_records') ||
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
        exact(item, [
          'label',
          input.report === 'provider_quality' ? 'provider_id' : 'model_id',
          'name',
          'unknown',
        ]) &&
        typeof item.label === 'string' &&
        (input.report === 'provider_quality' ? /^P[1-9][0-9]{0,2}$/ : /^M[1-9][0-9]{0,2}$/).test(
          item.label,
        ) &&
        Number(item.label.slice(1)) <= 500 &&
        (input.report === 'provider_quality'
          ? string(item.provider_id, 128) && item.provider_id.length > 0 && item.unknown === false
          : string(item.model_id, 128)) &&
        string(item.name, 1024) &&
        typeof item.unknown === 'boolean',
    ) ||
    new Set(value.labels.map((item) => item.label)).size !== value.labels.length ||
    new Set(
      value.labels.map((item) =>
        input.report === 'provider_quality' ? item.provider_id : item.model_id,
      ),
    ).size !== value.labels.length
  )
    throw new Error('Invalid Insights analysis response')
  return value as unknown as InsightsAnalysis
}

// Compare the quota frame at nanosecond precision; Date alone drops the
// journal's fractional digits and can admit mixed observation instants.
function teamQuotaInstant(value: string): bigint | null {
  if (!stamp(value)) return null
  const match = value.match(
    /^(\d{4})-(\d\d)-(\d\d)T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.(\d{1,9}))?(?:Z|[+-]\d\d:\d\d)$/,
  )
  if (!match) return null
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
    BigInt((match[4] ?? '').padEnd(9, '0'))
  )
}
function validTeamQuotaFrame(capture: Record<string, unknown>) {
  const fields = ['coverage_start', 'baseline_from', 'baseline_to', 'month_from', 'month_reset']
  if (capture.as_of === null) return fields.every((key) => capture[key] === null)
  const asOf = teamQuotaInstant(capture.as_of as string)
  const values = fields.map((key) =>
    capture[key] === null ? null : teamQuotaInstant(capture[key] as string),
  )
  const [coverage, from, to, month, reset] = values
  return (
    asOf !== null &&
    coverage !== null &&
    coverage <= asOf &&
    from !== null &&
    from === asOf - 604800000000000n &&
    to === asOf &&
    month !== null &&
    month <= asOf &&
    reset !== null &&
    reset > asOf
  )
}

export async function analyzeTeamQuotaInsights(
  input: InsightsTeamQuotaInput,
  csrf: string,
  signal: AbortSignal,
  reviewedCurrency?: string,
): Promise<InsightsTeamQuotaAnalysis> {
  if (
    !exact(input as unknown as Record<string, unknown>, [
      'report',
      'model_id',
      'language',
      'cursor',
      'limit',
      ...(Object.hasOwn(input, 'question') ? ['question'] : []),
    ]) ||
    !['team_quota_tokens', 'team_quota_money'].includes(input.report) ||
    (input.report === 'team_quota_money'
      ? typeof reviewedCurrency !== 'string' || !/^[A-Z]{3}$/.test(reviewedCurrency)
      : reviewedCurrency !== undefined) ||
    !modelID.test(input.model_id) ||
    !['en', 'zh'].includes(input.language) ||
    !string(input.cursor, 160) ||
    !/^[A-Za-z0-9_-]*$/.test(input.cursor) ||
    !Number.isInteger(input.limit) ||
    input.limit < 1 ||
    input.limit > 50 ||
    (Object.hasOwn(input, 'question') && !isInsightsQuestion(input.question)) ||
    bytes(JSON.stringify(input)) > 2048 ||
    !csrf
  )
    throw new Error('Invalid Team quota analysis request')
  const response = await client.post(
    '/admin/insights/analyze',
    { ...input },
    { signal, headers: { 'X-CSRF-Token': csrf } },
  )
  if (response.status !== 200) throw new Error('Invalid Team quota analysis status')
  const value: unknown = response.data
  if (
    !object(value) ||
    bytes(JSON.stringify(value)) > 1024 * 1024 ||
    !exact(value, ['request_id', 'completion', 'text', 'quota_capture', 'labels']) ||
    typeof value.request_id !== 'string' ||
    !requestID.test(value.request_id) ||
    typeof value.completion !== 'string' ||
    !['unknown', 'completed', 'handoff', 'blocked', 'incomplete'].includes(value.completion) ||
    (value.completion === 'completed'
      ? !string(value.text, 16 * 1024) || !value.text.trim()
      : value.text !== null) ||
    !object(value.quota_capture)
  )
    throw new Error('Invalid Team quota analysis response')
  const capture = value.quota_capture
  if (
    !exact(capture, [
      'source',
      'queried_at',
      'cursor',
      'limit',
      'has_more',
      'timezone',
      'as_of',
      'coverage_start',
      'baseline_from',
      'baseline_to',
      'month_from',
      'month_reset',
      'baseline_basis',
      ...(input.report === 'team_quota_money' ? ['dimension', 'currency'] : []),
    ]) ||
    capture.source !== 'local_quota_journal' ||
    (input.report === 'team_quota_money' &&
      (capture.dimension !== 'money' || capture.currency !== reviewedCurrency)) ||
    !stamp(capture.queried_at) ||
    capture.cursor !== input.cursor ||
    capture.limit !== input.limit ||
    typeof capture.has_more !== 'boolean' ||
    !string(capture.timezone, 100) ||
    !capture.timezone ||
    capture.timezone === 'Local' ||
    capture.baseline_basis !== 'settled_admissions_168h' ||
    !['as_of', 'coverage_start', 'baseline_from', 'baseline_to', 'month_from', 'month_reset'].every(
      (key) => capture[key] === null || stamp(capture[key]),
    ) ||
    !validTeamQuotaFrame(capture) ||
    !Array.isArray(value.labels) ||
    value.labels.length > input.limit ||
    !value.labels.every(
      (item, i) =>
        object(item) &&
        exact(item, ['label', 'team_id', 'name', 'unknown']) &&
        item.label === `T${i + 1}` &&
        typeof item.team_id === 'string' &&
        /^tea_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(item.team_id) &&
        string(item.name, 1024) &&
        item.unknown === false,
    ) ||
    new Set(value.labels.map((item) => item.team_id)).size !== value.labels.length
  )
    throw new Error('Invalid Team quota analysis capture')
  try {
    new Intl.DateTimeFormat('en', { timeZone: capture.timezone })
  } catch {
    throw new Error('Invalid Team quota analysis timezone')
  }
  return value as unknown as InsightsTeamQuotaAnalysis
}

export async function analyzeKeyQuotaInsights(
  input: InsightsKeyQuotaInput,
  csrf: string,
  signal: AbortSignal,
  reviewedCurrency?: string,
): Promise<InsightsKeyQuotaAnalysis> {
  if (
    !exact(input as unknown as Record<string, unknown>, [
      'report',
      'model_id',
      'language',
      'scope_kind',
      'scope_id',
      'cursor',
      'limit',
      ...(Object.hasOwn(input, 'question') ? ['question'] : []),
    ]) ||
    !['key_quota_tokens', 'key_quota_money'].includes(input.report) ||
    (input.report === 'key_quota_money'
      ? typeof reviewedCurrency !== 'string' || !/^[A-Z]{3}$/.test(reviewedCurrency)
      : reviewedCurrency !== undefined) ||
    !modelID.test(input.model_id) ||
    !['member', 'project'].includes(input.scope_kind) ||
    typeof input.scope_id !== 'string' ||
    !new RegExp(
      `^${input.scope_kind === 'member' ? 'usr' : 'prj'}_[0-7][0-9a-hjkmnp-tv-z]{25}$`,
    ).test(input.scope_id) ||
    !['en', 'zh'].includes(input.language) ||
    !string(input.cursor, 160) ||
    !/^[A-Za-z0-9_-]*$/.test(input.cursor) ||
    !Number.isInteger(input.limit) ||
    input.limit < 1 ||
    input.limit > 50 ||
    (Object.hasOwn(input, 'question') && !isInsightsQuestion(input.question)) ||
    bytes(JSON.stringify(input)) > 2048 ||
    !csrf
  )
    throw new Error('Invalid Key quota analysis request')
  const response = await client.post(
    '/admin/insights/analyze',
    { ...input },
    { signal, headers: { 'X-CSRF-Token': csrf } },
  )
  if (response.status !== 200) throw new Error('Invalid Key quota analysis status')
  const value: unknown = response.data
  if (
    !object(value) ||
    bytes(JSON.stringify(value)) > 1024 * 1024 ||
    !exact(value, ['request_id', 'completion', 'text', 'key_quota_capture', 'labels']) ||
    typeof value.request_id !== 'string' ||
    !requestID.test(value.request_id) ||
    typeof value.completion !== 'string' ||
    !['unknown', 'completed', 'handoff', 'blocked', 'incomplete'].includes(value.completion) ||
    (value.completion === 'completed'
      ? !string(value.text, 16 * 1024) || !value.text.trim()
      : value.text !== null) ||
    !object(value.key_quota_capture)
  )
    throw new Error('Invalid Key quota analysis response')
  const capture = value.key_quota_capture
  if (
    !exact(capture, [
      'source',
      'queried_at',
      'scope_kind',
      'scope_id',
      'cursor',
      'limit',
      'has_more',
      'timezone',
      'as_of',
      'coverage_start',
      'baseline_from',
      'baseline_to',
      'month_from',
      'month_reset',
      'baseline_basis',
      ...(input.report === 'key_quota_money' ? ['dimension', 'currency'] : []),
    ]) ||
    capture.source !== 'local_quota_journal' ||
    (input.report === 'key_quota_money' &&
      (capture.dimension !== 'money' || capture.currency !== reviewedCurrency)) ||
    capture.scope_kind !== input.scope_kind ||
    capture.scope_id !== input.scope_id ||
    !stamp(capture.queried_at) ||
    capture.cursor !== input.cursor ||
    capture.limit !== input.limit ||
    typeof capture.has_more !== 'boolean' ||
    !string(capture.timezone, 100) ||
    !capture.timezone ||
    capture.timezone === 'Local' ||
    capture.baseline_basis !== 'settled_admissions_168h' ||
    !['as_of', 'coverage_start', 'baseline_from', 'baseline_to', 'month_from', 'month_reset'].every(
      (key) => capture[key] === null || stamp(capture[key]),
    ) ||
    !validTeamQuotaFrame(capture) ||
    !Array.isArray(value.labels) ||
    value.labels.length > input.limit ||
    !value.labels.every(
      (item, i) =>
        object(item) &&
        exact(item, ['label', 'key_id', 'name', 'unknown']) &&
        item.label === `K${i + 1}` &&
        typeof item.key_id === 'string' &&
        new RegExp(
          `^${input.scope_kind === 'member' ? 'key' : 'pky'}_[0-7][0-9a-hjkmnp-tv-z]{25}$`,
        ).test(item.key_id) &&
        string(item.name, 1024) &&
        item.unknown === false,
    ) ||
    new Set(value.labels.map((item) => item.key_id)).size !== value.labels.length
  )
    throw new Error('Invalid Key quota analysis capture')
  try {
    new Intl.DateTimeFormat('en', { timeZone: capture.timezone })
  } catch {
    throw new Error('Invalid Key quota analysis timezone')
  }
  return value as unknown as InsightsKeyQuotaAnalysis
}
