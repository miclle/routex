export interface ModelMonthlyRequests {
  items: { model_id: string; requests: string }[]
  period_from: string
  period_to: string
  as_of: string
  timezone: 'UTC'
  source: 'persisted_call_records'
  may_lag: true
}
