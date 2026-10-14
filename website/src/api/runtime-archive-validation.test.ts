import { AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import client from './client'
import { validateRuntimeArchive } from './runtime-archives'

const adapter = client.defaults.adapter
const id = 'rcv_00000000000000000000000001'
const seed = {
  scope: 'archive_integrity_only',
  archive_id: id,
  archive_created_at: '2026-10-14T09:01:00.123456Z',
  codec_version: 1,
  observed_at: '2026-10-14T09:02:00.123456789Z',
  integrity: 'authenticated',
}
let body: unknown, status: number, calls: number, expectedSignal: AbortSignal | undefined
beforeEach(() => {
  body = structuredClone(seed)
  status = 200
  calls = 0
  expectedSignal = undefined
  client.defaults.adapter = async (config) => {
    calls++
    expect(config.url).toBe(`/admin/runtime/archives/${id}/validation`)
    expect(config.method).toBe('get')
    expect(config.params).toBeUndefined()
    expect(config.data).toBeUndefined()
    expect(config.headers.has('X-CSRF-Token')).toBe(false)
    expect(config.headers.has('If-Match')).toBe(false)
    expect(config.signal).toBe(expectedSignal)
    return { data: body, status, statusText: 'OK', config, headers: new AxiosHeaders() }
  }
})
afterEach(() => {
  client.defaults.adapter = adapter
})
describe('authenticated archive integrity closed wire boundary', () => {
  it('accepts only the exact read-only six facts and forwards cancellation', async () => {
    expectedSignal = new AbortController().signal
    expect(await validateRuntimeArchive(id, expectedSignal)).toEqual(seed)
    expect(calls).toBe(1)
  })
  it.each([
    { scope: 'runtime_ready' },
    { archive_id: 'rcv_00000000000000000000000002' },
    { archive_created_at: '2026-02-30T09:01:00Z' },
    { archive_created_at: '2026-10-14T09:01:00.1234567Z' },
    { observed_at: '2026-10-14T17:02:00+08:00' },
    { observed_at: '2026-10-14T09:02:00.1234567890Z' },
    { codec_version: 2 },
    { codec_version: '1' },
    { integrity: ['authenticated'] },
    { integrity: null },
    { content_digest: 'a'.repeat(64) },
    { plaintext: 'private' },
    { ready: true },
    { runtime_applied: true },
  ])('rejects malformed or stronger facts %j', async (change) => {
    body = { ...seed, ...change }
    await expect(validateRuntimeArchive(id)).rejects.toThrow()
  })
  it('rejects missing facts, array bodies and non-200 responses', async () => {
    const missing: Record<string, unknown> = { ...seed }
    delete missing.integrity
    body = missing
    await expect(validateRuntimeArchive(id)).rejects.toThrow()
    body = [seed]
    await expect(validateRuntimeArchive(id)).rejects.toThrow()
    body = seed
    status = 202
    await expect(validateRuntimeArchive(id)).rejects.toThrow()
  })
  it.each(['', id.toUpperCase(), `${id}\n`, '../other', 'rcv_80000000000000000000000001'])(
    'rejects noncanonical identity before HTTP %j',
    async (value) => {
      await expect(validateRuntimeArchive(value)).rejects.toThrow()
      expect(calls).toBe(0)
    },
  )
})
