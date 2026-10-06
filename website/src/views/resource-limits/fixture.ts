import type { LimitRecord } from '@/types/resource-limits'

/** Read-only policy response for page integration tests. */
export function limitFixture(): LimitRecord {
  const policy = { rpm: null, concurrency: null, ip_mode: 'none' as const, ip_ranges: [] }
  return {
    kind: 'user',
    id: 'usr_fixture',
    account_id: 'user_usr_fixture',
    etag: 'fixture',
    platform_currency: 'USD',
    quota_usage: null,
    stored: policy,
    effective: { rpm: null, concurrency: null },
    ip_policies: [policy],
    rpm_used: 0,
    active: 0,
    enforced: true,
  }
}

export function teamFixture(member = false): LimitRecord {
  const policy = {
    tokens_5h: 1000,
    tokens_7d: 5000,
    tokens_month: 10000,
    money_month: '999999999999999999.123456789012345678',
    currency: 'USD',
    rpm: 60,
    tpm: 500,
    concurrency: 4,
    ip_mode: 'none' as const,
    ip_ranges: [],
  }
  const stored = member
    ? {
        ...policy,
        tokens_5h: null,
        tokens_7d: null,
        tokens_month: null,
        money_month: null,
        currency: '',
        rpm: null,
        tpm: null,
        concurrency: null,
      }
    : { ...policy, tokens_month_behavior: 'stop' as const, money_month_behavior: 'stop' as const }
  return {
    kind: member ? 'team_member' : 'team',
    id: member ? 'usr_member' : 'tea_test',
    team_id: 'tea_test',
    account_id: member ? 'team_member_stable' : 'team_stable',
    etag: 'a'.repeat(64),
    ...(member ? { parent_etag: 'b'.repeat(64) } : {}),
    editable_fields: member
      ? ['tokens_month', 'money_month', 'rpm', 'tpm', 'concurrency']
      : [
          'tokens_5h',
          'tokens_7d',
          'tokens_month',
          'money_month',
          'rpm',
          'tpm',
          'concurrency',
          'tokens_month_behavior',
          'money_month_behavior',
        ],
    platform_currency: 'USD',
    stored,
    effective: policy,
    ip_policies: member
      ? [{ ...policy, tokens_month_behavior: 'stop', money_month_behavior: 'stop' }, stored]
      : [stored],
    quota_usage: null,
    rpm_used: null,
    active: null,
    enforced: true,
  }
}
