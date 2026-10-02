import { useTranslation } from 'react-i18next'
import type { ProjectRateLimit, ProjectRateLimitPatch } from '@/types/project-requests'

export function RateValues({
  rate,
  patch = false,
}: {
  rate: ProjectRateLimit | ProjectRateLimitPatch
  patch?: boolean
}) {
  const { t, i18n } = useTranslation('projectRequests')
  const number = new Intl.NumberFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
      {(['rpm', 'tpm', 'concurrency'] as const).map((key) => (
        <div key={key} className="contents">
          <dt className="text-muted-foreground">{t(`rate.${key}`)}</dt>
          <dd className="break-all">
            {rate[key] === undefined && patch
              ? t('quota.keep')
              : rate[key] == null
                ? t('quota.unlimited')
                : number.format(rate[key])}
          </dd>
        </div>
      ))}
    </dl>
  )
}
