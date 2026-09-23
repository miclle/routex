import { describe, expect, it } from 'vitest'
import type { UsagePeriod, UsageStats } from '@/types/usage'
import { bucketFraction, chartRatio, exactNumber, trendSegments } from './format'
import { readUsageFilters } from './filter-state'

const stats = (value: string | null): UsageStats => ({
  requests: 1,
  successes: 1,
  errors: 0,
  canceled: 0,
  success_rate: 1,
  average_duration_ms: 10,
  tokens: {
    input: { value, known: value ?? '3', unknown_calls: value === null ? 1 : 0 },
    output: { value: '0', known: '0', unknown_calls: 0 },
    total: { value, known: value ?? '3', unknown_calls: value === null ? 1 : 0 },
  },
  amounts: [],
  unknown_amount_calls: 1,
  pricing_statuses: { not_captured: 1 },
})
export const testPeriod: UsagePeriod = {
  from: '2026-09-01T00:00:00Z',
  to: '2026-09-01T03:00:00Z',
  summary: stats(null),
  trend: [0, 1, 2].map((index) => ({
    start: `2026-09-01T0${index}:00:00Z`,
    end: `2026-09-01T0${index + 1}:00:00Z`,
    stats: stats(index === 1 ? null : '10'),
  })),
  models: [],
  keys: [],
}

describe('usage precision and calendar positioning', () => {
  it('formats exact integer and decimal strings without rounding', () => {
    expect(exactNumber('900719925474099312345', 'en-US')).toBe('900,719,925,474,099,312,345')
    expect(exactNumber('12345678901234567890.000000000000000001', 'en-US')).toBe(
      '12,345,678,901,234,567,890.000000000000000001',
    )
    expect(exactNumber('0', 'zh-CN')).toBe('0')
    expect(chartRatio('9007199254740993', '18014398509481986')).toBe(0.5)
  })
  it('breaks line paths at unknown buckets instead of connecting to zero', () => {
    const segments = trendSegments(testPeriod, '10')
    expect(segments).toHaveLength(2)
    expect(segments.every((path) => !path.includes(' L'))).toBe(true)
    expect(bucketFraction(testPeriod.trend[0], testPeriod)).toBeCloseTo(1 / 6)
    const previous = { ...testPeriod, from: '2026-08-31T21:00:00Z', to: '2026-09-01T00:00:00Z' }
    expect(
      bucketFraction(
        { start: previous.from, end: '2026-08-31T22:00:00Z', stats: stats('10') },
        previous,
      ),
    ).toBeCloseTo(1 / 6)
  })
  it('validates ranges/timezones and cannot widen personal query scope', () => {
    const data = new FormData()
    data.set('period', 'custom')
    data.set('from', '2026-09-01T00:00:00')
    data.set('to', '2026-09-02T00:00:00Z')
    expect(readUsageFilters(data, false)).toBe('invalidRange')
    data.set('from', '2026-09-01T00:00:00Z')
    data.set('timezone', 'Invalid/Zone')
    expect(readUsageFilters(data, false)).toBe('invalidTimezone')
    data.set('timezone', 'UTC')
    data.set('user_id', 'usr_other')
    data.set('project_id', 'prj_other')
    data.set('connection_id', 'con_private')
    const filters = readUsageFilters(data, false)
    expect(filters).not.toHaveProperty('user_id')
    expect(filters).not.toHaveProperty('project_id')
    expect(filters).not.toHaveProperty('connection_id')
    expect(readUsageFilters(data, true)).toBe('invalidPrincipal')
  })
})
