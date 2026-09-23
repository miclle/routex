export type UsagePeriodName = 'today' | '24h' | '7d' | 'month' | '90d' | 'year'
export type UsageGranularity = 'auto' | 'hour' | 'day' | 'week' | 'month'
export interface UsageFilters {
  period?: UsagePeriodName
  from?: string
  to?: string
  timezone: string
  granularity: UsageGranularity
  compare: boolean
  model_id?: string
  key_id?: string
  status?: 'success' | 'error' | 'canceled'
  protocol?: 'openai_chat' | 'openai_responses' | 'anthropic_messages'
  stream?: boolean
  user_id?: string
  project_id?: string
  provider_model_id?: string
  connection_id?: string
}
export interface UsageCount {
  value: string | null
  known: string
  unknown_calls: number
}
export interface UsageAmount {
  currency: string
  amount: string
  calls: number
}
export interface UsageStats {
  requests: number
  successes: number
  errors: number
  canceled: number
  success_rate: number | null
  average_duration_ms: number | null
  tokens: { input: UsageCount; output: UsageCount; total: UsageCount }
  amounts: UsageAmount[]
  unknown_amount_calls: number
  pricing_statuses: Record<string, number>
}
export interface UsageGroup {
  id: string
  name?: string
  unknown: boolean
  stats: UsageStats
}
export interface UsageBucket {
  start: string
  end: string
  stats: UsageStats
}
export interface UsagePeriod {
  from: string
  to: string
  summary: UsageStats
  trend: UsageBucket[]
  models: UsageGroup[]
  keys: UsageGroup[]
  provider_models?: UsageGroup[]
  connections?: UsageGroup[]
}
export interface UsageReport {
  timezone: string
  granularity: Exclude<UsageGranularity, 'auto'>
  queried_at: string
  latest_completed_at: string | null
  source: 'persisted_call_records'
  may_lag: boolean
  current: UsagePeriod
  previous?: UsagePeriod
  available_dimensions: string[]
}
export interface UsageScope {
  admin?: boolean
  projectId?: string
}
