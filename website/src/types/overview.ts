import type { NotificationSeverity } from './notifications'
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

export interface OverviewProvider {
  provider_id: string
  name: string
  connection_count: number
  ready_connection_count: number
  credential_count: number
  model_count: number
  status: ProviderReadinessStatus
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
