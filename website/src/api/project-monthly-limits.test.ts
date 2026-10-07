import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import { getLimits, saveLimits } from './resource-limits'
const adapter = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = adapter
})
const policy = () => ({ rpm: null, concurrency: null, ip_mode: 'none' as const, ip_ranges: [] })
const response = (kind = 'project') => ({
  kind,
  id: kind === 'project' ? 'prj_test' : 'key_test',
  stored: policy(),
  effective: { rpm: null },
  ip_policies: kind === 'project' ? [policy()] : [policy(), policy()],
})
function serve(data: unknown) {
  client.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: '',
    headers: new AxiosHeaders(),
    data,
  })
}
it('canonicalizes legacy Project and Project-Key policies independently', async () => {
  serve(response())
  const aggregate = await getLimits('/projects/prj_test')
  expect(aggregate.stored.tokens_month_behavior).toBe('stop')
  expect(aggregate.ip_policies[0].money_month_behavior).toBe('stop')
  serve(response('project_key'))
  const child = await getLimits('/projects/prj_test/keys/key_test')
  expect(child.ip_policies[0].tokens_month_behavior).toBe('stop')
  expect(child.stored.tokens_month_behavior).toBe('stop')
  expect(child.ip_policies[1].money_month_behavior).toBe('stop')
})
it('emits independent reviewed Project modes and exact money, but no effective modes', async () => {
  const data = response()
  Object.assign(data.stored, { tokens_month_behavior: 'alert_only', money_month_behavior: 'stop' })
  data.ip_policies[0] = data.stored
  serve(data)
  const result = await saveLimits(
    '/projects/prj_test',
    'review',
    {
      ...policy(),
      tokens_month: 100,
      money_month: '0.000000000000000001',
      currency: 'USD',
      tokens_month_behavior: 'alert_only',
      money_month_behavior: 'stop',
      reason: 'review',
    },
    'csrf',
  )
  expect(result.stored.tokens_month_behavior).toBe('alert_only')
  expect(result.effective).not.toHaveProperty('tokens_month_behavior')
})
it.each([null, 'ALERT_ONLY', 'alert_only ', [], false])(
  'rejects malformed Project mode %j',
  async (mode) => {
    const data = response()
    Object.assign(data.stored, { tokens_month_behavior: mode })
    serve(data)
    await expect(getLimits('/projects/prj_test')).rejects.toThrow('monthly behavior')
  },
)
it('rejects malformed Project Key stored modes and chain without borrowing parent authority', async () => {
  const data = response('project_key')
  Object.assign(data.stored, { tokens_month_behavior: 'ALERT_ONLY' })
  serve(data)
  await expect(getLimits('/projects/prj_test/keys/key_test')).rejects.toThrow('monthly behavior')
  serve({ ...response('project_key'), ip_policies: [policy()] })
  await expect(getLimits('/projects/prj_test/keys/key_test')).rejects.toThrow('chain')
})
