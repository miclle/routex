export type ProjectRequestStatus = 'pending' | 'approved' | 'rejected' | 'withdrawn'
export type ProjectRequestAction = 'approve' | 'reject' | 'withdraw'
export interface ProjectRequest {
  id: string
  project_id: string
  applicant_user_id: string
  kind: 'MODEL_ACCESS'
  baseline_model_ids: string[]
  requested_model_ids: string[]
  reason: string
  status: ProjectRequestStatus
  decision_actor_id?: string
  decision_reason?: string
  created_at: string
  decided_at: string | null
}
export interface ProjectRequestPage {
  items: ProjectRequest[]
  next_cursor?: string
}
export interface ProjectRequestCandidate {
  id: string
  name: string
}
export interface CreateProjectRequest {
  request_id: string
  kind: 'MODEL_ACCESS'
  model_ids: string[]
  reason: string
}
export interface ProjectRequestDecision {
  action: ProjectRequestAction
  reason: string
}
