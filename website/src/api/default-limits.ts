import client from './client'
import {
  defaultIntegerFields,
  type DefaultLimitKind,
  type DefaultLimitInput,
  type DefaultLimitRecord,
  type DefaultLimitResetContext,
  type DefaultLimitResetResult,
  type DefaultResetTarget,
} from '@/types/default-limits'
const validMoney = (value: string) => /^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$/.test(value)

const etag = /^[a-f0-9]{64}$/
const object = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)
function validPolicy(value: unknown) {
  return (
    object(value) &&
    defaultIntegerFields.every(
      (field) =>
        value[field] === null || (Number.isSafeInteger(value[field]) && Number(value[field]) >= 0),
    ) &&
    (value.money_month === null ||
      (typeof value.money_month === 'string' && validMoney(value.money_month))) &&
    typeof value.currency === 'string' &&
    (value.money_month === null ? value.currency === '' : /^[A-Z]{3}$/.test(value.currency))
  )
}
export function validateDefaultLimits(value: unknown, kind: DefaultLimitKind): DefaultLimitRecord {
  if (
    !object(value) ||
    value.kind !== kind ||
    typeof value.etag !== 'string' ||
    !etag.test(value.etag) ||
    typeof value.rule_etag !== 'string' ||
    !etag.test(value.rule_etag) ||
    !validPolicy(value.policy) ||
    typeof value.platform_currency !== 'string' ||
    !/^[A-Z]{3}$/.test(value.platform_currency) ||
    typeof value.editable !== 'boolean' ||
    typeof value.updated_at !== 'string' ||
    !Number.isFinite(Date.parse(value.updated_at))
  )
    throw new Error('Invalid default limit response')
  return value as unknown as DefaultLimitRecord
}
function samePolicy(left: DefaultLimitRecord['policy'], right: DefaultLimitRecord['policy']) {
  const decimal = (value: string) =>
    value.includes('.') ? value.replace(/0+$/, '').replace(/\.$/, '') : value
  return (
    defaultIntegerFields.every((field) => left[field] === right[field]) &&
    left.currency === right.currency &&
    (left.money_month === right.money_month ||
      (left.money_month !== null &&
        right.money_month !== null &&
        decimal(left.money_month) === decimal(right.money_month)))
  )
}
export async function getDefaultLimits(kind: DefaultLimitKind, signal?: AbortSignal) {
  return validateDefaultLimits(
    (await client.get<unknown>(`/admin/default-limits/${kind}`, { signal })).data,
    kind,
  )
}
export async function saveDefaultLimits(
  kind: DefaultLimitKind,
  review: string,
  input: DefaultLimitInput,
  csrf: string,
) {
  const result = validateDefaultLimits(
    (
      await client.put<unknown>(`/admin/default-limits/${kind}`, input, {
        headers: { 'If-Match': `"${review}"`, 'X-CSRF-Token': csrf },
      })
    ).data,
    kind,
  )
  if (!samePolicy(result.policy, input.policy)) throw new Error('Unconfirmed default policy')
  return result
}
export function defaultResetPath(target: DefaultResetTarget) {
  return target.kind === 'user'
    ? `/admin/members/${encodeURIComponent(target.id)}/limits/default-reset`
    : `/teams/${encodeURIComponent(target.id)}/limits/default-reset`
}
function validLimit(value: unknown, target: DefaultResetTarget) {
  return (
    object(value) &&
    value.kind === target.kind &&
    value.id === target.id &&
    typeof value.etag === 'string' &&
    (target.kind === 'team' ? etag.test(value.etag) : /^[A-Za-z0-9_-]{1,64}$/.test(value.etag)) &&
    validPolicy(value.stored) &&
    object(value.stored) &&
    ['tokens_month_behavior', 'money_month_behavior'].every(
      (field) =>
        (target.kind !== 'team' && !Object.hasOwn(value.stored as object, field)) ||
        ((target.kind === 'user' || target.kind === 'team') &&
          ((value.stored as Record<string, unknown>)[field] === 'stop' ||
            (value.stored as Record<string, unknown>)[field] === 'alert_only')),
    ) &&
    object(value.effective) &&
    !['tokens_month_behavior', 'money_month_behavior'].some((field) =>
      Object.hasOwn(value.effective as object, field),
    ) &&
    validPolicy(value.effective) &&
    typeof value.enforced === 'boolean' &&
    typeof value.platform_currency === 'string' &&
    /^[A-Z]{3}$/.test(value.platform_currency) &&
    Array.isArray(value.ip_policies) &&
    typeof value.account_id === 'string'
  )
}
export async function getDefaultReset(target: DefaultResetTarget, signal?: AbortSignal) {
  const value = (await client.get<unknown>(defaultResetPath(target), { signal })).data
  if (
    !object(value) ||
    value.kind !== target.kind ||
    value.id !== target.id ||
    typeof value.etag !== 'string' ||
    !etag.test(value.etag) ||
    !validLimit(value.limit, target) ||
    typeof value.editable !== 'boolean' ||
    !(
      value.applied_default_etag === null ||
      (typeof value.applied_default_etag === 'string' && etag.test(value.applied_default_etag))
    )
  )
    throw new Error('Invalid default reset context')
  validateDefaultLimits(value.default_rule, target.kind)
  return value as unknown as DefaultLimitResetContext
}
export async function restoreDefaultLimits(
  target: DefaultResetTarget,
  review: DefaultLimitResetContext,
  reason: string,
  csrf: string,
  signal?: AbortSignal,
) {
  const value = (
    await client.post<unknown>(
      defaultResetPath(target),
      { reason },
      {
        signal,
        headers: { 'If-Match': `"${review.etag}"`, 'X-CSRF-Token': csrf },
      },
    )
  ).data
  if (
    !object(value) ||
    value.kind !== target.kind ||
    value.id !== target.id ||
    value.saved !== true ||
    value.default_reset_etag !== review.etag ||
    value.applied_default_etag !== review.default_rule.rule_etag ||
    !validLimit(value.limit, target) ||
    typeof value.runtime_applied !== 'boolean' ||
    !object(value.limit) ||
    value.runtime_applied !== value.limit.enforced ||
    ((target.kind === 'user' || target.kind === 'team') &&
      object(value.limit.stored) &&
      ((value.limit.stored.tokens_month_behavior ?? 'stop') !== 'stop' ||
        (value.limit.stored.money_month_behavior ?? 'stop') !== 'stop')) ||
    !samePolicy(value.limit.stored as DefaultLimitRecord['policy'], review.default_rule.policy) ||
    (object(value.limit.stored) &&
      (value.limit.stored.ip_mode !== review.limit.stored.ip_mode ||
        JSON.stringify(value.limit.stored.ip_ranges) !==
          JSON.stringify(review.limit.stored.ip_ranges)))
  )
    throw new Error('Unconfirmed default reset response')
  return value as unknown as DefaultLimitResetResult
}
