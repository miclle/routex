import { beforeEach, afterEach, describe, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getLimits, saveLimits } from './resource-limits'
import type { LimitInput } from '@/types/resource-limits'
const original = client.defaults.adapter
let response: unknown
let dispatched: InternalAxiosRequestConfig | undefined
beforeEach(() => {
  dispatched = undefined
  client.defaults.adapter = async (config) => {
    dispatched = config
    return {
      config,
      data: response,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
  }
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
  it('rejects modes fabricated in effective projection', async () => {
    response = { ...policy(), effective: { tokens_month_behavior: 'stop' } }
    await expect(getLimits('/admin/members/usr_member')).rejects.toThrow()
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

it('allows canonical User parent modes alongside a legacy hard Personal Key', async () => {
  response = {
    ...policy(),
    kind: 'personal_key',
    stored: {},
    ip_policies: [...policy().ip_policies, {}],
  }
  const key = await getLimits('/keys/key_member')
  expect(key.stored.tokens_month_behavior).toBe('stop')
  expect(key.ip_policies[0].money_month_behavior).toBe('alert_only')
})
it.each(['/projects/prj_member/keys/key_member'])(
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

it('accepts canonical Project stored and sole parent modes with the actual Project identity', async () => {
  const stored = {
    tokens_month: 100,
    money_month: '0.000000000000000001',
    currency: 'USD',
    tokens_month_behavior: 'alert_only',
    money_month_behavior: 'stop',
  }
  response = { ...policy(), kind: 'project', id: 'prj_member', stored, ip_policies: [stored] }
  const record = await getLimits('/projects/prj_member')
  expect(record.kind).toBe('project')
  expect(record.id).toBe('prj_member')
  expect(record.stored).toEqual(stored)
  expect(record.ip_policies).toEqual([stored])
  expect(record.effective).not.toHaveProperty('tokens_month_behavior')
  expect(record.effective).not.toHaveProperty('money_month_behavior')
})
it('submits both independent Project modes without rewriting the reviewed policy', async () => {
  const input: LimitInput = {
    tokens_month: 0,
    money_month: null,
    tokens_month_behavior: 'stop',
    money_month_behavior: 'alert_only',
    rpm: null,
    concurrency: null,
    ip_mode: 'none',
    ip_ranges: [],
    reason: 'Review Project monthly decisions',
  }
  const stored = {
    tokens_month: input.tokens_month,
    money_month: input.money_month,
    tokens_month_behavior: input.tokens_month_behavior,
    money_month_behavior: input.money_month_behavior,
    rpm: input.rpm,
    concurrency: input.concurrency,
    ip_mode: input.ip_mode,
    ip_ranges: input.ip_ranges,
  }
  response = {
    ...policy(),
    kind: 'project',
    id: 'prj_member',
    stored,
    ip_policies: [stored],
    enforced: true,
  }
  const record = await saveLimits('/projects/prj_member', 'a'.repeat(64), input, 'csrf')
  expect(dispatched?.method).toBe('put')
  expect(dispatched?.url).toBe('/projects/prj_member/limits')
  expect(JSON.parse(dispatched?.data as string)).toEqual(input)
  expect(dispatched?.headers.get('If-Match')).toBe('"' + 'a'.repeat(64) + '"')
  expect(dispatched?.headers.get('X-CSRF-Token')).toBe('csrf')
  expect(record.id).toBe('prj_member')
  expect(record.stored).toEqual(stored)
  expect(record.ip_policies).toEqual([stored])
  expect(record.enforced).toBe(true)
})

it.each(['project_key', 'team', 'team_member'])(
  'rejects User behaviors on nonuser %s IP policies',
  async (kind) => {
    response = { ...policy(), kind, stored: {} }
    await expect(getLimits('/projects/prj_member')).rejects.toThrow()
  },
)
it('rejects behaviors on the Project Key child policy', async () => {
  response = {
    ...policy(),
    kind: 'project_key',
    stored: {},
    ip_policies: [{}, { tokens_month_behavior: 'stop' }],
  }
  await expect(getLimits('/keys/key_member')).rejects.toThrow()
})
it('defaults both legacy Personal Key and User parent modes to stop', async () => {
  response = { ...policy(), kind: 'personal_key', stored: {}, ip_policies: [{}, {}] }
  const record = await getLimits('/keys/key_member')
  expect(record.ip_policies[0]).toMatchObject({
    tokens_month_behavior: 'stop',
    money_month_behavior: 'stop',
  })
  expect(record.ip_policies[1]).toMatchObject({
    tokens_month_behavior: 'stop',
    money_month_behavior: 'stop',
  })
})

it.each([
  ['user', []],
  ['personal_key', [{}]],
  ['project_key', [{}, {}, {}]],
])('rejects ambiguous %s policy chain', async (kind, ip_policies) => {
  response = { ...policy(), kind, stored: {}, ip_policies }
  await expect(getLimits('/keys/key_member')).rejects.toThrow('Invalid resource policy chain')
})
