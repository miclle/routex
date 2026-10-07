import axios from 'axios'
import client from './client'
import type {
  Notification,
  PersonalRollingQuotaWarningSnapshot,
  MonthlyQuotaNotificationSnapshot,
  MonthlyQuotaWarningSnapshot,
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

function invalidRecordedText(value: string) {
  return [...value].some((character) => {
    const code = character.codePointAt(0)!
    return code < 32 || (code >= 127 && code <= 159) || (code >= 0xd800 && code <= 0xdfff)
  })
}

export function recordedMonthlyQuota(
  notification: Notification,
  recipientId?: string,
): MonthlyQuotaNotificationSnapshot | undefined {
  const quota = notification.quota
  if (
    notification.kind !== 'monthly_quota_exhausted' ||
    !quota ||
    !['user', 'project', 'team', 'team_member'].includes(quota.scope_kind) ||
    typeof quota.scope_id !== 'string' ||
    !quota.scope_id.trim() ||
    typeof quota.policy_revision !== 'string' ||
    !quota.policy_revision.trim() ||
    typeof quota.time_zone !== 'string' ||
    !quota.time_zone.trim() ||
    typeof quota.month_start !== 'string' ||
    typeof quota.month_end !== 'string' ||
    typeof quota.as_of !== 'string' ||
    !Number.isFinite(Date.parse(quota.month_start)) ||
    !Number.isFinite(Date.parse(quota.month_end)) ||
    !Number.isFinite(Date.parse(quota.as_of))
  )
    return undefined
  const start = Date.parse(quota.month_start)
  const end = Date.parse(quota.month_end)
  const asOf = Date.parse(quota.as_of)
  if (
    end <= start ||
    asOf < start ||
    asOf >= end ||
    (notification.subject_type != null && notification.subject_type !== quota.scope_kind) ||
    (notification.subject_id != null && notification.subject_id !== quota.scope_id) ||
    (quota.scope_kind === 'team' &&
      (notification.subject_type !== 'team' || notification.subject_id !== quota.scope_id))
  )
    return undefined
  const tokens = quota.dimension === 'tokens'
  const pattern = tokens ? /^\d+$/ : /^\d+(?:\.\d+)?$/
  if (
    typeof quota.limit !== 'string' ||
    typeof quota.settled !== 'string' ||
    !pattern.test(quota.limit) ||
    !pattern.test(quota.settled) ||
    (tokens
      ? notification.detail_code !== 'tokens_month_exhausted' || quota.currency !== null
      : quota.dimension !== 'money' ||
        notification.detail_code !== 'money_month_exhausted' ||
        typeof quota.currency !== 'string' ||
        !quota.currency.trim())
  )
    return undefined
  if (quota.scope_kind === 'team_member') {
    const safeId = /^[A-Za-z0-9_-]{1,30}$/
    const tokensAmount = /^(0|[1-9]\d{0,18})$/
    const moneyAmount = /^(0|[1-9]\d{0,59})(?:\.\d{1,18})?$/
    if (
      !recipientId ||
      !safeId.test(recipientId) ||
      typeof quota.team_id !== 'string' ||
      !safeId.test(quota.team_id) ||
      quota.member_user_id !== recipientId ||
      !/^[A-Z2-7]{51}[AQ]$/.test(quota.scope_id) ||
      notification.subject_type !== 'team_member' ||
      notification.subject_id !== quota.scope_id ||
      (notification.subject_name != null &&
        (new TextEncoder().encode(notification.subject_name).length > 100 ||
          invalidRecordedText(notification.subject_name))) ||
      !/^[A-Za-z0-9_-]{1,64}$/.test(quota.policy_revision) ||
      quota.time_zone.trim() !== quota.time_zone ||
      quota.time_zone.length > 100 ||
      invalidRecordedText(quota.time_zone) ||
      notification.severity !== 'high' ||
      notification.occurrence_count !== 1 ||
      notification.alert_id != null ||
      notification.delivery_status != null ||
      (tokens
        ? !tokensAmount.test(quota.limit) ||
          !tokensAmount.test(quota.settled) ||
          BigInt(quota.limit) > 9223372036854775807n ||
          BigInt(quota.settled) > 9223372036854775807n
        : !moneyAmount.test(quota.limit) ||
          quota.limit.length > 40 ||
          !moneyAmount.test(quota.settled) ||
          !/^[A-Z]{3}$/.test(quota.currency ?? ''))
    )
      return undefined
    try {
      new Intl.DateTimeFormat('en', { timeZone: quota.time_zone })
    } catch {
      return undefined
    }
  }
  return quota
}

export function recordedMonthlyQuotaWarning(
  notification: Notification,
  recipientId: string,
): MonthlyQuotaWarningSnapshot | undefined {
  const warning = notification.quota_warning
  const safeId = /^[A-Za-z0-9_-]{1,30}$/
  if (
    notification.kind !== 'monthly_quota_warning' ||
    !warning ||
    !safeId.test(recipientId) ||
    !(
      (warning.scope_kind === 'user' &&
        warning.scope_id === recipientId &&
        warning.threshold_generation === 'personal-monthly-80-90-v1') ||
      (warning.scope_kind === 'team_member' &&
        typeof warning.scope_id === 'string' &&
        /^[A-Z2-7]{51}[AQ]$/.test(warning.scope_id) &&
        warning.threshold_generation === 'team-member-monthly-80-90-v1' &&
        !Object.hasOwn(warning, 'team_id') &&
        !Object.hasOwn(warning, 'member_user_id') &&
        (notification.subject_name == null ||
          (typeof notification.subject_name === 'string' &&
            !invalidRecordedText(notification.subject_name)))) ||
      (warning.scope_kind === 'project_key' &&
        typeof warning.scope_id === 'string' &&
        safeId.test(warning.scope_id) &&
        warning.threshold_generation === 'project-key-monthly-80-90-v1' &&
        !Object.hasOwn(warning, 'team_id') &&
        !Object.hasOwn(warning, 'member_user_id') &&
        (notification.subject_name == null ||
          (typeof notification.subject_name === 'string' &&
            !invalidRecordedText(notification.subject_name)))) ||
      (warning.scope_kind === 'personal_key' &&
        typeof warning.scope_id === 'string' &&
        safeId.test(warning.scope_id) &&
        warning.threshold_generation === 'personal-key-monthly-80-90-v1' &&
        !Object.hasOwn(warning, 'team_id') &&
        !Object.hasOwn(warning, 'member_user_id') &&
        (notification.subject_name == null ||
          (typeof notification.subject_name === 'string' &&
            !invalidRecordedText(notification.subject_name)))) ||
      ((warning.scope_kind === 'team' || warning.scope_kind === 'project') &&
        typeof warning.scope_id === 'string' &&
        safeId.test(warning.scope_id) &&
        warning.threshold_generation ===
          (warning.scope_kind === 'team' ? 'team-monthly-80-90-v1' : 'project-monthly-80-90-v1') &&
        (notification.subject_name == null ||
          (typeof notification.subject_name === 'string' &&
            !invalidRecordedText(notification.subject_name))))
    ) ||
    notification.subject_type !== warning.scope_kind ||
    notification.subject_id !== warning.scope_id ||
    !safeId.test(notification.id) ||
    !notification.id.startsWith(
      warning.scope_kind === 'project_key'
        ? 'jwi_'
        : warning.scope_kind === 'personal_key'
          ? 'kwi_'
          : warning.scope_kind === 'team_member'
            ? 'mwi_'
            : warning.scope_kind === 'team'
              ? 'twi_'
              : warning.scope_kind === 'project'
                ? 'pwi_'
                : 'qwi_',
    ) ||
    typeof notification.quota_warning_observation_id !== 'string' ||
    !safeId.test(notification.quota_warning_observation_id) ||
    !notification.quota_warning_observation_id.startsWith(
      warning.scope_kind === 'project_key'
        ? 'jwo_'
        : warning.scope_kind === 'personal_key'
          ? 'kwo_'
          : warning.scope_kind === 'team_member'
            ? 'mwo_'
            : warning.scope_kind === 'team'
              ? 'two_'
              : warning.scope_kind === 'project'
                ? 'pwo_'
                : 'qwo_',
    ) ||
    notification.quota != null ||
    notification.quota_observation_id != null ||
    notification.alert_id != null ||
    notification.delivery_status != null ||
    notification.occurrence_count !== 1 ||
    !(
      (warning.level === 'near' &&
        warning.threshold === 80 &&
        notification.severity === 'medium') ||
      (warning.level === 'critical' && warning.threshold === 90 && notification.severity === 'high')
    ) ||
    notification.detail_code !== `${warning.dimension}_month_${warning.level}` ||
    typeof warning.policy_revision !== 'string' ||
    !/^[A-Za-z0-9_-]{1,64}$/.test(warning.policy_revision) ||
    typeof warning.time_zone !== 'string' ||
    warning.time_zone.trim() !== warning.time_zone ||
    !warning.time_zone ||
    warning.time_zone.length > 100 ||
    invalidRecordedText(warning.time_zone) ||
    typeof warning.month_start !== 'string' ||
    typeof warning.month_end !== 'string' ||
    typeof warning.as_of !== 'string' ||
    !Number.isFinite(Date.parse(warning.month_start)) ||
    !Number.isFinite(Date.parse(warning.month_end)) ||
    !Number.isFinite(Date.parse(warning.as_of)) ||
    Date.parse(warning.month_end) <= Date.parse(warning.month_start) ||
    Date.parse(warning.as_of) < Date.parse(warning.month_start) ||
    Date.parse(warning.as_of) >= Date.parse(warning.month_end) ||
    typeof warning.limit !== 'string' ||
    typeof warning.settled !== 'string'
  )
    return undefined
  const tokens = warning.dimension === 'tokens'
  const tokenAmount = /^(0|[1-9]\d{0,18})$/
  const moneyAmount = /^(0|[1-9]\d{0,59})(?:\.\d{1,18})?$/
  if (
    tokens
      ? warning.currency !== null ||
        !tokenAmount.test(warning.limit) ||
        !tokenAmount.test(warning.settled) ||
        BigInt(warning.limit) <= 0n ||
        BigInt(warning.limit) > 9223372036854775807n ||
        BigInt(warning.settled) > 9223372036854775807n
      : warning.dimension !== 'money' ||
        !/^[A-Z]{3}$/.test(warning.currency ?? '') ||
        !moneyAmount.test(warning.limit) ||
        warning.limit.length > 40 ||
        !moneyAmount.test(warning.settled) ||
        warning.settled.length > 80 ||
        /^0(?:\.0+)?$/.test(warning.limit)
  )
    return undefined
  try {
    new Intl.DateTimeFormat('en', { timeZone: warning.time_zone })
  } catch {
    return undefined
  }
  return warning
}

export function recordedPersonalRollingQuotaWarning(
  notification: Notification,
  recipientId: string,
): PersonalRollingQuotaWarningSnapshot | undefined {
  const warning = notification.rolling_quota_warning
  if (!warning || notification.kind !== 'personal_rolling_quota_warning') return undefined
  const fields = [
    'scope_kind',
    'scope_id',
    'window_kind',
    'episode_id',
    'policy_revision',
    'window_start',
    'window_end',
    'as_of',
    'coverage_start',
    'resource_created_at',
    'time_zone',
    'limit',
    'settled',
    'level',
    'threshold',
    'threshold_generation',
  ]
  const itemFields = [
    'id',
    'kind',
    'detail_code',
    'severity',
    'occurrence_count',
    'read',
    'first_seen_at',
    'last_seen_at',
    'read_at',
    'subject_type',
    'subject_id',
    'rolling_quota_warning_observation_id',
    'rolling_quota_warning',
  ]
  const safeId = /^[A-Za-z0-9_-]{1,30}$/
  const tokens = /^(0|[1-9]\d{0,18})$/
  if (
    Object.keys(warning).length !== fields.length ||
    Object.keys(warning).some((key) => !fields.includes(key)) ||
    Object.keys(notification).some((key) => !itemFields.includes(key)) ||
    !safeId.test(recipientId) ||
    warning.scope_kind !== 'user' ||
    warning.scope_id !== recipientId ||
    notification.subject_type !== 'user' ||
    notification.subject_id !== recipientId ||
    typeof notification.id !== 'string' ||
    !safeId.test(notification.id) ||
    !notification.id.startsWith('rwi_') ||
    typeof notification.rolling_quota_warning_observation_id !== 'string' ||
    !safeId.test(notification.rolling_quota_warning_observation_id) ||
    !notification.rolling_quota_warning_observation_id.startsWith('rwo_') ||
    typeof warning.episode_id !== 'string' ||
    !safeId.test(warning.episode_id) ||
    !warning.episode_id.startsWith('rwe_') ||
    typeof warning.policy_revision !== 'string' ||
    !/^[A-Za-z0-9_-]{1,64}$/.test(warning.policy_revision) ||
    warning.threshold_generation !== 'personal-rolling-80-90-v1' ||
    !['5h', '7d'].includes(warning.window_kind) ||
    notification.detail_code !== `tokens_${warning.window_kind}_${warning.level}` ||
    notification.occurrence_count !== 1 ||
    typeof notification.read !== 'boolean' ||
    typeof warning.time_zone !== 'string' ||
    !warning.time_zone ||
    warning.time_zone === 'Local' ||
    warning.time_zone.length > 100 ||
    warning.time_zone.trim() !== warning.time_zone ||
    invalidRecordedText(warning.time_zone) ||
    typeof warning.limit !== 'string' ||
    !tokens.test(warning.limit) ||
    warning.limit === '0' ||
    typeof warning.settled !== 'string' ||
    !tokens.test(warning.settled) ||
    BigInt(warning.limit) > 9223372036854775807n ||
    BigInt(warning.settled) > 9223372036854775807n ||
    !(
      (warning.level === 'near' &&
        warning.threshold === 80 &&
        notification.severity === 'medium') ||
      (warning.level === 'critical' && warning.threshold === 90 && notification.severity === 'high')
    )
  )
    return undefined
  const scaled = BigInt(warning.settled) * 100n
  const cap = BigInt(warning.limit)
  if (scaled < cap * BigInt(warning.threshold) || (warning.level === 'near' && scaled >= cap * 90n))
    return undefined
  const timestamps = [
    warning.window_start,
    warning.window_end,
    warning.as_of,
    warning.coverage_start,
    warning.resource_created_at,
    notification.first_seen_at,
    notification.last_seen_at,
  ]
  if (
    !timestamps.every((value) => typeof value === 'string' && Number.isFinite(Date.parse(value))) ||
    (notification.read_at != null &&
      (typeof notification.read_at !== 'string' ||
        !Number.isFinite(Date.parse(notification.read_at))))
  )
    return undefined
  const [start, end, asOf, coverage, birth, first, last] = timestamps.map((value) =>
    Date.parse(value),
  )
  const duration = warning.window_kind === '5h' ? 5 * 60 * 60 * 1000 : 7 * 24 * 60 * 60 * 1000
  if (
    end !== asOf ||
    asOf - start !== duration ||
    Math.max(start, birth) < coverage ||
    coverage > asOf ||
    birth > asOf ||
    first !== asOf ||
    last !== asOf
  )
    return undefined
  try {
    new Intl.DateTimeFormat('en', { timeZone: warning.time_zone })
  } catch {
    return undefined
  }
  return warning
}

function validNotification(value: unknown, recipientId?: string): value is Notification {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const item = value as Record<string, unknown>
  if (
    item.kind === 'personal_rolling_quota_warning' ||
    item.rolling_quota_warning != null ||
    item.rolling_quota_warning_observation_id != null
  )
    return (
      !!recipientId &&
      !!recordedPersonalRollingQuotaWarning(item as unknown as Notification, recipientId)
    )
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
      'quota_warning_observation_id',
      'read_at',
      'subject_type',
      'subject_id',
      'subject_name',
      'delivery_status',
      'delivery_code',
      'delivery_updated_at',
    ].every((field) => item[field] == null || typeof item[field] === 'string') &&
    (item.quota == null || (typeof item.quota === 'object' && !Array.isArray(item.quota))) &&
    (item.quota_warning == null ||
      (typeof item.quota_warning === 'object' && !Array.isArray(item.quota_warning))) &&
    (!(
      item.subject_type === 'team_member' ||
      (item.quota as Record<string, unknown> | undefined)?.scope_kind === 'team_member'
    ) ||
      (item.kind === 'monthly_quota_warning' &&
        item.quota == null &&
        item.quota_observation_id == null) ||
      !!recordedMonthlyQuota(item as unknown as Notification, recipientId))
  )
}

