export interface InsightsModel {
  model_id: string
  name: string
}
export interface InsightsAnalysisInput {
  report: 'tokens_by_model'
  model_id: string
  from: string
  to: string
  timezone: string
  language: 'en' | 'zh'
}
export type InsightsCompletion = 'unknown' | 'completed' | 'handoff' | 'blocked' | 'incomplete'
export interface InsightsAnalysis {
  request_id: string
  completion: InsightsCompletion
  text: string | null
  capture: {
    source: 'persisted_call_records'
    from: string
    to: string
    timezone: string
    queried_at: string
    latest_completed_at: string | null
    may_lag: boolean
  }
  labels: { label: string; model_id: string; name: string; unknown: boolean }[]
}

export interface ProviderQualityRange {
  from: string
  to: string
  timezone: string
}
export interface ProviderQualityCounts {
  attempts: number
  eligible: number
  excluded: number
  credential_rejected: number
  successes: number
  success_rate_bps: number | null
  rate_limited: number
  server_errors: number
  known_duration: number
  unknown_duration: number
  p95_duration_ms: number | null
}
export interface ProviderQualityReport extends ProviderQualityRange {
  source: 'persisted_call_attempts'
  queried_at: string
  latest_completed_at: string | null
  may_lag: boolean
  total_attempts: number
  unknown_attribution: number
  summary: ProviderQualityCounts
  providers: (ProviderQualityCounts & { provider_id: string; name: string })[]
}
