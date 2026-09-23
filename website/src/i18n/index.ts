import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en/common'
import zh from './locales/zh/common'
import enGovernance from './locales/en/governance'
import zhGovernance from './locales/zh/governance'
import enActivity from './locales/en/activity'
import zhActivity from './locales/zh/activity'
import enCatalog from './locales/en/catalog'
import zhCatalog from './locales/zh/catalog'

export const languageStorageKey = 'routex.language'
export type Language = 'en' | 'zh'
export function savedLanguage(): Language {
  try {
    return window.localStorage.getItem(languageStorageKey) === 'zh' ? 'zh' : 'en'
  } catch {
    return 'en'
  }
}
void i18n.use(initReactI18next).init({
  resources: {
    en: { common: en, catalog: enCatalog, activity: enActivity, governance: enGovernance },
    zh: { common: zh, catalog: zhCatalog, activity: zhActivity, governance: zhGovernance },
  },
  lng: savedLanguage(),
  fallbackLng: 'en',
  supportedLngs: ['en', 'zh'],
  defaultNS: 'common',
  interpolation: { escapeValue: false },
  initAsync: false,
})
i18n.on('languageChanged', (language) => {
  const supported = language === 'zh' ? 'zh' : 'en'
  document.documentElement.lang = supported
  try {
    window.localStorage.setItem(languageStorageKey, supported)
  } catch {
    /* Language changes remain available when browser storage is blocked. */
  }
})
if (typeof document !== 'undefined')
  document.documentElement.lang = i18n.resolvedLanguage === 'zh' ? 'zh' : 'en'
export const t = i18n.t.bind(i18n)
export function locale() {
  return i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US'
}
export default i18n
