import { useTranslation } from 'react-i18next'
import type { SystemResourceSample } from '@/types/system-status'

export function ResourceGauge({
  label,
  sample,
  stale,
}: {
  label: string
  sample: SystemResourceSample | null
  stale: boolean
}) {
  const { t, i18n } = useTranslation('systemStatus')
  if (!sample || sample.percent === null)
    return (
      <span
        aria-label={t('resourceUnknown', { resource: label })}
        className="text-sm text-muted-foreground"
      >
        —
      </span>
    )

  const percent = Math.max(0, Math.min(100, sample.percent))
  const formatted = new Intl.NumberFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US', {
    maximumFractionDigits: 1,
  }).format(percent)
  const measurement =
    sample.used === null || sample.total === null
      ? sample.scope
      : `${sample.scope}: ${sample.used} / ${sample.total} ${sample.unit}`

  return (
    <div
      role="progressbar"
      aria-label={t(stale ? 'staleResourceGauge' : 'resourceGauge', {
        resource: label,
        percent: formatted,
      })}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent}
      title={measurement}
      className={`relative size-12 ${stale ? 'opacity-60' : ''}`}
    >
      <svg viewBox="0 0 48 48" className="size-12 -rotate-90" aria-hidden="true">
        <circle
          cx="24"
          cy="24"
          r="18"
          fill="none"
          strokeWidth="5"
          pathLength="100"
          className="stroke-muted"
        />
        <circle
          cx="24"
          cy="24"
          r="18"
          fill="none"
          strokeWidth="5"
          strokeLinecap="round"
          pathLength="100"
          strokeDasharray={`${percent} ${100 - percent}`}
          className={percent >= 80 ? 'stroke-destructive' : 'stroke-primary'}
        />
      </svg>
      <span className="absolute inset-0 flex items-center justify-center text-[10px] font-medium tabular-nums">
        {formatted}%
      </span>
    </div>
  )
}
