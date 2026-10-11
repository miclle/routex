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
