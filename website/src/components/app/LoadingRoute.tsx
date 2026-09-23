import { useTranslation } from 'react-i18next'

export default function LoadingRoute() {
  const { t } = useTranslation()
  return (
    <p role="status" className="p-6 text-sm text-muted-foreground">
      {t('loading_1d088')}
    </p>
  )
}
