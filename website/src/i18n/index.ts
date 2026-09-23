import enPlayground from './locales/en/playground'
import zhPlayground from './locales/zh/playground'
import enUsage from './locales/en/usage'
import zhUsage from './locales/zh/usage'
import enMFA from './locales/en/mfa'
import zhMFA from './locales/zh/mfa'
import enLimits from './locales/en/limits'
import zhLimits from './locales/zh/limits'
import i18n from 'i18next'
import enPriceImports from './locales/en/priceImports'
import zhPriceImports from './locales/zh/priceImports'
import enCurrency from './locales/en/currency'
import zhCurrency from './locales/zh/currency'
import enPricing from './locales/en/pricing'
import zhPricing from './locales/zh/pricing'
import { initReactI18next } from 'react-i18next'
import en from './locales/en/common'
import enResources from './locales/en/resources'
import zhResources from './locales/zh/resources'
import enProjectKeys from './locales/en/projectKeys'
import zhProjectKeys from './locales/zh/projectKeys'
import enProjectRequests from './locales/en/projectRequests'
import zhProjectRequests from './locales/zh/projectRequests'
import enOffboarding from './locales/en/offboarding'
import zhOffboarding from './locales/zh/offboarding'
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
    en: {
      common: en,
      playground: enPlayground,
      usage: enUsage,
      mfa: enMFA,
      limits: enLimits,
      pricing: enPricing,
      currency: enCurrency,
      priceImports: enPriceImports,
      resources: enResources,
      projectKeys: enProjectKeys,
      projectRequests: enProjectRequests,
      offboarding: enOffboarding,
      catalog: enCatalog,
      activity: enActivity,
      governance: enGovernance,
    },
    zh: {
      common: zh,
      playground: zhPlayground,
      usage: zhUsage,
      mfa: zhMFA,
      limits: zhLimits,
      pricing: zhPricing,
      currency: zhCurrency,
      priceImports: zhPriceImports,
      resources: zhResources,
      projectKeys: zhProjectKeys,
      projectRequests: zhProjectRequests,
      offboarding: zhOffboarding,
      catalog: zhCatalog,
      activity: zhActivity,
      governance: zhGovernance,
    },
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
