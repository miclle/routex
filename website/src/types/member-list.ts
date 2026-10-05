import type { RegistrationApprovalSummary } from './registration-approval'
import type { Member } from './governance'
import type { MemberRecentLogin } from './member-recent-login'
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
export type MemberListItem = Member &
  MemberRecentLogin & {
    registration_approval: RegistrationApprovalSummary
    updated_at: string
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
