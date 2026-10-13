import type { UsageFilters } from '@/types/usage'
import { insightsTimezone, isInsightsRange } from '@/lib/insights-range'

export interface InsightsRange {
  from: string
  to: string
  timezone: string
}

// Only a closed report intent is supported. Questions never become filters or SQL.
export function readInsightsRange(
  draft: InsightsRange,
): UsageFilters | 'invalidRange' | 'invalidTimezone' {
  if (!insightsTimezone(draft.timezone)) return 'invalidTimezone'
  if (!isInsightsRange(draft)) return 'invalidRange'
  return {
    from: draft.from,
    to: draft.to,
    timezone: draft.timezone,
    granularity: 'auto',
    compare: false,
  }
}
