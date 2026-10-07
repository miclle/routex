import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import {
  decodeConnectionStatus,
  getConnectionStatus,
  saveConnectionStatus,
} from './connection-status'
const original = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const row = () => ({
  id: 'con_one',
  provider_id: 'prv_one',
  name: 'Primary',
  protocol: 'openai_chat',
  base_url: 'https://upstream.example.invalid/v1',
  egress_mode: 'direct',
  egress_id: null,
  etag: token,
  can_edit: true,
  enabled: true,
})
let requests: InternalAxiosRequestConfig[], data: unknown, headers: AxiosHeaders
beforeEach(() => {
  requests = []
  data = row()
  headers = new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: `"${token}"` })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, status: 200, statusText: '', headers }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('GET uses one exact scoped read, retains false and complete contextual fields', async () => {
  data = { ...row(), enabled: false }
  expect((await getConnectionStatus('prv_one', 'con_one')).enabled).toBe(false)
  expect(requests.map((x) => x.url)).toEqual(['/admin/connections/con_one/status'])
})
it.each([undefined, null, 0, 'false', [], {}])('rejects non-boolean enabled %j', (value) => {
  expect(() => decodeConnectionStatus({ ...row(), enabled: value })).toThrow()
})
it.each([
  { ...row(), extra: true },
  { ...row(), protocol: ['openai_chat'] },
  { ...row(), egress_mode: ['direct'] },
  { ...row(), enabled: undefined },
])('rejects malformed exact context', (value) => {
  expect(() => decodeConnectionStatus(value)).toThrow()
})
it('preserves exact reviewed bool/reason/If-Match/CSRF with no child calls', async () => {
  data = { connection: { ...row(), enabled: false }, runtime_applied: true, changed: true }
  await saveConnectionStatus(
    'prv_one',
    'con_one',
    token,
    { enabled: false, reason: 'Reviewed' },
    'current-csrf',
  )
  expect(JSON.parse(requests[0].data)).toEqual({ enabled: false, reason: 'Reviewed' })
  expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('current-csrf')
  expect(requests).toHaveLength(1)
})
it.each([
  { connection: row(), runtime_applied: true, changed: true },
  { connection: { ...row(), enabled: false }, runtime_applied: false, changed: true },
  {
    connection: { ...row(), enabled: false, can_edit: false },
    runtime_applied: true,
    changed: false,
  },
  {
    connection: { ...row(), enabled: false, etag: `${'c'.repeat(64)}.${'d'.repeat(64)}` },
    runtime_applied: true,
    changed: false,
  },
])('cannot reconcile a malformed or unconfirmed PUT result', async (value) => {
  data = value
  await expect(
    saveConnectionStatus('prv_one', 'con_one', token, { enabled: false, reason: 'R' }, 'csrf'),
  ).rejects.toThrow()
})
it.each(['public, no-store', 'private', 'private, no-store, public'])(
  'requires private no-store %s',
  async (control) => {
    headers.set('Cache-Control', control)
    await expect(getConnectionStatus('prv_one', 'con_one')).rejects.toThrow()
  },
)
it('does not synthesize catalogue status when metadata has no enabled field', () => {
  const { enabled, ...legacy } = row()
  expect(enabled).toBe(true)
  expect(() => decodeConnectionStatus(legacy)).toThrow()
})
