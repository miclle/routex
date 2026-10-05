import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import type { DefaultLimitResetContext, DefaultResetTarget } from '@/types/default-limits'
import { useUncertainIntents } from '@/hooks/use-uncertain-intents'
import type { RetainedRestoreReview, SubmittedIntentClaim } from '@/types/uncertain-intents'

export type RestoreIntent = {
  target: DefaultResetTarget
  review: DefaultLimitResetContext
  reason: string
}
type RestoreState = {
  scope: string
  open: boolean
  reviewed: DefaultLimitResetContext | null
  reason: string
  intent: RestoreIntent | null
  uncertain: boolean
  busy: boolean
  requireReview: boolean
  issue: string | null
  notice: string | null
}
const initial = (scope: string): RestoreState => ({
  scope,
  open: false,
  reviewed: null,
  reason: '',
  intent: null,
  uncertain: false,
  busy: false,
  requireReview: false,
  issue: null,
  notice: null,
})

// A resource host retains only transient review intent when renewed reads hide its
// children. The owner never authorizes a read or write and never stores a Session.
export function useRestoreOwner(
  actor: string,
  target: DefaultResetTarget,
  context = '',
  enabled = true,
) {
  const shared = useUncertainIntents()
  const claim = useRef<SubmittedIntentClaim | null>(null)
  const previousShared = useRef(shared)
  const scope = JSON.stringify([actor, target.kind, target.id, context])
  const latest = useRef(scope)
  useLayoutEffect(() => {
    latest.current = scope
  }, [scope])
  const mounted = useRef(false)
  const [stored, setStored] = useState(() => initial(scope))
  const state = stored.scope === scope ? stored : initial(scope)
  if (stored.scope !== scope) setStored(state)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const active = useCallback(() => mounted.current && latest.current === scope, [scope])
  const update = useCallback(
    (change: (previous: RestoreState) => RestoreState) => {
      if (!active()) return
      setStored((previous) => (previous.scope === scope ? change(previous) : previous))
    },
    [active, scope],
  )
  useLayoutEffect(() => {
    if (previousShared.current !== shared) {
      previousShared.current = shared
      claim.current = null
      update(() => initial(scope))
    }
    if (!enabled || !shared || !active()) return
    const recovered = shared.recover(actor)
    if (
      recovered?.kind !== 'restore-defaults' ||
      recovered.payload.target.kind !== target.kind ||
      recovered.payload.target.id !== target.id ||
      claim.current === recovered.claim
    )
      return
    claim.current = recovered.claim
    const captured: RestoreIntent = {
      ...recovered.payload,
      review: restoredReview(recovered.payload.review),
    }
    update((previous) => ({
      ...previous,
      intent: captured,
      reviewed: captured.review,
      reason: captured.reason,
      busy: false,
      uncertain: true,
      requireReview: false,
      issue: 'uncertain',
    }))
  }) // Fresh Session recovery can notify without changing the renewed claim epoch.
  const capture = (value: RestoreIntent) => {
    if (!active() || !enabled) return null
    const captured: RestoreIntent = {
      target: { ...value.target },
      review: restoredReview(value.review),
      reason: value.reason,
    }
    if (shared) {
      claim.current = shared.capture(actor, { kind: 'restore-defaults', payload: captured })
      if (!claim.current) return null
    }
    return captured
  }
  const currentClaim = () => claim.current
  const claimCurrent = (captured: SubmittedIntentClaim | null) =>
    !shared || (!!captured && shared.isCurrent(captured))
  const clear = (captured: SubmittedIntentClaim | null) => {
    if (shared && (!captured || !shared.clear(captured))) return false
    claim.current = null
    return true
  }
  return { actor, target, scope, state, active, update, capture, currentClaim, claimCurrent, clear }
}
export type RestoreOwner = ReturnType<typeof useRestoreOwner>

// This is a historical submission review, never current usage or authority.
function restoredReview(value: RetainedRestoreReview): DefaultLimitResetContext {
  return {
    kind: value.kind,
    id: value.id,
    etag: value.etag,
    applied_default_etag: value.applied_default_etag,
    editable: false,
    default_rule: {
      kind: value.default_rule.kind,
      etag: value.default_rule.etag,
      rule_etag: value.default_rule.rule_etag,
      policy: structuredClone(value.default_rule.policy),
      platform_currency: value.default_rule.platform_currency,
      editable: false,
      updated_at: '',
    },
    limit: {
      kind: value.limit.kind,
      id: value.limit.id,
      account_id: value.limit.account_id,
      etag: value.limit.etag,
      platform_currency: value.limit.platform_currency,
      stored: structuredClone(value.limit.stored),
      effective: structuredClone(value.limit.stored),
      ip_policies: [],
      quota_usage: null,
      rpm_used: null,
      active: null,
      enforced: false,
    },
  }
}
