export interface ConnectionTestResult {
  connection_id: string
  credential_id: string
  outcome: 'passed' | 'failed'
  scope: 'model_discovery' | 'authentication_only'
  discovered_model_count: number | null
  checked_at: string
}
