import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import { getNotifications } from './notifications'

const item = {
  id: 'qni_1',
  kind: 'monthly_quota_exhausted',
  detail_code: 'tokens_month_exhausted',
  severity: 'high',
  occurrence_count: 1,
  read: false,
  read_at: null,
  first_seen_at: '2026-10-01T00:00:00Z',
  last_seen_at: '2026-10-01T00:00:00Z',
}
afterEach(() => vi.restoreAllMocks())

describe('Notification response boundary', () => {
  it.each([
    null,
    {},
    { items: null, unread_count: 0 },
    { items: {}, unread_count: 0 },
    { items: [null], unread_count: 1 },
    { items: [undefined], unread_count: 1 },
    { items: [item, {}], unread_count: 2 },
    { items: [{ ...item, subject_name: {} }], unread_count: 1 },
    { items: [], unread_count: '1' },
    { items: [], unread_count: -1 },
    { items: [], unread_count: 1.5 },
    { items: [], unread_count: Number.MAX_SAFE_INTEGER + 1 },
    { items: [], unread_count: 0, next_cursor: {} },
  ])('rejects a malformed successful response %#', async (data) => {
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    await expect(getNotifications('unread')).rejects.toThrow('Invalid notification response')
  })

  it('preserves exact snapshot strings and unknown notification kinds while normalizing omitted cursors', async () => {
    const snapshot = { limit: '9007199254740993', settled: '9007199254740994' }
    const data = {
      items: [{ ...item, kind: 'future_kind', quota: snapshot }],
      unread_count: 1,
    }
    const get = vi.spyOn(client, 'get').mockResolvedValueOnce({ data })
    const controller = new AbortController()
    expect(await getNotifications('all', 'scoped-cursor', controller.signal)).toEqual({
      ...data,
      next_cursor: null,
    })
    expect(get).toHaveBeenCalledWith('/notifications', {
      params: { status: 'all', cursor: 'scoped-cursor' },
      signal: controller.signal,
    })
  })

  it('accepts the real empty inbox response without inventing an unread count', async () => {
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [], unread_count: 0 } })
    expect(await getNotifications('unread')).toEqual({
      items: [],
      unread_count: 0,
      next_cursor: null,
    })
  })
})
