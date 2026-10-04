import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { downloadCallsCSV, exportCalls } from './calls'
const original = client.defaults.adapter
let requests: InternalAxiosRequestConfig[]
const blob = new Blob(['request_id\nreq_exact\n'], { type: 'text/csv' })
beforeEach(() => {
  requests = []
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return {
      config,
      data: blob,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({ 'content-type': 'text/csv; charset=utf-8' }),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = original
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('exports only the exact own-Team applied filter scope with Session transport', async () => {
  const signal = new AbortController().signal
  const filters = {
    status: 'success',
    model_id: 'mdl_exact',
    from: '2026-10-01T00:00:00Z',
    to: '2026-10-02T00:00:00Z',
  }
  expect(await exportCalls(false, filters, signal, undefined, 'tea_exact')).toBe(blob)
  expect(requests[0].url).toBe('/teams/tea_exact/calls/export.csv')
  expect(requests[0].params).toEqual(filters)
  expect(requests[0].signal).toBe(signal)
  expect(requests[0].responseType).toBe('blob')
  expect(requests[0].headers.has('Authorization')).toBe(false)
  expect(requests[0].headers.has('X-CSRF-Token')).toBe(false)
})
it('uses a fixed Team filename and removes the transient download node', async () => {
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:team'), revokeObjectURL: vi.fn() })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    expect(this.download).toBe('routex-team-calls.csv')
  })
  downloadCallsCSV(blob, false, undefined, 'tea_exact')
  expect(click).toHaveBeenCalledOnce()
  expect(document.querySelector('a[download]')).toBeNull()
  await vi.waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:team'))
})

it.each(['key_id', 'user_id', 'cursor', 'limit', 'team_id', 'project_id'])(
  'rejects even an empty %s Team selector before dispatch',
  async (field) => {
    await expect(
      exportCalls(false, { [field]: '' }, undefined, undefined, 'tea_exact'),
    ).rejects.toThrow('filters')
    expect(requests).toHaveLength(0)
  },
)
it.each([
  [true, undefined, 'tea_exact'],
  [false, 'prj_exact', 'tea_exact'],
  [false, undefined, '../other'],
] as const)('rejects mixed/unsafe export scope before dispatch', async (admin, project, team) => {
  await expect(exportCalls(admin, {}, undefined, project, team)).rejects.toThrow('scope')
  expect(requests).toHaveLength(0)
})
it.each([
  [false, undefined, undefined, '/calls/export.csv'],
  [true, undefined, undefined, '/admin/calls/export.csv'],
  [false, 'prj_exact', undefined, '/projects/prj_exact/calls/export.csv'],
] as const)(
  'retains legacy scope %s/%s URLs and exact filters',
  async (admin, project, team, path) => {
    const filters = admin ? { user_id: 'usr_exact' } : { key_id: 'key_exact' }
    expect(await exportCalls(admin, filters, undefined, project, team)).toBe(blob)
    expect(requests[0].url).toBe(path)
    expect(requests[0].params).toEqual(filters)
    expect(requests[0].withCredentials).toBe(true)
  },
)
it.each(['application/json', 'text/html', 'text/csv; charset=ascii', ''])(
  'refuses unexpected Team response MIME %s',
  async (mime) => {
    client.defaults.adapter = async (config) => ({
      config,
      data: blob,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': mime }),
    })
    await expect(exportCalls(false, {}, undefined, undefined, 'tea_exact')).rejects.toThrow(
      'response',
    )
  },
)
it('rejects a canceled or non-200/empty/oversized Team response without making a URL', async () => {
  for (const [status, data] of [
    [202, blob],
    [200, new Blob([])],
    [200, new Blob([new Uint8Array(8 * 1024 * 1024 + 1)])],
  ] as const) {
    client.defaults.adapter = async (config) => ({
      config,
      data,
      status,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': 'text/csv' }),
    })
    await expect(exportCalls(false, {}, undefined, undefined, 'tea_exact')).rejects.toThrow(
      'response',
    )
  }
  const controller = new AbortController()
  client.defaults.adapter = async (config) => {
    controller.abort()
    return {
      config,
      data: blob,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({ 'content-type': 'text/csv' }),
    }
  }
  await expect(exportCalls(false, {}, controller.signal, undefined, 'tea_exact')).rejects.toThrow()
})
it('releases its object URL even if a browser download click throws', async () => {
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:team'), revokeObjectURL: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {
    throw new Error('Controlled browser failure')
  })
  expect(() => downloadCallsCSV(blob, false, undefined, 'tea_exact')).toThrow(
    'Controlled browser failure',
  )
  expect(document.querySelector('a[download]')).toBeNull()
  await vi.waitFor(() => expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:team'))
})
