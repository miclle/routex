export interface Credential {
  storage_source?: 'inline' | 'vault'
  replaces_credential_id?: string | null
  id: string
  name: string
  priority: number
  enabled: boolean
  verification_status: 'pending' | 'verified' | 'failed'
  verified_at: string | null
  verification_transport_current?: boolean
}
export interface ProviderModel {
  enabled: boolean
  etag: string
  id: string
  supports_image_input: boolean
  supports_pdf_input: boolean
  upstream_name: string
  capabilities_transport_current?: boolean
  capability_review_etag?: string
}
export interface Connection {
  adapter?: 'native' | 'azure_openai_classic'
  api_version?: string | null
  enabled?: boolean // Absent catalogue state remains Unknown, never implicitly enabled.
  egress_mode?: 'default' | 'direct' | 'proxy'
  egress_id?: string | null
  etag?: string
  id: string
  name: string
  base_url: string
  protocol: 'openai_chat' | 'openai_responses' | 'anthropic_messages' | 'gemini_generate_content'
  credentials: Credential[]
  provider_models: ProviderModel[]
}
export interface Provider {
  enabled?: boolean // Absent catalogue state remains Unknown.
  id: string
  name: string
  connections: Connection[]
}
export interface Model {
  created_at?: string | null
  config_updated_at?: string | null
  id: string
  name: string
  status: 'active' | 'disabled' | 'archived'
  names: { name: string; is_current: boolean; expires_at: string | null }[]
  bindings: {
    id: string
    provider_model_id: string
    provider_id: string
    connection_id: string
    upstream_name: string
    protocol: string
    weight: number
    ready: boolean
    supply?: import('./model-routing').RoutingSupply
  }[]
  granted_user_ids: string[]
}
export interface CallableModel {
  id: string
  name: string
  status: Model['status']
  protocol: string
  protocols?: string[]
  input_capabilities?: Record<string, ('image' | 'pdf')[]>
}
export interface PersonalKey {
  id: string
  name: string
  prefix: string
  status: 'pending' | 'active' | 'disabled' | 'revoked'
  model_ids: string[]
  expires_at: string | null
  created_at: string
  replaces_key_id: string | null
  delivery_expires_at: string | null
}
export interface KeyDelivery {
  key: PersonalKey
  secret: string
}

export interface ModelAliasRetirementReview {
  model_id: string
  current_name: string
  alias: { name: string; is_current: boolean; expires_at: string | null }
  state: 'current' | 'compatibility' | 'retired'
  observed_at: string
  etag: string
  can_retire: boolean
  runtime_applied: boolean
}
export interface ModelAliasRetirementInput {
  name: string
  reason: string
}
export interface ModelAliasRetirementResult {
  alias: ModelAliasRetirementReview
  retired: true
  changed: boolean
  runtime_applied: boolean
}
