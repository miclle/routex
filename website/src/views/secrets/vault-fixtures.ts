import type { VaultConfigIntent, VaultIntegration, VaultProbe } from '@/types/vault-integrations'
export const vaultID = 'vlt_01k00000000000000000000000'
export const revisionID = 'vlr_01k00000000000000000000000'
export const vaultETag = 'a'.repeat(64) + '.' + 'b'.repeat(64)
export const requestID = '10000000-0000-4000-8000-000000000001'
export function vault(): VaultIntegration {
  return {
    id: vaultID,
    name: 'QA Vault',
    revision_id: revisionID,
    descriptor: {
      endpoint: 'https://vault.example.test/base',
      namespace: 'acme',
      mount: 'secret',
      prefix: 'providers',
      data_field: 'value',
    },
    writer_auth: { method: 'token', configured: true },
    reader_auth: { method: 'token', configured: true },
    review_etag: vaultETag,
    can_write: true,
    can_test: true,
    last_probe: null,
  }
}
export function probe(): VaultProbe {
  return {
    id: 'c'.repeat(32),
    request_id: requestID,
    integration_id: vaultID,
    revision_id: revisionID,
    review_etag: vaultETag,
    state: 'awaiting_read',
    version: 1,
    write: { attempted: true, succeeded: true, duration_ms: '0.123456', failure: null },
    read: { attempted: false, succeeded: false, duration_ms: '0', failure: null },
    cleanup: {
      state: 'not_attempted',
      observation: { attempted: false, succeeded: false, duration_ms: '0', failure: null },
    },
    created_at: '2026-10-07T00:00:00Z',
    finished_at: null,
  }
}
export function configuration(): VaultConfigIntent {
  return {
    id: vaultID,
    etag: vaultETag,
    input: {
      request_id: requestID,
      name: 'QA Vault',
      descriptor: vault().descriptor,
      writer_auth: { action: 'replace', token: 'writer-secret' },
      reader_auth: { action: 'replace', token: 'reader-secret' },
      reason: 'Reviewed configuration',
    },
  }
}
