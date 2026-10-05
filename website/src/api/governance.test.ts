import { afterEach, expect, it } from 'vitest'
import { AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import client from './client'
import { getRoles } from './governance'

const original = client.defaults.adapter
afterEach(() => {
  client.defaults.adapter = original
})
function serveCount(count: unknown) {
  const requests: InternalAxiosRequestConfig[] = []
  const row = {
    id: 'rol_custom',
    name: 'Retained role',
    builtin: false,
    permissions: ['providers.read'],
    ...(count === undefined ? {} : { member_count: count }),
  }
  const data = { items: [row], available_permissions: ['providers.read'] }
  client.defaults.adapter = async (config) => {
    requests.push(config)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data }
  }
  return { requests, row }
}
it.each([0, 1, 27, Number.MAX_SAFE_INTEGER])(
  'reads the authoritative safe retained member count %s without additional directory calls',
  async (count) => {
    const { requests, row } = serveCount(count)
    const signal = new AbortController().signal
    const page = await getRoles(signal)
    expect(page.items[0].member_count).toBe(count)
    expect(page.items[0].permissions).toEqual(['providers.read'])
    expect(page.available_permissions).toEqual(['providers.read'])
    expect(page.items[0]).not.toBe(row)
    expect(requests.map((r) => r.url)).toEqual(['/admin/roles'])
    expect(requests[0].signal).toBe(signal)
  },
)
it.each([undefined, null, -1, 1.5, '2', NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])(
  'keeps absent or invalid member count %s unknown without numeric coercion or zero fallback',
  async (count) => {
    const { row } = serveCount(count)
    const page = await getRoles()
    expect(page.items[0].member_count).toBeNull()
    expect('member_count' in row ? row.member_count : undefined).toBe(count)
  },
)
