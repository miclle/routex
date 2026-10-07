export type CredentialStorageSource = 'inline' | 'vault'
export interface ProviderStorageChoice {
  id: string
  name: string
  birth: string
  revision_id: string
}
export interface ProviderStoragePolicy {
  mode: CredentialStorageSource
  integration_id: string | null
  revision_id: string | null
  choices: ProviderStorageChoice[]
  etag: string
  can_edit: boolean
}
export interface ProviderStorageInput {
  mode: CredentialStorageSource
  integration_id: string | null
  revision_id: string | null
  reason: string
}
export interface CredentialStorageContext {
  storage_source: CredentialStorageSource
  etag: string
}
