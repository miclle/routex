import { expect, it } from 'vitest'
import { overviewTrend, usageShare } from './usage-overview-values'
import { homeUsageFixture } from './usage-overview-fixture'
it('keeps token shares exact above Number safety and rejects unknown/zero denominators', () => {
  const period = homeUsageFixture().current,
    group = period.models[0]
  expect(usageShare(group, period)).toBe('100.0')
  group.stats.tokens.total.value = '4503599627370496'
  expect(usageShare(group, period)).toBe('49.9')
  period.summary.tokens.total.value = null
  expect(usageShare(group, period)).toBeNull()
  period.summary.tokens.total.value = '0'
  expect(usageShare(group, period)).toBeNull()
})
it('creates smooth bounded areas and breaks them at unknown buckets', () => {
  const period = homeUsageFixture().current
  period.trend[2].stats.tokens.total.value = null
  const trend = overviewTrend(period)
  expect(trend.maximum).toBe('9007199254740993')
  expect(trend.segments).toHaveLength(2)
  expect(trend.segments[0].line).toContain(' C')
  expect(trend.segments.every((segment) => segment.area.endsWith(' Z'))).toBe(true)
  expect(
    trend.segments.every((segment) =>
      segment.points.every((point) => point.y >= 28 && point.y <= 204),
    ),
  ).toBe(true)
  expect(trend.segments[0].points).toHaveLength(2)
  expect(trend.segments[1].points).toHaveLength(28)
})
it('does not draw an all-unknown series and retains known zero points', () => {
  const period = homeUsageFixture(true).current
  expect(overviewTrend(period).segments[0].points.every((point) => point.y === 204)).toBe(true)
  period.trend.forEach((bucket) => {
    bucket.stats.tokens.total.value = null
  })
  expect(overviewTrend(period).segments).toEqual([])
})
