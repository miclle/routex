import { expect, it } from 'vitest'
import { insightsInstant, insightsTimezone, isInsightsRange } from './insights-range'
import { readInsightsRange } from '@/views/insights/range'

it('accepts one nanosecond and exactly 366 elapsed days without rewriting reviewed strings', () => {
  const range = {
    from: '2026-10-01T08:00:00.123456789+08:00',
    to: '2026-10-01T08:00:00.123456790+08:00',
    timezone: 'Asia/Shanghai',
  }
  expect(insightsInstant(range.to)! - insightsInstant(range.from)!).toBe(1n)
  expect(insightsInstant(range.from)).toBe(insightsInstant('2026-10-01T00:00:00.123456789Z'))
  expect(isInsightsRange(range)).toBe(true)
  expect(readInsightsRange(range)).toEqual({ ...range, granularity: 'auto', compare: false })
  const maximum = {
    from: '2024-01-01T00:00:00.000000001Z',
    to: '2025-01-01T00:00:00.000000001Z',
    timezone: 'UTC',
  }
  expect(isInsightsRange(maximum)).toBe(true)
  expect(insightsInstant('2024-02-29T00:00:00Z')).not.toBeNull()
})
it('rejects reversed, equal and over-bound ranges by one nanosecond', () => {
  for (const range of [
    { from: '2026-10-01T00:00:00.000000002Z', to: '2026-10-01T00:00:00.000000001Z' },
    { from: '2026-10-01T00:00:00.000000001Z', to: '2026-10-01T00:00:00.000000001Z' },
    { from: '2024-01-01T00:00:00.000000001Z', to: '2025-01-01T00:00:00.000000002Z' },
  ]) {
    expect(isInsightsRange({ ...range, timezone: 'UTC' })).toBe(false)
    expect(readInsightsRange({ ...range, timezone: 'UTC' })).toBe('invalidRange')
  }
})
it('rejects normalized calendar mistakes, malformed offsets and invalid time zones', () => {
  for (const value of [
    '2026-02-29T00:00:00Z',
    '2024-02-30T00:00:00Z',
    '2026-13-01T00:00:00Z',
    '2026-01-00T00:00:00Z',
    '2026-01-01T24:00:00Z',
    '2026-01-01T00:00:60Z',
    '2026-01-01T00:00:00+24:00',
    '2026-01-01T00:00:00+01:60',
    '2026-01-01T00:00:00.1234567890Z',
    '2026-01-01',
    null,
  ])
    expect(insightsInstant(value)).toBeNull()
  for (const timezone of ['', 'Local', '+08:00', 'not/a-zone', 'a'.repeat(101)]) {
    expect(insightsTimezone(timezone)).toBe(false)
    expect(
      readInsightsRange({ from: '2026-01-01T00:00:00Z', to: '2026-01-02T00:00:00Z', timezone }),
    ).toBe('invalidTimezone')
  }
  expect(insightsTimezone('UTC')).toBe(true)
  expect(
    isInsightsRange({
      from: '2026-01-01T00:00:00Z',
      to: '2026-01-02T00:00:00Z',
      timezone: 'UTC',
      question: 'not a filter',
    }),
  ).toBe(false)
})
