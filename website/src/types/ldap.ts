import type { Session } from './auth'
import type { MFAChallenge, MFAProof } from './mfa'

export type LDAPIdentityAttribute = '' | 'entryUUID' | 'objectGUID'
export interface LDAPMethod {
  available: boolean
  name: string
}
export interface LDAPIdentity extends LDAPMethod {
  bound: boolean
  mfa_required: boolean
  review_etag: string
}
export interface LDAPConfig {
  name: string
  endpoint: string
  bind_dn: string
  base_dn: string
  user_filter: string
  identity_attribute: LDAPIdentityAttribute
  secret_configured: boolean
  mfa_required: boolean
  enabled: boolean
  verified: boolean
  review_etag: string
}
export interface LDAPConfigInput {
  name: string
  endpoint: string
  bind_dn: string
  base_dn: string
  user_filter: string
  identity_attribute: LDAPIdentityAttribute
  secret_action: 'keep' | 'replace'
  bind_password: string
  reason: string
}
export interface LDAPLocalProof {
  password: string
  proof: MFAProof | Record<string, never>
  reason: string
}
export interface LDAPBindingProof extends LDAPLocalProof {
  username: string
  directory_password: string
}
export type LDAPLoginResult =
  { kind: 'session'; session: Session } | { kind: 'challenge'; challenge: MFAChallenge }
