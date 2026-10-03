import type { QuotaUsage } from './resource-limits'
export type TeamQuotaDimension = 'tokens' | 'money'
export type TeamRequestStatus =
  'pending_team_owner' | 'pending_quota_admin' | 'approved' | 'rejected' | 'withdrawn' | 'cancelled'
export type TeamApprovalStage = 'team_owner' | 'quota_admin'
export type TeamStepStatus = 'pending' | 'approved' | 'rejected' | 'withdrawn' | 'cancelled'
export type TeamRequestAction = 'approve' | 'reject' | 'withdraw'
export interface TeamQuotaContext {
  team_id: string
  dimension: TeamQuotaDimension
  currency: string | null
  platform_currency: string
  member_stored: string | null
  member_effective: string | null
  team_stored: string | null
  team_effective: string | null
  member_policy_etag: string
  team_policy_etag: string
  member_usage: QuotaUsage | null
  team_usage: QuotaUsage | null
  time_zone: string
  month_start: string
  month_end: string
  calendar_etag: string
  pricing_etag: string
  resource_created_at: string
  etag: string
  eligible: boolean
  blockers: string[]
}
export interface TeamApprovalStep {
  id: string
  stage: TeamApprovalStage
  status: TeamStepStatus
  entered_at: string
  acted_at: string | null
  actor_id: string | null
  actor_name: string | null
  reason: string
}
export interface TeamQuotaRequest {
  id: string
  request_id: string
  team_id: string
  team_name: string
  applicant_user_id: string
  applicant_name: string
  dimension: TeamQuotaDimension
  target_value: string
  currency: string | null
  reason: string
  status: TeamRequestStatus
  created_at: string
  updated_at: string
  resolved_at: string | null
  submitted_snapshot: TeamQuotaContext
  steps: TeamApprovalStep[]
  current_step_id: string | null
  escalation_reason: null | 'no_eligible_owner' | 'target_exceeds_team' | 'owner_unavailable'
}
export interface TeamRequestApplication {
  runtime_applied: boolean
  application_status: 'pending' | 'applied' | 'superseded'
}
export interface TeamApprovalPreview {
  member_before: string | null
  member_after: string
  team_before: string | null
  team_after: string | null
  escalates: boolean
}
export interface TeamRequestDetail extends TeamQuotaRequest {
  approval_preview: TeamApprovalPreview | null
  workspace_available: boolean
  current_context: TeamQuotaContext | null
  allowed_actions: TeamRequestAction[]
  etag: string
  application: TeamRequestApplication | null
}
export interface TeamRequestPage {
  items: TeamQuotaRequest[]
  total: number
  next_cursor: string | null
}
export interface TeamRequestFilters {
  status: TeamRequestStatus | ''
  dimension: TeamQuotaDimension | ''
  team_id?: string
}
export interface CreateTeamRequest {
  request_id: string
  dimension: TeamQuotaDimension
  target_value: string
  reason: string
}
export interface TeamRequestDecision {
  decision_id: string
  step_id: string
  action: TeamRequestAction
  reason: string
}
export interface TeamDecisionReceipt {
  decision_id: string
  step_id: string
  action: TeamRequestAction
  committed: true
  saved_step: TeamApprovalStep
  request: TeamRequestDetail
}
