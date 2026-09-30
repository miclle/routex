import axios from 'axios'
import client from './client'
import type {
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

export async function getNotifications(status: NotificationReadStatus, signal?: AbortSignal) {
  return (
    await client.get<NotificationsPage>('/notifications', {
      params: { status },
      signal,
    })
  ).data
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