export async function getNotifications(
  status: NotificationReadStatus,
  cursor?: string | null,
  signal?: AbortSignal,
  recipientId?: string,
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
    !data.items.every((item) => validNotification(item, recipientId)) ||
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

function notificationSettings(value: unknown): NotificationSettings {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid notification settings response')
  const row = value as Record<string, unknown>
  const keys = [
    'in_app_enabled',
    'external_email',
    'email_high',
    'email_medium',
    'etag',
    'updated_at',
  ]
  if (
    Object.keys(row).length !== keys.length ||
    !keys.every((key) => Object.hasOwn(row, key)) ||
    row.in_app_enabled !== true ||
    typeof row.external_email !== 'string' ||
    invalidRecordedText(row.external_email) ||
    typeof row.email_high !== 'boolean' ||
    typeof row.email_medium !== 'boolean' ||
    typeof row.etag !== 'string' ||
    !(row.etag === '0' || /^rev_[0-7][0-9a-hjkmnp-tv-z]{25}$/.test(row.etag)) ||
    typeof row.updated_at !== 'string' ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(
      row.updated_at,
    ) ||
    !Number.isFinite(Date.parse(row.updated_at))
  )
    throw new Error('Invalid notification settings response')
  return row as unknown as NotificationSettings
}

export async function getNotificationSettings(signal?: AbortSignal) {
  const response = await client.get<unknown>('/notification-settings', { signal })
  return notificationSettings(response.data)
}

export async function updateNotificationSettings(
  input: UpdateNotificationSettingsInput,
  csrf: string,
) {
  try {
    const response = await client.put<unknown>('/notification-settings', input, {
      headers: { 'X-CSRF-Token': csrf, 'If-Match': input.etag },
    })
    return notificationSettings(response.data)
  } catch (error) {
    throw new NotificationError(statusOf(error))
  }
}
