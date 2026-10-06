import type { NotificationSeverity } from './notifications'
import type { ProviderQualitySummary } from './provider-quality'
import type { UsageCount } from './usage'

export interface OverviewToday {
  calls: number
  tokens: UsageCount
  success_rate: number | null
  active_principals: number
}

export interface OverviewTrendPoint {
  date: string
  tokens: UsageCount
}

export type ProviderReadinessStatus = 'ready' | 'degraded' | 'unconfigured'
export type ProviderQualityUnavailableReason = 'query_budget' | 'range_too_large' | 'invalid_policy'

export interface OverviewProvider {
  provider_id: string
  name: string
  connection_count: number
  ready_connection_count: number
  credential_count: number
  model_count: number
  status: ProviderReadinessStatus
  quality?: ProviderQualitySummary | null
  quality_unavailable_reason?: ProviderQualityUnavailableReason | null
}

export interface OverviewProviderReadiness {
  providers: number
  connections: number
  ready_connections: number
  unready_connections: number
  items: OverviewProvider[]
}

export interface OverviewModel {
  id: string
  name: string
  calls: number
  tokens: UsageCount
}

export type OperationalAlertState = 'open' | 'handling' | 'resolved'

export interface OperationalAlert {
  id: string
  kind: string
  detail_code: string
  severity: NotificationSeverity
  state: OperationalAlertState
  occurrence_count: number
  first_seen_at: string
  last_seen_at: string
  updated_at: string
  etag: string
  subject_type?: string | null
  subject_id?: string | null
  subject_name?: string | null
}

export interface AdminOverview {
  observed_at: string
  today: OverviewToday
  token_trend: OverviewTrendPoint[]
  provider_readiness: OverviewProviderReadiness
  top_models: OverviewModel[]
  alerts: OperationalAlert[]
}

export interface UpdateOperationalAlertInput {
  state: Exclude<OperationalAlertState, 'open'>
  etag: string
}

export interface OverviewAccountUsage {
  as_of: string
  time_zone: string
  month_start: string
  month_end: string
  covered: boolean
  tokens_used: string
  tokens_held: string
  tokens_unknown: string
  money_used: Record<string, string>
  money_held: Record<string, string>
  money_unknown: string
}

export interface MonthlyAccount {
  account_id: string
  policy_etag: string
  tokens_month: string | null
  money_month: string | null
  currency: string | null
  runtime_applied: boolean
  usage_status: 'active' | 'inactive' | 'unavailable'
  usage: OverviewAccountUsage | null
  active_reservations: OverviewActiveReservations | null
}

export interface OverviewActiveReservations {
  tokens_held: string
  money_held: Record<string, string>
}

export interface OverviewTeamAccount {
  id: string
  name: string
  membership_id: string
  aggregate: MonthlyAccount
  member: MonthlyAccount
}

export interface OverviewAccountsPage {
  actor_user_id: string
  observed_at: string
  platform_currency: string
  personal: MonthlyAccount
  teams: OverviewTeamAccount[]
  next_cursor: string | null
}

export interface OverviewRoleLabel {
  id: string
  name: string | null
  builtin: boolean
  assignment_kind: 'explicit'
}

export interface OverviewRolesPage {
  actor_user_id: string
  observed_at: string
  identity_role: 'admin' | 'member'
  roles: OverviewRoleLabel[]
  next_cursor: string | null
}
