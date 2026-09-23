export interface LimitPolicy {
  tokens_5h?: number | null
  tokens_7d?: number | null
  tokens_month?: number | null
  tpm?: number | null
  money_month?: string | null
  currency?: string
  rpm: number | null
  concurrency: number | null
  ip_mode: 'none' | 'allowlist' | 'denylist'
  ip_ranges: string[]
}
export interface LimitRecord {
  kind: 'user' | 'project' | 'personal_key' | 'project_key'
  id: string
  account_id: string
  etag: string
  parent_etag?: string
  stored: LimitPolicy
  effective: Pick<LimitPolicy, 'rpm' | 'concurrency'>
  ip_policies: LimitPolicy[]
  rpm_used: number | null
  active: number | null
  enforced: boolean
}
export interface LimitInput extends LimitPolicy {
  reason: string
}
