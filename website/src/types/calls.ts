export interface CallRecord {
  request_id: string
  model_id: string
  model_name: string
  key_id: string
  protocol: string
  status: 'success' | 'error' | 'canceled'
  stream: boolean
  started_at: string
  completed_at: string
  duration_ms: number
  input_tokens: number | null
  output_tokens: number | null
}
export interface CallAttempt {
  id: string
  provider_model_id: string
  connection_id: string
  attempt_number: number
  status: string
  failure_class:
    'success' | 'credential_rejected' | 'connection_failure' | 'rate_limited' | 'permanent_failure'
  work_evidence: 'not_sent' | 'rejected_without_work' | 'unknown' | 'completed'
  output_started: boolean
  final_usage_known: boolean
  evidence_code:
    | ''
    | 'pre_request_connection'
    | 'native_auth_rejection'
    | 'native_rate_rejection'
    | 'upstream_response'
    | 'transport_ambiguous'
  http_status: number
  error_code: string
  started_at: string
  completed_at: string
}
export interface AdminCallRecord extends CallRecord {
  user_id: string
  project_id?: string
}
export interface AdminCallDetail extends AdminCallRecord {
  provider_model_id: string
  connection_id: string
  error_code: string
  route_stop_reason:
    | ''
    | 'succeeded'
    | 'unsafe_to_replay'
    | 'permanent_failure'
    | 'attempt_budget_exhausted'
    | 'no_candidates'
    | 'canceled'
    | 'blocked'
  attempts: CallAttempt[]
}
export interface CallFilters {
  status?: string
  model_id?: string
  key_id?: string
  user_id?: string
  from?: string
  to?: string
}
export interface CallPage {
  items: (CallRecord | AdminCallRecord)[]
  next_cursor: string | null
}
