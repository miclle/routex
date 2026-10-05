export interface MemberStateRecord {
  user_id: string
  name: string
  base_role: 'member' | 'admin'
  disabled: boolean
  offboarded_at: string | null
  status: 'active' | 'disabled' | 'offboarded'
  can_change_base_role: boolean
  can_change_status: boolean
  activation_mode: 'enable' | 'reactivate' | null
  etag: string
  account_access_runtime_applied: boolean
}
export type MemberStateInput =
  { role: 'member' | 'admin'; reason: string } | { disabled: boolean; reason: string }
export interface MemberStateResult extends MemberStateRecord {
  confirmation: 'current_member_state'
  effect: 'current_base_identity' | 'current_account_access'
}
