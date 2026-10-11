import { expect, it } from 'vitest'
import en from './locales/en/authEnforcement'
import zh from './locales/zh/authEnforcement'
import i18n from './index'
it('registers every authentication enforcement key in paired English and Chinese catalogs', () => {
  expect(Object.keys(en).sort()).toEqual(Object.keys(zh).sort())
  for (const key of Object.keys(en)) {
    expect(i18n.exists(`authEnforcement:${key}`, { lng: 'en' })).toBe(true)
    expect(i18n.exists(`authEnforcement:${key}`, { lng: 'zh' })).toBe(true)
    expect(i18n.t(`authEnforcement:${key}`, { lng: 'en' })).not.toBe(`authEnforcement:${key}`)
  }
})
