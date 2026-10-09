import type { RoutingProtocol } from './model-routing'

export interface ModelWeightRow {
  binding_id: string
  binding_created_at: string | null
  provider_model_id: string
  provider_model_created_at: string | null
  connection_id: string
  connection_created_at: string | null
  provider_id: string
  provider_created_at: string | null
  protocol: RoutingProtocol
  weight: number
}
export interface ModelWeightVersion {
  version_id: string
  model_id: string
  model_created_at: string | null
  captured_at: string
  source: 'observed_baseline' | 'legacy_editor' | 'rollback'
  parent_version_id: string | null
  rollback_version_id: string | null
  actor_id: string
  reason: string | null
  binding_count: number
  valid_weight_set: boolean
}
export interface ModelWeightDetail {
  version: ModelWeightVersion
  weights: ModelWeightRow[]
}
export interface ModelWeightPage {
  model_id: string
  items: ModelWeightVersion[]
  next_cursor: string | null
}
export interface ModelWeightReview {
  model_id: string
  version_id: string
  current_version_id: string | null
  current_weights: ModelWeightRow[]
  proposed_weights: ModelWeightRow[]
  eligible: boolean
  can_rollback: boolean
  blocker_codes: string[]
  review_etag: string
  observed_at: string
}
export interface ModelWeightRollbackInput {
  version_id: string
  request_id: string
  reason: string
}
export interface ModelWeightReceipt extends ModelWeightRollbackInput {
  model_id: string
  source_version_id: string | null
  saved_version_id: string | null
  effect: 'changed' | 'noop'
  created_at: string
}
export interface ModelWeightRollbackResult {
  receipt: ModelWeightReceipt
  application_status: 'applied' | 'pending' | 'superseded' | 'unknown'
  runtime_applied: boolean
}
