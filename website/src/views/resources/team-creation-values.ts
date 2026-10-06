import {
  teamCreationFields,
  type TeamCreationField,
  type TeamCreationContext,
  type TeamInitialLimits,
} from '@/types/resources'
export type TeamLimitDrafts = Partial<Record<TeamCreationField, string>>
const money = /^(0|[1-9]\d{0,17})(\.\d{1,18})?$/
export function teamCreationReason(value: string) {
  return (
    value.length > 0 &&
    value === value.trim() &&
    new TextEncoder().encode(value).length <= 2000 &&
    !/[\p{Cc}\p{Cs}]/u.test(value)
  )
}
export function teamTokenMillions(tokens: string) {
  const digits = tokens.padStart(7, '0')
  const fraction = digits.slice(-6).replace(/0+$/, '')
  return `${digits.slice(0, -6)}${fraction ? `.${fraction}` : ''}`
}
export function teamCreationValue(field: TeamCreationField, text: string): number | string | null {
  if (text === '') return null
  if (field === 'money_month') {
    if (!money.test(text)) throw new Error('invalid')
    return text
  }
  const tokens = field.startsWith('tokens_')
  if (!(tokens ? /^(0|[1-9]\d{0,15})(\.\d{1,6})?$/ : /^(0|[1-9]\d{0,15})$/).test(text))
    throw new Error('invalid')
  const [whole, fraction = ''] = text.split('.')
  const exact = tokens ? BigInt(whole) * 1000000n + BigInt(fraction.padEnd(6, '0')) : BigInt(whole)
  if (exact > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('invalid')
  return Number(exact)
}
export function teamCreationLimits(
  drafts: TeamLimitDrafts,
  context: TeamCreationContext,
  reason: string,
): TeamInitialLimits | undefined {
  const fields = teamCreationFields.filter((field) => Object.hasOwn(drafts, field))
  if (!fields.length) return undefined
  if (
    !teamCreationReason(reason) ||
    fields.some((field) => !context.editable_fields.includes(field))
  )
    throw new Error('invalid')
  const result: TeamInitialLimits = { reason }
  for (const field of fields) {
    const value = teamCreationValue(field, drafts[field]!)
    if (field === 'money_month') result.money_month = value as string | null
    else result[field] = value as number | null
  }
  if (result.money_month !== undefined && result.money_month !== null) {
    if (!context.platform_currency) throw new Error('invalid')
    result.currency = context.platform_currency
  }
  return result
}

export function teamCreationSearch(value: string) {
  return new TextEncoder().encode(value).length <= 200 && !/\p{Cs}/u.test(value)
}

export function teamCreationMetadata(name: string, description: string) {
  return (
    !!name.trim() &&
    Array.from(name.trim()).length <= 100 &&
    !/[\p{Cc}\p{Cs}]/u.test(name.trim()) &&
    Array.from(description).length <= 2000 &&
    !Array.from(description).some(
      (char) => /[\p{Cc}\p{Cs}]/u.test(char) && char !== '\n' && char !== '\t',
    )
  )
}
