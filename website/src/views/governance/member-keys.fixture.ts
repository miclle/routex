import type { MemberKeyRecord } from '@/types/member-keys'
export const memberKeysUser = 'usr_01aaaaaaaaaaaaaaaaaaaaaaaa'
export const memberKeysActor = 'usr_01bbbbbbbbbbbbbbbbbbbbbbbb'
export const memberKeysID = 'key_01cccccccccccccccccccccccc'
export function memberKeyFixture(): MemberKeyRecord {
  const policy = {
    tokens_5h: null,
    tokens_7d: null,
    tokens_month: 0,
    tpm: null,
    rpm: 0,
    concurrency: null,
    money_month: '0.000000000000000001',
    currency: 'USD',
  }
  const month = {
    covered: false,
    tokens_used: '9007199254740993',
    tokens_held: '2',
    tokens_unknown: '1',
    money_unknown: '1',
    money_used: { USD: '1.000000000000000001', EUR: '0' },
    money_held: { USD: '0.000000000000000001' },
  }
  return {
    id: memberKeysID,
    name: 'Application',
    status: 'active',
    expired: false,
    model_ids: ['mdl_recorded'],
    expires_at: null,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-02T00:00:00Z',
    etag: 'a'.repeat(64),
    disable_eligible: true,
    last_used_at: null,
    last_use_coverage: 'unknown',
    limits: {
      platform_currency: 'USD',
      quota_root_id: memberKeysID,
      shared_rotation_quota: true,
      stored: { ...policy },
      effective: { ...policy },
      rpm_used: '9007199254740993',
      active: null,
      enforced: false,
      quota_usage: {
        activated: true,
        as_of: '2026-10-04T00:00:00Z',
        coverage_start: '2026-10-02T00:00:00Z',
        time_zone: 'UTC',
        month,
        active: {
          ...month,
          tokens_used: '0',
          tokens_held: '5',
          money_used: {},
          money_held: { USD: '5.000000000000000002' },
        },
        minute: null,
        five_hours: null,
        seven_days: null,
      },
    },
  }
}
