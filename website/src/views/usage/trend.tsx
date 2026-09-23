import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table } from '@/components/ui/table'
import type { UsageReport } from '@/types/usage'
import {
  bucketFraction,
  chartRatio,
  exactNumber,
  maximumTokens,
  trendSegments,
  usageTime,
} from './format'
import { TokenValue } from './stats'

export default function UsageTrend({ report }: { report: UsageReport }) {
  const { t, i18n } = useTranslation('usage')
  const locale = i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
  const id = useId()
  const [selection, setSelection] = useState(0)
  const [inspect, setInspect] = useState(false)
  const periods = [report.current, ...(report.previous ? [report.previous] : [])]
  const maximum = maximumTokens(periods)
  const index = Math.min(selection, Math.max(0, report.current.trend.length - 1))
  const selected = report.current.trend[index]
  const anyKnown = periods.some((period) =>
    period.trend.some(
      (bucket) => bucket.stats.requests > 0 && bucket.stats.tokens.total.value !== null,
    ),
  )
  return (
    <Card>
      <CardHeader className="gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <CardTitle>
            {t('trend')} · {t(report.granularity === 'month' ? 'monthGrain' : report.granularity)}
          </CardTitle>
          <p className="mt-2 text-xs text-muted-foreground">{report.timezone}</p>
        </div>
        <div className="flex gap-4 text-xs">
          <span className="flex items-center gap-2">
            <span className="w-5 border-t-2 border-primary" />
            {t('current')}
          </span>
          {report.previous && (
            <span className="flex items-center gap-2 text-muted-foreground">
              <span className="w-5 border-t-2 border-dashed border-muted-foreground" />
              {t('previous')}
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <svg
          viewBox="0 0 800 238"
          className="w-full min-w-0"
          role="img"
          aria-labelledby={`${id}-title`}
          aria-describedby={`${id}-help`}
        >
          <title id={`${id}-title`}>{t('trendLabel')}</title>
          {[28, 116, 204].map((y) => (
            <line
              key={y}
              x1="36"
              x2="764"
              y1={y}
              y2={y}
              className="stroke-border"
              strokeDasharray="3 4"
            />
          ))}
          <text x="36" y="16" className="fill-muted-foreground text-[11px]">
            {exactNumber(maximum, locale)}
          </text>
          <text x="24" y="208" className="fill-muted-foreground text-[11px]">
            0
          </text>
          {periods.map((period, series) => (
            <g
              key={series}
              data-series={series === 0 ? 'current' : 'previous'}
              className={series === 0 ? 'text-primary' : 'text-muted-foreground'}
            >
              {trendSegments(period, maximum).map((path, i) => (
                <path
                  key={i}
                  d={path}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={series === 0 ? 2.5 : 1.8}
                  strokeDasharray={series === 0 ? undefined : '5 4'}
                  vectorEffect="non-scaling-stroke"
                />
              ))}
              {period.trend.map((bucket, i) =>
                bucket.stats.tokens.total.value === null ? null : (
                  <circle
                    key={i}
                    cx={36 + bucketFraction(bucket, period) * 728}
                    cy={204 - chartRatio(bucket.stats.tokens.total.value, maximum) * 176}
                    r="1.6"
                    fill="currentColor"
                  >
                    <title>
                      {usageTime(bucket.start, locale, report.timezone)}:{' '}
                      {exactNumber(bucket.stats.tokens.total.value, locale)}
                    </title>
                  </circle>
                ),
              )}
            </g>
          ))}
          {selected && (
            <line
              x1={36 + bucketFraction(selected, report.current) * 728}
              x2={36 + bucketFraction(selected, report.current) * 728}
              y1="28"
              y2="204"
              className="stroke-foreground/30"
            />
          )}
          <text x="36" y="230" className="fill-muted-foreground text-[10px]">
            {usageTime(report.current.from, locale, report.timezone)}
          </text>
          <text x="764" y="230" textAnchor="end" className="fill-muted-foreground text-[10px]">
            {usageTime(report.current.to, locale, report.timezone)}
          </text>
        </svg>
        {!anyKnown && <p className="text-sm text-muted-foreground">{t('noCounters')}</p>}
        <p id={`${id}-help`} className="text-xs text-muted-foreground">
          {t('trendHelp')}
          {report.previous && <> {t('comparisonHelp')}</>}
        </p>
        {selected && (
          <div className="space-y-2 rounded-md bg-muted/40 p-3">
            <label className="flex flex-col gap-2 text-xs">
              <span>{t('inspectBucket')}</span>
              <input
                type="range"
                min="0"
                max={Math.max(0, report.current.trend.length - 1)}
                value={index}
                onChange={(event) => setSelection(Number(event.target.value))}
                className="w-full accent-primary"
              />
            </label>
            <div className="flex flex-wrap justify-between gap-2 text-sm">
              <span>
                {usageTime(selected.start, locale, report.timezone)} —{' '}
                {usageTime(selected.end, locale, report.timezone)}
              </span>
              <TokenValue value={selected.stats.tokens.total} />
            </div>
          </div>
        )}
        <details onToggle={(event) => setInspect(event.currentTarget.open)}>
          <summary className="w-fit cursor-pointer text-sm text-muted-foreground">
            {t('inspectData')}
          </summary>
          {inspect && (
            <div className="mt-3 max-h-80 overflow-auto">
              <Table aria-label={t('inspectData')}>
                <thead>
                  <tr>
                    <th>{t('period')}</th>
                    <th>{t('bucket')}</th>
                    <th>{t('requests')}</th>
                    <th>{t('input')}</th>
                    <th>{t('output')}</th>
                    <th>{t('totalTokens')}</th>
                  </tr>
                </thead>
                <tbody>
                  {periods.flatMap((period, series) =>
                    period.trend.map((bucket, i) => (
                      <tr key={`${series}-${i}`}>
                        <td>{t(series === 0 ? 'current' : 'previous')}</td>
                        <td className="whitespace-nowrap text-xs">
                          {usageTime(bucket.start, locale, report.timezone)} —{' '}
                          {usageTime(bucket.end, locale, report.timezone)}
                        </td>
                        <td>{bucket.stats.requests}</td>
                        <td>
                          <TokenValue value={bucket.stats.tokens.input} compact />
                        </td>
                        <td>
                          <TokenValue value={bucket.stats.tokens.output} compact />
                        </td>
                        <td>
                          <TokenValue value={bucket.stats.tokens.total} compact />
                        </td>
                      </tr>
                    )),
                  )}
                </tbody>
              </Table>
            </div>
          )}
        </details>
      </CardContent>
    </Card>
  )
}
