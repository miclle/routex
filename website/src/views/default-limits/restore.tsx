import { useCallback, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getDefaultReset, restoreDefaultLimits } from '@/api/default-limits'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { FormField, QueryState } from '@/components/app/CatalogUI'
import type { Session } from '@/types/auth'
import type { DefaultLimitResetContext, DefaultResetTarget } from '@/types/default-limits'
import { PolicyRows } from './policy'
import type { DefaultLimitPolicy } from '@/types/default-limits'
import { useRestoreOwner, type RestoreOwner, type RestoreIntent } from './restore-owner'

export default function RestoreDefaults({
  target,
  owner,
  visible = true,
  hostCurrent,
}: {
  target: DefaultResetTarget
  owner?: RestoreOwner
  visible?: boolean
  hostCurrent?: () => boolean
}) {
  const session = useSession()
  const access = usePermissions()
  const actor = session.data?.user.id ?? ''
  const allowed =
    visible &&
    !!actor &&
    !session.isError &&
    !session.isFetching &&
    !access.isError &&
    !access.isFetching &&
    (target.kind === 'user'
      ? access.can('limits.users.write')
      : ['teams.tokens.write', 'teams.money.write', 'teams.rates.write'].every(access.can))
  if (!actor) return null
  return (
    <RestoreControls
      key={`${actor}:${target.kind}:${target.id}`}
      target={target}
      actor={actor}
      visible={allowed}
      owner={owner}
      hostCurrent={hostCurrent}
    />
  )
}
export function RestoreControls({
  target,
  actor,
  visible,
  managed,
  owner,
  hostCurrent,
}: {
  target: DefaultResetTarget
  actor: string
  visible: boolean
  managed?: { canDispatch: () => boolean; generation: string }
  owner?: RestoreOwner
  hostCurrent?: () => boolean
}) {
  const { t } = useTranslation('defaultLimits')
  const localOwner = useRestoreOwner(actor, target, '', owner === undefined)
  const retained = owner ?? localOwner
  const { open, notice } = retained.state
  const sameOwner =
    retained.actor === actor &&
    retained.target.kind === target.kind &&
    retained.target.id === target.id
  return (
    <div className="space-y-2">
      {visible && sameOwner && (
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            if ((managed && !managed.canDispatch()) || (hostCurrent && !hostCurrent())) return
            retained.update((previous) => ({ ...previous, open: true, notice: null }))
          }}
        >
          {t('restore')}
        </Button>
      )}
      {visible && sameOwner && notice && <p role="status">{t(notice)}</p>}
      {sameOwner && (
        <RestoreDialog
          key={`${actor}:${target.kind}:${target.id}`}
          target={target}
          actor={actor}
          visible={visible && open}
          managed={managed}
          owner={retained}
          hostCurrent={hostCurrent}
          close={(result) => {
            retained.update((previous) => ({ ...previous, notice: result, open: false }))
          }}
        />
      )}
    </div>
  )
}
function RestoreDialog({
  target,
  actor,
  visible,
  close,
  managed,
  owner,
  hostCurrent,
}: {
  target: DefaultResetTarget
  actor: string
  visible: boolean
  owner: RestoreOwner
  hostCurrent?: () => boolean
  close: (notice: string | null) => void
  managed?: { canDispatch: () => boolean; generation: string }
}) {
  const { t } = useTranslation('defaultLimits')
  const { t: limitText } = useTranslation('limits')
  const cache = useQueryClient()
  const queryKey = managed
    ? ['default-reset', actor, target.kind, target.id, managed.generation]
    : ['default-reset', actor, target.kind, target.id]
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => getDefaultReset(target, signal),
    enabled: visible,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const { reviewed, reason, issue, uncertain, busy, requireReview } = owner.state
  const ownerUpdate = owner.update
  const setReviewed = useCallback(
    (value: DefaultLimitResetContext) =>
      ownerUpdate((previous) => ({ ...previous, reviewed: structuredClone(value) })),
    [ownerUpdate],
  )
  const setIssue = (value: string | null) =>
    owner.update((previous) => ({ ...previous, issue: value }))
  const setUncertain = (value: boolean) =>
    owner.update((previous) => ({ ...previous, uncertain: value }))
  const setBusy = (value: boolean) => owner.update((previous) => ({ ...previous, busy: value }))
  const lock = useRef(false)
  const alive = useRef(false)
  const intent = useRef<RestoreIntent | null>(owner.state.intent)
  useLayoutEffect(() => {
    intent.current = owner.state.intent
  }, [owner.state.intent])
  const controller = useRef<AbortController | null>(null)
  const serial = useRef(0)
  const writing = useRef(false)
  const [abandon, setAbandon] = useState(false)
  const keys = JSON.stringify([sessionKey, ['permissions', actor], queryKey])
  const snapshot = () =>
    JSON.parse(keys)
      .map((key: unknown[]) => {
        const state = cache.getQueryState(key)
        return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
      })
      .join('|')
  useSyncExternalStore(
    (notify) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.parse(keys).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    snapshot,
    snapshot,
  )
  function authorized() {
    const session = cache.getQueryState<Session>(sessionKey)
    const permission = cache.getQueryState<string[]>(['permissions', actor])
    return (
      visible &&
      owner.active() &&
      session?.status === 'success' &&
      session.fetchStatus === 'idle' &&
      !session.error &&
      !session.isInvalidated &&
      session.data?.user.id === actor &&
      !!session.data.csrf_token &&
      permission?.status === 'success' &&
      permission.fetchStatus === 'idle' &&
      !permission.error &&
      !permission.isInvalidated &&
      (target.kind === 'user'
        ? permission.data?.includes('limits.users.write')
        : ['teams.tokens.write', 'teams.money.write', 'teams.rates.write'].every((code) =>
            permission.data?.includes(code),
          )) &&
      (!managed || managed.canDispatch()) &&
      (!hostCurrent || hostCurrent())
    )
  }
  const reviewState = cache.getQueryState<DefaultLimitResetContext>(queryKey)
  const fresh =
    !!authorized() &&
    query.isSuccess &&
    !query.isFetching &&
    reviewState?.status === 'success' &&
    reviewState.fetchStatus === 'idle' &&
    !reviewState.error &&
    !reviewState.isInvalidated &&
    reviewState.data === query.data
  function currentReview() {
    const state = cache.getQueryState<DefaultLimitResetContext>(queryKey)
    return (
      !!authorized() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated &&
      state.data === query.data
    )
  }
  function currentActor() {
    return alive.current && owner.active() && authorized()
  }
  const stop = useCallback(() => {
    alive.current = false
    serial.current++
    controller.current?.abort()
    if (lock.current)
      ownerUpdate((previous) => ({
        ...previous,
        busy: false,
        uncertain: writing.current && !!intent.current ? true : previous.uncertain,
        issue: writing.current && intent.current ? 'uncertain' : previous.issue,
      }))
    lock.current = false
  }, [ownerUpdate])
  useLayoutEffect(() => {
    alive.current = true
    return stop
  }, [stop])
  useLayoutEffect(() => {
    if (fresh && query.data && !reviewed && !intent.current && !requireReview)
      setReviewed(query.data)
  }, [fresh, query.data, reviewed, requireReview, setReviewed])
  useLayoutEffect(() => {
    if (!fresh && writing.current && lock.current && intent.current) {
      serial.current++
      controller.current?.abort()
      lock.current = false
      writing.current = false
      ownerUpdate((previous) => ({
        ...previous,
        busy: false,
        uncertain: true,
        issue: 'uncertain',
      }))
    }
  }, [fresh, ownerUpdate])
  const context = reviewed ?? query.data ?? null
  const stale = !!reviewed && query.data?.etag !== reviewed.etag
  const blocked = stale || requireReview || issue === 'conflict' || issue === 'failed'
  async function dispatch(retry = false) {
    const session = cache.getQueryData<Session>(sessionKey)
    if (
      lock.current ||
      !currentActor() ||
      !session ||
      !currentReview() ||
      !query.data?.editable ||
      (!retry && (blocked || uncertain || !!intent.current))
    )
      return
    if (!retry) {
      if (!context) return
      if (
        !reason.trim() ||
        new TextEncoder().encode(reason.trim()).length > 1024 ||
        /\p{Cc}/u.test(reason.trim())
      ) {
        setIssue('requiredReason')
        return
      }
      const submission: RestoreIntent = {
        target: { ...target },
        review: structuredClone(context),
        reason: reason.trim(),
      }
      const retainedSubmission = owner.capture(submission)
      if (!retainedSubmission) return
      intent.current = retainedSubmission
      const captured = intent.current
      owner.update((previous) => ({
        ...previous,
        intent: captured,
        reviewed: captured.review,
        issue: null,
      }))
    }
    if (!intent.current || !owner.claimCurrent(owner.currentClaim())) return
    lock.current = true
    writing.current = true
    setBusy(true)
    const turn = ++serial.current
    const captured = intent.current
    const dispatchedClaim = owner.currentClaim()
    const startReview = cache.getQueryState<DefaultLimitResetContext>(queryKey)
    const pending = new AbortController()
    controller.current = pending
    try {
      const result = await restoreDefaultLimits(
        captured.target,
        captured.review,
        captured.reason,
        session.csrf_token,
        pending.signal,
      )
      const latestReview = cache.getQueryState<DefaultLimitResetContext>(queryKey)
      const obsoleteReview =
        latestReview?.isInvalidated ||
        latestReview?.fetchStatus !== 'idle' ||
        latestReview.status !== 'success' ||
        !!latestReview.error ||
        latestReview.dataUpdateCount !== startReview?.dataUpdateCount ||
        latestReview.errorUpdateCount !== startReview?.errorUpdateCount
      if (!alive.current || turn !== serial.current || !owner.active()) return
      if (
        !currentActor() ||
        pending.signal.aborted ||
        obsoleteReview ||
        !owner.claimCurrent(dispatchedClaim)
      ) {
        setUncertain(true)
        setIssue('uncertain')
      } else {
        if (!owner.clear(dispatchedClaim)) return
        intent.current = null
        owner.update((previous) => ({
          ...previous,
          intent: null,
          uncertain: false,
          reviewed: null,
          reason: '',
          requireReview: false,
          issue: null,
        }))
        void cache.invalidateQueries({ queryKey: ['resource-limits'] })
        close(result.runtime_applied ? 'restored' : 'pending')
      }
    } catch {
      if (alive.current && turn === serial.current && owner.active()) {
        setUncertain(true)
        setIssue('uncertain')
      }
    } finally {
      if (alive.current && turn === serial.current && owner.active()) {
        lock.current = false
        writing.current = false
        setBusy(false)
      }
    }
  }
  async function review() {
    if (lock.current || !currentActor()) return
    lock.current = true
    setBusy(true)
    try {
      const result = await query.refetch()
      if (currentActor() && result.data && !result.error && !intent.current) {
        setReviewed(result.data)
        owner.update((previous) => ({
          ...previous,
          requireReview: false,
          issue: previous.requireReview ? 'abandoned' : null,
        }))
      }
    } finally {
      lock.current = false
      if (alive.current && owner.active()) setBusy(false)
    }
  }
  if (!visible || !authorized()) return null
  return (
    <>
      <Dialog
        open
        busy={busy}
        onOpenChange={(value) => {
          if (!value) close(uncertain ? 'restoreClosed' : null)
        }}
        title={t('restoreTitle')}
        description={t('restoreHelp')}
        width={800}
      >
        <QueryState pending={query.isFetching} error={query.error} retry={() => void review()} />
        {(issue || uncertain || stale) && (
          <p role="alert" className="mb-4 text-destructive">
            {t(uncertain ? 'uncertain' : stale ? 'conflict' : issue!)}
          </p>
        )}
        {fresh && context && (
          <form
            className="space-y-6"
            onSubmit={(event) => {
              event.preventDefault()
              void dispatch()
            }}
          >
            <div className="grid gap-6 sm:grid-cols-2">
              <section aria-label={t(owner.state.intent ? 'capturedCurrent' : 'current')}>
                <h3 className="mb-4 font-medium">
                  {t(owner.state.intent ? 'capturedCurrent' : 'current')}
                </h3>
                <PolicyRows
                  policy={context.limit.stored as DefaultLimitPolicy}
                  currency={context.limit.platform_currency}
                />
                {(context.limit.kind === 'user' || context.limit.kind === 'team') && (
                  <div className="mt-3 space-y-1 text-sm">
                    <p>
                      {limitText('tokensMonthBehavior')}:{' '}
                      {limitText(
                        context.limit.stored.tokens_month_behavior === 'alert_only'
                          ? 'monthlyAlertOnly'
                          : 'monthlyStop',
                      )}
                    </p>
                    <p>
                      {limitText('moneyMonthBehavior')}:{' '}
                      {limitText(
                        context.limit.stored.money_month_behavior === 'alert_only'
                          ? 'monthlyAlertOnly'
                          : 'monthlyStop',
                      )}
                    </p>
                  </div>
                )}
              </section>
              <section aria-label={t(owner.state.intent ? 'capturedDefaults' : 'defaults')}>
                <h3 className="mb-4 font-medium">
                  {t(owner.state.intent ? 'capturedDefaults' : 'defaults')}
                </h3>
                <PolicyRows
                  policy={context.default_rule.policy}
                  currency={context.default_rule.platform_currency}
                />
                {(context.limit.kind === 'user' || context.limit.kind === 'team') && (
                  <p className="mt-3 text-sm text-muted-foreground">
                    {limitText(
                      context.limit.kind === 'team' ? 'teamMonthlyResetStop' : 'monthlyResetStop',
                    )}
                  </p>
                )}
              </section>
            </div>
            <FormField label={t('reason')}>
              <Input
                aria-label={t('reason')}
                disabled={busy || !!owner.state.intent || !query.data?.editable}
                value={reason}
                onValueChange={(value) =>
                  owner.update((previous) => ({ ...previous, reason: value }))
                }
              />
            </FormField>
            <Button type="submit" disabled={busy || uncertain || blocked || !query.data?.editable}>
              {t('confirm')}
            </Button>
          </form>
        )}
        <div className="mt-4 flex flex-wrap gap-2">
          {(blocked || uncertain) && (
            <Button type="button" variant="outline" disabled={busy} onClick={() => void review()}>
              {t('review')}
            </Button>
          )}
          {uncertain && (
            <Button
              type="button"
              disabled={busy || !fresh || !query.data?.editable}
              onClick={() => void dispatch(true)}
            >
              {t('retry')}
            </Button>
          )}
          {owner.state.intent && (
            <Button
              type="button"
              variant="outline"
              disabled={busy || !fresh || !query.data?.editable}
              onClick={() => setAbandon(true)}
            >
              {t('abandon')}
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() => close(uncertain ? 'restoreClosed' : null)}
          >
            {t('cancel')}
          </Button>
        </div>
      </Dialog>
      <Dialog
        open={abandon && fresh}
        onOpenChange={setAbandon}
        title={t('abandonTitle')}
        description={t('abandonHelp')}
        busy={busy}
      >
        <div className="mt-4 flex gap-2">
          <Button
            disabled={busy || !fresh || !query.data?.editable}
            onClick={() => {
              if (
                lock.current ||
                !currentActor() ||
                !currentReview() ||
                !query.data?.editable ||
                !intent.current
              )
                return
              if (!owner.clear(owner.currentClaim())) return
              intent.current = null
              owner.update((previous) => ({
                ...previous,
                intent: null,
                uncertain: false,
                requireReview: true,
                issue: 'abandoned',
              }))
              setAbandon(false)
            }}
          >
            {t('confirmAbandon')}
          </Button>
          <Button variant="outline" disabled={busy} onClick={() => setAbandon(false)}>
            {t('cancel')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
