import { useTranslation } from 'react-i18next'
import { Card, CardHeader, CardContent, CardTitle } from '@/components/ui/card'
import type { UsageGroup } from '@/types/usage'
import { chartRatio, exactNumber } from './format'

const colors = [
  'text-primary',
  'text-emerald-600 dark:text-emerald-400',
  'text-amber-600 dark:text-amber-400',
  'text-blue-600 dark:text-blue-400',
  'text-violet-600 dark:text-violet-400',
  'text-muted-foreground',
]
export default function UsageDistribution({
  title,
  groups,
}: {
  title: string
  groups: UsageGroup[]
}) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const sorted = [...groups].sort((a, b) => {
    const left = BigInt(a.stats.tokens.total.known),
      right = BigInt(b.stats.tokens.total.known)
    return left > right ? -1 : left < right ? 1 : a.id.localeCompare(b.id)
  })
  const total = sorted.reduce((sum, group) => sum + BigInt(group.stats.tokens.total.known), 0n)
  const items = sorted.slice(0, 5).map((group) => ({
    name: group.name || group.id || t('unknownIdentity'),
    value: group.stats.tokens.total.known,
  }))
  if (sorted.length > 5)
    items.push({
      name: t('other'),
      value: sorted
        .slice(5)
        .reduce((sum, group) => sum + BigInt(group.stats.tokens.total.known), 0n)
        .toString(),
    })
  const slices = items.map((item, index) => {
    const fraction = chartRatio(item.value, total.toString())
    const start = items
      .slice(0, index)
      .reduce((sum, previous) => sum + chartRatio(previous.value, total.toString()), 0)
    return { ...item, index, start, fraction }
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground">{t('distributionHelp')}</p>
        {total === 0n ? (
          <p className="py-12 text-center text-sm text-muted-foreground">{t('noKnownTokens')}</p>
        ) : (
          <div className="flex flex-col items-center gap-4 py-3 sm:flex-row">
            <svg viewBox="0 0 180 180" className="size-44 shrink-0" role="img" aria-label={title}>
              <circle
                cx="90"
                cy="90"
                r="60"
                fill="none"
                strokeWidth="25"
                className="stroke-muted"
              />
              {slices.map((slice) => (
                <circle
                  key={slice.index}
                  cx="90"
                  cy="90"
                  r="60"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="25"
                  pathLength="100"
                  strokeDasharray={`${slice.fraction * 100} ${100 - slice.fraction * 100}`}
                  strokeDashoffset={-slice.start * 100}
                  transform="rotate(-90 90 90)"
                  className={colors[slice.index]}
                >
                  <title>
                    {slice.name}: {exactNumber(slice.value, locale)}
                  </title>
                </circle>
              ))}
            </svg>
            <ul className="min-w-0 flex-1 space-y-3 text-sm">
              {slices.map((slice) => (
                <li key={slice.index} className="flex items-start justify-between gap-3">
                  <span className="flex min-w-0 items-start gap-2">
                    <span
                      aria-hidden
                      className={`mt-1.5 size-2 shrink-0 rounded-full bg-current ${colors[slice.index]}`}
                    />
                    <span className="min-w-0">
                      <span className="block break-all">{slice.name}</span>
                      <span className="text-xs tabular-nums text-muted-foreground">
                        {exactNumber(slice.value, locale)}
                      </span>
                    </span>
                  </span>
                  <span className="shrink-0 tabular-nums" title={exactNumber(slice.value, locale)}>
                    {new Intl.NumberFormat(locale, {
                      style: 'percent',
                      maximumFractionDigits: 1,
                    }).format(slice.fraction)}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
