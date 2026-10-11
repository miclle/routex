import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface TelegramMethod {
  available: boolean
  name: string
}
export interface TelegramIdentity extends TelegramMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface TelegramConfig {
  name: string
  client_id: string
  callback_url: string
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface TelegramConfigInput {
  name: string
  client_id: string
  callback_url: string
  secret_action: 'keep' | 'replace'
  client_secret: string
  reason: string
}
export interface TelegramLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type TelegramCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
