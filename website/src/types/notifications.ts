export type NotificationSeverity = 'high' | 'medium' | 'low'
export type NotificationReadStatus = 'unread' | 'all'
export type NotificationDeliveryStatus =
  'pending' | 'retry' | 'sending' | 'accepted' | 'failed' | 'unknown'

export interface MonthlyQuotaNotificationSnapshot {
  scope_kind: 'user' | 'project' | 'team' | 'team_member'
  team_id?: string
  member_user_id?: string
  scope_id: string
  dimension: 'tokens' | 'money'
  policy_revision: string
  month_start: string
  month_end: string
  time_zone: string
  as_of: string
  limit: string
  settled: string
  currency: string | null
}

export interface MonthlyQuotaWarningSnapshot extends Omit<
  MonthlyQuotaNotificationSnapshot,
  'scope_kind' | 'team_id' | 'member_user_id'
> {
  scope_kind: 'user' | 'team' | 'project' | 'team_member' | 'personal_key' | 'project_key'
  level: 'near' | 'critical'
  threshold: 80 | 90
  threshold_generation:
    | 'personal-monthly-80-90-v1'
    | 'team-monthly-80-90-v1'
    | 'project-monthly-80-90-v1'
    | 'team-member-monthly-80-90-v1'
    | 'personal-key-monthly-80-90-v1'
    | 'project-key-monthly-80-90-v1'
}

export interface PersonalRollingQuotaWarningSnapshot {
  scope_kind: 'user'
  scope_id: string
  window_kind: '5h' | '7d'
  episode_id: string
  policy_revision: string
  window_start: string
  window_end: string
  as_of: string
  coverage_start: string
  resource_created_at: string
  time_zone: string
  limit: string
  settled: string
  level: 'near' | 'critical'
  threshold: 80 | 90
  threshold_generation: 'personal-rolling-80-90-v1'
}

export interface Notification {
  rolling_quota_warning_observation_id?: string
  rolling_quota_warning?: PersonalRollingQuotaWarningSnapshot
  id: string
  alert_id?: string
  quota_observation_id?: string
  quota_warning_observation_id?: string
  quota_warning?: MonthlyQuotaWarningSnapshot
  quota?: MonthlyQuotaNotificationSnapshot
  kind: string
  detail_code: string
  severity: NotificationSeverity
  occurrence_count: number
  read: boolean
  first_seen_at: string
  last_seen_at: string
  read_at: string | null
  delivery_status?: NotificationDeliveryStatus | null
  delivery_code?: string | null
  delivery_attempts?: number
  delivery_updated_at?: string | null
  subject_type?: string | null
  subject_id?: string | null
  subject_name?: string | null
}

export interface NotificationsPage {
  items: Notification[]
  next_cursor: string | null
  unread_count: number
}

export interface NotificationSettings {
  etag: string
  in_app_enabled: true
  external_email: string
  email_high: boolean
  email_medium: boolean
  updated_at: string
}

export interface UpdateNotificationSettingsInput {
  external_email: string
  email_high: boolean
  email_medium: boolean
  etag: string
}
