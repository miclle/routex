import { useTranslation } from 'react-i18next'
import type { ProjectQuota, ProjectQuotaPatch } from '@/types/project-requests'

export function QuotaValues({
  quota,
  patch = false,
}: {
  quota: ProjectQuota | ProjectQuotaPatch
  patch?: boolean
}) {
  const { t, i18n } = useTranslation('projectRequests')
  const tokens = quota.tokens_month
  const money = quota.money_month
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
      <dt className="text-muted-foreground">{t('quota.tokens')}</dt>
      <dd className="break-all">
        {tokens === undefined && patch
          ? t('quota.keep')
          : tokens == null
            ? t('quota.unlimited')
            : new Intl.NumberFormat(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US').format(
                tokens,
              )}
      </dd>
      <dt className="text-muted-foreground">{t('quota.money')}</dt>
      <dd className="break-all">
        {money === undefined && patch
          ? t('quota.keep')
          : money == null
            ? t('quota.unlimited')
            : t('quota.amount', { amount: money, currency: quota.currency })}
      </dd>
    </dl>
  )
}
