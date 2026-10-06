import { useLayoutEffect, useMemo, type ReactNode } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useLocation } from 'react-router'
import { sessionKey } from '@/hooks/use-auth'
import { UncertainIntentContext } from '@/hooks/use-uncertain-intents'
import type { Session } from '@/types/auth'
import type { DefaultLimitPolicy } from '@/types/default-limits'
import type { LimitPolicy } from '@/types/resource-limits'
import type {
  DefaultLimitSaveSubmittedIntent,
  RetainedRestoreReview,
  RetainedSubmittedIntent,
  SubmittedIntent,
  SubmittedIntentClaim,
  SubmittedIntentOwner,
  TeamCreateSubmittedIntent,
} from '@/types/uncertain-intents'

const integerFields = [
  'tokens_5h',
  'tokens_7d',
  'tokens_month',
  'rpm',
  'tpm',
  'concurrency',
] as const
function caps(value: Partial<LimitPolicy>) {
  const result: Partial<LimitPolicy> = {}
  for (const field of Object.keys(value)) {
    if (integerFields.includes(field as (typeof integerFields)[number])) {
      const key = field as (typeof integerFields)[number]
      result[key] = value[key]
    } else if (field === 'money_month') result.money_month = value.money_month
    else if (field === 'currency') result.currency = value.currency
  }
  return result
}
function initialLimits(value: NonNullable<TeamCreateSubmittedIntent['body']['initial_limits']>) {
  const result = {} as typeof value
  for (const field of Object.keys(value)) {
    if (integerFields.includes(field as (typeof integerFields)[number])) {
      const key = field as (typeof integerFields)[number]
      result[key] = value[key]
    } else if (field === 'money_month') result.money_month = value.money_month
    else if (field === 'currency') result.currency = value.currency
    else if (field === 'reason') result.reason = value.reason
  }
  return result
}

function policy(value: LimitPolicy): LimitPolicy {
  return {
    ...caps(value),
    rpm: value.rpm,
    concurrency: value.concurrency,
    ip_mode: value.ip_mode,
    ip_ranges: [...value.ip_ranges],
  }
}
function restoreReview(value: RetainedRestoreReview): RetainedRestoreReview {
  return {
    kind: value.kind,
    id: value.id,
    etag: value.etag,
    applied_default_etag: value.applied_default_etag,
    default_rule: {
      kind: value.default_rule.kind,
      rule_etag: value.default_rule.rule_etag,
      etag: value.default_rule.etag,
      policy: caps(value.default_rule.policy) as DefaultLimitPolicy,
      platform_currency: value.default_rule.platform_currency,
    },
    limit: {
      kind: value.limit.kind,
      id: value.limit.id,
      account_id: value.limit.account_id,
      etag: value.limit.etag,
      platform_currency: value.limit.platform_currency,
      stored: policy(value.limit.stored),
    },
  }
}
function defaultSaveInput(value: DefaultLimitSaveSubmittedIntent['input']) {
  const result = {} as typeof value
  for (const field of Object.keys(value)) {
    if (field === 'policy') result.policy = caps(value.policy) as DefaultLimitPolicy
    else if (field === 'reason') result.reason = value.reason
  }
  return result
}
function copySubmission(value: SubmittedIntent): SubmittedIntent {
  if (value.kind === 'connection-name') {
    return {
      kind: value.kind,
      payload: {
        provider_id: value.payload.provider_id,
        connection_id: value.payload.connection_id,
        etag: value.payload.etag,
        input: { name: value.payload.input.name, reason: value.payload.input.reason },
      },
    }
  }
  if (value.kind === 'default-limit-save') {
    return {
      kind: value.kind,
      payload: {
        target: value.payload.target,
        etag: value.payload.etag,
        input: defaultSaveInput(value.payload.input),
      },
    }
  }
  if (value.kind === 'team-create') {
    const body = value.payload.body
    const copied = {} as TeamCreateSubmittedIntent['body']
    for (const key of Object.keys(body)) {
      switch (key) {
        case 'creation_id':
          copied.creation_id = body.creation_id
          break
        case 'name':
          copied.name = body.name
          break
        case 'description':
          copied.description = body.description
          break
        case 'owner_ids':
          copied.owner_ids = [...body.owner_ids]
          break
        case 'model_ids':
          if (body.model_ids !== undefined) copied.model_ids = [...body.model_ids]
          break
        case 'model_review_token':
          if (body.model_review_token !== undefined)
            copied.model_review_token = body.model_review_token
          break
        case 'initial_limits':
          if (body.initial_limits !== undefined)
            copied.initial_limits = initialLimits(body.initial_limits)
          break
      }
    }
    return { kind: value.kind, payload: { etag: value.payload.etag, body: copied } }
  }

  return {
    kind: value.kind,
    payload: {
      target: { kind: value.payload.target.kind, id: value.payload.target.id },
      review: restoreReview(value.payload.review),
      reason: value.payload.reason,
    },
  }
}

