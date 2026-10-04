import type { MemberTeamsPage, MemberTeamRecord, MemberTeamPolicy } from '@/types/member-teams'
export const memberTeamsActor = 'usr_01aaaaaaaaaaaaaaaaaaaaaaaa'
export const memberTeamsTarget = 'usr_01bbbbbbbbbbbbbbbbbbbbbbbb'
export const memberTeamID = (tail = 'c') => `tea_01${tail.repeat(24)}`
export const emptyMemberTeamPolicy = (): MemberTeamPolicy => ({
  tokens_month: null,
  money_month: null,
  currency: null,
  rpm: null,
  tpm: null,
  concurrency: null,
})
export function memberTeamRow(tail = 'c'): MemberTeamRecord {
  return {
    id: memberTeamID(tail),
    name: `Recorded Team ${tail}`,
    status: 'active',
    membership_id: `tmm_01${tail.repeat(24)}`,
    membership_role: 'member',
    membership_status: 'active',
    joined_at: null,
    limits: {
      policy_recorded: true,
      policy_etag: 'revision1',
      stored: {
        tokens_month: '0',
        money_month: '0.000000000000000001',
        currency: 'USD',
        rpm: '0',
        tpm: null,
        concurrency: '2',
      },
      parent_stored: { ...emptyMemberTeamPolicy(), tokens_month: '500' },
      runtime_applied: false,
      usage_status: 'active',
      usage: {
        as_of: '2026-10-05T00:00:00Z',
        time_zone: 'UTC',
        month_start: '2026-10-01T00:00:00Z',
        month_end: '2026-11-01T00:00:00Z',
        covered: false,
        tokens_used: '9007199254740993',
        tokens_held: '0',
        tokens_unknown: '1',
        money_used: { USD: '5.000000000000000002', EUR: '0' },
        money_held: { USD: '0.000000000000000003' },
        money_unknown: '2',
      },
      active_reservations: { tokens_held: '5', money_held: { USD: '1.000000000000000001' } },
    },
  }
}
export const memberTeamsPage = (rows = [memberTeamRow()]): MemberTeamsPage => ({
  user_id: memberTeamsTarget,
  observed_at: '2026-10-05T01:00:00Z',
  platform_currency: 'USD',
  items: rows,
  next_cursor: null,
})
