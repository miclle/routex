export interface ProviderMetadata {
  id: string
  name: string
  etag: string
  can_edit: boolean
}
export interface ProviderMetadataInput {
  name: string
  reason: string
}
export interface ProviderMetadataResult {
  provider: ProviderMetadata
  runtime_applied: true
  changed: boolean
}
