import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react'
import { useQuery, useQueryClient, type InfiniteData } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AxiosError } from 'axios'
import { getMemberState, setMemberState, validMemberStateReason } from '@/api/member-state'
import type { MemberStateInput, MemberStateRecord, MemberStateResult } from '@/types/member-state'
import type { Session } from '@/types/auth'
import type { MemberDetail } from '@/types/member-recent-login'
import type { MemberListPage } from '@/types/member-list'
import { FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog } from '@/components/ui/dialog'

type Action = { role: 'member' | 'admin' } | { disabled: boolean }
type Operation = 'baseRole' | 'disable' | 'enable' | 'reactivate'
type Review = { etag: string; action: Action; operation: Operation }
type Intent = { etag: string; body: MemberStateInput }
type Props = {
  actor: string
  target: string
  generation: number
  ready: boolean
  contextKind: 'detail' | 'list'
  contextQueryKey: readonly unknown[]
  mode: 'settings' | 'status'
  owner: string
  returnFocus?: () => HTMLButtonElement | false
  onClose?: () => void
  children?: ReactNode
}
function useCacheRevision(keys: string) {
  const cache = useQueryClient()
  const snapshot = useCallback(
    () =>
      JSON.parse(keys)
        .map((key: unknown[]) => {
          const s = cache.getQueryState(key)
          return `${s?.status}:${s?.fetchStatus}:${s?.isInvalidated}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
        })
        .join('|'),
    [cache, keys],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.parse(keys).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, keys],
  )
  return { snapshot, revision: useSyncExternalStore(subscribe, snapshot, snapshot) }
}
export default function MemberState(props: Props) {
  return <State key={`${props.actor}:${props.target}:${props.owner}`} {...props} />
}
function State({
  actor,
  target,
  generation,
  ready,
  contextKind,
  contextQueryKey,
  mode,
  returnFocus,
  onClose,
  children,
}: Props) {
  const { t } = useTranslation('governance'),
    cache = useQueryClient()
  const parent = useCacheRevision(
    JSON.stringify([['auth', 'session'], ['permissions', actor], contextQueryKey]),
  )
  const [authLost, setAuthLost] = useState(false)
  function authority() {
    const auth = cache.getQueryState<Session>(['auth', 'session']),
      grants = cache.getQueryState<string[]>(['permissions', actor]),
      context = cache.getQueryState<MemberDetail | InfiniteData<MemberListPage>>(contextQueryKey)
    const subject = context?.data
    return (
      ready &&
      !authLost &&
      parent.snapshot() === parent.revision &&
      [auth, grants, context].every(
        (s) => s?.status === 'success' && s.fetchStatus === 'idle' && !s.isInvalidated && !s.error,
      ) &&
      auth?.data?.user.id === actor &&
      grants?.data?.some((p) => p === 'members.read' || p === 'members.write') === true &&
      !!subject &&
      (contextKind === 'detail'
        ? 'id' in subject && subject.id === target
        : 'pages' in subject &&
          subject.pages.every((p) => p.actor_user_id === actor) &&
          subject.pages.some((p) => p.items.some((r) => r.id === target)))
    )
  }
  const queryKey = ['admin', 'member-state', actor, target, generation, parent.revision]
  const query = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Member state authority unavailable')
      const data = await getMemberState(target, signal)
      if (signal.aborted || !authority()) throw new Error('Member state authority unavailable')
      return data
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const resource = useCacheRevision(JSON.stringify([queryKey]))
  function current() {
    const s = cache.getQueryState<MemberStateRecord>(queryKey)
    return (
      authority() &&
      resource.snapshot() === resource.revision &&
      query.isSuccess &&
      !query.isFetching &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.isInvalidated &&
      !s.error &&
      s.data === query.data
    )
  }
  function canWrite(action: Action, record = query.data) {
    const auth = cache.getQueryData<Session>(['auth', 'session']),
      grants = cache.getQueryData<string[]>(['permissions', actor])
    return (
      current() &&
      !!auth?.csrf_token &&
      grants?.includes('members.write') === true &&
      !!record &&
      ('role' in action
        ? auth.user.role === 'admin' && record.can_change_base_role
        : record.can_change_status &&
          (auth.user.role === 'admin' || (target !== actor && record.base_role !== 'admin')))
    )
  }
  const [draft, setDraft] = useState<{ role: 'member' | 'admin'; etag: string } | null>(null)
  const [review, setReview] = useState<Review | null>(null)
  const [needsReview, setNeedsReview] = useState(false)
  const [intent, setIntent] = useState<Intent | null>(null)
  const [reason, setReason] = useState(''),
    [open, setOpen] = useState(false)
  const [notice, setNotice] = useState<
    'conflict' | 'uncertain' | 'invalidReason' | 'authorityLost' | 'authLost' | null
  >(null)
  const [confirmed, setConfirmed] = useState<{ record: MemberStateResult; retry: boolean } | null>(
    null,
  )
  const scope = `${generation}:${parent.revision}:${resource.revision}`
  const [busyScope, setBusyScope] = useState<string | null>(null)
  const busy = busyScope === scope
  const fresh = current(),
    page = fresh ? query.data : undefined
  const retryAllowed = !intent || canWrite(intent.body)
  const visible = !!page && retryAllowed && !authLost
  if (mode === 'status' && page && !review && !confirmed) {
    setReview({
      etag: page.etag,
      action: { disabled: !page.disabled },
      operation: page.disabled
        ? page.activation_mode === 'reactivate'
          ? 'reactivate'
          : 'enable'
        : 'disable',
    })
    setOpen(true)
  }
  const alive = useRef(true),
    serial = useRef(0),
    locked = useRef(false),
    controller = useRef<AbortController | null>(null),
    trigger = useRef<HTMLButtonElement | null>(null),
    baseTrigger = useRef<HTMLButtonElement | null>(null)
  useLayoutEffect(() => {
    if (!fresh) {
      serial.current++
      controller.current?.abort()
      locked.current = false
    }
  }, [fresh, generation, parent.revision])
  useLayoutEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
    }
  }, [])
  const action = intent?.body ?? review?.action
  const stale = !!page && !!review && (review.etag !== page.etag || needsReview) && !intent
  const operationFor = (action: Action, record: MemberStateRecord): Operation =>
    'role' in action
      ? 'baseRole'
      : action.disabled
        ? 'disable'
        : record.activation_mode === 'reactivate'
          ? 'reactivate'
          : 'enable'
  const actionName = review?.operation ?? 'baseRole'
  function start(next: Action, event: HTMLButtonElement | null) {
    if (!canWrite(next) || !page || busy || intent) return
    trigger.current = event
    setReview({
      etag: 'role' in next ? (draft?.etag ?? page.etag) : page.etag,
      action: next,
      operation: operationFor(next, page),
    })
    setOpen(true)
    setNotice(null)
    setConfirmed(null)
  }
  async function reviewCurrent() {
    if (!authority() || intent || busy) return
    const data = await query.refetch()
    const state = cache.getQueryState<MemberStateRecord>(queryKey)
    if (
      !alive.current ||
      !authority() ||
      !data.isSuccess ||
      !state?.data ||
      state.fetchStatus !== 'idle' ||
      state.isInvalidated ||
      state.error ||
      state.data !== data.data
    )
      return
    if (review)
      setReview({
        ...review,
        etag: data.data.etag,
        operation: operationFor(review.action, data.data),
      })
    if (draft) setDraft({ ...draft, etag: data.data.etag })
    setNotice(null)
    setNeedsReview(false)
  }
  async function submit() {
    if (!action || locked.current || busy || !canWrite(action) || stale) return
    if (!intent && !validMemberStateReason(reason)) {
      setNotice('invalidReason')
      return
    }
    const captured = intent ?? {
      etag: review!.etag,
      body: { ...review!.action, reason } as MemberStateInput,
    }
    const auth = cache.getQueryData<Session>(['auth', 'session'])
    if (!auth?.csrf_token) return
    const operation = ++serial.current,
      abort = new AbortController()
    controller.current = abort
    locked.current = true
    setBusyScope(scope)
    setIntent(captured)
    setNotice(null)
    try {
      const result = await setMemberState(
        target,
        captured.etag,
        captured.body,
        auth.csrf_token,
        abort.signal,
      )
      if (
        !alive.current ||
        operation !== serial.current ||
        abort.signal.aborted ||
        !canWrite(captured.body)
      )
        return
      setConfirmed({ record: result, retry: !!intent })
      setIntent(null)
      setDraft(null)
      setNotice(null)
      void cache.invalidateQueries({ queryKey: ['admin', 'member-state', actor, target] })
      void cache.invalidateQueries({ queryKey: ['admin', 'member', actor, target] })
      void cache.invalidateQueries({ queryKey: ['admin', 'member-access-summary', actor, target] })
      void cache.invalidateQueries({ queryKey: ['admin', 'members'] })
      void cache.invalidateQueries({ queryKey: ['permissions'] })
      if (target === actor) void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
    } catch (error) {
      if (!alive.current || operation !== serial.current) return
      const status = error instanceof AxiosError ? error.response?.status : undefined
      if (status === 401) {
        setAuthLost(true)
        setNotice('authLost')
        return
      }
      if (status === 403) {
        setNotice('authorityLost')
        void cache.invalidateQueries({ queryKey: ['auth', 'session'] })
        void cache.invalidateQueries({ queryKey: ['permissions'] })
        void cache.invalidateQueries({ queryKey })
        return
      }
      if (status === 409) {
        setNotice('conflict')
        void query.refetch()
      } else setNotice('uncertain')
    } finally {
      if (alive.current && operation === serial.current) {
        locked.current = false
        setBusyScope(null)
      }
    }
  }
  const success =
    confirmed &&
    page &&
    confirmed.record.etag === page.etag &&
    (confirmed.record.effect === 'current_base_identity' || page.account_access_runtime_applied)
  const guidance = authLost
    ? 'authLost'
    : !retryAllowed
      ? 'authorityLost'
      : query.isError
        ? 'failed'
        : 'loading'
  const dialog = (
    <Dialog
      open={visible && open && !!action && canWrite(action)}
      onOpenChange={(next) => {
        if (!next) {
          setOpen(false)
          if (mode === 'status') onClose?.()
        }
      }}
      busy={busy}
      finalFocus={() => {
        if (!current() || !action || !canWrite(action)) return false
        const node = mode === 'status' ? returnFocus?.() : trigger.current
        return node && node.isConnected ? node : false
      }}
      title={t(`memberState.${actionName}Title`)}
      description={t(`memberState.${actionName}Description`, { name: page?.name ?? '' })}
    >
      {success ? (
        <p role="status">
          {t(confirmed!.retry ? 'memberState.reconciled' : 'memberState.confirmed', {
            effect: t(`memberState.${confirmed!.record.effect}`),
          })}
        </p>
      ) : (
        <>
          {action && 'role' in action && (
            <p className="mb-3 text-sm">
              {t('memberState.desiredRole', {
                role: t(action.role === 'admin' ? 'common.admin' : 'common.member'),
              })}
            </p>
          )}
          <FormField label={t('memberState.reason')}>
            <Textarea
              value={intent?.body.reason ?? reason}
              onChange={(event) => {
                if (!intent && !busy && current()) setReason(event.target.value)
              }}
              disabled={busy || !!intent}
            />
          </FormField>
          {notice && (
            <p role="alert" className="mt-3 text-sm">
              {t(`memberState.${notice}`)}
            </p>
          )}
          {intent && notice !== 'uncertain' && (
            <p role="status" className="mt-3 text-sm">
              {t('memberState.uncertain')}
            </p>
          )}
          {stale && (
            <p role="alert" className="mt-3 text-sm">
              {t('memberState.stale')}
            </p>
          )}
          {intent && (
            <>
              <p className="mt-3 text-sm">{t('memberState.abandonHelp')}</p>
              <Button
                variant="outline"
                className="mt-3"
                disabled={busy || !action || !canWrite(action)}
                onClick={() => {
                  if (!intent || locked.current || busy || !canWrite(intent.body)) return
                  setReason(intent.body.reason)
                  setIntent(null)
                  setNeedsReview(true)
                  setNotice(null)
                }}
              >
                {t('memberState.abandon')}
              </Button>
            </>
          )}
          {!intent && stale && (
            <Button variant="outline" className="mt-3" onClick={() => void reviewCurrent()}>
              {t('memberState.review')}
            </Button>
          )}
          <Button
            className={
              actionName === 'disable'
                ? 'mt-4 bg-destructive text-white hover:bg-destructive/90'
                : 'mt-4'
            }
            disabled={busy || stale || !action || !canWrite(action)}
            onClick={() => void submit()}
          >
            {t(intent ? 'memberState.retry' : `memberState.confirm_${actionName}`)}
          </Button>
          <Button
            variant="outline"
            className="mt-4 ml-2"
            disabled={busy}
            onClick={() => {
              setOpen(false)
              if (mode === 'status') onClose?.()
            }}
          >
            {t('common:cancel_4d0b4')}
          </Button>
        </>
      )}
    </Dialog>
  )
  if (!visible)
    return (
      <>
        {mode === 'settings' && children}
        <p role={query.isError || authLost || !retryAllowed ? 'alert' : 'status'}>
          {t(`memberState.${guidance}`)}
        </p>
        {authority() && query.isError && (
          <Button variant="outline" onClick={() => void query.refetch()}>
            {t('memberState.refresh')}
          </Button>
        )}
      </>
    )
  if (mode === 'status')
    return action && canWrite(action) ? (
      dialog
    ) : (
      <p role="alert">{t('memberState.actionUnavailable')}</p>
    )
  return (
    <>
      <section className="rounded-lg border">
        <h3 className="border-b p-4 font-medium">{t('common.baseRole')}</h3>
        <form
          className="space-y-4 p-4"
          aria-label={t('common.baseRole')}
          onSubmit={(event) => {
            event.preventDefault()
            const role = String(new FormData(event.currentTarget).get('role'))
            if (role === 'member' || role === 'admin') start({ role }, baseTrigger.current)
          }}
        >
          <FormField label={t('common.baseRole')}>
            <select
              name="role"
              className="h-10 rounded-md border bg-background px-3"
              value={draft?.role ?? page!.base_role}
              onChange={(event) => {
                if (canWrite({ role: page!.base_role }) && !intent)
                  setDraft({ role: event.target.value as 'member' | 'admin', etag: page!.etag })
              }}
              disabled={!canWrite({ role: page!.base_role }) || busy || !!intent}
            >
              <option value="member">{t('common.member')}</option>
              <option value="admin">{t('common.admin')}</option>
            </select>
          </FormField>
          {canWrite({ role: page!.base_role }) && !intent && (
            <Button ref={baseTrigger} type="submit">
              {t('members.saveBaseRole')}
            </Button>
          )}
          {draft && draft.etag !== page!.etag && !intent && (
            <>
              <p role="alert">{t('memberState.stale')}</p>
              <Button variant="outline" onClick={() => void reviewCurrent()}>
                {t('memberState.review')}
              </Button>
            </>
          )}
        </form>
      </section>
      {children}
      <section className="rounded-lg border p-4">
        <h3 className="mb-3 font-medium">{t('members.accountAccess')}</h3>
        <p className="mb-4 text-sm text-muted-foreground">{t('members.disableExplanation')}</p>
        <p className="mb-3 text-sm">
          {t(`memberState.status_${page!.status}`)} ·{' '}
          {t(page!.account_access_runtime_applied ? 'memberState.applied' : 'memberState.unknown')}
        </p>
        {canWrite({ disabled: !page!.disabled }) && !intent && (
          <Button
            variant="outline"
            onClick={(event) => start({ disabled: !page!.disabled }, event.currentTarget)}
          >
            {t(
              page!.activation_mode === 'reactivate'
                ? 'memberState.reactivate'
                : page!.disabled
                  ? 'common.enable'
                  : 'common.disable',
            )}
          </Button>
        )}
      </section>
      {intent && (
        <>
          <p role="status">{t('memberState.uncertain')}</p>
          <Button
            variant="outline"
            onClick={() => {
              if (canWrite(intent.body)) setOpen(true)
            }}
          >
            {t('memberState.resume')}
          </Button>
        </>
      )}
      {success && (
        <p role="status">
          {t(confirmed!.retry ? 'memberState.reconciled' : 'memberState.confirmed', {
            effect: t(`memberState.${confirmed!.record.effect}`),
          })}
        </p>
      )}
      {dialog}
    </>
  )
}
