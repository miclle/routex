import type {
  ModelCreationProtocol,
  ModelCreationPage,
  ModelCreationFilter,
} from './model-creation'
import type { CredentialStorageSource } from './provider-storage'

// exact_id is a resource-scoped identity read, separate from name/prefix search.
export interface ModelAccessPickerFilter extends ModelCreationFilter {
  exact_id?: string
}

export interface ModelAccessProvider {
  id: string
  name: string
}
export type ModelAccessProviderPage = ModelCreationPage<ModelAccessProvider>
export interface ModelAccessInput {
  request_id: string
  storage_policy_etag: string
  name: string
  connection_name?: string
  credential_name: string
  base_url: string
  protocol: ModelCreationProtocol
  adapter: 'native' | 'azure_openai_classic'
  api_version: string | null
  egress_mode: 'default' | 'direct' | 'proxy'
  egress_id: string | null
  secret: string
}
export interface ModelAccessSaved {
  provider_id: string
  provider_name: string
  connection_id: string
  connection_name: string
  credential_id: string
  credential_name: string
  connection_enabled: boolean
  protocol: ModelCreationProtocol
  adapter: 'native' | 'azure_openai_classic'
  api_version: string | null
  base_url: string
  storage_source: CredentialStorageSource
}
export interface ModelAccessCredential {
  id: string
  connection_id: string
  enabled: boolean
  verification_status: 'pending' | 'verified' | 'failed'
  verified_at: string | null
}
export interface ModelAccessEgress {
  id: string
  name: string
  enabled: boolean
}
