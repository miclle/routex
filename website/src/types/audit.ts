import type { ModelPrice, PricePage } from './pricing'
import type { LimitPolicy } from './resource-limits'

export type AuditRange = '24h' | '7d' | '30d'
export type AuditCategory =
  'all' | 'models' | 'keys' | 'limits' | 'credentials' | 'pricing' | 'identity' | 'site' | 'system'
export interface AuditFilters {
  range: AuditRange
  category: AuditCategory
  q: string
}
type AuditValues =
  | LimitPolicy
  | { name: string; priority: number }
  | { id: string; connection_id: string; name: string; priority: number }
  | { source_id: string; connection_id: string; name: string; priority: number }
  | { absent: true }
  | { etag: string; items: ModelPrice[] }
  | { etag: string; currency: PricePage['currency'] }
export interface AuditBeforeAfterChanges {
  before: AuditValues
  after: AuditValues
  reason?: string
  etag?: string
}
export interface SystemInstanceCleanupAuditChanges {
  revision: number
}
export interface OIDCIdentityAuditChanges {
  kind: 'oidc_identity'
  reason: string
}
export interface LDAPIdentityAuditChanges {
  kind: 'ldap_identity'
  reason: string
}
export interface OAuthIdentityAuditChanges {
  kind: 'oauth_identity'
  reason: string
}
export interface SAMLIdentityAuditChanges {
  kind: 'saml_identity'
  reason: string
}
export type AuditChanges =
  | AuditBeforeAfterChanges
  | SystemInstanceCleanupAuditChanges
  | OIDCIdentityAuditChanges
  | OAuthIdentityAuditChanges
  | LDAPIdentityAuditChanges
  | SAMLIdentityAuditChanges
export interface AuditRecord {
  id: string
  actor_id: string
  actor_name: string
  action: string
  resource_type: string
  resource_id: string
  created_at: string
  result: 'committed'
  source: string | null
  ip: null
  request_id: null
  changes: AuditChanges | null
}
export interface AuditPage {
  items: AuditRecord[]
  next_cursor: string | null
}
