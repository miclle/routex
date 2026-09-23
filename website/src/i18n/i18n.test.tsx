import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LanguageSwitcher } from '@/components/app/LanguageSwitcher'
import i18n, { languageStorageKey, savedLanguage } from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let container: HTMLDivElement
let root: Root
beforeEach(() => {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.restoreAllMocks()
})

function flatten(value: Record<string, unknown>, prefix = ''): Record<string, string> {
  return Object.fromEntries(
    Object.entries(value).flatMap(([key, item]) => {
      const path = prefix ? `${prefix}.${key}` : key
      return typeof item === 'string'
        ? [[path, item]]
        : Object.entries(flatten(item as Record<string, unknown>, path))
    }),
  )
}
function placeholders(value: string) {
  return [...value.matchAll(/{{\s*([^}]+)\s*}}/g)].map((match) => match[1].trim()).sort()
}

describe('localization contract', () => {
  it('defaults to English and ignores unsupported saved preferences', () => {
    expect(savedLanguage()).toBe('en')
    expect(i18n.resolvedLanguage).toBe('en')
    expect(document.documentElement.lang).toBe('en')
    localStorage.setItem(languageStorageKey, 'fr')
    expect(savedLanguage()).toBe('en')
    localStorage.setItem(languageStorageKey, 'zh')
    expect(savedLanguage()).toBe('zh')
  })

  it('switches the visible language and persists only the language preference', async () => {
    await act(async () => root.render(<LanguageSwitcher />))
    const select = container.querySelector('select')!
    expect(select.getAttribute('aria-label')).toBe('Language')
    await act(async () => {
      select.value = 'zh'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(select.getAttribute('aria-label')).toBe('语言')
    expect(document.documentElement.lang).toBe('zh')
    expect(savedLanguage()).toBe('zh')
    expect(Object.keys(localStorage)).toEqual([languageStorageKey])
    await act(async () => {
      select.value = 'en'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(select.getAttribute('aria-label')).toBe('Language')
    expect(savedLanguage()).toBe('en')
  })

  it('keeps language switching usable when browser storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    expect(savedLanguage()).toBe('en')
    await act(async () => i18n.changeLanguage('zh'))
    expect(i18n.resolvedLanguage).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
  })

  it('keeps locale keys, interpolation fields, and plural forms aligned', () => {
    const bundles = i18n.options.resources!
    expect(Object.keys(bundles).sort()).toEqual(['en', 'zh'])
    expect(Object.keys(bundles.en).sort()).toEqual(Object.keys(bundles.zh).sort())
    for (const namespace of Object.keys(bundles.en)) {
      const en = flatten(bundles.en[namespace] as Record<string, unknown>)
      const zh = flatten(bundles.zh[namespace] as Record<string, unknown>)
      expect(Object.keys(en).sort(), namespace).toEqual(Object.keys(zh).sort())
      for (const key of Object.keys(en)) {
        expect(en[key].trim(), `${namespace}:${key}`).not.toBe('')
        expect(zh[key].trim(), `${namespace}:${key}`).not.toBe('')
        expect(placeholders(en[key]), `${namespace}:${key}`).toEqual(placeholders(zh[key]))
      }
    }
    expect(i18n.t('modelCount', { count: 1 })).toBe('1 model')
    expect(i18n.t('modelCount', { count: 2 })).toBe('2 models')
    expect(i18n.t('modelCount', { count: 2, lng: 'zh' })).toBe('2 个模型')
  })

  it('resolves every literal translation key used by application source', () => {
    const sources = import.meta.glob('../{api,components,views}/**/*.{ts,tsx}', {
      query: '?raw',
      import: 'default',
      eager: true,
    })
    for (const [file, value] of Object.entries(sources)) {
      if (file.includes('.test.')) continue
      const source = value as string
      const namespace = source.match(/useTranslation\(['"]([^'"]+)['"]\)/)?.[1] ?? 'common'
      for (const match of source.matchAll(/\bt\(['"]([^'"]+)['"]/g)) {
        expect(
          i18n.exists(match[1], { ns: namespace, lng: 'en', count: 2 }),
          `${file}: ${namespace}:${match[1]}`,
        ).toBe(true)
        expect(
          i18n.exists(match[1], { ns: namespace, lng: 'zh', count: 2 }),
          `${file}: ${namespace}:${match[1]}`,
        ).toBe(true)
      }
      if (!file.endsWith('LanguageSwitcher.tsx'))
        expect(source, file).not.toMatch(/[\u3400-\u9fff]/)
    }
  })
})
