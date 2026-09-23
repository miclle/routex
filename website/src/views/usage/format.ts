import type { UsageBucket, UsagePeriod } from '@/types/usage'

// Only bounded chart ratios become Numbers. Raw token and monetary values stay
// decimal strings, including fractional precision beyond Number's safe range.
export function exactNumber(value: string, locale: string): string {
  const [whole, fraction] = value.split('.')
  const formatted = new Intl.NumberFormat(locale).format(BigInt(whole))
  const separator =
    new Intl.NumberFormat(locale).formatToParts(1.1).find((part) => part.type === 'decimal')
      ?.value ?? '.'
  return fraction === undefined ? formatted : `${formatted}${separator}${fraction}`
}
export function chartRatio(value: string, total: string): number {
  const denominator = BigInt(total)
  if (denominator <= 0n) return 0
  return Number((BigInt(value) * 1_000_000n) / denominator) / 1_000_000
}
export function bucketFraction(bucket: UsageBucket, period: UsagePeriod): number {
  const from = Date.parse(period.from),
    to = Date.parse(period.to)
  const center =
    (Math.max(from, Date.parse(bucket.start)) + Math.min(to, Date.parse(bucket.end))) / 2
  return to > from ? Math.max(0, Math.min(1, (center - from) / (to - from))) : 0
}
export function trendSegments(period: UsagePeriod, maximum: string): string[] {
  const segments: string[] = []
  let path = ''
  for (const bucket of period.trend) {
    const value = bucket.stats.tokens.total.value
    if (value === null) {
      if (path) segments.push(path)
      path = ''
      continue
    }
    const x = 36 + bucketFraction(bucket, period) * 728
    const y = 204 - chartRatio(value, maximum) * 176
    path += `${path ? ' L' : 'M'}${x.toFixed(3)},${y.toFixed(3)}`
  }
  if (path) segments.push(path)
  return segments
}
export function maximumTokens(periods: UsagePeriod[]): string {
  let maximum = 0n
  for (const period of periods)
    for (const bucket of period.trend) {
      if (bucket.stats.tokens.total.value !== null) {
        const value = BigInt(bucket.stats.tokens.total.value)
        if (value > maximum) maximum = value
      }
    }
  return maximum.toString()
}
export function usageTime(value: string, locale: string, timezone: string): string {
  return new Intl.DateTimeFormat(locale, {
    timeZone: timezone,
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    timeZoneName: 'shortOffset',
  }).format(new Date(value))
}
