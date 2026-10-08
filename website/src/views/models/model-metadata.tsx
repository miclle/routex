import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { ModelMonthlyView } from './use-model-metadata'
export function ModelRecordedDate({ value }: { value?: string | null }) {
  const { t, i18n } = useTranslation('catalog')
  return (
    <>
      {value
        ? new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
        : t('modelMetadata.unknown')}
    </>
  )
}
export function ModelMonthlyCount({ view, modelId }: { view: ModelMonthlyView; modelId: string }) {
  const { t } = useTranslation('catalog')
  return (
    <span className="tabular-nums">
      {view.state === 'available'
        ? (view.report?.items.find((item) => item.model_id === modelId)?.requests ??
          t('modelMetadata.unknown'))
        : t(view.state === 'unavailable' ? 'modelMetadata.unavailable' : 'modelMetadata.unknown')}
    </span>
  )
}
export function ModelMonthlyContext({ view }: { view: ModelMonthlyView }) {
  const { t, i18n } = useTranslation('catalog')
  if (view.state === 'available' && view.report)
    return (
      <p role="note" className="text-xs text-muted-foreground">
        {t('modelMetadata.source', {
          from: new Date(view.report.period_from).toLocaleString(
            i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
            { timeZone: 'UTC' },
          ),
          asOf: new Date(view.report.as_of).toLocaleString(
            i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
            { timeZone: 'UTC' },
          ),
        })}
      </p>
    )
  if (view.state === 'unavailable')
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span>{t('modelMetadata.queryUnavailable')}</span>
        <Button variant="ghost" size="sm" onClick={view.retry}>
          {t('modelMetadata.retry')}
        </Button>
      </div>
    )
  return null
}
