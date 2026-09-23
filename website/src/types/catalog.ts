export interface Credential {
  id: string
  name: string
  priority: number
  enabled: boolean
  verification_status: 'pending' | 'verified' | 'failed'
  verified_at: string | null
}
export interface ProviderModel {
  enabled: boolean
  etag: string
  id: string
  upstream_name: string
}
export interface Connection {
  id: string
  name: string
  base_url: string
  protocol: 'openai_chat' | 'openai_responses'
  credentials: Credential[]
  provider_models: ProviderModel[]
}
export interface Provider {
  id: string
  name: string
  connections: Connection[]
}
export interface Model {
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
  }[]
  granted_user_ids: string[]
}
export interface CallableModel {
  id: string
  name: string
  status: Model['status']
  protocol: string
  protocols?: string[]
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
