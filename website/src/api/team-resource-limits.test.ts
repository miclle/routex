import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getTeamLimits, saveTeamLimits } from './resource-limits'
import { teamFixture } from '@/views/resource-limits/fixture'
const originalAdapter = client.defaults.adapter
let value: unknown, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  value = teamFixture()
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data: value, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(() => {
  client.defaults.adapter = originalAdapter
})
describe('Team limit API boundary', () => {
  it.each([
    { id: 'tea_other' },
    { team_id: 'tea_other' },
    { kind: 'user' },
    { etag: 'weak' },
    { editable_fields: ['ip_mode'] },
    { editable_fields: ['rpm', 'rpm'] },
    { enforced: 'true' },
    { stored: { ...teamFixture().stored, money_month: 12 } },
    { ip_policies: [] },
    { quota_usage: { activated: true, time_zone: 'invalid' } },
  ])('rejects malformed or cross-scoped readback %j', async (patch) => {
    value = { ...teamFixture(), ...patch }
    await expect(getTeamLimits({ teamId: 'tea_test' })).rejects.toThrow()
  })
  it('rejects member rolling edits or a missing reviewed parent', async () => {
    value = { ...teamFixture(true), editable_fields: ['tokens_5h'] }
    await expect(getTeamLimits({ teamId: 'tea_test', userId: 'usr_member' })).rejects.toThrow()
    value = { ...teamFixture(true), parent_etag: undefined }
    await expect(getTeamLimits({ teamId: 'tea_test', userId: 'usr_member' })).rejects.toThrow()
  })
  it('requires published exact target before confirming a write', async () => {
    await expect(
      saveTeamLimits(
        { teamId: 'tea_test' },
        'a'.repeat(64),
        { tokens_month: 0, reason: 'Close' },
        'csrf',
      ),
    ).rejects.toThrow('Unconfirmed')
    value = { ...teamFixture(), enforced: false }
    await expect(
      saveTeamLimits({ teamId: 'tea_test' }, 'a'.repeat(64), { rpm: 60, reason: 'Review' }, 'csrf'),
    ).rejects.toThrow('Unconfirmed')
  })
  it('accepts normalized exact decimals while preserving original input and validator', async () => {
    value = { ...teamFixture(), stored: { ...teamFixture().stored, money_month: '1.2' } }
    await expect(
      saveTeamLimits(
        { teamId: 'tea_test' },
        'a'.repeat(64),
        { money_month: '1.200', currency: 'USD', reason: 'Exact' },
        'fresh-csrf',
      ),
    ).resolves.toMatchObject({ enforced: true })
    expect(JSON.parse(requests[0].data)).toEqual({
      money_month: '1.200',
      currency: 'USD',
      reason: 'Exact',
    })
    expect(requests[0].headers.get('If-Match')).toBe(`"${'a'.repeat(64)}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  })
})

describe('Team monthly behavior transport boundary', () => {
  it.each([null, '', 'ALERT_ONLY', 'alert_only ', [], true, 0, undefined])(
    'rejects malformed aggregate mode %j',
    async (mode) => {
      const base = teamFixture()
      value = { ...base, stored: { ...base.stored, tokens_month_behavior: mode } }
      await expect(getTeamLimits({ teamId: 'tea_test' })).rejects.toThrow()
    },
  )
  it('requires aggregate modes and rejects synthetic effective modes', async () => {
    const base = teamFixture()
    const stored = { ...base.stored }
    delete stored.tokens_month_behavior
    value = { ...base, stored }
    await expect(getTeamLimits({ teamId: 'tea_test' })).rejects.toThrow()
    value = { ...base, effective: { ...base.effective, tokens_month_behavior: 'alert_only' } }
    await expect(getTeamLimits({ teamId: 'tea_test' })).rejects.toThrow()
  })
  it('accepts independent member and aggregate modes in the exact two-policy chain', async () => {
    const base = teamFixture(true)
    base.ip_policies[0].tokens_month_behavior = 'alert_only'
    value = base
    await expect(
      getTeamLimits({ teamId: 'tea_test', userId: 'usr_member' }),
    ).resolves.toMatchObject({
      stored: { tokens_month: null },
      ip_policies: [{ tokens_month_behavior: 'alert_only' }, {}],
    })
    value = { ...base, stored: { ...base.stored, tokens_month_behavior: 'STOP' } }
    await expect(getTeamLimits({ teamId: 'tea_test', userId: 'usr_member' })).rejects.toThrow()
  })
  it('rejects invalid member modes without issuing a PUT and requires exact saved aggregate mode', async () => {
    await expect(
      saveTeamLimits(
        { teamId: 'tea_test', userId: 'usr_member' },
        'a'.repeat(64),
        { tokens_month_behavior: 'STOP', reason: 'Invalid child' } as never,
        'csrf',
      ),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
    await expect(
      saveTeamLimits(
        { teamId: 'tea_test' },
        'a'.repeat(64),
        { tokens_month_behavior: 'alert_only', reason: 'Soft parent' },
        'csrf',
      ),
    ).rejects.toThrow('Unconfirmed')
    value = {
      ...teamFixture(),
      stored: { ...teamFixture().stored, tokens_month_behavior: 'alert_only' },
    }
    await expect(
      saveTeamLimits(
        { teamId: 'tea_test' },
        'a'.repeat(64),
        { tokens_month_behavior: 'alert_only', reason: 'Soft parent' },
        'csrf-current',
      ),
    ).resolves.toMatchObject({ enforced: true })
    expect(JSON.parse(requests[1].data)).toEqual({
      tokens_month_behavior: 'alert_only',
      reason: 'Soft parent',
    })
  })
})

describe('Team-member own monthly behavior', () => {
  it('confirms exact independent stored modes with sparse original input', async () => {
    const base = teamFixture(true)
    base.stored.tokens_month_behavior = 'alert_only'
    base.ip_policies[1] = { ...base.stored }
    value = base
    const input = { tokens_month_behavior: 'alert_only' as const, reason: 'Member alert threshold' }
    await expect(
      saveTeamLimits({ teamId: 'tea_test', userId: 'usr_member' }, base.etag, input, 'new-csrf'),
    ).resolves.toMatchObject({
      stored: { tokens_month_behavior: 'alert_only', money_month_behavior: 'stop' },
    })
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(requests[0].headers.get('If-Match')).toBe(`"${base.etag}"`)
    expect(requests[0].headers.get('X-CSRF-Token')).toBe('new-csrf')
    base.stored.tokens_month_behavior = 'stop'
    value = base
    await expect(
      saveTeamLimits({ teamId: 'tea_test', userId: 'usr_member' }, base.etag, input, 'new-csrf'),
    ).rejects.toThrow('Unconfirmed')
  })
  it.each(['STOP', null, [], true])('rejects invalid own and parent enum %j', async (mode) => {
    const base = teamFixture(true)
    value = {
      ...base,
      ip_policies: [base.ip_policies[0], { ...base.stored, money_month_behavior: mode }],
    }
    await expect(getTeamLimits({ teamId: 'tea_test', userId: 'usr_member' })).rejects.toThrow()
  })
})
