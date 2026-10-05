import type { RegistrationApprovalSummary } from './registration-approval'
import type { Member } from './governance'

export type MemberRecentLogin =
  | { last_login_status: 'recorded'; last_login_at: string }
  | { last_login_status: 'historical_unavailable'; last_login_at: null }

// Only the authorized member detail GET adds recorded sign-in metadata.
export type MemberDetail = Member &
  MemberRecentLogin & { registration_approval: RegistrationApprovalSummary }
