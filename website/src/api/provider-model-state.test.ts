import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { saveProviderModelState } from './provider-model-state'
const original = client.defaults.adapter
let requests: InternalAxiosRequestConfig[], data: unknown
const row = () => ({
  id: 'pmo_one',
  upstream_name: 'Model',
  etag: '0',
  enabled: true,
  supports_image_input: true,
  supports_pdf_input: false,
  capabilities_transport_current: false,
  capability_review_etag: 'a'.repeat(64),
})
beforeEach(() => {
  requests = []
  data = row()
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, data, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(() => {
  client.defaults.adapter = original
})
it('availability-only writes preserve declarations and use only opaque body revision, boolean and CSRF', async () => {
  data = { ...row(), enabled: false }
  await saveProviderModelState('pmo_one', { etag: '0', enabled: false }, 'fresh-csrf')
  expect(JSON.parse(requests[0].data)).toEqual({ etag: '0', enabled: false })
  expect(requests[0].url).toBe('/admin/provider-models/pmo_one')
  expect(requests[0].headers.get('If-Match')).toBeUndefined()
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
})
it.each([false, true])(
  'complete capability review (combined availability=%s) retains exact pair and transport token',
  async (combined) => {
    const input = {
      etag: '0',
      capability_review_etag: 'a'.repeat(64),
      supports_image_input: false,
      supports_pdf_input: true,
      ...(combined ? { enabled: false } : {}),
    }
    data = {
      ...row(),
      supports_image_input: false,
      supports_pdf_input: true,
      capabilities_transport_current: true,
      ...(combined ? { enabled: false } : {}),
    }
    await saveProviderModelState('pmo_one', input, 'csrf')
    expect(JSON.parse(requests[0].data)).toEqual(input)
    expect(requests[0].headers.get('If-Match')).toBeUndefined()
  },
)
it.each([
  { etag: '0', enabled: false, supports_image_input: true, supports_pdf_input: false },
  { etag: '0', capability_review_etag: 'a'.repeat(64), supports_image_input: true },
  {
    etag: '0',
    capability_review_etag: 'A'.repeat(64),
    supports_image_input: true,
    supports_pdf_input: false,
  },
  { etag: '0', enabled: false, reason: 'invented' },
])('rejects incomplete or unreviewed capability writes before dispatch %#', async (input) => {
  await expect(saveProviderModelState('pmo_one', input as never, 'csrf')).rejects.toThrow()
  expect(requests).toHaveLength(0)
})
it.each([
  { id: 'pmo_other' },
  { capabilities_transport_current: undefined },
  { capability_review_etag: 'raw-row' },
  { supports_image_input: false },
])('rejects obsolete/malformed confirmation %#', async (patch) => {
  data = { ...row(), capabilities_transport_current: true, ...patch }
  await expect(
    saveProviderModelState(
      'pmo_one',
      {
        etag: '0',
        capability_review_etag: 'a'.repeat(64),
        supports_image_input: true,
        supports_pdf_input: false,
      },
      'csrf',
    ),
  ).rejects.toThrow('unavailable')
  expect(requests).toHaveLength(1)
})