function createOwner(cache: QueryClient, routeScope: string) {
  let epoch = 0,
    alive = false,
    revision = 0,
    suspended = false,
    authorityLost = false,
    awaitingExpiryCleanup = false
  let retained: RetainedSubmittedIntent | null = null
  const listeners = new Set<() => void>()
  const changed = () => {
    revision++
    for (const listener of listeners) listener()
  }
  const discard = () => {
    retained = null
    suspended = false
    epoch++
    changed()
  }
  const freshActor = (actor: string) => {
    const state = cache.getQueryState<Session | null>(sessionKey)
    return (
      alive &&
      !authorityLost &&
      !!actor &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated &&
      state.data?.user.id === actor
    )
  }
  const current = (claim: SubmittedIntentClaim) =>
    retained?.claim === claim &&
    claim.epoch === epoch &&
    claim.routeScope === routeScope &&
    freshActor(claim.actor)
  const owner: SubmittedIntentOwner = {
    get epoch() {
      return epoch
    },
    capture(actor, submission) {
      if (!freshActor(actor) || retained) return null
      if (
        submission.kind === 'restore-defaults' &&
        (submission.payload.target.kind !== submission.payload.review.kind ||
          submission.payload.target.id !== submission.payload.review.id ||
          submission.payload.target.kind !== submission.payload.review.limit.kind ||
          submission.payload.target.id !== submission.payload.review.limit.id)
      )
        return null
      if (
        submission.kind === 'default-limit-save' &&
        submission.payload.target !== 'user' &&
        submission.payload.target !== 'team'
      )
        return null
      if (
        submission.kind === 'connection-name' &&
        (!/^prv_[A-Za-z0-9_-]*$/.test(submission.payload.provider_id) ||
          submission.payload.provider_id.length > 30 ||
          !/^con_[A-Za-z0-9_-]*$/.test(submission.payload.connection_id) ||
          submission.payload.connection_id.length > 30)
      )
        return null
      const claim = Object.freeze({
        identity: Symbol('submitted intent'),
        actor,
        routeScope,
        epoch: ++epoch,
        targetScope:
          submission.kind === 'connection-name'
            ? JSON.stringify([
                submission.kind,
                submission.payload.provider_id,
                submission.payload.connection_id,
              ])
            : submission.kind === 'team-create'
              ? JSON.stringify([submission.kind, submission.payload.body.creation_id])
              : submission.kind === 'default-limit-save'
                ? JSON.stringify([submission.kind, submission.payload.target])
                : JSON.stringify([
                    submission.kind,
                    submission.payload.target.kind,
                    submission.payload.target.id,
                  ]),
      })
      retained = { ...copySubmission(submission), claim, uncertain: true }
      changed()
      return claim
    },
    recover(actor) {
      if (!retained || !freshActor(actor) || retained.claim.actor !== actor) return null
      return { ...copySubmission(retained), claim: retained.claim, uncertain: true }
    },
    isCurrent: current,
    clear(claim) {
      if (!current(claim)) return false
      discard()
      return true
    },
  }
  return {
    owner,
    subscribe: (listener: () => void) => {
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
      }
    },
    snapshot: () => revision,
    activate() {
      alive = true
      const unsubscribe = cache.getQueryCache().subscribe((event) => {
        const key = event.query.queryKey
        if (key.length !== 2 || key[0] !== sessionKey[0] || key[1] !== sessionKey[1]) return
        const currentQuery = cache.getQueryCache().find({ queryKey: sessionKey, exact: true })
        if (event.type === 'removed') {
          if (!currentQuery || currentQuery === event.query) {
            awaitingExpiryCleanup = false
            authorityLost = true
            discard()
          }
          return
        }
        // An obsolete removed Query must not clear or reauthorize a newer Session.
        if (currentQuery !== event.query) return
        const data = event.query.state.data as Session | null | undefined
        if (data === null || (data && retained && data.user.id !== retained.claim.actor)) {
          if (data === null) awaitingExpiryCleanup = false
          authorityLost = true
          discard()
        } else {
          const state = event.query.state
          const unavailable =
            state.status !== 'success' ||
            state.fetchStatus !== 'idle' ||
            !!state.error ||
            state.isInvalidated
          if (retained && unavailable && !suspended) {
            retained = {
              ...retained,
              claim: Object.freeze({
                ...retained.claim,
                identity: Symbol('renewed intent claim'),
                epoch: ++epoch,
              }),
            }
          }
          suspended = unavailable
          changed()
        }
        if (
          event.type === 'updated' &&
          event.action.type === 'success' &&
          !event.action.manual &&
          !awaitingExpiryCleanup &&
          data
        ) {
          authorityLost = false
          changed()
        }
      })
      const expired = () => {
        awaitingExpiryCleanup = true
        authorityLost = true
        discard()
      }
      window.addEventListener('routex:session-expired', expired)
      changed()
      return () => {
        alive = false
        unsubscribe()
        window.removeEventListener('routex:session-expired', expired)
        discard()
      }
    },
  }
}
// This owner survives GateState, not private-route departure. It never mounts a
// Session query or grants permission to render/dispatch a retained operation.
export function UncertainIntentProvider({ children }: { children: ReactNode }) {
  const cache = useQueryClient()
  const location = useLocation()
  const routeScope = JSON.stringify([
    location.pathname,
    new URLSearchParams(location.search).get('tab') ?? '',
  ])
  const store = useMemo(() => createOwner(cache, routeScope), [cache, routeScope])
  useLayoutEffect(() => store.activate(), [store])
  return <UncertainIntentContext.Provider value={store}>{children}</UncertainIntentContext.Provider>
}
