import axios from 'axios'
import client from './client'
import type {
  Notification,
  NotificationReadStatus,
  NotificationSettings,
  NotificationsPage,
  UpdateNotificationSettingsInput,
} from '@/types/notifications'

export const notificationsKey = (recipientId: string, status: NotificationReadStatus) =>
  ['notifications', recipientId, status] as const
export const notificationSettingsKey = (recipientId: string) =>
  ['notification-settings', recipientId] as const

export class NotificationError extends Error {
  constructor(public readonly status: number) {
    super('Notification request failed')
  }
}

function statusOf(error: unknown) {
  return axios.isAxiosError(error) ? (error.response?.status ?? 0) : 0
}

function validNotification(value: unknown): value is Notification {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const item = value as Record<string, unknown>
  return (
    ['id', 'kind', 'detail_code', 'first_seen_at', 'last_seen_at'].every(
      (field) => typeof item[field] === 'string',
    ) &&
    item.id !== '' &&
    ['high', 'medium', 'low'].includes(item.severity as string) &&
    typeof item.read === 'boolean' &&
    Number.isSafeInteger(item.occurrence_count) &&
    (item.occurrence_count as number) >= 0 &&
    [
      'alert_id',
      'quota_observation_id',
      'read_at',
      'subject_type',
      'subject_id',
      'subject_name',
      'delivery_status',
      'delivery_code',
      'delivery_updated_at',
    ].every((field) => item[field] == null || typeof item[field] === 'string') &&
    (item.quota == null || (typeof item.quota === 'object' && !Array.isArray(item.quota)))
  )
}

export async function getNotifications(
  status: NotificationReadStatus,
  cursor?: string | null,
  signal?: AbortSignal,
) {
  const data = (
    await client.get<NotificationsPage>('/notifications', {
      params: { status, cursor: cursor || undefined },
      signal,
    })
  ).data
  if (
    !data ||
    !Array.isArray(data.items) ||
    !data.items.every(validNotification) ||
    !Number.isSafeInteger(data.unread_count) ||
    data.unread_count < 0 ||
    (data.next_cursor != null && typeof data.next_cursor !== 'string')
  )
    throw new Error('Invalid notification response')
  return { ...data, next_cursor: data.next_cursor ?? null }
}

export async function markNotificationRead(id: string, csrf: string) {
  try {
    await client.post(`/notifications/${encodeURIComponent(id)}/read`, null, {
      headers: { 'X-CSRF-Token': csrf },
    })
  } catch (error) {
    throw new NotificationError(statusOf(error))
  }
}

export async function markAllNotificationsRead(csrf: string) {
  try {
    await client.post('/notifications/read-all', null, {
      headers: { 'X-CSRF-Token': csrf },
    })
  } catch (error) {
    throw new NotificationError(statusOf(error))
  }
}

export async function getNotificationSettings(signal?: AbortSignal) {
  const response = await client.get<NotificationSettings>('/notification-settings', { signal })
  return {
    ...response.data,
    etag: response.data.etag || response.headers.etag || '',
  }
}

export async function updateNotificationSettings(
  input: UpdateNotificationSettingsInput,
  csrf: string,
) {
  try {
    const response = await client.put<NotificationSettings>('/notification-settings', input, {
      headers: { 'X-CSRF-Token': csrf, 'If-Match': input.etag },
    })
    return {
      ...response.data,
      etag: response.data.etag || response.headers.etag || '',
    }
  } catch (error) {
    throw new NotificationError(statusOf(error))
  }
}
