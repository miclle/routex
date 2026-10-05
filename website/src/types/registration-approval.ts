export type RegistrationApprovalStatus =
  'not_required' | 'pending' | 'approved' | 'rejected' | 'unknown'
export interface RegistrationApprovalSummary {
  status: RegistrationApprovalStatus
  admission_eligible: boolean
}
export interface RegistrationPolicy {
  enabled: boolean
  approval_required: boolean
}
export interface RegistrationPolicyReview extends RegistrationPolicy {
  review_etag: string
}
export interface RegistrationPolicyInput extends RegistrationPolicy {
  reason: string
}
export interface RegistrationPolicyResult extends RegistrationPolicyReview {
  confirmation: 'current_registration_policy'
}
export interface RegistrationApplication {
  id: string
  state: 'pending' | 'approved' | 'rejected'
  created_at: string
  decided_at: string | null
  decision_actor_id: string | null
  decision_reason: string | null
}
export interface MemberApprovalReview {
  user_id: string
  name: string
  identity_role: 'admin' | 'member'
  disabled: boolean
  offboarded_at: string | null
  approval_status: Exclude<RegistrationApprovalStatus, 'unknown'>
  application: RegistrationApplication | null
  can_approve: boolean
  can_reject: boolean
  admission_eligible: boolean
  runtime_applied: boolean
  review_etag: string
}
export interface MemberApprovalInput {
  decision: 'approve' | 'reject'
  reason: string
}
export interface MemberApprovalResult {
  confirmation: 'current_account_approval'
  user_id: string
  application_id: string
  decision: 'approved' | 'rejected'
  admission_eligible: boolean
  runtime_applied: true
}
