export type QuotaInsightsKind = 'team_quota' | 'key_quota'
export type QuotaInsightsDimension = 'tokens' | 'money'
export type QuotaInsightsScope =
  { kind: 'readable_teams'; id: '' } | { kind: 'member' | 'project'; id: string }
export interface QuotaInsightsInput {
  kind: QuotaInsightsKind
  dimension: QuotaInsightsDimension
  scope: QuotaInsightsScope
  cursor: string | null
}
export type QuotaEstimateStatus =
  | 'estimated'
  | 'already_reached'
  | 'no_local_cap'
  | 'zero_cap'
  | 'inactive'
  | 'unavailable'
  | 'incomplete_coverage'
  | 'unknown_usage'
  | 'zero_rate'
  | 'after_reset'
export interface QuotaInsightsRow {
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived' | 'pending' | 'revoked'
  shared_key_count: number
  local_cap: string | null
  behavior: 'stop' | 'alert_only'
  month_settled: string | null
  baseline_settled: string | null
  baseline_held: string | null
  month_held: string | null
  active_held: string | null
  month_unknown: string | null
  baseline_unknown: string | null
  month_covered: boolean
  baseline_covered: boolean
  utilization_percent: string | null
  level: 'normal' | 'near' | 'critical' | 'reached' | 'unknown'
  estimate_at: string | null
  estimate_within_seven_days: boolean
  estimate_status: QuotaEstimateStatus
}
export interface QuotaInsightsReport {
  kind: QuotaInsightsKind
  dimension: QuotaInsightsDimension
  currency: string
  scope: QuotaInsightsScope
  as_of: string | null
  coverage_start: string | null
  time_zone: string
  baseline_from: string | null
  baseline_to: string | null
  month_from: string | null
  month_reset: string | null
  baseline_basis: 'settled_admissions_168h'
  items: QuotaInsightsRow[]
  next_cursor: string | null
}
export interface QuotaScopeOption {
  id: string
  name: string
}
export interface QuotaScopePage {
  items: QuotaScopeOption[]
  next_cursor: string | null
}
