import { afterEach, beforeEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { saveProviderModelStatus } from './provider-model-status'
const original = client.defaults.adapter
let requests: InternalAxiosRequestConfig[], data: unknown
const row = () => ({
  id: 'pmd_one',
  enabled: false,
  etag: 'pms_saved',
  upstream_name: 'Exact/Name',
  supports_image_input: true,
  supports_pdf_input: false,
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
it('writes only enabled and the exact body revision with current CSRF and cancellation', async () => {
  const controller = new AbortController()
  const input = { enabled: false, etag: 'pms_reviewed', supports_image_input: false }
  const result = await saveProviderModelStatus('pmd_one', input, 'fresh-csrf', controller.signal)
  expect(JSON.parse(requests[0].data)).toEqual({ enabled: false, etag: 'pms_reviewed' })
  expect(requests[0].url).toBe('/admin/provider-models/pmd_one')
  expect(requests[0].method).toBe('patch')
  expect(requests[0].headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  expect(requests[0].headers.get('If-Match')).toBeUndefined()
  expect(requests[0].signal).toBe(controller.signal)
  expect(result.supports_image_input).toBe(true)
  expect(requests).toHaveLength(1)
})
it.each([
  null,
  { ...row(), id: 'pmd_other' },
  { ...row(), enabled: true },
  { ...row(), etag: '' },
  { ...row(), supports_image_input: undefined },
  { ...row(), supports_pdf_input: 'false' },
])('rejects a malformed or mismatched status response %j', async (value) => {
  data = value
  await expect(
    saveProviderModelStatus('pmd_one', { etag: 'pms_reviewed', enabled: false }, 'csrf'),
  ).rejects.toThrow()
})
