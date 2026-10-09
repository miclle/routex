import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { decodeProviderStatus, getProviderStatus, saveProviderStatus } from './provider-status'
const original = client.defaults.adapter
const token = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const next = `${'a'.repeat(64)}.${'c'.repeat(64)}`
const row = () => ({ id: 'prv_one', name: 'Provider', etag: token, can_edit: true, enabled: true })
let data: unknown, headers: AxiosHeaders, requests: InternalAxiosRequestConfig[]
beforeEach(() => {
  data = row()
  headers = new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: `"${token}"` })
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, headers, status: 200, statusText: '' }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('GET reads one exact Provider status with false enabled and independent read-only projection', async () => {
  data = { ...row(), enabled: false, can_edit: false }
  const controller = new AbortController()
  expect(await getProviderStatus('prv_one', controller.signal)).toEqual(data)
  expect(requests).toHaveLength(1)
  expect(requests[0].url).toBe('/admin/providers/prv_one/status')
  expect(requests[0].signal).toBe(controller.signal)
})
it('PUT submits only the exact reviewed bool/reason/If-Match with fresh CSRF', async () => {
  data = {
    provider: { ...row(), enabled: false, etag: next },
    changed: true,
    runtime_applied: true,
  }
  headers.set('ETag', `"${next}"`)
  const controller = new AbortController()
  await saveProviderStatus(
    'prv_one',
    token,
    { enabled: false, reason: 'Exact reason' },
    'fresh-csrf',
    controller.signal,
  )
  expect(JSON.parse(requests[0].data)).toEqual({ enabled: false, reason: 'Exact reason' })
  expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  expect(requests[0].signal).toBe(controller.signal)
  expect(requests[0].method).toBe('put')
  expect(requests).toHaveLength(1)
})
it('accepts current-target reconciliation without interpreting changed=false as original historical proof', async () => {
  data = {
    provider: { ...row(), enabled: false, etag: next, name: 'New current name' },
    changed: false,
    runtime_applied: true,
  }
  headers.set('ETag', `"${next}"`)
  expect(
    (await saveProviderStatus('prv_one', token, { enabled: false, reason: 'Original' }, 'csrf'))
      .changed,
  ).toBe(false)
})
it.each([
  null,
  {},
  { ...row(), enabled: undefined },
  { ...row(), enabled: 'false' },
  { ...row(), enabled: 0 },
  { ...row(), can_edit: 'true' },
  { ...row(), extra: true },
  { ...row(), etag: 'opaque' },
  { ...row(), name: '' },
])('rejects malformed or absent authoritative state %j', (value) => {
  expect(() => decodeProviderStatus(value)).toThrow()
})
it.each(['public, no-store', 'private', 'private, no-store, public', 'no-store'])(
  'requires private/no-store %s',
  async (value) => {
    headers.set('Cache-Control', value)
    await expect(getProviderStatus('prv_one')).rejects.toThrow()
  },
)
it('rejects wrong Provider and mismatched quoted read proof', async () => {
  data = { ...row(), id: 'prv_other' }
  await expect(getProviderStatus('prv_one')).rejects.toThrow()
  data = row()
  headers.set('ETag', token)
  await expect(getProviderStatus('prv_one')).rejects.toThrow()
})
it.each([
  { provider: { ...row(), enabled: false }, changed: true, runtime_applied: false },
  { provider: { ...row(), enabled: false, id: 'prv_other' }, changed: true, runtime_applied: true },
  { provider: { ...row(), enabled: false, can_edit: false }, changed: true, runtime_applied: true },
  { provider: row(), changed: true, runtime_applied: true },
  {
    provider: { ...row(), enabled: false, etag: `${'d'.repeat(64)}.${'b'.repeat(64)}` },
    changed: true,
    runtime_applied: true,
  },
  { provider: { ...row(), enabled: false }, changed: 'true', runtime_applied: true },
])(
  'requires exact saved current identity/status and actual local runtime application %j',
  async (value) => {
    data = value
    const body = value.provider
    headers.set('ETag', `"${body.etag}"`)
    await expect(
      saveProviderStatus('prv_one', token, { enabled: false, reason: 'Reviewed' }, 'csrf'),
    ).rejects.toThrow()
  },
)
it.each(['', ' padded ', '\n', '\ud800', '中'.repeat(342)])(
  'rejects invalid reason before dispatch %j',
  async (reason) => {
    await expect(
      saveProviderStatus('prv_one', token, { enabled: false, reason }, 'csrf'),
    ).rejects.toThrow()
    expect(requests).toHaveLength(0)
  },
)
it('rejects child/name fields before dispatch instead of rewriting unrelated configuration', async () => {
  const input = { enabled: false, reason: 'Reviewed', name: 'Rewrite', connection_ids: [] }
  await expect(saveProviderStatus('prv_one', token, input, 'csrf')).rejects.toThrow()
  expect(requests).toHaveLength(0)
})
