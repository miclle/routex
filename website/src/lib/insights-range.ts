import type { ProviderQualityRange } from '@/types/insights'

// Date.parse alone normalizes invalid calendar dates and loses sub-millisecond
// differences. Validate calendar/offset syntax first, then compare exact instants.
export function insightsInstant(value: unknown): bigint | null {
  if (typeof value !== 'string') return null
  const match = value.match(
    /^(\d{4})-(\d\d)-(\d\d)T([01]\d|2[0-3]):([0-5]\d):([0-5]\d)(?:\.(\d{1,9}))?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/,
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
  const fraction = match[7] ?? ''
  return (
    BigInt(Date.parse(value.replace(/\.\d{1,9}/, ''))) * 1000000n + BigInt(fraction.padEnd(9, '0'))
  )
}
export function insightsTimezone(value: unknown): value is string {
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
export function isInsightsRange(value: unknown): value is ProviderQualityRange {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const range = value as Record<string, unknown>
  if (
    Object.keys(range).length !== 3 ||
    !['from', 'to', 'timezone'].every((key) => Object.hasOwn(range, key)) ||
    !insightsTimezone(range.timezone)
  )
    return false
  const from = insightsInstant(range.from),
    to = insightsInstant(range.to)
  return from !== null && to !== null && to > from && to - from <= BigInt(366 * 86400000) * 1000000n
}
