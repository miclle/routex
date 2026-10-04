import client from './client'
import { isPersonalUsageReport } from './usage'
import type { UsageReport } from '@/types/usage'

export const homeUsageKey = (actorId: string, generation: number) =>
  ['home-usage', actorId, 'personal', generation, '30d', 'UTC'] as const

export async function getHomeUsage(signal?: AbortSignal): Promise<UsageReport> {
  const value: unknown = (
    await client.get('/usage', {
      params: { period: '30d', timezone: 'UTC', granularity: 'day', compare: false },
      signal,
    })
  ).data
  if (
    !isPersonalUsageReport(value) ||
    value.timezone !== 'UTC' ||
    value.granularity !== 'day' ||
    Date.parse(value.queried_at) !== Date.parse(value.current.to) ||
    Date.parse(value.current.to) - Date.parse(value.current.from) !== 30 * 24 * 60 * 60 * 1000 ||
    value.current.trend.some(
      (bucket) => Date.parse(bucket.end) - Date.parse(bucket.start) !== 86400000,
    )
  )
    throw new Error('Invalid Personal overview usage')
  return value
}
