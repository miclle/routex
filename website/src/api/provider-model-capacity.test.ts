import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getProviderModelCapacity, saveProviderModelCapacity } from './provider-model-capacity'
const original = client.defaults.adapter,
  token = 'a'.repeat(64)
let requests: InternalAxiosRequestConfig[], data: unknown, headers: AxiosHeaders
const row = () => ({
  provider_model_id: 'pmo_one',
  protocol: 'openai_chat',
  etag: token,
  revision: `bnd_${'0'.repeat(26)}`,
  transport_current: false,
  configured: true,
  max_input_tokens: 8000,
  max_output_tokens: 2000,
  evidence: 'Recorded old transport',
  updated_at: '2026-10-09T01:00:00Z',
})
beforeEach(() => {
  requests = []
  data = row()
  headers = new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: `"${token}"` })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, headers, status: 200, statusText: '' }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('retains historical evidence/revision independently from current review and transport status', async () => {
  expect(await getProviderModelCapacity('pmo_one')).toEqual(row())
  expect(requests).toHaveLength(1)
})
it('uses only four original body fields with exact strong64hex review and fresh CSRF, preserving multiline evidence', async () => {
  const input = {
    max_input_tokens: 12000,
    max_output_tokens: 3000,
    evidence: 'Current source\nNative contract',
    reason: 'Explicit transport attestation',
  }
  data = { ...row(), ...input, transport_current: true }
  delete (data as Record<string, unknown>).reason
  await saveProviderModelCapacity('pmo_one', token, input, 'fresh-csrf')
  expect(JSON.parse(requests[0].data)).toEqual(input)
  expect(requests[0].headers.get('If-Match')).toBe(`"${token}"`)
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
})
it.each([
  { etag: '0' },
  { revision: token },
  { transport_current: undefined },
  { provider_model_id: 'pmo_other' },
  { runtime_applied: true },
])('rejects malformed or invented capacity proof %#', async (patch) => {
  data = { ...row(), ...patch }
  await expect(getProviderModelCapacity('pmo_one')).rejects.toThrow('unavailable')
})
it.each(['no-store', 'public, no-store', 'private, no-store, public'])(
  'requires exact private/no-store response: %s',
  async (control) => {
    headers.set('Cache-Control', control)
    await expect(getProviderModelCapacity('pmo_one')).rejects.toThrow()
  },
)
it('does not pretend a stale or mismatched saved bound is a confirmed current attestation', async () => {
  const input = {
    max_input_tokens: 8000,
    max_output_tokens: 2000,
    evidence: 'Recorded old transport',
    reason: 'Review',
  }
  await expect(saveProviderModelCapacity('pmo_one', token, input, 'csrf')).rejects.toThrow()
  expect(requests).toHaveLength(1)
})
