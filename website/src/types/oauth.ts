import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export interface OAuthMethod {
  available: boolean
  name: string
}
export interface OAuthIdentity extends OAuthMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface OAuthConfig {
  name: string
  authorization_url: string
  token_url: string
  user_info_url: string
  client_auth_method: '' | 'client_secret_basic' | 'client_secret_post'
  scopes: string[]
  subject_path: string[]
  client_id: string
  callback_url: string
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface OAuthConfigInput {
  name: string
  authorization_url: string
  token_url: string
  user_info_url: string
  client_auth_method: '' | 'client_secret_basic' | 'client_secret_post'
  scopes: string[]
  subject_path: string[]
  client_id: string
  callback_url: string
  secret_action: 'keep' | 'replace'
  client_secret: string
  reason: string
}
export interface OAuthLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export type OAuthCompletion =
  | { kind: 'session'; session: Session }
  | { kind: 'challenge'; challenge: MFAChallenge }
  | { kind: 'bound' | 'verified' }
