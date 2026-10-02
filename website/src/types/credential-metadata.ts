import type { Credential } from './catalog'

export interface CredentialMetadata extends Credential {
  connection_id: string
  etag: string
}

export interface CredentialMetadataInput {
  name: string
  priority: number
  reason: string
}
