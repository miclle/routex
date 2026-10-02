export interface CredentialReplacementInput {
  request_id: string
  name: string
  secret: string
  reason: string
}

export interface CredentialReplacementReceipt {
  id: string
  connection_id: string
  replaces_credential_id: string
}
