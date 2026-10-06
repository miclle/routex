import { afterEach, describe, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  getProviderMetadata,
  saveProviderMetadata,
  trimProviderMetadata,
  validProviderName,
  validProviderReason,
} from './provider-metadata'
const etag = `${'a'.repeat(64)}.${'b'.repeat(64)}`
const record = { id: 'prv_one', name: 'Supplier', etag, can_edit: true }
const response = (data: unknown = record) => ({
  data,
  status: 200,
  headers: new AxiosHeaders({ 'Cache-Control': 'private, no-store', ETag: `"${etag}"` }),
})
afterEach(() => vi.restoreAllMocks())
describe('Provider current metadata wire', () => {
  it('reads only exact target and strong private no-store proof with cancellation', async () => {
    const get = vi.spyOn(client, 'get').mockResolvedValue(response())
    const signal = new AbortController().signal
    expect(await getProviderMetadata('prv_one', signal)).toEqual(record)
    expect(get).toHaveBeenCalledExactlyOnceWith('/admin/providers/prv_one/metadata', { signal })
  })
  it.each(['prv_', 'prv_one ', 'PRV_one', 'prv_' + 'x'.repeat(27), 'prv_一'])(
    'rejects malformed target %s before dispatch',
    async (id) => {
      const get = vi.spyOn(client, 'get')
      await expect(getProviderMetadata(id)).rejects.toThrow()
      expect(get).not.toHaveBeenCalled()
    },
  )
  it.each([
    { ...record, secret: 'never' },
    { ...record, can_edit: null },
    { ...record, name: '' },
    { ...record, id: 'prv_other' },
    { ...record, etag: 'a'.repeat(64) },
    { ...record, name: 'bad\nname' },
    { ...record, name: '\ud800' },
  ])('rejects malformed or mismatched required GET record', async (data) => {
    vi.spyOn(client, 'get').mockResolvedValue(response(data))
    await expect(getProviderMetadata('prv_one')).rejects.toThrow()
  })
  it.each(['W/"' + etag + '"', '"other"', etag, '*'])(
    'rejects nonmatching strong proof %s',
    async (header) => {
      const value = response()
      value.headers.set('ETag', header)
      vi.spyOn(client, 'get').mockResolvedValue(value)
      await expect(getProviderMetadata('prv_one')).rejects.toThrow()
    },
  )
  it.each(['no-store', 'private', 'public, no-store', 'private, no-store, max-age=0'])(
    'rejects wrong cache proof %s',
    async (header) => {
      const value = response()
      value.headers.set('Cache-Control', header)
      vi.spyOn(client, 'get').mockResolvedValue(value)
      await expect(getProviderMetadata('prv_one')).rejects.toThrow()
    },
  )
  it('submits exact two-field name/reason and accepts authorized current no-op with preserved identity', async () => {
    const data = {
      provider: { ...record, name: '\ufeffExact' },
      runtime_applied: true,
      changed: false,
    }
    const put = vi.spyOn(client, 'put').mockResolvedValue(response(data))
    const signal = new AbortController().signal
    expect(
      (
        await saveProviderMetadata(
          'prv_one',
          etag,
          { name: '\ufeffExact', reason: 'Reason' },
          'fresh-csrf',
          signal,
        )
      ).changed,
    ).toBe(false)
    expect(put).toHaveBeenCalledExactlyOnceWith(
      '/admin/providers/prv_one/metadata',
      { name: '\ufeffExact', reason: 'Reason' },
      { headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': 'fresh-csrf' }, signal },
    )
  })
  it.each([
    { provider: { ...record, name: 'New' }, runtime_applied: false, changed: true },
    { provider: { ...record, name: 'Other' }, runtime_applied: true, changed: true },
    { provider: { ...record, name: 'New', can_edit: false }, runtime_applied: true, changed: true },
    {
      provider: { ...record, name: 'New', etag: `${'d'.repeat(64)}.${'b'.repeat(64)}` },
      runtime_applied: true,
      changed: true,
    },
    { provider: { ...record, name: 'New' }, runtime_applied: true, changed: 'true' },
    {
      provider: { ...record, name: 'New' },
      runtime_applied: true,
      changed: true,
      receipt: 'not allowed',
    },
  ])('rejects any incomplete/mismatched current confirmation', async (data) => {
    vi.spyOn(client, 'put').mockResolvedValue(response(data))
    await expect(
      saveProviderMetadata('prv_one', etag, { name: 'New', reason: 'Reason' }, 'csrf'),
    ).rejects.toThrow()
  })
  it('matches Go whitespace and exact Unicode code-point/UTF8 boundaries without stripping FEFF', () => {
    expect(trimProviderMetadata('\u2003Name\u00a0')).toBe('Name')
    expect(trimProviderMetadata('\ufeffName\ufeff')).toBe('\ufeffName\ufeff')
    expect(validProviderName('😀'.repeat(100))).toBe(true)
    expect(validProviderName('😀'.repeat(101))).toBe(false)
    expect(validProviderReason('😀'.repeat(256))).toBe(true)
    expect(validProviderReason('😀'.repeat(257))).toBe(false)
    for (const bad of ['', ' leading', 'trailing ', 'line\nfeed', '\ud800', 'tab\t']) {
      expect(validProviderName(bad)).toBe(false)
      expect(validProviderReason(bad)).toBe(false)
    }
  })
  it('rejects forged extra input fields and invalid reason without dispatch', async () => {
    const put = vi.spyOn(client, 'put')
    await expect(
      saveProviderMetadata(
        'prv_one',
        etag,
        { name: 'New', reason: 'Reason', protocol: 'forged' } as { name: string; reason: string },
        'csrf',
      ),
    ).rejects.toThrow()
    await expect(
      saveProviderMetadata('prv_one', etag, { name: 'New', reason: 'line\nfeed' }, 'csrf'),
    ).rejects.toThrow()
    expect(put).not.toHaveBeenCalled()
  })
})
