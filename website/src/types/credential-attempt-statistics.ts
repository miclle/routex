export interface CredentialAttemptStatisticsItem {
  credential_id: string
  connection_id: string
  inspected_attempts: number
  has_more: boolean
  last_attempt: {
    state: 'no_records' | 'recorded' | 'unknown'
    completed_at: string | null
  }
  failure_streak: {
    state: 'no_records' | 'exact' | 'lower_bound' | 'unknown'
    count: number | null
    lower_bound: number
  }
  recent_error: {
    state: 'no_records' | 'recorded' | 'none' | 'unknown'
    code: string | null
    completed_at: string | null
  }
}
export interface CredentialAttemptStatistics {
  provider_id: string
  observed_at: string
  attempt_limit: 100
  recorded_only: true
  items: CredentialAttemptStatisticsItem[]
}
export interface CredentialAttemptTarget {
  credentialId: string
  connectionId: string
}
