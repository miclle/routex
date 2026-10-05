import type { Member } from './governance'
import type { MonthlyAccount } from './overview'

export interface MemberListTeam {
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
  membership_status: 'active' | 'disabled'
  membership_role: 'owner' | 'member'
}
export type MemberListTeams =
  | { status: 'available'; items: MemberListTeam[] }
  | { status: 'not_authorized' | 'overflow' | 'unavailable'; items: null }
export interface MemberListItem extends Member {
  updated_at: string
  last_login_at: null
  last_login_status: 'historical_unavailable'
  total_personal_keys: string
  personal_policy_stored: boolean
  personal: MonthlyAccount
  teams: MemberListTeams
}
export interface MemberListPage {
  actor_user_id: string
  observed_at: string
  platform_currency: string
  items: MemberListItem[]
  next_cursor: string | null
}
