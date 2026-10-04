export interface MemberKeyPolicy {
  tokens_5h: number | null
  tokens_7d: number | null
  tokens_month: number | null
  tpm: number | null
  rpm: number | null
  concurrency: number | null
  money_month: string | null
  currency: string
}
export interface MemberKeyWindow {
  covered: boolean
  tokens_used: string
  tokens_held: string
  tokens_unknown: string
  money_used: Record<string, string>
  money_held: Record<string, string>
  money_unknown: string
}
export interface MemberKeyUsage {
  as_of: string | null
  activated: boolean
  coverage_start: string | null
  time_zone: string
  active: MemberKeyWindow | null
  minute: MemberKeyWindow | null
  five_hours: MemberKeyWindow | null
  seven_days: MemberKeyWindow | null
  month: MemberKeyWindow | null
}
export interface MemberKeyRecord {
  id: string
  name: string
  status: 'pending' | 'active' | 'disabled' | 'revoked'
  expired: boolean
  model_ids: string[]
  expires_at: string | null
  created_at: string
  updated_at: string
  etag: string
  disable_eligible: boolean
  last_used_at: string | null
  last_use_coverage: 'recorded' | 'no_recorded_use' | 'unknown'
  limits: {
    platform_currency: string
    quota_root_id: string
    shared_rotation_quota: boolean
    stored: MemberKeyPolicy
    effective: MemberKeyPolicy
    quota_usage: MemberKeyUsage | null
    rpm_used: string | null
    active: string | null
    enforced: boolean
  }
}
export interface MemberKeyPage {
  items: MemberKeyRecord[]
  next_cursor: string | null
}
export interface MemberKeyDisableResult {
  user_id: string
  id: string
  status: 'disabled'
  etag: string
  runtime_applied: true
  confirmation: 'current_disabled_state'
}
