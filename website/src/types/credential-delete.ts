export interface CredentialDeleteInput {
  reason: string
}

export interface CredentialDeleteResult {
  id: string
  absent: true
  runtime_applied: true
}
