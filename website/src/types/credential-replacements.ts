export interface CredentialReplacementInput {
  storage_policy_etag?: string
  request_id: string
  name: string
  secret: string
  reason: string
}

export interface CredentialReplacementReceipt {
  storage_source: 'inline' | 'vault'
  id: string
  connection_id: string
  replaces_credential_id: string
}
