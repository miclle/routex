export interface DeploymentCoverage {
  credential_id: string
  connection_id: string
  adapter: 'azure_openai_classic'
  api_version: string
  verification_status: 'pending' | 'verified' | 'failed'
  verified_at: string | null
  coverage_source: 'administrator_attestation'
  provider_models: { id: string; upstream_name: string; can_attest: boolean; attested: boolean }[]
  etag: string
  can_edit: boolean
}
export interface DeploymentCoverageInput {
  provider_model_ids: string[]
  reason: string
}
export interface DeploymentCoverageResult {
  coverage: DeploymentCoverage
  runtime_applied: true
  changed: boolean
}
