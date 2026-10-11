import { describe, expect, it } from 'vitest'
import i18n from './index'

describe('operations analysis localization', () => {
  it('registers the English-default namespace and preserves exact reviewed range metadata', () => {
    expect(i18n.resolvedLanguage).toBe('en')
    expect(i18n.t('title', { ns: 'insights' })).toBe('Operations analysis')
    const from = '2026-10-01T00:00:00+08:00'
    const to = '2026-10-02T00:00:00+08:00'
    const timezone = 'Asia/Shanghai'
    for (const lng of ['en', 'zh']) {
      const value = i18n.t('range', { ns: 'insights', lng, from, to, timezone })
      expect(value).toContain(from)
      expect(value).toContain(to)
      expect(value).toContain(timezone)
      expect(value).not.toContain('{{')
    }
  })
  it('distinguishes known coverage from plural unknown coverage in both languages', () => {
    expect(i18n.t('unknownCoverage', { ns: 'insights', lng: 'en', count: 1 })).toBe(
      '1 call has unknown token coverage.',
    )
    expect(i18n.t('unknownCoverage', { ns: 'insights', lng: 'en', count: 2 })).toBe(
      '2 calls have unknown token coverage.',
    )
    expect(i18n.t('unknownCoverage', { ns: 'insights', lng: 'zh', count: 2 })).toBe(
      '2 次调用的 Token 覆盖情况未知。',
    )
    for (const lng of ['en', 'zh']) {
      expect(i18n.t('completeCoverage', { ns: 'insights', lng })).not.toBe(
        i18n.t('unknownCoverage', { ns: 'insights', lng, count: 0 }),
      )
    }
  })
})
