import type {
  DefaultLimitRecord,
  DefaultLimitResetContext,
  DefaultResetTarget,
} from './default-limits'
import type { LimitRecord } from './resource-limits'

export type TeamCreateSubmittedIntent = {
  etag: string
  body: {
    creation_id: string
    name: string
    description: string
    owner_ids: string[]
    initial_limits?: Partial<
      Record<
        'tokens_5h' | 'tokens_7d' | 'tokens_month' | 'rpm' | 'tpm' | 'concurrency',
        number | null
      >
    > & { money_month?: string | null; currency?: string; reason: string }
  }
}
export type RetainedRestoreReview = Pick<
  DefaultLimitResetContext,
  'kind' | 'id' | 'etag' | 'applied_default_etag'
> & {
  default_rule: Pick<
    DefaultLimitRecord,
    'kind' | 'etag' | 'rule_etag' | 'policy' | 'platform_currency'
  >
  limit: Pick<LimitRecord, 'kind' | 'id' | 'account_id' | 'etag' | 'platform_currency' | 'stored'>
}
export type RestoreSubmittedIntent = {
  target: DefaultResetTarget
  review: RetainedRestoreReview
  reason: string
}
export type SubmittedIntent =
  | { kind: 'team-create'; payload: TeamCreateSubmittedIntent }
  | { kind: 'restore-defaults'; payload: RestoreSubmittedIntent }

// Identity is opaque. Callers must keep the returned claim, not reconstruct it.
export type SubmittedIntentClaim = Readonly<{
  identity: symbol
  epoch: number
  actor: string
  routeScope: string
  targetScope: string
}>
export type RetainedSubmittedIntent = SubmittedIntent & {
  claim: SubmittedIntentClaim
  uncertain: true
}
export interface SubmittedIntentOwner {
  readonly epoch: number
  capture(actor: string, submission: SubmittedIntent): SubmittedIntentClaim | null
  recover(actor: string): RetainedSubmittedIntent | null
  isCurrent(claim: SubmittedIntentClaim): boolean
  clear(claim: SubmittedIntentClaim): boolean
}
