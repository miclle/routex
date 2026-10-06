import { describe, expect, it } from 'vitest'
import {
  teamCreationValue,
  teamTokenMillions,
  teamCreationLimits,
  teamCreationReason,
} from './team-creation-values'
import type { TeamCreationContext } from '@/types/resources'
const context: TeamCreationContext = {
  review_etag: 'a'.repeat(64),
  default_rule_etag: 'b'.repeat(64),
  can_set_models: false,
  platform_currency: 'USD',
  editable_fields: [
    'tokens_5h',
    'tokens_7d',
    'tokens_month',
    'money_month',
    'rpm',
    'tpm',
    'concurrency',
  ],
  default_policy: {},
}
describe('exact Team initial limit values', () => {
  it.each(['0', '1', '1000001', '9007199254740993', '9223372036854775807'])(
    'preserves exact default token preview %s',
    (value) => {
      const millions = teamTokenMillions(value)
      const [whole, part = ''] = millions.split('.')
      expect(BigInt(whole) * 1000000n + BigInt(part.padEnd(6, '0'))).toBe(BigInt(value))
    },
  )
  it('distinguishes unchanged, cleared, and finite zero without submitting hidden defaults', () => {
    expect(teamCreationLimits({}, context, '')).toBeUndefined()
    expect(teamCreationLimits({ tokens_month: '0', money_month: '' }, context, 'Reviewed')).toEqual(
      { tokens_month: 0, money_month: null, reason: 'Reviewed' },
    )
  })
  it('keeps money exact and attaches currency only to nonnull money', () => {
    expect(
      teamCreationLimits({ money_month: '12.500000000000000001' }, context, 'Reviewed'),
    ).toEqual({ money_month: '12.500000000000000001', currency: 'USD', reason: 'Reviewed' })
    expect(teamCreationLimits({ money_month: '0' }, context, 'Reviewed')?.money_month).toBe('0')
  })
  it('accepts exact safe Token ceiling and rejects its first unsafe successor', () => {
    expect(teamCreationValue('tokens_month', '9007199254.740991')).toBe(Number.MAX_SAFE_INTEGER)
    expect(() => teamCreationValue('tokens_month', '9007199254.740992')).toThrow()
  })
  it.each(['1e2', '-1', ' 1', '1 ', '01', '0.0000001', '1.2345678', 'Infinity', 'NaN'])(
    'rejects invalid M Token input %s',
    (text) => expect(() => teamCreationValue('tokens_5h', text)).toThrow(),
  )
  it.each(['0.1', '1e2', '-1', '01', '9007199254740992'])('rejects invalid rate %s', (text) =>
    expect(() => teamCreationValue('rpm', text)).toThrow(),
  )
  it('rejects unauthorized cap presence and missing currency', () => {
    expect(() =>
      teamCreationLimits({ tokens_month: '1' }, { ...context, editable_fields: [] }, 'Reviewed'),
    ).toThrow()
    expect(() =>
      teamCreationLimits({ money_month: '1' }, { ...context, platform_currency: null }, 'Reviewed'),
    ).toThrow()
  })
  it('validates reason bytes without rewriting Unicode', () => {
    expect(teamCreationReason('界'.repeat(666))).toBe(true)
    expect(teamCreationReason('界'.repeat(667))).toBe(false)
    expect(teamCreationReason(' reason')).toBe(false)
    expect(teamCreationReason('reason\n')).toBe(false)
    expect(teamCreationReason('\ud800')).toBe(false)
    expect(teamCreationReason('😀'.repeat(500))).toBe(true)
  })
})
