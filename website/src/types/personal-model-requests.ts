import type { ModelInputCapability } from './model-catalog'

export interface PersonalModelCandidate {
  id: string
  name: string
  status: 'active'
  created_at: string
  protocols: string[]
  input_capabilities: Record<string, ModelInputCapability[]>
  personal_granted: boolean
  pending_request_id: string | null
  review_etag: string
}
export type PersonalModelRequestStatus =
  'pending' | 'approved' | 'rejected' | 'withdrawn' | 'cancelled'
export type PersonalModelDecisionAction = 'approve' | 'reject' | 'withdraw'
export interface PersonalModelRequest {
  id: string
  request_id: string
  applicant_user_id: string
  applicant_name: string
  model_id: string
  model_name: string
  reason: string
  status: PersonalModelRequestStatus
  created_at: string
  updated_at: string
  resolved_at: string | null
  cancelled_reason: string | null
  decision: null | {
    decision_id: string
    actor_id: string
    actor_name: string
    action: PersonalModelDecisionAction
    reason: string
    decided_at: string
  }
}
export interface PersonalModelRequestDetail extends PersonalModelRequest {
  current_model: null | { id: string; name: string; status: 'active' | 'disabled' | 'archived' }
  current_granted: boolean
  review_etag: string
  allowed_actions: PersonalModelDecisionAction[]
  runtime_applied: boolean
  application_status: 'pending' | 'applied' | 'superseded'
}
export interface PersonalModelRequestPage {
  items: PersonalModelRequest[]
  total: number
  next_cursor: string | null
}
export interface PersonalModelRequestIntent {
  body: { request_id: string; model_id: string; reason: string }
  etag: string
}
export interface PersonalModelDecisionIntent {
  body: { decision_id: string; action: PersonalModelDecisionAction; reason: string }
  etag: string
}
export interface PersonalModelDecisionReceipt {
  decision_id: string
  committed: true
  saved_request: PersonalModelRequest
  current_granted: boolean
  runtime_applied: boolean
  application_status: 'pending' | 'applied' | 'superseded'
}
export interface PersonalModelWorkspace {
  user_id: string
  models: { id: string; name: string; status: 'active' | 'disabled' | 'archived' }[]
  model_count: number
  can_review_requests: boolean
}
