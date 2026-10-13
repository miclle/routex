import client from './client'
import { exportReportCSV } from './usage-export'
import type {
  ProviderQualityCounts,
  ProviderQualityRange,
  ProviderQualityReport,
} from '@/types/insights'

const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
const exact = (value: Record<string, unknown>, keys: readonly string[]) =>
  Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key))
const stamp = (value: unknown): value is string =>
  typeof value === 'string' &&
  /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.test(value) &&
  Number.isFinite(Date.parse(value))
// Date.parse alone loses sub-millisecond differences. Compare strict RFC3339
// instants at nanosecond precision while allowing the server's IANA offset.
function instant(value: string): bigint {
  const fraction = value.match(/\.(\d{1,9})/)?.[1] ?? ''
  const whole = value.replace(/\.\d{1,9}/, '')
  return BigInt(Date.parse(whole)) * 1000000n + BigInt(fraction.padEnd(9, '0'))
}
const bounded = (value: unknown, maximum = 100000): value is number =>
  Number.isSafeInteger(value) && (value as number) >= 0 && (value as number) <= maximum
const counts = [
  'attempts',
  'eligible',
  'excluded',
  'credential_rejected',
  'successes',
  'success_rate_bps',
  'rate_limited',
  'server_errors',
  'known_duration',
  'unknown_duration',
  'p95_duration_ms',
  'average_duration_ms',
] as const
function validCounts(value: Record<string, unknown>) {
  if (
    !counts
      .filter(
        (key) =>
          key !== 'success_rate_bps' && key !== 'p95_duration_ms' && key !== 'average_duration_ms',
      )
      .every((key) => bounded(value[key]))
  )
    return false
  const c = value as unknown as ProviderQualityCounts
  return (
    c.attempts === c.eligible + c.excluded + c.credential_rejected &&
    c.successes <= c.eligible &&
    c.rate_limited <= c.eligible &&
    c.server_errors <= c.eligible &&
    c.known_duration + c.unknown_duration === c.eligible &&
    (c.eligible === 0
      ? c.success_rate_bps === null
      : c.success_rate_bps === Math.floor((c.successes * 10000) / c.eligible)) &&
    (c.known_duration === 0
      ? c.p95_duration_ms === null && c.average_duration_ms === null
      : bounded(c.p95_duration_ms, Number.MAX_SAFE_INTEGER) &&
        typeof c.average_duration_ms === 'number' &&
        Number.isFinite(c.average_duration_ms) &&
        c.average_duration_ms >= 0 &&
        // Means are approximate floats; the backend rounds its int64 maximum
        // to 2^63. Safe-integer bounds apply to counts, not this metric.
        c.average_duration_ms <= 2 ** 63)
  )
}
function validateRange(input: ProviderQualityRange) {
  if (
    !object(input) ||
    !exact(input, ['from', 'to', 'timezone']) ||
    !stamp(input.from) ||
    !stamp(input.to) ||
    instant(input.to) <= instant(input.from) ||
    instant(input.to) - instant(input.from) > BigInt(366 * 86400000) * 1000000n ||
    typeof input.timezone !== 'string' ||
    !input.timezone ||
    input.timezone === 'Local' ||
    input.timezone.length > 100
  )
    throw new Error('Invalid Provider quality range')
  try {
    new Intl.DateTimeFormat('en', { timeZone: input.timezone })
  } catch {
    throw new Error('Invalid Provider quality time zone')
  }
}
export function parseProviderQuality(
  value: unknown,
  input: ProviderQualityRange,
): ProviderQualityReport {
  if (
    !object(value) ||
    new TextEncoder().encode(JSON.stringify(value)).length > 1024 * 1024 ||
    !exact(value, [
      'source',
      'from',
      'to',
      'timezone',
      'queried_at',
      'latest_completed_at',
      'may_lag',
      'total_attempts',
      'unknown_attribution',
      'summary',
      'providers',
    ]) ||
    value.source !== 'persisted_call_attempts' ||
    !stamp(value.from) ||
    !stamp(value.to) ||
    instant(value.from) !== instant(input.from) ||
    instant(value.to) !== instant(input.to) ||
    value.timezone !== input.timezone ||
    !stamp(value.queried_at) ||
    !(value.latest_completed_at === null || stamp(value.latest_completed_at)) ||
    typeof value.may_lag !== 'boolean' ||
    !bounded(value.total_attempts) ||
    !bounded(value.unknown_attribution) ||
    !object(value.summary) ||
    !exact(value.summary, counts) ||
    !validCounts(value.summary) ||
    !Array.isArray(value.providers) ||
    value.providers.length > 500 ||
    !value.providers.every(
      (row) =>
        object(row) &&
        exact(row, ['provider_id', 'name', ...counts]) &&
        typeof row.provider_id === 'string' &&
        /^[A-Za-z0-9_-]{1,30}$/.test(row.provider_id) &&
        typeof row.name === 'string' &&
        new TextEncoder().encode(row.name).length <= 1024 &&
        validCounts(row),
    )
  )
    throw new Error('Invalid Provider quality report')
  const report = value as unknown as ProviderQualityReport
  if (
    new Set(report.providers.map((row) => row.provider_id)).size !== report.providers.length ||
    report.summary.attempts + report.unknown_attribution !== report.total_attempts ||
    [
      'attempts',
      'eligible',
      'excluded',
      'credential_rejected',
      'successes',
      'rate_limited',
      'server_errors',
      'known_duration',
      'unknown_duration',
    ].some(
      (key) =>
        report.providers.reduce((sum, row) => sum + row[key as keyof ProviderQualityCounts]!, 0) !==
        report.summary[key as keyof ProviderQualityCounts],
    ) ||
    (report.total_attempts === 0) !== (report.latest_completed_at === null) ||
    (report.latest_completed_at !== null &&
      (instant(report.latest_completed_at) < instant(report.from) ||
        instant(report.latest_completed_at) >= instant(report.to)))
  )
    throw new Error('Inconsistent Provider quality report')
  return report
}
export async function getProviderQualityReport(input: ProviderQualityRange, signal: AbortSignal) {
  validateRange(input)
  const response = await client.get('/admin/insights/provider-quality', {
    params: { ...input },
    signal,
  })
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
  if (response.status !== 200) throw new Error('Invalid Provider quality status')
  return parseProviderQuality(response.data, input)
}
export async function exportProviderQuality(input: ProviderQualityRange, signal: AbortSignal) {
  validateRange(input)
  return exportReportCSV('/admin/insights/provider-quality/export.csv', { ...input }, signal)
}
