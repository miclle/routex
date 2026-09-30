import type { LimitPolicy } from '@/types/resource-limits'

export const quotaFields = ['tokens_5h', 'tokens_7d', 'tokens_month'] as const
export const rateFields = ['rpm', 'tpm', 'concurrency'] as const
export const integerFields = [...quotaFields, ...rateFields] as const
export type IntegerField = (typeof integerFields)[number]
export type NumericDraft = Record<IntegerField, string>

export function integerDraft(policy: LimitPolicy): NumericDraft {
  return Object.fromEntries(
    integerFields.map((field) => [field, policy[field]?.toString() ?? '']),
  ) as NumericDraft
}
export function parseInteger(value: string): number | null | undefined {
  const trimmed = value.trim()
  if (!trimmed) return null
  const numeric = Number(trimmed)
  return /^\d+$/.test(trimmed) && Number.isSafeInteger(numeric) ? numeric : undefined
}
export function validMoney(value: string): boolean {
  return /^(0|[1-9][0-9]{0,17})(\.[0-9]{1,18})?$/.test(value)
}
/** Compare exact decimal amounts without rounding through JavaScript numbers. */
export function moneyAbove(value: string, parent: string): boolean {
  const scaled = (amount: string) => {
    const [integer, fraction = ''] = amount.split('.')
    return BigInt(integer + fraction.padEnd(18, '0'))
  }
  return scaled(value) > scaled(parent)
}
