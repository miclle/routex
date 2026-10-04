import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
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

export default function RestoreDefaults({ target }: { target: DefaultResetTarget }) {
  const session = useSession()
  const access = usePermissions()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const allowed =
    !!actor &&
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
    />
  )
}
export function RestoreControls({
  target,
  actor,
  visible,
  managed,
}: {
  target: DefaultResetTarget
  actor: string
  visible: boolean
  managed?: { canDispatch: () => boolean; generation: string }
}) {
  const { t } = useTranslation('defaultLimits')
  const [open, setOpen] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  return (
    <div className="space-y-2">
      {visible && (
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            if (managed && !managed.canDispatch()) return
            setNotice(null)
            setOpen(true)
          }}
        >
          {t('restore')}
        </Button>
      )}
      {visible && notice && <p role="status">{t(notice)}</p>}
      {open && (
        <RestoreDialog
          key={`${actor}:${target.kind}:${target.id}`}
          target={target}
          actor={actor}
          visible={visible}
          managed={managed}
          close={(result) => {
            setNotice(result)
            setOpen(false)
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
}: {
  target: DefaultResetTarget
  actor: string
  visible: boolean
  close: (notice: string | null) => void
  managed?: { canDispatch: () => boolean; generation: string }
}) {
  const { t } = useTranslation('defaultLimits')
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
  const [reviewed, setReviewed] = useState<DefaultLimitResetContext | null>(null)
  const [reason, setReason] = useState('')
  const [issue, setIssue] = useState<string | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const alive = useRef(true)
  const intent = useRef<{ review: DefaultLimitResetContext; reason: string } | null>(null)
  const controller = useRef<AbortController | null>(null)
  useLayoutEffect(() => {
    if (managed && (!visible || query.isFetching)) controller.current?.abort()
  }, [managed, visible, query.isFetching])
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
    }
  }, [])
  const currentActor = () =>
    alive.current &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    (!managed || managed.canDispatch())
  const fresh = visible && query.isSuccess && !query.isFetching
  if (fresh && query.data && !reviewed) setReviewed(query.data)
  const context = reviewed ?? query.data ?? null
  const stale = !!reviewed && query.data?.etag !== reviewed.etag
  const blocked = stale || issue === 'conflict' || issue === 'failed'
  async function dispatch(retry = false) {
    const session = cache.getQueryData<Session>(sessionKey)
    // A cache refetch can begin before React hides the reviewed controls.
    const reviewState = cache.getQueryState<DefaultLimitResetContext>(queryKey)
    const currentReview =
      !managed ||
      (reviewState?.status === 'success' &&
        reviewState.fetchStatus === 'idle' &&
        !reviewState.error &&
        reviewState.data === query.data)
    if (
      lock.current ||
      !currentReview ||
      !currentActor() ||
      !session ||
      !fresh ||
      !query.data?.editable ||
      (!retry && (blocked || uncertain))
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
      intent.current = { review: context, reason: reason.trim() }
      setReviewed(context)
    }
    if (!intent.current) return
    lock.current = true
    setBusy(true)
    const pending = new AbortController()
    controller.current = pending
    try {
      const result = await restoreDefaultLimits(
        target,
        intent.current.review,
        intent.current.reason,
        session.csrf_token,
        managed ? pending.signal : undefined,
      )
      const latestReview = cache.getQueryState<DefaultLimitResetContext>(queryKey)
      const obsoleteReview =
        latestReview?.fetchStatus !== 'idle' ||
        latestReview.status !== 'success' ||
        !!latestReview.error ||
        latestReview.dataUpdateCount !== reviewState?.dataUpdateCount ||
        latestReview.errorUpdateCount !== reviewState?.errorUpdateCount
      if (
        managed &&
        alive.current &&
        (!currentActor() || pending.signal.aborted || obsoleteReview || !result.runtime_applied)
      ) {
        setUncertain(true)
        setIssue('uncertain')
      } else if (currentActor()) {
        void cache.invalidateQueries({ queryKey: ['resource-limits'] })
        close(result.runtime_applied ? 'restored' : 'pending')
      }
    } catch (error) {
      if (currentActor() || (managed && alive.current)) {
        const status = isAxiosError(error) ? error.response?.status : undefined
        if (!status || status >= 500) setUncertain(true)
        setIssue(status === 409 ? 'conflict' : !status || status >= 500 ? 'uncertain' : 'failed')
      }
    } finally {
      lock.current = false
      if (currentActor() || (managed && alive.current)) setBusy(false)
    }
  }
  async function review() {
    if (lock.current || (managed && !currentActor())) return
    lock.current = true
    setBusy(true)
    try {
      const result = await query.refetch()
      if (currentActor() && result.data && !result.error && !uncertain) {
        setReviewed(result.data)
        intent.current = null
        setIssue(null)
      }
    } finally {
      lock.current = false
      if (currentActor() || (managed && alive.current)) setBusy(false)
    }
  }
  if (!visible) return null
  return (
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
            <section aria-label={t('current')}>
              <h3 className="mb-4 font-medium">{t('current')}</h3>
              <PolicyRows
                policy={context.limit.stored as DefaultLimitPolicy}
                currency={context.limit.platform_currency}
              />
            </section>
            <section aria-label={t('defaults')}>
              <h3 className="mb-4 font-medium">{t('defaults')}</h3>
              <PolicyRows
                policy={context.default_rule.policy}
                currency={context.default_rule.platform_currency}
              />
            </section>
          </div>
          <FormField label={t('reason')}>
            <Input
              aria-label={t('reason')}
              disabled={busy || uncertain || !query.data?.editable}
              value={reason}
              onValueChange={setReason}
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
  )
}
