import type { LimitRecord } from '@/types/resource-limits'

/** Read-only policy response for page integration tests. */
export function limitFixture(): LimitRecord {
  const policy = { rpm: null, concurrency: null, ip_mode: 'none' as const, ip_ranges: [] }
  return {
    kind: 'user',
    id: 'usr_fixture',
    account_id: 'user_usr_fixture',
    etag: 'fixture',
    stored: policy,
    effective: { rpm: null, concurrency: null },
    ip_policies: [policy],
    rpm_used: 0,
    active: 0,
    enforced: true,
  }
}
