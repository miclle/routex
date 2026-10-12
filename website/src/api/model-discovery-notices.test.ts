import { afterEach, describe, expect, it, vi } from 'vitest'
import client from './client'
import { getNotifications, recordedModelDiscovery } from './notifications'
import { getAdminOverview, validModelDiscoveryAlert } from './overview'

const suffix = '01j00000000000000000000001'
const notice = {
  id: `ntf_${suffix}`,
  alert_id: `alr_${suffix}`,
  kind: 'models_first_recorded',
  severity: 'low',
  detail_code: 'first_recorded',
  first_recorded_model_count: 2,
  occurrence_count: 3,
  read: false,
  read_at: null,
  first_seen_at: '2026-10-12T01:00:00Z',
  last_seen_at: '2026-10-12T02:00:00Z',
  subject_type: 'provider',
  subject_id: `prv_${suffix}`,
  subject_name: 'Recorded supplier',
}
const alert = {
  kind: notice.kind,
  severity: notice.severity,
  detail_code: notice.detail_code,
  first_recorded_model_count: notice.first_recorded_model_count,
  occurrence_count: notice.occurrence_count,
  first_seen_at: notice.first_seen_at,
  last_seen_at: notice.last_seen_at,
  subject_type: notice.subject_type,
  subject_id: notice.subject_id,
  subject_name: notice.subject_name,
  id: `alr_${suffix}`,
  state: 'open',
  etag: `rev_${suffix}`,
  updated_at: notice.last_seen_at,
}
afterEach(() => vi.restoreAllMocks())

describe('first-recorded Model notification envelopes', () => {
  it('accepts canonical lowercase generated identities and rejects uppercase suffix aliases', () => {
    expect(recordedModelDiscovery(notice)).toBe(true)
    expect(validModelDiscoveryAlert(alert)).toBe(true)
    for (const change of [
      { id: `ntf_${suffix.toUpperCase()}` },
      { alert_id: `alr_${suffix.toUpperCase()}` },
      { subject_id: `prv_${suffix.toUpperCase()}` },
    ]) {
      expect(recordedModelDiscovery({ ...notice, ...change })).toBe(false)
    }
    for (const change of [
      { id: `alr_${suffix.toUpperCase()}` },
      { etag: `rev_${suffix.toUpperCase()}` },
      { subject_id: `prv_${suffix.toUpperCase()}` },
    ]) {
      expect(validModelDiscoveryAlert({ ...alert, ...change })).toBe(false)
    }
  })
  it('accepts recorded counts independently of grouped occurrences and does not invent email state', async () => {
    expect(recordedModelDiscovery(notice)).toBe(true)
    expect(validModelDiscoveryAlert(alert)).toBe(true)
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [notice], unread_count: 1 } })
    const page = await getNotifications('unread')
    expect(page.items[0].first_recorded_model_count).toBe(2)
    expect(page.items[0].occurrence_count).toBe(3)
    expect(page.items[0].delivery_status).toBeUndefined()
  })
  it.each([
    { first_recorded_model_count: 0 },
    { first_recorded_model_count: 2001 },
    { first_recorded_model_count: '2' },
    { first_recorded_model_count: 1.5 },
    { severity: 'medium' },
    { kind: 'MODELS_FIRST_RECORDED' },
    { detail_code: 'First_recorded' },
    { subject_type: 'model' },
    { subject_id: `PRV_${suffix}` },
    { subject_name: 'private\nname' },
    { occurrence_count: 0 },
    { delivery_status: 'accepted' },
    { quota: {} },
    { connection_id: 'private' },
    { credential_id: 'private' },
    { read_at: '2026-10-12T02:00:00Z' },
  ])('rejects borrowed or malformed inbox fields %#', async (change) => {
    const bad = { ...notice, ...change }
    expect(recordedModelDiscovery(bad)).toBe(false)
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { items: [bad], unread_count: 1 } })
    await expect(getNotifications('unread')).rejects.toThrow('Invalid notification response')
  })
  it.each([
    { first_recorded_model_count: null },
    { first_recorded_model_count: 2001 },
    { severity: 'high' },
    { state: 'unknown' },
    { private_reason: 'hidden' },
    { subject_type: 'credential' },
  ])('rejects invalid successful overview source %#', async (change) => {
    const bad = { ...alert, ...change }
    expect(validModelDiscoveryAlert(bad)).toBe(false)
    vi.spyOn(client, 'get').mockResolvedValueOnce({ data: { alerts: [bad] } })
    await expect(getAdminOverview()).rejects.toThrow('Overview request failed')
  })
  it.each(['\u0000', '\u001f', '\u007f', '\u009f'])(
    'rejects control characters in recorded Provider subjects %j',
    (control) => {
      const subject_name = `Recorded${control}supplier`
      expect(recordedModelDiscovery({ ...notice, subject_name })).toBe(false)
      expect(validModelDiscoveryAlert({ ...alert, subject_name })).toBe(false)
    },
  )
  it('accepts a recorded read timestamp and exact inclusive count bounds', () => {
    expect(
      recordedModelDiscovery({
        ...notice,
        read: true,
        read_at: notice.last_seen_at,
        first_recorded_model_count: 1,
      }),
    ).toBe(true)
    expect(recordedModelDiscovery({ ...notice, first_recorded_model_count: 2000 })).toBe(true)
  })
})
