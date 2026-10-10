import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface OIDCMethod {
  available: boolean
  name: string
}
export interface OIDCIdentity extends OIDCMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface OIDCConfig {
  name: string
  issuer: string
  client_id: string
  callback_url: string
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface OIDCConfigInput {
  name: string
  issuer: string
  client_id: string
  callback_url: string
  secret_action: 'keep' | 'replace'
  client_secret: string
  reason: string
}
export interface OIDCLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type OIDCCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
