import type { OverviewAccountUsage, OverviewActiveReservations } from './overview'

export type MemberTeamPolicy = {
  tokens_month: string | null
  money_month: string | null
  currency: string | null
  rpm: string | null
  tpm: string | null
  concurrency: string | null
}
export type MemberTeamRecord = {
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
  membership_id: string
  membership_role: 'owner' | 'member'
  membership_status: 'active' | 'disabled'
  joined_at: string | null
  limits: {
    policy_recorded: boolean
    policy_etag: string
    stored: MemberTeamPolicy
    parent_stored: MemberTeamPolicy
    runtime_applied: boolean
    usage_status: 'active' | 'inactive' | 'unavailable'
    usage: OverviewAccountUsage | null
    active_reservations: OverviewActiveReservations | null
  }
}
export type MemberTeamsPage = {
  user_id: string
  observed_at: string
  platform_currency: string
  items: MemberTeamRecord[]
  next_cursor: string | null
}
