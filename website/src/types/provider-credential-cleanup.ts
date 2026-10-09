import type { VaultObservation } from './vault-integrations'

export interface ProviderOrphan {
  creation_request_id: string
  kind: 'provider' | 'connection' | 'credential' | 'replacement'
  provider_id: string
  connection_id: string
  credential_id: string
  integration_id: string
  revision_id: string
  created_at: string
  state: 'writing' | 'awaiting_read' | 'unknown' | 'owned' | 'orphan' | 'committed'
  write: VaultObservation
  read: VaultObservation
  ownership_recorded: boolean
  // Server permission for a bounded cleanup attempt; never proof of joined holders.
  eligible: boolean
  blocker_codes: string[]
  can_cleanup: boolean
  review_etag: string
}
export interface ProviderOrphanPage {
  items: ProviderOrphan[]
  next_cursor: string | null
}
export interface ProviderCleanupReceipt {
  creation_request_id: string
  request_id: string
  integration_id: string
  revision_id: string
  state: 'pending' | 'unknown' | 'failed' | 'acknowledged'
  ownership: VaultObservation
  cleanup: {
    state: 'not_attempted' | 'unknown' | 'failed' | 'acknowledged'
    observation: VaultObservation
  }
  started_at: string
  finished_at: string | null
}
// Cleanup authentication is deliberately absent from retained intent.
export interface ProviderCleanupIntent {
  integration_id: string
  creation_request_id: string
  revision_id: string
  etag: string
  input: { request_id: string; reason: string }
}
