import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import { getLimits, saveLimits } from './resource-limits'
const adapter = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = adapter
})
const policy = () => ({ rpm: null, concurrency: null, ip_mode: 'none' as const, ip_ranges: [] })
function response(kind = 'personal_key') {
  return {
    kind,
    id: 'key_test',
    account_id: 'key_root',
    etag: 'current',
    stored: policy(),
    effective: { rpm: null, concurrency: null },
    ip_policies: kind === 'user' || kind === 'project' ? [policy()] : [policy(), policy()],
  }
}
function serve(data: unknown) {
  client.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: '',
    headers: new AxiosHeaders(),
    data,
  })
}
it('canonicalizes both Personal owned policies while effective stays numeric-only', async () => {
  serve(response())
  const record = await getLimits('/keys/key_test')
  expect(record.stored.tokens_month_behavior).toBe('stop')
  expect(record.ip_policies.map((p) => p.money_month_behavior)).toEqual(['stop', 'stop'])
  expect(record.effective).not.toHaveProperty('tokens_month_behavior')
})
it.each(['stop', 'alert_only'] as const)(
  'accepts exact Key mode %s in both response and request',
  async (mode) => {
    const data = response()
    Object.assign(data.stored, { tokens_month_behavior: mode, money_month_behavior: mode })
    data.ip_policies[1] = { ...data.stored }
    serve(data)
    const saved = await saveLimits(
      '/keys/key_test',
      'current',
      {
        ...policy(),
        ip_mode: 'none',
        tokens_month_behavior: mode,
        money_month_behavior: mode,
        reason: 'review',
      },
      'fresh-csrf',
    )
    expect(saved.stored.tokens_month_behavior).toBe(mode)
  },
)
it.each(['project_key'])(
  'rejects %s returned through an unrelated aggregate endpoint',
  async (kind) => {
    const data = response(kind)
    Object.assign(data.stored, { tokens_month_behavior: 'alert_only' })
    serve(data)
    await expect(getLimits('/projects/prj_test')).rejects.toThrow('scope')
  },
)
it.each([null, '', 'ALERT_ONLY', 'alert_only ', [], 1])(
  'rejects malformed Key mode %j',
  async (mode) => {
    const data = response()
    Object.assign(data.stored, { tokens_month_behavior: mode })
    serve(data)
    await expect(getLimits('/keys/key_test')).rejects.toThrow('monthly behavior')
  },
)
it('rejects malformed mode input on Project Key route before any HTTP dispatch', async () => {
  let calls = 0
  client.defaults.adapter = async () => {
    calls++
    throw new Error('dispatch')
  }
  await expect(
    saveLimits(
      '/projects/prj_test/keys/key_test',
      'current',
      { ...policy(), ip_mode: 'none', tokens_month_behavior: 'ALERT_ONLY' as 'stop', reason: 'r' },
      'csrf',
    ),
  ).rejects.toThrow('Invalid Personal')
  expect(calls).toBe(0)
})
it('rejects mixed scalar effective behavior and malformed policy-chain lengths', async () => {
  const data = response()
  Object.assign(data.effective, { money_month_behavior: 'alert_only' })
  serve(data)
  await expect(getLimits('/keys/key_test')).rejects.toThrow('effective')
  for (const policies of [[], [policy()], [policy(), policy(), policy()]]) {
    const x = response()
    x.ip_policies = policies
    serve(x)
    await expect(getLimits('/keys/key_test')).rejects.toThrow('chain')
  }
})

it('accepts exact Project Key stored modes through the selected resource while rejecting another Key response', async () => {
  const data = response('project_key')
  data.id = 'pky_test'
  Object.assign(data.stored, { tokens_month_behavior: 'alert_only', money_month_behavior: 'stop' })
  data.ip_policies[1] = { ...data.stored }
  serve(data)
  expect((await getLimits('/projects/prj_test/keys/pky_test')).stored.tokens_month_behavior).toBe(
    'alert_only',
  )
  await expect(getLimits('/projects/prj_test/keys/pky_other')).rejects.toThrow('scope')
})

it('rejects a valid-enum Project Key PUT result that differs from the captured monthly intent', async () => {
  const data = response('project_key')
  data.id = 'pky_test'
  Object.assign(data.stored, { tokens_month_behavior: 'alert_only', money_month_behavior: 'stop' })
  data.ip_policies[1] = { ...data.stored }
  serve(data)
  await expect(
    saveLimits(
      '/projects/prj_test/keys/pky_test',
      'original',
      { ...policy(), tokens_month_behavior: 'stop', reason: 'Original hard decision' },
      'csrf',
    ),
  ).rejects.toThrow('confirmation mismatch')
})
