export interface CredentialRetirementInput {
  request_id: string
  replacement_credential_id: string
  evidence_attempt_id: string
  snapshot_id: string
  reason: string
}

export interface CredentialRetirementResult {
  request_id: string
  source_credential_id: string
  replacement_credential_id: string
  committed: true
  committed_at: string
  runtime_applied: boolean
  current_snapshot_id: string | null
  blockers: string[]
}

export const credentialRetirementBlockers = [
  'source_reenabled',
  'source_missing',
  'replacement_missing',
  'relationship_changed',
  'replacement_disabled',
  'replacement_unverified',
  'runtime_unavailable',
  'runtime_stale',
  'route_unavailable',
  'coverage_missing',
  'no_eligible_routes',
  'scope_overflow',
] as const
