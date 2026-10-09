import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { decodeConnectionTest, testConnection } from './connection-test'
const original = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const result = () => ({
  connection_id: 'con_one',
  credential_id: 'crd_two',
  outcome: 'passed',
  scope: 'model_discovery',
  discovered_model_count: 0,
  checked_at: '2026-10-09T13:02:01.123456789Z',
})
afterEach(() => {
  client.defaults.adapter = original
})
it('accepts empty and complete bounded discovery, failed and authentication-only results', () => {
  expect(decodeConnectionTest(result()).discovered_model_count).toBe(0)
  expect(
    decodeConnectionTest({ ...result(), discovered_model_count: 2000 }).discovered_model_count,
  ).toBe(2000)
  expect(
    decodeConnectionTest({ ...result(), outcome: 'failed', discovered_model_count: null }).outcome,
  ).toBe('failed')
  for (const outcome of ['passed', 'failed'])
    expect(
      decodeConnectionTest({
        ...result(),
        outcome,
        scope: 'authentication_only',
        discovered_model_count: null,
      }).scope,
    ).toBe('authentication_only')
})
it.each([
  null,
  [],
  {},
  { ...result(), secret: 'private' },
  { ...result(), runtime_applied: true },
  { ...result(), outcome: 'complete' },
  { ...result(), scope: 'inference' },
  { ...result(), credential_id: 'con_other' },
  { ...result(), connection_id: 'con_one/other' },
  ...[-1, 2001, 1.1, null, '1'].map((discovered_model_count) => ({
    ...result(),
    discovered_model_count,
  })),
  { ...result(), outcome: 'failed', discovered_model_count: 1 },
  { ...result(), scope: 'authentication_only' },
  ...[
    '2026-02-30T00:00:00Z',
    '2026-10-09T24:00:00Z',
    '2026-10-09T13:02:01+08:00',
    '2026-10-09T13:02:01.1234567890Z',
    'invalid',
  ].map((checked_at) => ({ ...result(), checked_at })),
])('rejects invalid, contradictory or private result %j', (value) => {
  expect(() => decodeConnectionTest(value)).toThrow('Connection test response unavailable')
})
it('posts only the explicitly chosen credential with exact strong review and CSRF; no discovery read', async () => {
  const requests: InternalAxiosRequestConfig[] = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ 'Cache-Control': 'private, no-store' }),
      data: result(),
    }
  }
  await expect(testConnection('con_one', 'crd_two', token, 'current')).resolves.toEqual(result())
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/admin/connections/con_one/test')
  expect(requests[0].method).toBe('post')
  expect(requests[0].timeout).toBe(15_000)
  expect(JSON.parse(requests[0].data)).toEqual({ credential_id: 'crd_two' })
  expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('current')
})
it.each([
  { ...result(), connection_id: 'con_other' },
  { ...result(), credential_id: 'crd_other' },
])('rejects borrowed response identity', async (data) => {
  client.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: 'OK',
    headers: new AxiosHeaders({ 'Cache-Control': 'private,no-store' }),
    data,
  })
  await expect(testConnection('con_one', 'crd_two', token, 'current')).rejects.toThrow(
    'Connection test response unavailable',
  )
})
it.each(['', 'public, no-store', 'private', 'private,no-store,max-age=0'])(
  'rejects response privacy %s',
  async (control) => {
    client.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ 'Cache-Control': control }),
      data: result(),
    })
    await expect(testConnection('con_one', 'crd_two', token, 'current')).rejects.toThrow()
  },
)
it('rejects malformed review and cancelled dispatch without sending', async () => {
  let calls = 0
  client.defaults.adapter = async (config) => {
    calls++
    return { config, status: 200, statusText: 'OK', headers: {}, data: result() }
  }
  await expect(testConnection('con_one', 'crd_two', 'bad', 'current')).rejects.toThrow()
  const controller = new AbortController()
  controller.abort()
  await expect(
    testConnection('con_one', 'crd_two', token, 'current', controller.signal),
  ).rejects.toThrow()
  expect(calls).toBe(0)
})
