import { AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import { getRoutingApplications } from './runtime-applications'
import type { RoutingApplicationRecord } from '@/types/runtime-applications'

const originalAdapter = client.defaults.adapter
const instance = 'ins_00000000000000000000000001'
const item: RoutingApplicationRecord = {
  id: 'rap_00000000000000000000000002',
  instance_id: instance,
  instance_started_at: '2026-10-08T09:00:00.123456Z',
  snapshot_id: 'cfg_00000000000000000000000001',
  published_at: '2026-10-08T09:01:00Z',
  applied_at: '2026-10-08T09:01:00.123456Z',
  instance_status: 'online',
  current_serving_snapshot_matches: null,
}
let body: unknown
let count: number
beforeEach(() => {
  body = {
    scope: 'routing_only',
    observed_at: '2026-10-08T09:02:00.123456789Z',
    items: [structuredClone(item)],
    next_cursor: null,
  }
  count = 0
  client.defaults.adapter = async (config) => {
    count++
    expect(config.url).toBe('/admin/runtime/applications')
    expect(config.method).toBe('get')
    expect(config.headers.has('X-CSRF-Token')).toBe(false)
    return { data: body, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config }
  }
})
afterEach(() => {
  client.defaults.adapter = originalAdapter
})

describe('routing application read boundary', () => {
  it('retains explicit unknown and exact historical timestamps without write authority', async () => {
    const page = await getRoutingApplications({ instance_id: instance })
    expect(page.items[0]).toEqual(item)
    expect(page.items[0].current_serving_snapshot_matches).toBeNull()
    expect(count).toBe(1)
  })
  it.each([true, false])('retains independently reported current match %s', async (match) => {
    ;(body as { items: RoutingApplicationRecord[] }).items[0] = {
      ...item,
      current_serving_snapshot_matches: match,
    }
    expect((await getRoutingApplications()).items[0].current_serving_snapshot_matches).toBe(match)
  })
  it.each([
    { id: 'rap_80000000000000000000000002' },
    { instance_id: instance.toUpperCase() },
    { snapshot_id: `${item.snapshot_id}\n` },
    { instance_id: 'ins_00000000000000000000000002' },
    { applied_at: '2026-10-08T09:01:00.1234567Z' },
    { applied_at: '2026-10-08T17:01:00+08:00' },
    { applied_at: '2026-02-30T09:01:00Z' },
    { instance_status: ['online'] },
    { current_serving_snapshot_matches: undefined },
    { instance_status: 'offline', current_serving_snapshot_matches: true },
    { route_digest: 'private' },
  ])('rejects malformed or overclaimed record %j', async (change) => {
    ;(body as { items: unknown[] }).items = [{ ...item, ...change }]
    await expect(getRoutingApplications({ instance_id: instance })).rejects.toThrow()
  })
  it.each([
    { scope: 'complete_configuration' },
    { next_cursor: 'cursor\n' },
    { observed_at: '2026-10-08T09:02:00.1234567890Z' },
    { token: 'private' },
  ])('rejects malformed or stronger page %j', async (change) => {
    body = { ...(body as object), ...change }
    await expect(getRoutingApplications()).rejects.toThrow()
  })
  it('rejects duplicate and out-of-order retained records', async () => {
    const page = body as { items: (typeof item)[] }
    page.items = [item, item]
    await expect(getRoutingApplications()).rejects.toThrow()
    page.items = [{ ...item, id: 'rap_00000000000000000000000001' }, item]
    await expect(getRoutingApplications()).rejects.toThrow()
  })
  it('rejects oversized pages and nonadvancing cursors', async () => {
    ;(body as { items: unknown[] }).items = Array.from({ length: 21 }, () => item)
    await expect(getRoutingApplications()).rejects.toThrow()
    body = {
      scope: 'routing_only',
      observed_at: '2026-10-08T09:02:00Z',
      items: [item],
      next_cursor: 'same',
    }
    await expect(getRoutingApplications({ cursor: 'same' })).rejects.toThrow()
  })
  it.each([
    { instance_id: 'ins_online' },
    { instance_id: `${instance}\n` },
    { cursor: '' },
    { limit: 0 },
    { limit: 101 },
    { limit: 1.5 },
  ])('rejects invalid filters before HTTP %j', async (filter) => {
    await expect(getRoutingApplications(filter)).rejects.toThrow()
    expect(count).toBe(0)
  })
})
