import { describe, expect, it } from 'vitest'
import { readInsightsRange } from './range'

describe('closed Insights report range', () => {
  it('preserves explicit nanosecond-offset timestamps and uses only fixed usage fields', () => {
    expect(
      readInsightsRange({
        from: '2026-09-01T00:00:00.123456789+08:00',
        to: '2026-09-02T00:00:00.123456789+08:00',
        timezone: 'Asia/Shanghai',
      }),
    ).toEqual({
      from: '2026-09-01T00:00:00.123456789+08:00',
      to: '2026-09-02T00:00:00.123456789+08:00',
      timezone: 'Asia/Shanghai',
      granularity: 'auto',
      compare: false,
    })
  })
  it.each(['', 'Local', 'SQL; DROP TABLE calls', 'Mars/Olympus'])(
    'rejects invalid timezone %s',
    (timezone) => {
      expect(
        readInsightsRange({ from: '2026-09-01T00:00:00Z', to: '2026-09-02T00:00:00Z', timezone }),
      ).toBe('invalidTimezone')
    },
  )
  it.each([
    ['', '2026-09-02T00:00:00Z'],
    ['2026-09-01T00:00:00', '2026-09-02T00:00:00Z'],
    ['2026-09-02T00:00:00Z', '2026-09-01T00:00:00Z'],
    ['2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z'],
    ['2026-01-01T00:00:00Z', '2027-01-03T00:00:00Z'],
  ])('rejects missing offsets, empty, reversed and overlong ranges', (from, to) => {
    expect(readInsightsRange({ from, to, timezone: 'UTC' })).toBe('invalidRange')
  })
})
