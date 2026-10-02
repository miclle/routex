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
