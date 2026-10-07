export type VaultAuthInput = { action: 'keep' | 'remove' } | { action: 'replace'; token: string }
export interface VaultDescriptor {
  endpoint: string
  namespace: string
  mount: string
  prefix: string
  data_field: string
}
export interface VaultObservation {
  attempted: boolean
  succeeded: boolean
  duration_ms: string
  failure: {
    stage: 'prepare' | 'write' | 'read' | 'cleanup'
    code: string
    http_status: number
  } | null
}
export interface VaultProbe {
  id: string
  request_id: string
  integration_id: string
  revision_id: string
  review_etag: string
  state:
    | 'planned'
    | 'writing'
    | 'awaiting_read'
    | 'reading'
    | 'cleanup_pending'
    | 'completed'
    | 'interrupted'
  version: 1 | null
  write: VaultObservation
  read: VaultObservation
  cleanup: {
    state: 'not_attempted' | 'acknowledged' | 'failed' | 'unknown'
    observation: VaultObservation
  }
  created_at: string
  finished_at: string | null
}
export interface VaultIntegration {
  id: string
  name: string
  revision_id: string
  descriptor: VaultDescriptor
  writer_auth: { method: 'token'; configured: boolean }
  reader_auth: { method: 'token'; configured: boolean }
  review_etag: string
  can_write: boolean
  can_test: boolean
  last_probe: VaultProbe | null
}
export interface VaultPage {
  items: VaultIntegration[]
  next_cursor: string | null
  review_etag: string
  can_write: boolean
  can_test: boolean
}
export interface VaultConfigInput {
  request_id: string
  name: string
  descriptor: VaultDescriptor
  writer_auth: VaultAuthInput
  reader_auth: VaultAuthInput
  reason: string
}
export interface VaultConfigIntent {
  id?: string
  etag: string
  input: VaultConfigInput
}
export interface VaultSaved {
  request_id: string
  integration_id: string
  revision_id: string
  committed: true
  changed: boolean
}
export interface VaultProbeIntent {
  id: string
  probe_id?: string
  action: 'write' | 'read' | 'cleanup'
  etag: string
  input: { request_id: string; reason: string }
}
