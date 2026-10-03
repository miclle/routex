import type { LimitRecord } from './resource-limits'

export type DefaultLimitKind = 'user' | 'team'
export const defaultIntegerFields = [
  'tokens_5h',
  'tokens_7d',
  'tokens_month',
  'rpm',
  'tpm',
  'concurrency',
] as const
export type DefaultIntegerField = (typeof defaultIntegerFields)[number]
export type DefaultLimitPolicy = Record<DefaultIntegerField, number | null> & {
  money_month: string | null
  currency: string
}
export interface DefaultLimitRecord {
  kind: DefaultLimitKind
  rule_etag: string
  etag: string
  policy: DefaultLimitPolicy
  platform_currency: string
  editable: boolean
  updated_at: string
}
export interface DefaultLimitInput {
  policy: DefaultLimitPolicy
  reason: string
}
export interface DefaultResetTarget {
  kind: DefaultLimitKind
  id: string
}
export interface DefaultLimitResetContext extends DefaultResetTarget {
  etag: string
  limit: LimitRecord
  default_rule: DefaultLimitRecord
  applied_default_etag: string | null
  editable: boolean
}
export interface DefaultLimitResetResult extends DefaultResetTarget {
  saved: true
  limit: LimitRecord
  applied_default_etag: string
  default_reset_etag: string
  runtime_applied: boolean
}
