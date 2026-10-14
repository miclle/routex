import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/keyControllers'
import zh from '@/i18n/locales/zh/keyControllers'
import i18n from '@/i18n'
function entries(value: unknown, prefix = ''): Array<[string, string]> {
  return Object.entries(value as Record<string, unknown>).flatMap(([key, item]) =>
    typeof item === 'string'
      ? [[prefix + key, item] as [string, string]]
      : entries(item, `${prefix}${key}.`),
  )
}
describe('controller locale contract', () => {
  it('registers paired complete keys and matching interpolation', () => {
    const english = entries(en),
      chinese = entries(zh)
    expect(chinese.map(([key]) => key)).toEqual(english.map(([key]) => key))
    for (const [key, value] of english) {
      const translated = chinese.find(([other]) => key === other)![1]
      expect([...translated.matchAll(/{{([^}]+)}}/g)].map((match) => match[1]).sort()).toEqual(
        [...value.matchAll(/{{([^}]+)}}/g)].map((match) => match[1]).sort(),
      )
      expect(i18n.exists(key, { lng: 'en', ns: 'keyControllers' })).toBe(true)
      expect(i18n.exists(key, { lng: 'zh', ns: 'keyControllers' })).toBe(true)
    }
  })
})
