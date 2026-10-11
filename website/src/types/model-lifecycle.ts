export interface ModelLifecycleReview {
  model_id: string
  current_name: string
  created_at: string
  status: 'active' | 'disabled' | 'archived'
  observed_at: string
  etag: string
  can_change: boolean
  runtime_applied: boolean
}
export interface ModelLifecycleInput {
  status: 'active' | 'disabled' | 'archived'
  reason: string
}
export interface ModelLifecycleResult {
  model: ModelLifecycleReview
  changed: boolean
  runtime_applied: boolean
}
