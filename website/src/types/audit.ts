import type { ModelPrice, PricePage } from './pricing'
import type { LimitPolicy } from './resource-limits'

export type AuditRange = '24h' | '7d' | '30d'
export type AuditCategory =
  'all' | 'models' | 'keys' | 'limits' | 'credentials' | 'pricing' | 'identity' | 'site'
export interface AuditFilters {
  range: AuditRange
  category: AuditCategory
  q: string
}
type AuditValues =
  | LimitPolicy
  | { etag: string; items: ModelPrice[] }
  | { etag: string; currency: PricePage['currency'] }
export interface AuditChanges {
  before: AuditValues
  after: AuditValues
  reason?: string
  etag?: string
}
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
