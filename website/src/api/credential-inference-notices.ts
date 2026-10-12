import type { Notification } from '@/types/notifications'
import type { OperationalAlert } from '@/types/overview'

const identity = (value: unknown): value is string =>
  typeof value === 'string' && /^[A-Za-z0-9_-]{1,30}$/.test(value)
const id = (value: unknown, prefix: string) =>
  typeof value === 'string' && new RegExp(`^${prefix}_[0-7][0-9a-hjkmnp-tv-z]{25}$`).test(value)
const stamp = (value: unknown): value is string =>
  typeof value === 'string' &&
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value) &&
  Number.isFinite(Date.parse(value))

// This source alone carries an immutable, non-secret Credential identity. It
// cannot borrow quota, model-discovery or current catalog metadata.
function credentialNotice(value: unknown, alert: boolean): boolean {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const item = value as Record<string, unknown>
  const allowed = [
    'id',
    'kind',
    'severity',
    'detail_code',
    'credential_id',
    'subject_type',
    'subject_id',
    'subject_name',
    'occurrence_count',
    'first_seen_at',
    'last_seen_at',
    ...(alert
      ? ['state', 'updated_at', 'etag']
      : [
          'alert_id',
          'read',
          'read_at',
          'delivery_status',
          'delivery_code',
          'delivery_attempts',
          'delivery_updated_at',
        ]),
  ]
  const subject =
    (item.subject_type == null && item.subject_id == null && item.subject_name == null) ||
    (item.subject_type === 'provider' &&
      identity(item.subject_id) &&
      typeof item.subject_name === 'string' &&
      item.subject_name.length > 0 &&
      item.subject_name.length <= 400 &&
      !/\p{Cc}/u.test(item.subject_name))
  if (
    !Object.keys(item).every((key) => allowed.includes(key)) ||
    !id(item.id, alert ? 'alr' : 'ntf') ||
    !identity(item.credential_id) ||
    item.kind !== 'credential_inference_failure' ||
    item.severity !== 'medium' ||
    item.detail_code !== 'five_consecutive_recorded_failures' ||
    !subject ||
    !Number.isSafeInteger(item.occurrence_count) ||
    (item.occurrence_count as number) < 1 ||
    !stamp(item.first_seen_at) ||
    !stamp(item.last_seen_at) ||
    Date.parse(item.first_seen_at) > Date.parse(item.last_seen_at)
  )
    return false
  if (alert)
    return (
      ['open', 'handling', 'resolved'].includes(item.state as string) &&
      stamp(item.updated_at) &&
      id(item.etag, 'rev')
    )
  const delivery =
    item.delivery_status == null
      ? item.delivery_code == null &&
        item.delivery_attempts == null &&
        item.delivery_updated_at == null
      : ['pending', 'retry', 'sending', 'accepted', 'failed', 'unknown'].includes(
          item.delivery_status as string,
        ) &&
        (item.delivery_code == null ||
          (typeof item.delivery_code === 'string' && item.delivery_code.length <= 80)) &&
        (item.delivery_attempts == null ||
          (Number.isSafeInteger(item.delivery_attempts) &&
            (item.delivery_attempts as number) >= 0)) &&
        stamp(item.delivery_updated_at)
  return (
    id(item.alert_id, 'alr') &&
    typeof item.read === 'boolean' &&
    (item.read ? stamp(item.read_at) : item.read_at === null) &&
    delivery
  )
}

export function recordedCredentialInference(
  value: unknown,
): value is Notification & { kind: 'credential_inference_failure'; credential_id: string } {
  return credentialNotice(value, false)
}
export function validCredentialInferenceAlert(
  value: unknown,
): value is OperationalAlert & { kind: 'credential_inference_failure'; credential_id: string } {
  return credentialNotice(value, true)
}
