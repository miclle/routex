import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import { getLimits, saveLimits } from './resource-limits'
import type { LimitInput } from '@/types/resource-limits'
const original = client.defaults.adapter
let response: unknown
beforeEach(() => {
  client.defaults.adapter = async (config) => ({
    config,
    data: response,
    status: 200,
    statusText: '',
    headers: new AxiosHeaders(),
  })
})
afterEach(() => {
  client.defaults.adapter = original
})
function policy() {
  return {
    kind: 'user',
    id: 'usr_member',
    stored: { tokens_month_behavior: 'stop', money_month_behavior: 'alert_only' },
    effective: {},
    ip_policies: [{ tokens_month_behavior: 'stop', money_month_behavior: 'alert_only' }],
  }
}
describe('Personal monthly behavior boundary', () => {
  it.each([null, '', 'unknown', 0, false, ['stop'], { mode: 'stop' }])(
    'rejects invalid recorded mode %j',
    async (mode) => {
      response = {
        ...policy(),
        stored: { tokens_month_behavior: mode, money_month_behavior: 'stop' },
      }
      await expect(getLimits('/admin/members/usr_member')).rejects.toThrow()
    },
  )
  it('defaults omitted legacy User modes to stop without adding effective fields', async () => {
    response = { ...policy(), stored: {}, ip_policies: [{}] }
    const record = await getLimits('/admin/members/usr_member')
    expect(record.stored.tokens_month_behavior).toBe('stop')
    expect(record.stored.money_month_behavior).toBe('stop')
    expect(record.effective).not.toHaveProperty('tokens_month_behavior')
  })
  it('preserves independent inert null-cap modes and rejects malformed parent modes', async () => {
    response = {
      ...policy(),
      stored: {
        tokens_month: null,
        money_month: null,
        tokens_month_behavior: 'alert_only',
        money_month_behavior: 'stop',
      },
    }
    expect((await getLimits('/admin/members/usr_member')).stored).toMatchObject({
      tokens_month: null,
      money_month: null,
      tokens_month_behavior: 'alert_only',
      money_month_behavior: 'stop',
    })
    response = { ...policy(), ip_policies: [{ tokens_month_behavior: ['stop'] }] }
    await expect(getLimits('/admin/members/usr_member')).rejects.toThrow()
  })
  it('rejects modes fabricated in effective projection or a nonuser stored policy', async () => {
    response = { ...policy(), effective: { tokens_month_behavior: 'stop' } }
    await expect(getLimits('/admin/members/usr_member')).rejects.toThrow()
    response = { ...policy(), kind: 'project' }
    await expect(getLimits('/projects/prj_member')).rejects.toThrow()
  })
  it('checks a PUT result before accepting current application', async () => {
    response = { ...policy(), stored: { tokens_month_behavior: null } }
    await expect(
      saveLimits(
        '/admin/members/usr_member',
        'a'.repeat(64),
        { reason: 'Review' } as LimitInput,
        'csrf',
      ),
    ).rejects.toThrow()
  })
})

it('allows canonical User parent modes on a Personal Key without inventing a Key behavior', async () => {
  response = {
    ...policy(),
    kind: 'personal_key',
    stored: {},
    ip_policies: [...policy().ip_policies, {}],
  }
  const key = await getLimits('/keys/key_member')
  expect(key.stored).not.toHaveProperty('tokens_month_behavior')
  expect(key.ip_policies[0].money_month_behavior).toBe('alert_only')
})
it.each(['/keys/key_member', '/projects/prj_member'])(
  'never submits behavior fields to other stored scopes %s',
  async (path) => {
    response = policy()
    await expect(
      saveLimits(
        path,
        'a'.repeat(64),
        { tokens_month_behavior: 'stop', reason: 'Review' } as LimitInput,
        'csrf',
      ),
    ).rejects.toThrow('Invalid Personal monthly behavior input')
  },
)

it.each(['project', 'project_key', 'team', 'team_member'])(
  'rejects User behaviors on nonuser %s IP policies',
  async (kind) => {
    response = { ...policy(), kind, stored: {} }
    await expect(getLimits('/projects/prj_member')).rejects.toThrow()
  },
)
it('rejects behaviors on the Personal Key child policy', async () => {
  response = {
    ...policy(),
    kind: 'personal_key',
    stored: {},
    ip_policies: [{}, { tokens_month_behavior: 'stop' }],
  }
  await expect(getLimits('/keys/key_member')).rejects.toThrow()
})
it('defaults only the legacy Personal Key User parent modes to stop', async () => {
  response = { ...policy(), kind: 'personal_key', stored: {}, ip_policies: [{}, {}] }
  const record = await getLimits('/keys/key_member')
  expect(record.ip_policies[0]).toMatchObject({
    tokens_month_behavior: 'stop',
    money_month_behavior: 'stop',
  })
  expect(record.ip_policies[1]).not.toHaveProperty('tokens_month_behavior')
})

it.each([
  ['user', []],
  ['personal_key', [{}]],
  ['project_key', [{}, {}, {}]],
])('rejects ambiguous %s policy chain', async (kind, ip_policies) => {
  response = { ...policy(), kind, stored: {}, ip_policies }
  await expect(getLimits('/keys/key_member')).rejects.toThrow('Invalid resource policy chain')
})
