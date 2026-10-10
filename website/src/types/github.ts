import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface GitHubMethod {
  available: boolean
  name: string
}
export interface GitHubIdentity extends GitHubMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface GitHubConfig {
  name: string
  client_id: string
  callback_url: string
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface GitHubConfigInput {
  name: string
  client_id: string
  callback_url: string
  secret_action: 'keep' | 'replace'
  client_secret: string
  reason: string
}
export interface GitHubLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type GitHubCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
