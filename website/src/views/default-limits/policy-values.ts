import type { DefaultLimitPolicy, DefaultIntegerField } from '@/types/default-limits'

export const sections = [
  { key: 'budget', fields: ['money_month'] },
  { key: 'tokens', fields: ['tokens_5h', 'tokens_7d', 'tokens_month'] },
  { key: 'rates', fields: ['rpm', 'tpm', 'concurrency'] },
] as const
export type PolicyDraft = Record<DefaultIntegerField | 'money_month', string>
export function policyDraft(policy: DefaultLimitPolicy): PolicyDraft {
  return Object.fromEntries(
    sections
      .flatMap((section) => section.fields)
      .map((field) => [field, policy[field]?.toString() ?? '']),
  ) as PolicyDraft
}
