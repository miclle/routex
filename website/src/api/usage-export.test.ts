import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { exportUsage, downloadUsageCSV, usageExportFilename } from './usage-export'
const original = client.defaults.adapter
let requests: InternalAxiosRequestConfig[], data: Blob, mime: string, status: number
const filters = {
  timezone: 'UTC',
  granularity: 'day' as const,
  compare: true,
  from: '2026-09-01T00:00:00Z',
  to: '2026-09-02T00:00:00Z',
  stream: false,
  status: 'canceled' as const,
  protocol: 'anthropic_messages' as const,
}
beforeEach(() => {
  requests = []
  data = new Blob(["text_encoding,tokens\napostrophe_text_v1,'9007199254740993\n"])
  mime = 'text/csv; charset=utf-8'
  status = 200
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      data,
      status,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime }),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
it.each([
  [{}, '/usage/export.csv', 'personal'],
  [{ projectId: 'prj_exact' }, '/projects/prj_exact/usage/export.csv', 'project'],
  [{ teamId: 'tea_exact' }, '/teams/tea_exact/usage/export.csv', 'team'],
  [{ admin: true }, '/admin/usage/export.csv', 'platform'],
] as const)(
  'uses captured %j scope, opaque server bytes and fixed filename',
  async (scope, path, name) => {
    const signal = new AbortController().signal
    expect(await exportUsage(scope, filters, signal)).toBe(data)
    expect(requests).toHaveLength(1)
    expect(requests[0].url).toBe(path)
    expect(requests[0].params).toEqual(filters)
    expect(requests[0].responseType).toBe('blob')
    expect(requests[0].headers.has('Authorization')).toBe(false)
    expect(requests[0].headers.has('X-CSRF-Token')).toBe(false)
    expect(usageExportFilename(scope)).toBe(`routex-${name}-usage.csv`)
  },
)
it.each([
  { teamId: '../other' },
  { projectId: 'prj_a', admin: true },
  { teamId: 'tea_a', projectId: 'prj_a' },
  { teamId: 'tea_a', admin: true },
])('rejects ambiguous/unsafe scope %j before dispatch', async (scope) => {
  await expect(exportUsage(scope, filters, new AbortController().signal)).rejects.toThrow('scope')
  expect(requests).toHaveLength(0)
})
it.each([
  [{}, { user_id: 'usr_a' }],
  [{}, { cursor: 'x' }],
  [{ teamId: 'tea_a' }, { key_id: 'key_a' }],
  [{ teamId: 'tea_a' }, { team_id: 'tea_b' }],
  [{ projectId: 'prj_a' }, { provider_id: 'prv_a' }],
] as const)('rejects unauthorized selector for %j', async (scope, extra) => {
  await expect(
    exportUsage(scope, { ...filters, ...extra }, new AbortController().signal),
  ).rejects.toThrow('filters')
  expect(requests).toHaveLength(0)
})
it('permits platform-only historic selectors without converting exact filters', async () => {
  await exportUsage(
    { admin: true },
    { ...filters, team_id: 'tea_a', provider_id: 'prv_historical' },
    new AbortController().signal,
  )
  expect(requests[0].params.team_id).toBe('tea_a')
})
it.each(['application/json', 'text/html', 'text/csv; charset=ascii', ''])(
  'rejects unexpected MIME %s',
  async (value) => {
    mime = value
    await expect(exportUsage({}, filters, new AbortController().signal)).rejects.toThrow('response')
  },
)
it('rejects non-200, empty and over-limit responses without generating a file', async () => {
  status = 202
  await expect(exportUsage({}, filters, new AbortController().signal)).rejects.toThrow('response')
  status = 200
  data = new Blob([])
  await expect(exportUsage({}, filters, new AbortController().signal)).rejects.toThrow('response')
  data = new Blob([new Uint8Array(8 * 1024 * 1024 + 1)])
  await expect(exportUsage({}, filters, new AbortController().signal)).rejects.toThrow('response')
})
it('accepts exactly 8 MiB and aborts over-limit progress before a late response', async () => {
  data = new Blob([new Uint8Array(8 * 1024 * 1024)])
  expect((await exportUsage({}, filters, new AbortController().signal)).size).toBe(data.size)
  client.defaults.adapter = async (config) => {
    config.onDownloadProgress!({ loaded: 8 * 1024 * 1024 + 1, bytes: 1, lengthComputable: false })
    return {
      config,
      data,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime }),
    }
  }
  await expect(exportUsage({}, filters, new AbortController().signal)).rejects.toThrow()
})
it('does not accept a late response after external cancellation', async () => {
  const controller = new AbortController()
  client.defaults.adapter = async (config) => {
    controller.abort()
    return {
      config,
      data,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime }),
    }
  }
  await expect(exportUsage({}, filters, controller.signal)).rejects.toThrow()
})
it('keeps downloads transient with fixed filenames and prompt URL cleanup', async () => {
  vi.useFakeTimers()
  const create = vi.fn(() => 'blob:export'),
    revoke = vi.fn()
  vi.stubGlobal('URL', { createObjectURL: create, revokeObjectURL: revoke })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    expect(this.download).toBe('routex-team-usage.csv')
    expect(this.href).toBe('blob:export')
  })
  try {
    downloadUsageCSV(data, { teamId: 'tea_exact' })
    expect(click).toHaveBeenCalledOnce()
    expect(document.querySelector('a[download]')).toBeNull()
    await vi.runAllTimersAsync()
    expect(revoke).toHaveBeenCalledWith('blob:export')
  } finally {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  }
})
it.each(['8388609', 'invalid'])(
  'rejects unsafe Content-Length %s without requiring that optional header',
  async (length) => {
    client.defaults.adapter = async (config) => ({
      config,
      data,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime, 'content-length': length }),
    })
    await expect(exportUsage({}, filters, new AbortController().signal)).rejects.toThrow('response')
  },
)
