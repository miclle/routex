import type { Member } from '@/types/governance'
import type { MemberListItem, MemberListPage } from '@/types/member-list'
export function memberListRow(base: Partial<Member> = {}): MemberListItem {
  const id = base.id ?? 'usr_target'
  return {
    id,
    name: 'Target',
    email: 'target@example.invalid',
    role: 'member',
    disabled: false,
    offboarded_at: null,
    created_at: '2026-09-23T00:00:00Z',
    role_ids: [],
    ...base,
    updated_at: '2026-10-04T00:00:00Z',
    last_login_at: null,
    last_login_status: 'historical_unavailable',
    total_personal_keys: '9007199254740993',
    personal_policy_stored: true,
    personal: {
      account_id: `user_${id}`,
      policy_etag: 'reviewed',
      tokens_month: '9007199254740995',
      money_month: '99999999999999999.000000000000000001',
      currency: 'USD',
      runtime_applied: false,
      usage_status: 'active',
      usage: {
        as_of: '2026-10-05T01:00:00Z',
        time_zone: 'Asia/Shanghai',
        month_start: '2026-09-30T16:00:00Z',
        month_end: '2026-10-31T16:00:00Z',
        covered: false,
        tokens_used: '9007199254740993',
        tokens_held: '3',
        tokens_unknown: '1',
        money_used: { USD: '1.000000000000000001', EUR: '2.000000000000000003' },
        money_held: { USD: '0.000000000000000001' },
        money_unknown: '2',
      },
      active_reservations: { tokens_held: '5', money_held: { USD: '0.000000000000000003' } },
    },
    teams: {
      status: 'available',
      items: [
        {
          id: 'tea_retained',
          name: 'Retained Team',
          status: 'archived',
          membership_status: 'disabled',
          membership_role: 'owner',
        },
      ],
    },
  }
}
export function memberListPage(actor = 'usr_admin', items = [memberListRow()]): MemberListPage {
  return {
    actor_user_id: actor,
    observed_at: '2026-10-05T01:00:00Z',
    platform_currency: 'USD',
    items,
    next_cursor: null,
  }
}
