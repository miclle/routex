import type { Session } from './auth'
export interface AuthenticationPolicy {
  enforced: boolean
  password_login_enabled: boolean
  emergency_entry_enabled: boolean
}
export interface AuthenticationPolicyReview {
  enforced: boolean
  emergency_admin_id: string | null
  emergency_admin_created_at: string | null
  emergency_mfa_enabled: boolean | null
  oidc_available: boolean
  ldap_available: boolean
  review_emergency_admin: { id: string; created_at: string; mfa_enabled: boolean } | null
  review_etag: string
}
export interface AuthenticationPolicyInput {
  enforced: boolean
  emergency_admin_id: string
  reason: string
}
export interface AuthenticationPolicyReceipt extends AuthenticationPolicyReview {
  confirmation: 'saved'
  runtime_applied: boolean
  reauthentication_required: boolean
}
export interface ReauthenticationRequired {
  reauthentication_required: true
}
export type SecuritySessionResult = Session | ReauthenticationRequired
