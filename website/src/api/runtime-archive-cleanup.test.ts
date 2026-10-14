import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import {
  cleanupRuntimeArchives,
  previewRuntimeArchiveCleanup,
  validRuntimeArchiveCleanupReason,
} from './runtime-archive-cleanup'

const adapter = client.defaults.adapter
const ids = ['rcv_00000000000000000000000001']
const etag = 'a'.repeat(64)
const input = {
  archive_ids: ids,
  request_id: '605670ca-5a6a-4f98-8021-000000000001',
  reason: 'Reviewed storage cleanup',
}
let body: Record<string, unknown>, tag: string, status: number
beforeEach(() => {
  body = {
    scope: 'archive_storage_only',
    archive_ids: ids,
    retained_count: 2,
    encrypted_bytes: 200,
    selected_count: 1,
    selected_encrypted_bytes: 100,
    version_cap: 100,
    encrypted_byte_cap: 268435456,
  }
  tag = `"${etag}"`
  status = 200
  client.defaults.adapter = async (config) => {
    expect(config.method).toBe('post')
    expect(config.headers.get('X-CSRF-Token')).toBe('csrf')
    if (config.url?.endsWith('/preview')) expect(config.headers.has('If-Match')).toBe(false)
    else expect(config.headers.get('If-Match')).toBe(`"${etag}"`)
    return {
      config,
      data: body,
      status,
      statusText: 'OK',
      headers: new AxiosHeaders({ etag: tag }),
    }
  }
})
afterEach(() => {
  client.defaults.adapter = adapter
})
describe('archive cleanup captured review and receipt boundary', () => {
  it('uses the exact server preview and strong review token', async () => {
    expect(await previewRuntimeArchiveCleanup(ids, 'csrf')).toEqual({ ...body, etag })
  })
  it.each([
    { scope: 'runtime_applied' },
    { archive_ids: [] },
    { selected_count: 2 },
    { selected_encrypted_bytes: 201 },
    { selected_encrypted_bytes: 0 },
    { version_cap: 101 },
    { encrypted_byte_cap: 256 },
    { source: 'private' },
  ])('rejects malformed or stronger preview %j', async (change) => {
    body = { ...body, ...change }
    await expect(previewRuntimeArchiveCleanup(ids, 'csrf')).rejects.toThrow()
  })
  it.each([etag, `W/"${etag}"`, `"${etag.toUpperCase()}"`])(
    'rejects unreviewable ETag %s',
    async (invalid) => {
      tag = invalid
      await expect(previewRuntimeArchiveCleanup(ids, 'csrf')).rejects.toThrow()
    },
  )
  it('accepts only exact confirmed cleanup identity and original selection', async () => {
    body = {
      scope: 'archive_storage_only',
      request_id: input.request_id,
      archive_ids: ids,
      deleted_count: 1,
      deleted_encrypted_bytes: 100,
      completed_at: '2026-10-14T09:01:00.123456Z',
    }
    expect(await cleanupRuntimeArchives(input, etag, 'csrf')).toEqual(body)
    for (const change of [
      { request_id: 'other' },
      { archive_ids: [] },
      { deleted_count: 2 },
      { deleted_encrypted_bytes: 0 },
      { completed_at: '2026-02-30T09:01:00Z' },
      { runtime_applied: true },
    ]) {
      const valid = body
      body = { ...valid, ...change }
      await expect(cleanupRuntimeArchives(input, etag, 'csrf')).rejects.toThrow()
      body = valid
    }
  })
  it('never exposes private Axios request or response bodies', async () => {
    client.defaults.adapter = async (config) => {
      throw new AxiosError('private-source', '', config, undefined, {
        config,
        status: 503,
        statusText: 'private',
        headers: new AxiosHeaders(),
        data: 'private-content',
      })
    }
    const failure = await previewRuntimeArchiveCleanup(ids, 'csrf').catch((error) => error)
    expect(failure.status).toBe(503)
    expect(JSON.stringify(failure)).not.toContain('private')
    expect(failure.message).toBe('Configuration archive cleanup failed')
  })
  it('accepts the server character boundary including complete Unicode scalars', () => {
    expect(validRuntimeArchiveCleanupReason('a'.repeat(1000))).toBe(true)
    expect(validRuntimeArchiveCleanupReason('😀'.repeat(1000))).toBe(true)
    expect(validRuntimeArchiveCleanupReason('Reviewed 中文 reason')).toBe(true)
  })
  it.each([
    '',
    ' ',
    ' leading',
    'trailing ',
    'a'.repeat(1001),
    '😀'.repeat(1001),
    'unsafe\u0001reason',
    'line\nreason',
    '\uD800',
    '\uDC00',
  ])('rejects an invalid reason before HTTP dispatch %#', async (reason) => {
    let dispatches = 0
    client.defaults.adapter = async () => {
      dispatches++
      throw new Error('Unexpected dispatch')
    }
    expect(validRuntimeArchiveCleanupReason(reason)).toBe(false)
    await expect(cleanupRuntimeArchives({ ...input, reason }, etag, 'csrf')).rejects.toThrow()
    expect(dispatches).toBe(0)
  })
  it('rejects missing, duplicate and aliased selections before dispatch', async () => {
    for (const invalid of [[], [...ids, ...ids], [ids[0].toUpperCase()], [`${ids[0]}\n`]])
      await expect(previewRuntimeArchiveCleanup(invalid, 'csrf')).rejects.toThrow()
  })
})
