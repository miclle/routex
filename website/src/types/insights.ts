export interface InsightsModel {
  model_id: string
  name: string
}
export interface InsightsAnalysisInput {
  report: 'tokens_by_model' | 'provider_quality'
  question?: string
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
    source: 'persisted_call_records' | 'persisted_call_attempts'
    from: string
    to: string
    timezone: string
    queried_at: string
    latest_completed_at: string | null
    may_lag: boolean
  }
  labels: (
    | { label: string; model_id: string; provider_id?: never; name: string; unknown: boolean }
    | { label: string; provider_id: string; model_id?: never; name: string; unknown: false }
  )[]
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
  average_duration_ms: number | null
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

export type InsightsRangeReportKind = 'tokens_by_model' | 'provider_quality'
export type InsightsReportKind = InsightsRangeReportKind | 'team_quota' | 'key_quota'
export type InsightsReportParameters =
  | (ProviderQualityRange & { kind: InsightsRangeReportKind })
  | { kind: 'team_quota'; dimension: 'tokens' | 'money' }
  | {
      kind: 'key_quota'
      dimension: 'tokens' | 'money'
      scope_kind: 'member' | 'project'
      scope_id: string
    }
export type InsightsReportInput = InsightsReportParameters & { request_id: string }
export type InsightsReportDefinition = InsightsReportParameters & {
  id: string
  created_at: string
}
export interface InsightsReportPage {
  items: InsightsReportDefinition[]
  next_cursor: string
}
