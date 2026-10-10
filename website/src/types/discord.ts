import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface DiscordMethod {
  available: boolean
  name: string
}
export interface DiscordIdentity extends DiscordMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface DiscordConfig {
  name: string
  client_id: string
  callback_url: string
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface DiscordConfigInput {
  name: string
  client_id: string
  callback_url: string
  secret_action: 'keep' | 'replace'
  client_secret: string
  reason: string
}
export interface DiscordLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type DiscordCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
