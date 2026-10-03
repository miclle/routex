import type { ProjectInitialResources } from '@/types/resources'
import { parseInteger, validMoney } from '@/views/resource-limits/quota-values'

export const creationLimitFields = [
  'tokens_month',
  'money_month',
  'rpm',
  'tpm',
  'concurrency',
] as const
export type CreationResourceDraft = Record<(typeof creationLimitFields)[number], string> & {
  models: { id: string; name: string }[]
  reason: string
}
export const emptyCreationResources = (): CreationResourceDraft => ({
  tokens_month: '',
  money_month: '',
  rpm: '',
  tpm: '',
  concurrency: '',
  models: [],
  reason: '',
})
export function creationResources(
  draft: CreationResourceDraft,
  currency: string,
): ProjectInitialResources | null | undefined {
  if (
    draft.models.length > 1000 ||
    new Set(draft.models.map((model) => model.id)).size !== draft.models.length
  )
    return undefined
  const value: ProjectInitialResources = { reason: draft.reason.trim() }
  if (draft.models.length) value.model_ids = draft.models.map((model) => model.id)
  for (const field of creationLimitFields) {
    if (field === 'money_month') {
      const amount = draft[field].trim()
      if (amount) {
        if (!validMoney(amount)) return undefined
        value.money_month = amount
        value.currency = currency
      }
    } else {
      const number = parseInteger(draft[field])
      if (number === undefined) return undefined
      if (number !== null) value[field] = number
    }
  }
  return Object.keys(value).length === 1 ? null : value
}
