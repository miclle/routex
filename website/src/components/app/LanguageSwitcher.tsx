import { useTranslation } from 'react-i18next'
import { setLanguagePreference, type Language } from '@/i18n'
export function LanguageSwitcher() {
  const { t, i18n } = useTranslation('common')
  return (
    <select
      aria-label={t('language')}
      value={i18n.resolvedLanguage === 'zh' ? 'zh' : 'en'}
      onChange={(event) => void setLanguagePreference(event.target.value as Language)}
      className="h-8 rounded-md border bg-background px-2 text-xs"
    >
      <option value="en">English</option>
      <option value="zh">中文</option>
    </select>
  )
}
