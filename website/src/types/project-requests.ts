export type ProjectRequestStatus = 'pending' | 'approved' | 'rejected' | 'withdrawn'
export type ProjectRequestAction = 'approve' | 'reject' | 'withdraw'
interface ProjectRequestBase {
  id: string
  project_id: string
  applicant_user_id: string
  reason: string
  status: ProjectRequestStatus
  decision_actor_id?: string
  decision_reason?: string
  created_at: string
  decided_at: string | null
}
export interface ProjectModelRequest extends ProjectRequestBase {
  kind: 'MODEL_ACCESS'
  baseline_model_ids: string[]
  requested_model_ids: string[]
}
export interface ProjectQuota {
  tokens_month: number | null
  money_month: string | null
  currency: string
}
export interface ProjectQuotaPatch {
  tokens_month?: number
  money_month?: string
  currency?: string
}
export interface ProjectQuotaRequest extends ProjectRequestBase {
  kind: 'QUOTA'
  baseline_quota: ProjectQuota
  requested_quota: ProjectQuotaPatch
  baseline_policy_etag: string
  approved_quota?: ProjectQuota
  approved_policy_etag?: string
}
export interface ProjectRateLimit {
  rpm: number | null
  tpm: number | null
  concurrency: number | null
}
export interface ProjectRateLimitPatch {
  rpm?: number
  tpm?: number
  concurrency?: number
}
export interface ProjectRateLimitRequest extends ProjectRequestBase {
  kind: 'RATE_LIMIT'
  baseline_rate_limit: ProjectRateLimit
  requested_rate_limit: ProjectRateLimitPatch
  baseline_policy_etag: string
  approved_rate_limit?: ProjectRateLimit
  approved_policy_etag?: string
}
export type ProjectPolicyRequest = ProjectQuotaRequest | ProjectRateLimitRequest
export type ProjectRequest = ProjectModelRequest | ProjectPolicyRequest
export interface ProjectQuotaContext {
  project_id: string
  review_etag: string
  policy_etag: string
  current_quota: ProjectQuota
  platform_currency: string
}
export interface ProjectRequestLimitsContext extends ProjectQuotaContext {
  current_rate_limit: ProjectRateLimit
}
export interface ProjectQuotaRequestDetail extends ProjectQuotaRequest {
  approval_review_etag?: string
  current_quota: ProjectQuota
  current_policy_etag: string
  platform_currency: string
  runtime_applied?: boolean
  application_status?: 'pending' | 'applied' | 'superseded'
}
export interface ProjectRateLimitRequestDetail extends ProjectRateLimitRequest {
  approval_review_etag?: string
  current_rate_limit: ProjectRateLimit
  current_policy_etag: string
  platform_currency: string
  runtime_applied?: boolean
  application_status?: 'pending' | 'applied' | 'superseded'
}
export type ProjectPolicyRequestDetail = ProjectQuotaRequestDetail | ProjectRateLimitRequestDetail
export interface ProjectRequestPage {
  items: ProjectRequest[]
  next_cursor?: string
}
export interface ProjectRequestCandidate {
  id: string
  name: string
}
export interface CreateProjectModelRequest {
  request_id: string
  kind: 'MODEL_ACCESS'
  model_ids: string[]
  reason: string
}
export interface CreateProjectQuotaRequest {
  request_id: string
  kind: 'QUOTA'
  quota: ProjectQuotaPatch
  reason: string
}
export interface CreateProjectRateLimitRequest {
  request_id: string
  kind: 'RATE_LIMIT'
  rate_limit: ProjectRateLimitPatch
  reason: string
}
export type CreateProjectPolicyRequest = CreateProjectQuotaRequest | CreateProjectRateLimitRequest
export type CreateProjectRequest = CreateProjectModelRequest | CreateProjectPolicyRequest
export interface ProjectRequestDecision {
  action: ProjectRequestAction
  reason: string
}
