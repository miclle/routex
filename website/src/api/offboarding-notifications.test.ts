import { afterEach, expect, it, vi } from 'vitest'
import client from './client'
import { getNotifications, recordedOffboardingCompletion } from './notifications'

const notice = {
  id: 'ntf_01j00000000000000000000001',
  kind: 'offboarding_completed',
  severity: 'medium',
  detail_code: 'completed',
  occurrence_count: 1,
  read: false,
  read_at: null,
  first_seen_at: '2026-10-12T01:02:03.123456Z',
  last_seen_at: '2026-10-12T01:02:03.123456Z',
}
afterEach(() => vi.restoreAllMocks())
it('accepts only the privacy-bounded completion envelope and exact recorded email state', async () => {
  const item = {
    ...notice,
    delivery_status: 'unknown',
    delivery_code: 'acceptance_failed',
    delivery_attempts: 1,
    delivery_updated_at: notice.first_seen_at,
  }
  vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [item], unread_count: 1 } })
  expect((await getNotifications('all', null, undefined, 'usr_successor')).items).toEqual([item])
  expect(
    recordedOffboardingCompletion({ ...notice, read: true, read_at: notice.first_seen_at }),
  ).toBe(true)
})
it.each([
  { case_id: 'off_private' },
  { member_user_id: 'usr_private' },
  { subject_name: 'Private' },
  { alert_id: 'alr_private' },
  { proof: 'private' },
  { quota: {} },
  { severity: 'high' },
  { detail_code: 'Completed' },
  { occurrence_count: 2 },
  { read: true },
  { read_at: notice.first_seen_at },
  { first_seen_at: 'bad' },
  { last_seen_at: '2026-10-12T01:02:04Z' },
  { delivery_status: 'accepted', delivery_updated_at: 'bad' },
  { delivery_status: 'future', delivery_updated_at: notice.first_seen_at },
  {
    delivery_status: 'failed',
    delivery_code: 'raw server message',
    delivery_updated_at: notice.first_seen_at,
  },
  { delivery_status: 'pending', delivery_attempts: 4, delivery_updated_at: notice.first_seen_at },
  { id: 'ntf_untrusted' },
  { id: `ntf_${'A'.repeat(26)}` },
])('rejects malformed or private completion metadata %#', async (change) => {
  vi.spyOn(client, 'get').mockResolvedValueOnce({
    data: { items: [{ ...notice, ...change }], unread_count: 1 },
  })
  await expect(getNotifications('all')).rejects.toThrow('Invalid notification response')
})
