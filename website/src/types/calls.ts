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
  status: string
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
