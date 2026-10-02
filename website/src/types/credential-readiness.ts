import type { CredentialMetadata } from './credential-metadata'

export const credentialReadinessBlockers = [
  'source_disabled',
  'replacement_disabled',
  'replacement_unverified',
  'runtime_unavailable',
  'runtime_stale',
  'route_unavailable',
  'coverage_missing',
  'no_eligible_routes',
  'scope_overflow',
  'evidence_missing',
] as const

export interface CredentialReadiness {
  source: CredentialMetadata
  replacement: CredentialMetadata
  snapshot_id: string | null
  evidence: { attempt_id: string; completed_at: string } | null
  eligible_route_count: number
  eligible: boolean
  blockers: string[]
  etag: string
}
