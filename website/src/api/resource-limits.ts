import client from './client'
import {
  teamLimitFields,
  type TeamLimitInput,
  type TeamLimitScope,
  type LimitInput,
  type LimitRecord,
} from '@/types/resource-limits'
export async function getLimits(path: string, signal?: AbortSignal) {
  return (await client.get<LimitRecord>(`${path}/limits`, { signal })).data
}
export async function saveLimits(path: string, etag: string, input: LimitInput, csrf: string) {
  return (
    await client.put<LimitRecord>(`${path}/limits`, input, {
      headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
    })
  ).data
}

const strongETag = /^[a-f0-9]{64}$/
const amount = /^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$/
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
export function teamLimitPath(scope: TeamLimitScope) {
  return `/teams/${encodeURIComponent(scope.teamId)}${scope.userId ? `/members/${encodeURIComponent(scope.userId)}` : ''}`
}
function validateTeamLimits(value: unknown, scope: TeamLimitScope): LimitRecord {
  if (
    !object(value) ||
    value.kind !== (scope.userId ? 'team_member' : 'team') ||
    value.id !== (scope.userId ?? scope.teamId) ||
    value.team_id !== scope.teamId ||
    typeof value.etag !== 'string' ||
    !strongETag.test(value.etag) ||
    (scope.userId &&
      (typeof value.parent_etag !== 'string' || !strongETag.test(value.parent_etag))) ||
    typeof value.account_id !== 'string' ||
    !value.account_id ||
    typeof value.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(value.platform_currency) ||
    typeof value.enforced !== 'boolean' ||
    !Array.isArray(value.editable_fields) ||
    new Set(value.editable_fields).size !== value.editable_fields.length ||
    value.editable_fields.some(
      (field) =>
        !teamLimitFields.includes(field) ||
        (scope.userId && (field === 'tokens_5h' || field === 'tokens_7d')),
    ) ||
    !object(value.stored) ||
    !object(value.effective) ||
    !Array.isArray(value.ip_policies) ||
    value.ip_policies.length !== (scope.userId ? 2 : 1) ||
    !(
      value.rpm_used === null ||
      (Number.isSafeInteger(value.rpm_used) && Number(value.rpm_used) >= 0)
    ) ||
    !(value.active === null || (Number.isSafeInteger(value.active) && Number(value.active) >= 0)) ||
    !(value.quota_usage === null || object(value.quota_usage))
  )
    throw new Error('Invalid Team limit response')
  for (const policy of [value.stored, value.effective, ...value.ip_policies]) {
    if (!object(policy)) throw new Error('Invalid Team parent policy')
    for (const field of teamLimitFields) {
      const item = policy[field]
      if (
        field === 'money_month'
          ? !(item === null || (typeof item === 'string' && amount.test(item)))
          : !(item === null || (Number.isSafeInteger(item) && Number(item) >= 0))
      )
        throw new Error('Invalid Team limit policy')
    }
    if (
      typeof policy.currency !== 'string' ||
      (policy.money_month === null ? policy.currency !== '' : !/^[A-Z]{3}$/.test(policy.currency))
    )
      throw new Error('Invalid Team limit currency')
  }
  for (const policy of [value.stored, ...value.ip_policies]) {
    if (policy.ip_mode !== 'none' || !Array.isArray(policy.ip_ranges) || policy.ip_ranges.length)
      throw new Error('Invalid Team IP policy')
  }
  if (scope.userId && (value.stored.tokens_5h !== null || value.stored.tokens_7d !== null))
    throw new Error('Invalid member rolling policy')
  if (value.quota_usage !== null) validateQuotaUsage(value.quota_usage)
  return value as unknown as LimitRecord
}

function validateQuotaUsage(value: unknown) {
  if (
    !object(value) ||
    typeof value.activated !== 'boolean' ||
    typeof value.time_zone !== 'string' ||
    !(
      value.as_of === null ||
      (typeof value.as_of === 'string' && Number.isFinite(Date.parse(value.as_of)))
    ) ||
    !(
      value.coverage_start === null ||
      (typeof value.coverage_start === 'string' &&
        Number.isFinite(Date.parse(value.coverage_start)))
    )
  )
    throw new Error('Invalid Team quota usage')
  new Intl.DateTimeFormat('en', { timeZone: value.time_zone })
  for (const field of ['active', 'minute', 'five_hours', 'seven_days', 'month']) {
    const window = value[field]
    if (window === null) continue
    if (
      !object(window) ||
      typeof window.covered !== 'boolean' ||
      ['tokens_used', 'tokens_held', 'tokens_unknown', 'money_unknown'].some(
        (key) => !Number.isSafeInteger(window[key]) || Number(window[key]) < 0,
      )
    )
      throw new Error('Invalid Team quota window')
    for (const key of ['money_used', 'money_held']) {
      const amounts = window[key]
      if (
        !object(amounts) ||
        Object.entries(amounts).some(
          ([currency, value]) =>
            !/^[A-Z]{3}$/.test(currency) || typeof value !== 'string' || !amount.test(value),
        )
      )
        throw new Error('Invalid Team quota amounts')
    }
  }
}

export async function getTeamLimits(scope: TeamLimitScope, signal?: AbortSignal) {
  return validateTeamLimits(
    (await client.get<unknown>(`${teamLimitPath(scope)}/limits`, { signal })).data,
    scope,
  )
}
function decimalEqual(left: string, right: string) {
  const normalize = (value: string) =>
    value.includes('.') ? value.replace(/0+$/, '').replace(/\.$/, '') : value
  return normalize(left) === normalize(right)
}
export async function saveTeamLimits(
  scope: TeamLimitScope,
  etag: string,
  input: TeamLimitInput,
  csrf: string,
) {
  const data = validateTeamLimits(
    (
      await client.put<unknown>(`${teamLimitPath(scope)}/limits`, input, {
        headers: { 'If-Match': `"${etag}"`, 'X-CSRF-Token': csrf },
      })
    ).data,
    scope,
  )
  if (
    !data.enforced ||
    teamLimitFields.some(
      (field) =>
        Object.hasOwn(input, field) &&
        (field === 'money_month' &&
        typeof input[field] === 'string' &&
        typeof data.stored[field] === 'string'
          ? !decimalEqual(input[field], data.stored[field])
          : input[field] !== data.stored[field]),
    ) ||
    (input.money_month != null && data.stored.currency !== input.currency)
  )
    throw new Error('Unconfirmed Team limit response')
  return data
}
