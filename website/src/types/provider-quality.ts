export type ProviderQualityStatus = 'healthy' | 'degraded' | 'insufficient_data' | 'unconfigured'

export interface ProviderQualitySummary {
  provider_id: string
  name: string
  window_start: string
  window_end: string
  evaluated_at: string
  latest_completed_at: string | null
  data_through: string | null
  requests: number
  eligible_attempts: number
  excluded_attempts: number
  credential_rejected_attempts: number
  unknown_attribution_attempts: number
  successes: number
  success_rate: number | null
  success_rate_bps: number | null
  p95_duration_ms: number | null
  known_duration_attempts: number
  rate_limited_attempts: number
  server_error_attempts: number
  unknown_duration_attempts: number
  may_lag: boolean
  status: ProviderQualityStatus
}

export interface ProviderQualityPolicy {
  provider_id: string
  enabled: boolean
  window_minutes: number
  minimum_attempts: number
  min_success_rate_bps: number
  max_p95_duration_ms: number | null
  etag: string
  updated_at: string | null
  updated_by: string
  update_reason: string
}

export interface UpdateProviderQualityPolicyInput {
  enabled: boolean
  window_minutes: number
  minimum_attempts: number
  min_success_rate_bps: number
  max_p95_duration_ms: number | null
  reason: string
  etag: string
}
