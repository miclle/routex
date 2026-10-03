import type { ModelInputCapability } from './model-catalog'

export type TeamModelRequestStatus = 'pending' | 'approved' | 'rejected' | 'withdrawn' | 'cancelled'
export type TeamModelDecisionAction = 'approve' | 'reject' | 'withdraw'
export type TeamModelApplicationStatus = 'pending' | 'applied' | 'superseded' | 'unavailable'
export interface TeamModelRequestTeam {
  id: string
  name: string
  membership_id: string
}
export interface TeamModelRequestTeamPage {
  items: TeamModelRequestTeam[]
  next_cursor: string | null
}
export interface TeamModelCandidate {
  id: string
  name: string
  status: 'active'
  created_at: string
  protocols: string[]
  input_capabilities: Record<string, ModelInputCapability[]>
  team_id: string
  team_granted: boolean
  pending_request: boolean
  own_pending_request_id: string | null
  review_etag: string
}
export interface TeamModelRequest {
  id: string
  request_id: string
  team_id: string
  team_name: string
  applicant_user_id: string
  applicant_name: string
  applicant_membership_id: string
  model_id: string
  model_name: string
  reason: string
  status: TeamModelRequestStatus
  created_at: string
  updated_at: string
  resolved_at: string | null
  cancelled_reason: string | null
  decision: null | {
    decision_id: string
    actor_id: string
    actor_name: string
    action: TeamModelDecisionAction
    reason: string
    decided_at: string
  }
}
export interface TeamModelCurrentResource {
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
}
export interface TeamModelApplication {
  current_membership_matches: boolean | null
  current_granted: boolean | null
  runtime_applied: boolean | null
  application_status: TeamModelApplicationStatus
}
export interface TeamModelRequestDetail extends TeamModelRequest, TeamModelApplication {
  current_team: TeamModelCurrentResource | null
  current_model: TeamModelCurrentResource | null
  review_etag: string
  allowed_actions: TeamModelDecisionAction[]
}
export interface TeamModelRequestPage {
  items: TeamModelRequest[]
  total: number
  next_cursor: string | null
}
export interface TeamModelRequestIntent {
  body: { request_id: string; team_id: string; model_id: string; reason: string }
  etag: string
}
export interface TeamModelDecisionIntent {
  body: { decision_id: string; action: TeamModelDecisionAction; reason: string }
  etag: string
}
export interface TeamModelDecisionReceipt extends TeamModelApplication {
  decision_id: string
  committed: true
  saved_request: TeamModelRequest
}
export interface TeamModelWorkspace {
  team_id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
  models: TeamModelCurrentResource[]
  model_count: number
  can_review_requests: true
}
