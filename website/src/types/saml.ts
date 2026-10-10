import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface SAMLMethod {
  available: boolean
  name: string
}
export interface SAMLIdentity extends SAMLMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface SAMLConfig {
  name: string
  idp_issuer: string
  sso_url: string
  sp_entity_id: string
  acs_url: string
  signing_certificate_pem: string
  enabled: boolean
  verified: boolean
  mfa_required: boolean
  review_etag: string
}
export interface SAMLConfigInput {
  name: string
  idp_issuer: string
  sso_url: string
  sp_entity_id: string
  acs_url: string
  signing_certificate_pem: string
  reason: string
}
export interface SAMLProofInput {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type SAMLCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
