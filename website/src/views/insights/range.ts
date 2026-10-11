import type { UsageFilters } from '@/types/usage'
import { readUsageFilters } from '@/views/usage/filter-state'

export interface InsightsRange {
  from: string
  to: string
  timezone: string
}

// Only a closed report intent is supported. Questions never become filters or SQL.
export function readInsightsRange(
  draft: InsightsRange,
): UsageFilters | 'invalidRange' | 'invalidTimezone' {
  if (!draft.timezone.trim()) return 'invalidTimezone'
  const form = new FormData()
  form.set('period', 'custom')
  form.set('from', draft.from)
  form.set('to', draft.to)
  form.set('timezone', draft.timezone)
  form.set('granularity', 'auto')
  const result = readUsageFilters(form, true)
  if (typeof result === 'string') return result === 'invalidTimezone' ? result : 'invalidRange'
  return result
}
