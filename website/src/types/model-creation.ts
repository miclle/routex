export type ModelCreationProtocol =
  'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_generate_content'
export interface ModelCreationConnection {
  adapter: 'native' | 'azure_openai_classic'
  api_version: string | null
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
export type ModelCreationInitialTarget =
  | { target: 'new'; name: string }
  | { target: 'existing'; model_id: string; name: string; initial_weight: 0 | 100 }
export interface ModelCreationProviderModel {
  initial_target: ModelCreationInitialTarget | null
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
export type ModelCreationMediaDeclaration =
  | { supports_image_input: boolean; supports_pdf_input: boolean }
  | { supports_image_input?: never; supports_pdf_input?: never }
export type ModelCreationSource =
  | {
      provider_model_id: string
      upstream_name?: never
      supports_image_input?: never
      supports_pdf_input?: never
    }
  | ({ upstream_name: string; provider_model_id?: never } & ModelCreationMediaDeclaration)
export type ModelCreationItem = ModelCreationSource &
  ({ target: 'new'; name: string } | { target: 'existing'; model_id: string })
export interface ModelCreationReviewedItem {
  supports_image_input?: boolean
  supports_pdf_input?: boolean
  warning_codes?: ['credential_coverage_unproven']
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
  supports_image_input?: boolean
  supports_pdf_input?: boolean
  manual_upstream_name?: string
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
