import { AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import { getRuntimeArchives } from './runtime-archives'
import type { RuntimeArchiveMetadataRecord } from '@/types/runtime-archives'
const adapter = client.defaults.adapter
const row: RuntimeArchiveMetadataRecord = {
  id: 'rcv_00000000000000000000000002',
  created_at: '2026-10-14T09:01:00.123456Z',
  codec_version: 1,
  content_digest: 'a'.repeat(64),
  document_bytes: 32000,
}
let body: unknown, count: number
beforeEach(() => {
  count = 0
  body = {
    scope: 'archive_metadata_only',
    observed_at: '2026-10-14T09:02:00.123456789Z',
    items: [structuredClone(row)],
    next_cursor: null,
  }
  client.defaults.adapter = async (config) => {
    count++
    expect(config.url).toBe('/admin/runtime/archives')
    expect(config.method).toBe('get')
    expect(config.headers.has('X-CSRF-Token')).toBe(false)
    return { data: body, status: 200, statusText: 'OK', headers: new AxiosHeaders(), config }
  }
})
afterEach(() => {
  client.defaults.adapter = adapter
})
describe('configuration archive header read boundary', () => {
  it('retains recorded header facts without source or writes', async () => {
    expect((await getRuntimeArchives()).items[0]).toEqual(row)
    expect(count).toBe(1)
  })
  it('preserves an empty archive', async () => {
    ;(body as { items: unknown[] }).items = []
    expect((await getRuntimeArchives()).items).toEqual([])
  })
  it.each([
    { id: 'rcv_80000000000000000000000002' },
    { id: row.id.toUpperCase() },
    { id: row.id + '\n' },
    { created_at: '2026-02-30T09:01:00Z' },
    { created_at: '2026-10-14T09:01:00.1234567Z' },
    { created_at: '2026-10-14T17:01:00+08:00' },
    { codec_version: 2 },
    { content_digest: 'A'.repeat(64) },
    { content_digest: 'a'.repeat(64) + '\n' },
    { document_bytes: 0 },
    { document_bytes: 16777217 },
    { document_bytes: 1.1 },
    { auth_ciphertext: 'private' },
    { snapshot_id: 'cfg_fake' },
    { complete: true },
  ])('rejects malformed or stronger row %j', async (change) => {
    ;(body as { items: unknown[] }).items = [{ ...row, ...change }]
    await expect(getRuntimeArchives()).rejects.toThrow()
  })
  it.each([
    { scope: 'complete_configuration' },
    { next_cursor: 'cursor\n' },
    { observed_at: '2026-10-14T09:02:00.1234567890Z' },
    { canonical: 'private' },
  ])('rejects malformed page %j', async (change) => {
    body = { ...(body as object), ...change }
    await expect(getRuntimeArchives()).rejects.toThrow()
  })
  it('rejects duplicate, unordered, oversized and nonadvancing pages', async () => {
    const page = body as { items: RuntimeArchiveMetadataRecord[]; next_cursor: string | null }
    page.items = [row, row]
    await expect(getRuntimeArchives()).rejects.toThrow()
    page.items = [{ ...row, id: 'rcv_00000000000000000000000001' }, row]
    await expect(getRuntimeArchives()).rejects.toThrow()
    page.items = Array.from({ length: 21 }, () => row)
    await expect(getRuntimeArchives()).rejects.toThrow()
    page.items = [row]
    page.next_cursor = 'same'
    await expect(getRuntimeArchives({ cursor: 'same' })).rejects.toThrow()
    page.items = []
    await expect(getRuntimeArchives()).rejects.toThrow()
  })
  it.each([
    { cursor: '' },
    { cursor: 'bad\n' },
    { limit: 0 },
    { limit: 101 },
    { limit: 1.5 },
    { snapshot_id: 'cfg_other' },
  ])('rejects invalid filter before HTTP %j', async (filter) => {
    await expect(getRuntimeArchives(filter)).rejects.toThrow()
    expect(count).toBe(0)
  })
})
