import { AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import { getRuntimeInstallations } from './runtime-installations'
import type { RuntimeInstallationRecord } from '@/types/runtime-installations'

const originalAdapter = client.defaults.adapter
const instance = 'ins_00000000000000000000000001'
const item: RuntimeInstallationRecord = {
  id: 'rin_00000000000000000000000002',
  projection_version: 1,
  instance_id: instance,
  instance_started_at: '2026-10-08T09:00:00.123456Z',
  snapshot_id: 'cfg_00000000000000000000000001',
  routes_published_at: '2026-10-08T09:01:00Z',
  first_observed_at: '2026-10-08T09:01:00.123456Z',
  instance_status: 'online',
  current_serving_installation_matches: null,
}
let body: unknown
let count: number
let responseStatus: number
beforeEach(() => {
  body = {
    scope: 'single_process_gateway_admission',
    observed_at: '2026-10-08T09:02:00.123456789Z',
    items: [structuredClone(item)],
    next_cursor: null,
  }
  count = 0
  responseStatus = 200
  client.defaults.adapter = async (config) => {
    count++
    expect(config.url).toBe('/admin/runtime/installations')
    expect(config.method).toBe('get')
    expect(config.headers.has('X-CSRF-Token')).toBe(false)
    return {
      data: body,
      status: responseStatus,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      config,
    }
  }
})
afterEach(() => {
  client.defaults.adapter = originalAdapter
})

describe('single-process Gateway installation read boundary', () => {
  it.each([201, 202, 204, 403, 503])(
    'rejects non-200 HTTP %s without publishing observations',
    async (status) => {
      responseStatus = status
      await expect(getRuntimeInstallations()).rejects.toThrow(
        'Invalid Gateway installation observations',
      )
      expect(count).toBe(1)
    },
  )
  it('rejects an unknown filter before HTTP', async () => {
    const filter = { instance_id: instance, unexpected: 'value' }
    await expect(getRuntimeInstallations(filter)).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
    expect(count).toBe(0)
  })

  it('retains explicit unknown and exact historical timestamps without write authority', async () => {
    const page = await getRuntimeInstallations({ instance_id: instance })
    expect(page.items[0]).toEqual(item)
    expect(page.items[0].current_serving_installation_matches).toBeNull()
    expect(count).toBe(1)
  })
  it.each([true, false])('retains independently reported current match %s', async (match) => {
    ;(body as { items: RuntimeInstallationRecord[] }).items[0] = {
      ...item,
      current_serving_installation_matches: match,
    }
    expect((await getRuntimeInstallations()).items[0].current_serving_installation_matches).toBe(
      match,
    )
  })
  it.each([
    { id: 'rin_80000000000000000000000002' },
    { instance_id: instance.toUpperCase() },
    { snapshot_id: `${item.snapshot_id}\n` },
    { instance_id: 'ins_00000000000000000000000002' },
    { first_observed_at: '2026-10-08T09:01:00.1234567Z' },
    { first_observed_at: '2026-10-08T17:01:00+08:00' },
    { first_observed_at: '2026-02-30T09:01:00Z' },
    { instance_status: ['online'] },
    { current_serving_installation_matches: undefined },
    { instance_status: 'offline', current_serving_installation_matches: true },
    { route_digest: 'private' },
    { source_digest: 'private' },
    { projection_version: 0 },
    { projection_version: 2 },
    { projection_version: '1' },
    { projection_version: undefined },
    { first_observed_at: '2026-10-08T09:00:00.123455Z' },
    { routes_published_at: '2026-10-08T09:01:00.123457Z' },
    {
      routes_published_at: '2026-10-08T09:00:00.123455Z',
      current_serving_installation_matches: true,
    },
  ])('rejects malformed or overclaimed record %j', async (change) => {
    ;(body as { items: unknown[] }).items = [{ ...item, ...change }]
    await expect(getRuntimeInstallations({ instance_id: instance })).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
  })
  it.each([
    { scope: 'complete_configuration' },
    { scope: 'routing_only' },
    { scope: 'all_nodes' },
    { next_cursor: 'cursor\n' },
    { observed_at: '2026-10-08T09:02:00.1234567890Z' },
    { token: 'private' },
  ])('rejects malformed or stronger page %j', async (change) => {
    body = { ...(body as object), ...change }
    await expect(getRuntimeInstallations()).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
  })
  it('retains microseconds and distinguishes an exact same instant with omitted fractional zeroes', async () => {
    ;(body as { items: RuntimeInstallationRecord[] }).items[0] = {
      ...item,
      routes_published_at: '2026-10-08T09:01:00.000000Z',
      first_observed_at: '2026-10-08T09:01:00Z',
    }
    expect((await getRuntimeInstallations()).items[0].first_observed_at).toBe(
      '2026-10-08T09:01:00Z',
    )
  })
  it('accepts publication at the exact process birth without rounding microseconds', async () => {
    ;(body as { items: RuntimeInstallationRecord[] }).items[0] = {
      ...item,
      routes_published_at: item.instance_started_at,
      current_serving_installation_matches: true,
    }
    expect((await getRuntimeInstallations()).items[0].routes_published_at).toBe(
      item.instance_started_at,
    )
  })
  it('rejects duplicate and out-of-order retained records', async () => {
    const page = body as { items: (typeof item)[] }
    page.items = [item, item]
    await expect(getRuntimeInstallations()).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
    page.items = [{ ...item, id: 'rin_00000000000000000000000001' }, item]
    await expect(getRuntimeInstallations()).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
  })
  it('rejects oversized pages and nonadvancing cursors', async () => {
    ;(body as { items: unknown[] }).items = Array.from({ length: 21 }, () => item)
    await expect(getRuntimeInstallations()).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
    body = {
      scope: 'single_process_gateway_admission',
      observed_at: '2026-10-08T09:02:00Z',
      items: [item],
      next_cursor: 'same',
    }
    await expect(getRuntimeInstallations({ cursor: 'same' })).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
  })
  it.each([
    { instance_id: 'ins_online' },
    { instance_id: `${instance}\n` },
    { cursor: '' },
    { limit: 0 },
    { limit: 101 },
    { limit: 1.5 },
  ])('rejects invalid filters before HTTP %j', async (filter) => {
    await expect(getRuntimeInstallations(filter)).rejects.toThrow(
      'Invalid Gateway installation observations',
    )
    expect(count).toBe(0)
  })
})
