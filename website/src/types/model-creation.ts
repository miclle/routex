export type ModelCreationProtocol =
  'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_generate_content'
export interface ModelCreationConnection {
  id: string
  provider_id: string
  provider_name: string
  name: string
  protocol: ModelCreationProtocol
  base_url: string
}
export interface ModelCreationContext {
  connection: ModelCreationConnection
  can_create: boolean
  observed_at: string
}
export interface ModelCreationProviderModel {
  id: string
  upstream_name: string
  disabled: boolean
  input_capabilities: string[]
  credential_ready: boolean
  selectable: boolean
  blocker_codes: string[]
}
export interface ModelCreationTarget {
  id: string
  name: string
  initial_weight: number
  selectable: boolean
  blocker_codes: string[]
}
export interface ModelCreationPage<T> {
  items: T[]
  next_cursor: string | null
}
export type ModelCreationItem =
  | { provider_model_id: string; target: 'new'; name: string }
  | { provider_model_id: string; target: 'existing'; model_id: string }
export interface ModelCreationReviewedItem {
  provider_model_id: string
  upstream_name: string
  target: 'new' | 'existing'
  model_id: string | null
  name: string
  protocol: ModelCreationProtocol
  initial_weight: number
  blocker_codes: string[]
}
export interface ModelCreationPreview {
  connection: ModelCreationConnection
  items: ModelCreationReviewedItem[]
  review_etag: string
  observed_at: string
  can_commit: boolean
}
export interface ModelCreationInput {
  request_id: string
  reason: string
  items: ModelCreationItem[]
}
export interface ModelCreationReceiptItem {
  provider_model_id: string
  model_id: string
  binding_id: string
  created_model: boolean
  name: string
  protocol: ModelCreationProtocol
  weight: number
}
export interface ModelCreationResult {
  receipt: {
    request_id: string
    connection_id: string
    created_at: string
    items: ModelCreationReceiptItem[]
  }
  committed: true
  changed: boolean
  current_items: ModelCreationReceiptItem[] | null
  runtime_applied: boolean
  application_status: 'pending' | 'applied' | 'superseded' | 'unavailable'
}
export interface ModelCreationFilter {
  q?: string
  cursor?: string
  limit?: number
}
