export type MonthlyQuotaBehavior = 'stop' | 'alert_only'
export interface LimitPolicy {
  // Canonical on User, Team aggregate and Personal Key owned policies; legacy omissions read as stop.
  tokens_month_behavior?: MonthlyQuotaBehavior
  money_month_behavior?: MonthlyQuotaBehavior
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
export interface QuotaWindow {
  covered: boolean
  tokens_used: number
  tokens_held: number
  tokens_unknown: number
  money_used: Record<string, string>
  money_held: Record<string, string>
  money_unknown: number
}
export interface QuotaUsage {
  as_of: string | null
  activated: boolean
  coverage_start: string | null
  time_zone: string
  active: QuotaWindow | null
  minute: QuotaWindow | null
  five_hours: QuotaWindow | null
  seven_days: QuotaWindow | null
  month: QuotaWindow | null
}
export interface LimitRecord {
  kind: 'user' | 'project' | 'personal_key' | 'project_key' | 'team' | 'team_member'
  team_id?: string
  editable_fields?: TeamLimitEditableField[]
  id: string
  account_id: string
  etag: string
  parent_etag?: string
  platform_currency: string
  stored: LimitPolicy
  effective: Omit<
    LimitPolicy,
    'ip_mode' | 'ip_ranges' | 'tokens_month_behavior' | 'money_month_behavior'
  >
  quota_usage: QuotaUsage | null
  ip_policies: LimitPolicy[]
  rpm_used: number | null
  active: number | null
  enforced: boolean
}
export interface LimitInput extends LimitPolicy {
  reason: string
}

export const teamLimitFields = [
  'tokens_5h',
  'tokens_7d',
  'tokens_month',
  'money_month',
  'rpm',
  'tpm',
  'concurrency',
] as const
export type TeamLimitField = (typeof teamLimitFields)[number]
export const teamMonthlyBehaviorFields = ['tokens_month_behavior', 'money_month_behavior'] as const
export type TeamMonthlyBehaviorField = (typeof teamMonthlyBehaviorFields)[number]
export type TeamLimitEditableField = TeamLimitField | TeamMonthlyBehaviorField
export interface TeamLimitScope {
  teamId: string
  userId?: string
}
export type TeamLimitInput = Partial<Pick<LimitPolicy, TeamLimitEditableField>> & {
  currency?: string
  reason: string
}
