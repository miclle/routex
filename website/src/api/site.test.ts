import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getAnnouncements, getSite } from './site'
afterEach(() => vi.restoreAllMocks())
it('rejects malformed announcement envelopes so the shell shows a recoverable read error', async () => {
  for (const data of [
    null,
    {},
    { items: null },
    { items: 'invalid' },
    { items: [null] },
    { items: [{ id: 'id', content: {}, status: 'active' }] },
  ]) {
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    await expect(getAnnouncements(false)).rejects.toThrow('Invalid announcement response')
  }
})
it('rejects invalid public branding instead of applying an unsupported language', async () => {
  for (const data of [null, {}, { name: 'Site', default_language: 'unsupported' }]) {
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    await expect(getSite()).rejects.toThrow('Invalid site settings response')
  }
})
