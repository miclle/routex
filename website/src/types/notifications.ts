export type NotificationSeverity = 'high' | 'medium' | 'low'
export type NotificationReadStatus = 'unread' | 'all'
export type NotificationDeliveryStatus =
  'pending' | 'retry' | 'sending' | 'accepted' | 'failed' | 'unknown'

export interface Notification {
  id: string
  alert_id: string
  kind: string
  detail_code: string
  severity: NotificationSeverity
  occurrence_count: number
  read: boolean
  first_seen_at: string
  last_seen_at: string
  read_at: string | null
  delivery_status: NotificationDeliveryStatus | null
  delivery_code: string | null
  delivery_attempts: number
  delivery_updated_at: string | null
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
