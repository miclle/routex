import type { UsagePeriod, UsageGroup } from '@/types/usage'
import { bucketFraction, chartRatio, maximumTokens } from '@/views/usage/format'

export function usageShare(group: UsageGroup, period: UsagePeriod): string | null {
  const total = period.summary.tokens.total.value
  const value = group.stats.tokens.total.value
  if (total === null || value === null || BigInt(total) === 0n || BigInt(value) > BigInt(total))
    return null
  const tenths = (BigInt(value) * 1000n) / BigInt(total)
  return `${tenths / 10n}.${tenths % 10n}`
}

export function overviewTrend(period: UsagePeriod) {
  const maximum = maximumTokens([period])
  const groups: { x: number; y: number }[][] = []
  let points: { x: number; y: number }[] = []
  for (const bucket of period.trend) {
    if (bucket.stats.tokens.total.value === null) {
      if (points.length) groups.push(points)
      points = []
    } else {
      points.push({
        x: 36 + bucketFraction(bucket, period) * 728,
        y: 204 - chartRatio(bucket.stats.tokens.total.value, maximum) * 176,
      })
    }
  }
  if (points.length) groups.push(points)
  return {
    maximum,
    segments: groups.map((points) => {
      let line = `M${points[0].x.toFixed(3)},${points[0].y.toFixed(3)}`
      for (let i = 1; i < points.length; i++) {
        const left = points[i - 1],
          right = points[i]
        const midpoint = ((left.x + right.x) / 2).toFixed(3)
        line += ` C${midpoint},${left.y.toFixed(3)} ${midpoint},${right.y.toFixed(3)} ${right.x.toFixed(3)},${right.y.toFixed(3)}`
      }
      return {
        line,
        area: `${line} L${points.at(-1)!.x.toFixed(3)},204 L${points[0].x.toFixed(3)},204 Z`,
        points,
      }
    }),
  }
}
