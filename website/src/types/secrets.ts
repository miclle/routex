export type SecretAction = 'start' | 'resume' | 'retire' | 'rollback'
export const legacySecretDomains = [
  'provider_credentials',
  'egresses',
  'smtp_settings',
  'storage_revisions',
  'user_mfa',
] as const
export const v2SecretDomains = [
  ...legacySecretDomains,
  'vault_writer_auth',
  'vault_reader_auth',
] as const
export const v3SecretDomains = [...v2SecretDomains, 'oidc_providers'] as const
export const v4SecretDomains = [...v3SecretDomains, 'oauth_providers'] as const
export const secretDomains = [...v4SecretDomains, 'ldap_providers'] as const
export interface SecretRotation {
  inventory_version: 1 | 2 | 3 | 4 | 5
  id: string
  status: 'migrating' | 'blocked' | 'observing' | 'ready' | 'completed' | 'rolled_back'
  phase: 'migration' | 'verification' | 'observation' | 'completed'
  source_key_id: string
  target_key_id: string
  domains: {
    code: (typeof secretDomains)[number]
    coverage: 'observed' | 'not_scanned'
    scanned: string | null
    rewrapped: string | null
    already_target: string | null
    deleted: string | null
    changed: string | null
    blocked: string | null
  }[]
  blocker_codes: string[]
  observation_started_at: string | null
  observation_eligible_at: string | null
  allowed_actions: Exclude<SecretAction, 'start'>[]
}
export interface SecretStore {
  inventory_version: 5
  mode: 'internal'
  observed_at: string
  review_etag: string
  can_read: true
  can_rotate: boolean
  policy: { write_key_id: string | null; epoch: string }
  keys: {
    id: string
    state: 'write' | 'decrypt_only' | 'retired'
    configured: boolean
    verified: boolean
  }[]
  process: {
    id: string | null
    verified: boolean
    policy_epoch: string | null
    snapshot_id: string | null
    verified_at: string | null
  }
  rotation: SecretRotation | null
}
export interface SecretIntent {
  action: SecretAction
  rotationId?: string
  etag: string
  input: { request_id: string; reason: string; target_key_id?: string }
}
export interface SecretResult {
  receipt: { request_id: string; rotation_id: string; action: SecretAction; created_at: string }
  committed: true
  write_policy_applied: boolean
  publication_applied: boolean
  application_status: 'applied' | 'pending' | 'superseded' | 'unavailable'
  rotation: SecretRotation | null
}
