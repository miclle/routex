import { describe, expect, it } from 'vitest'
import i18n from './index'

describe('Gateway configuration archive root inventory localization', () => {
  it('renders the exact new domain in both supported languages', () => {
    expect(i18n.t('runtime_config_chunks', { ns: 'secrets', lng: 'en' })).toBe(
      'Gateway configuration archive chunks',
    )
    expect(i18n.t('runtime_config_chunks', { ns: 'secrets', lng: 'zh' })).toBe(
      'Gateway 配置归档分块',
    )
  })
})
