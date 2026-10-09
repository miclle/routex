export interface ProviderStatus {
  id: string
  name: string
  etag: string
  can_edit: boolean
  enabled: boolean
}
export interface ProviderStatusInput {
  enabled: boolean
  reason: string
}
export interface ProviderStatusResult {
  provider: ProviderStatus
  runtime_applied: true
  changed: boolean
}
