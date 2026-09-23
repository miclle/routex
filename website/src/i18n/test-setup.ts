import { beforeEach } from 'vitest'
import i18n, { languageStorageKey } from './index'
beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.localStorage.removeItem(languageStorageKey)
})
