import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface GoogleMethod {
  available: boolean
  name: string
}
export interface GoogleIdentity extends GoogleMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface GoogleConfig {
  name: string
  client_id: string
  callback_url: string
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface GoogleConfigInput {
  name: string
  client_id: string
  callback_url: string
  secret_action: 'keep' | 'replace'
  client_secret: string
  reason: string
}
export interface GoogleLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type GoogleCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
