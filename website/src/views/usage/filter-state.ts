import type { UsageFilters, UsageGranularity, UsagePeriodName } from '@/types/usage'

export const defaultUsageFilters: UsageFilters = {
  period: 'month',
  timezone: 'UTC',
  granularity: 'auto',
  compare: false,
}
export function readUsageFilters(
  data: FormData,
  admin: boolean,
): UsageFilters | 'invalidRange' | 'invalidTimezone' | 'invalidPrincipal' {
  const value = (name: string) => String(data.get(name) ?? '').trim()
  const timezone = value('timezone') || 'UTC'
  try {
    if (timezone === 'Local') return 'invalidTimezone'
    new Intl.DateTimeFormat('en', { timeZone: timezone }).format()
  } catch {
    return 'invalidTimezone'
  }
  const next: UsageFilters = {
    timezone,
    granularity: (value('granularity') || 'auto') as UsageGranularity,
    compare: data.get('compare') === 'on',
  }
  if (value('period') === 'custom') {
    const from = value('from'),
      to = value('to')
    const stamp = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/
    const span = Date.parse(to) - Date.parse(from)
    if (
      !stamp.test(from) ||
      !stamp.test(to) ||
      !Number.isFinite(span) ||
      span <= 0 ||
      span > 366 * 86400000
    )
      return 'invalidRange'
    next.from = from
    next.to = to
  } else next.period = (value('period') || 'month') as UsagePeriodName
  for (const key of [
    'key_id',
    'model_id',
    'status',
    'protocol',
    ...(admin ? ['user_id', 'project_id', 'provider_model_id', 'connection_id'] : []),
  ]) {
    if (value(key)) Object.assign(next, { [key]: value(key) })
  }
  if (value('stream')) next.stream = value('stream') === 'true'
  if (next.user_id && next.project_id) return 'invalidPrincipal'
  return next
}
