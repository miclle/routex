import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type RefObject,
} from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import axios from 'axios'
import { KeyRound, MoreHorizontal, Pause } from 'lucide-react'
import { getPermissions } from '@/api/governance'
import {
  disableMemberKey,
  getMemberKey,
  listMemberKeys,
  validMemberKeyReason,
} from '@/api/member-keys'
import type { MemberKeyRecord } from '@/types/member-keys'
import type { Session } from '@/types/auth'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Menu, MenuItem } from '@/components/ui/menu'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { FormField, QueryState } from '@/components/app/CatalogUI'

type Props = {
  userId: string
  actorId: string
  csrf: string
  generation: number
  sessionReady: boolean
  targetReady: boolean
  targetQueryKey: readonly unknown[]
}
type Intent = { keyId: string; etag: string; reason: string }
function useReadState(key: readonly unknown[]) {
  const cache = useQueryClient(),
    serialized = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((e) => {
        if (JSON.stringify(e.query.queryKey) === serialized) notify()
      }),
    [cache, serialized],
  )
  const snapshot = useCallback(() => {
    const s = cache.getQueryState(JSON.parse(serialized))
    return `${s?.dataUpdateCount ?? 0}:${s?.errorUpdateCount ?? 0}:${s?.status}:${s?.fetchStatus}`
  }, [cache, serialized])
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}
export default function MemberKeys(props: Props) {
  return <Keys key={`${props.actorId}:${props.userId}`} {...props} />
}
function Keys({
  userId,
  actorId,
  csrf,
  generation,
  sessionReady,
  targetReady,
  targetQueryKey,
}: Props) {
  const { t, i18n } = useTranslation('governance'),
    cache = useQueryClient()
  const permissionKey = ['member-keys-permissions', actorId, generation] as const
  const permissions = useQuery({
    queryKey: permissionKey,
    queryFn: async ({ signal }) => {
      const data = await getPermissions(signal)
      if (!Array.isArray(data) || !data.every((value) => typeof value === 'string'))
        throw new Error('Member Key authority unavailable')
      return data
    },
    enabled: sessionReady && !!actorId,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const permissionState = useReadState(permissionKey),
    sessionState = useReadState(['auth', 'session']),
    targetState = useReadState(targetQueryKey)
  const authorized =
    sessionReady &&
    targetReady &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    permissions.data.includes('members.read')
  const basis = `${generation}:${permissionState}`
  const listKey = ['member-keys', actorId, userId, basis] as const
  const list = useInfiniteQuery({
    queryKey: listKey,
    queryFn: ({ pageParam, signal }) => listMemberKeys(userId, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (p) => p.next_cursor ?? undefined,
    enabled: authorized,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const listState = useReadState(listKey)
  const [selected, setSelected] = useState<string | null>(null),
    [reason, setReason] = useState(''),
    [reviewed, setReviewed] = useState<MemberKeyRecord | null>(null)
  const [uncertain, setUncertain] = useState(false),
    [conflict, setConflict] = useState(false),
    [busyToken, setBusyToken] = useState<string | null>(null)
  const [notice, setNotice] = useState<
    '' | 'reasonError' | 'uncertain' | 'conflict' | 'failed' | 'confirmed'
  >('')
  const [confirmed, setConfirmed] = useState<{
    id: string
    etag: string
    generation: number
  } | null>(null)
  const intent = useRef<Intent | null>(null),
    controller = useRef<AbortController | null>(null),
    lock = useRef(false),
    alive = useRef(true)
  const detailKey = [
    'member-key-review',
    actorId,
    userId,
    basis,
    listState.split(':')[0],
    selected,
  ] as const
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: async ({ signal }) => {
      const prefix = `${actorId}:${userId}:${basis}:${sessionState}:${targetState}:${listState}:`
      const row = await getMemberKey(userId, selected!, signal)
      if (
        !signal.aborted &&
        alive.current &&
        currentToken.current.startsWith(prefix) &&
        !intent.current
      ) {
        setReviewed((existing) => existing ?? row)
      }
      return row
    },
    enabled: authorized && !!selected,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const detailState = useReadState(detailKey)
  const listReady = authorized && list.isSuccess && !list.isFetching
  const rowsReady = listReady && (!selected || (detail.isSuccess && !detail.isFetching))
  const detailReady =
    rowsReady && detail.isSuccess && !detail.isFetching && detail.data.id === selected
  const canWrite = permissions.data?.includes('members.keys.disable') === true
  const token = `${actorId}:${userId}:${basis}:${sessionState}:${targetState}:${listState}:${detailState}`
  const currentToken = useRef(token)
  useLayoutEffect(() => {
    currentToken.current = token
  }, [token])
  const busy = busyToken === token
  function live(captured: string) {
    const s = cache.getQueryState(['auth', 'session']),
      p = cache.getQueryState(permissionKey),
      l = cache.getQueryState(listKey),
      d = cache.getQueryState(detailKey),
      target = cache.getQueryState<{ id: string }>(targetQueryKey)
    const snap = (state: typeof s) =>
      `${state?.dataUpdateCount ?? 0}:${state?.errorUpdateCount ?? 0}:${state?.status}:${state?.fetchStatus}`
    return (
      alive.current &&
      currentToken.current === captured &&
      authorized &&
      canWrite &&
      !!csrf &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      snap(s) === sessionState &&
      p?.status === 'success' &&
      p.fetchStatus === 'idle' &&
      snap(p) === permissionState &&
      l?.status === 'success' &&
      l.fetchStatus === 'idle' &&
      snap(l) === listState &&
      target?.status === 'success' &&
      target.fetchStatus === 'idle' &&
      target.data?.id === userId &&
      snap(target) === targetState &&
      d?.status === 'success' &&
      d.fetchStatus === 'idle' &&
      snap(d) === detailState
    )
  }
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      controller.current?.abort()
    }
  }, [])
  useEffect(() => {
    controller.current?.abort()
    controller.current = null
    lock.current = false
  }, [token, authorized])

  // Reviewing a Key replaces row nodes; focus the current exact row, never the removed menu item.
  const rowTriggers = useRef(new Map<string, RefObject<HTMLButtonElement | null>>())
  const registerTrigger = useCallback((id: string, ref: RefObject<HTMLButtonElement | null>) => {
    rowTriggers.current.set(id, ref)
    return () => {
      if (rowTriggers.current.get(id) === ref) rowTriggers.current.delete(id)
    }
  }, [])
  const focusKey = useRef<string | null>(null)
  function restoreRowFocus() {
    const session = cache.getQueryState<Session>(['auth', 'session'])
    const permission = cache.getQueryState<string[]>(permissionKey)
    const target = cache.getQueryState<{ id: string }>(targetQueryKey)
    const currentList = cache.getQueryState(listKey)
    const trigger = focusKey.current
      ? rowTriggers.current.get(focusKey.current)?.current
      : undefined
    if (
      !alive.current ||
      !sessionReady ||
      !targetReady ||
      [session, permission, target, currentList].some(
        (state) => state?.status !== 'success' || state.fetchStatus !== 'idle' || !!state.error,
      ) ||
      session?.data?.user.id !== actorId ||
      target?.data?.id !== userId ||
      !permission?.data?.includes('members.read') ||
      !permission.data.includes('members.keys.disable') ||
      !trigger?.isConnected
    )
      return false
    return trigger
  }
  function open(row: MemberKeyRecord) {
    if (!rowsReady || !canWrite || !row.disable_eligible || lock.current || intent.current) return
    focusKey.current = row.id
    setSelected(row.id)
    setReviewed(null)
    setReason('')
    setNotice('')
    setConflict(false)
  }
  function close() {
    if (lock.current) return
    setSelected(null)
    setBusyToken(null)
    setReviewed(null)
    setReason('')
    intent.current = null
    setUncertain(false)
    setConflict(false)
    setNotice('')
  }
  async function review() {
    if (lock.current || uncertain || !detailReady) return
    const prefix = `${actorId}:${userId}:${basis}:${sessionState}:${targetState}:${listState}:`
    const result = await detail.refetch()
    if (alive.current && result.isSuccess && currentToken.current.startsWith(prefix)) {
      setReviewed(result.data)
      setConflict(false)
      setNotice('')
    }
  }
  async function submit(retry = false) {
    const capturedToken = token
    if (lock.current || !detailReady || !live(capturedToken)) return
    if (
      retry
        ? !uncertain || !intent.current
        : uncertain ||
          conflict ||
          !reviewed ||
          reviewed.etag !== detail.data.etag ||
          !detail.data.disable_eligible
    )
      return
    if (!retry) {
      if (!validMemberKeyReason(reason)) {
        setNotice('reasonError')
        return
      }
      intent.current = { keyId: selected!, etag: reviewed!.etag, reason: reason.trim() }
    }
    const captured = intent.current!
    lock.current = true
    setBusyToken(token)
    setUncertain(true)
    setNotice('')
    const owned = new AbortController()
    controller.current = owned
    try {
      const result = await disableMemberKey(
        userId,
        captured.keyId,
        captured.etag,
        captured.reason,
        csrf,
        owned.signal,
      )
      if (!live(capturedToken) || owned.signal.aborted) return
      intent.current = null
      setUncertain(false)
      setSelected(null)
      setReviewed(null)
      setReason('')
      setConfirmed({ id: result.id, etag: result.etag, generation })
      setNotice('confirmed')
      void cache.invalidateQueries({ queryKey: ['member-keys', actorId, userId] })
    } catch (error) {
      if (!live(capturedToken) || owned.signal.aborted) return
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      if (retry || !status || status >= 500) {
        setNotice('uncertain')
      } else {
        intent.current = null
        setUncertain(false)
        setConflict(status === 409)
        setNotice(status === 409 ? 'conflict' : 'failed')
      }
      if (status === 401 || status === 403) {
        void cache.invalidateQueries({ queryKey: permissionKey })
        void cache.invalidateQueries({ queryKey: detailKey })
      }
    } finally {
      if (controller.current === owned) {
        controller.current = null
        lock.current = false
        if (alive.current) setBusyToken(null)
      }
    }
  }
  const unknown = t('memberKeys.unknown'),
    unlimited = t('memberKeys.unlimited')
  const maximum = (v: number | null) =>
    v === null ? unlimited : v.toLocaleString(i18n.resolvedLanguage)
  const date = (v: string) =>
    new Date(v).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  const rows = rowsReady ? list.data.pages.flatMap((p) => p.items) : []
  return (
    <section aria-label={t('memberKeys.title')} className="space-y-4">
      {!authorized && (
        <QueryState
          pending={sessionReady && (permissions.isPending || permissions.isFetching)}
          error={permissions.error}
          retry={() => void permissions.refetch()}
        />
      )}
      {permissions.isSuccess &&
        !permissions.isFetching &&
        !permissions.data.includes('members.read') && (
          <p role="alert">{t('memberKeys.readDenied')}</p>
        )}
      {authorized && (
        <QueryState
          pending={list.isFetching}
          error={list.error}
          retry={() => void list.refetch()}
        />
      )}
      {rowsReady && (
        <>
          <p className="text-sm text-muted-foreground">{t('memberKeys.help')}</p>
          <Table aria-label={t('memberKeys.table')} className="min-w-[1420px]">
            <thead>
              <tr>
                {(
                  [
                    'name',
                    'status',
                    'models',
                    'tokens',
                    'budget',
                    'rates',
                    'lastUse',
                    'created',
                    'updated',
                    'actions',
                  ] as const
                ).map((key) => (
                  <th key={key}>{t(`memberKeys.${key}`)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const month = row.limits.quota_usage?.activated
                    ? row.limits.quota_usage.month
                    : null,
                  active = row.limits.quota_usage?.activated ? row.limits.quota_usage.active : null,
                  policy = row.limits.effective
                return (
                  <tr key={row.id}>
                    <td className="w-[180px]">
                      <span className="flex items-center gap-2">
                        <KeyRound aria-hidden="true" className="size-4 shrink-0" />
                        {row.name}
                      </span>
                    </td>
                    <td className="w-[86px]">
                      <Badge variant="outline">{t(`memberKeys.state_${row.status}`)}</Badge>
                      {row.expired && <p>{t('memberKeys.expired')}</p>}
                    </td>
                    <td className="w-[320px]">
                      <div className="flex flex-wrap gap-1">
                        {row.model_ids.length
                          ? row.model_ids.map((id) => (
                              <Badge key={id} variant="outline">
                                {id}
                              </Badge>
                            ))
                          : t('memberKeys.noModels')}
                      </div>
                    </td>
                    <td className="w-[155px] space-y-1">
                      <p>
                        {t('memberKeys.monthly', {
                          used: month?.tokens_used ?? unknown,
                          limit: maximum(policy.tokens_month),
                        })}
                      </p>
                      {row.limits.stored.tokens_month !== policy.tokens_month && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberKeys.storedTokens', {
                            value: maximum(row.limits.stored.tokens_month),
                          })}
                        </p>
                      )}
                      {row.limits.quota_usage?.as_of && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberKeys.observed', {
                            value: new Date(row.limits.quota_usage.as_of).toLocaleString(
                              i18n.resolvedLanguage,
                              { timeZone: row.limits.quota_usage.time_zone },
                            ),
                            zone: row.limits.quota_usage.time_zone,
                          })}
                        </p>
                      )}
                      {month && (
                        <>
                          <p>{t(month.covered ? 'memberKeys.covered' : 'memberKeys.incomplete')}</p>
                          <p>{t('memberKeys.tokenUnknown', { value: month.tokens_unknown })}</p>
                          <p>{t('memberKeys.monthlyHeld', { value: month.tokens_held })}</p>
                        </>
                      )}
                      <p>{t('memberKeys.liveTokens', { value: active?.tokens_held ?? unknown })}</p>
                      {row.limits.shared_rotation_quota && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberKeys.shared', { id: row.limits.quota_root_id })}
                        </p>
                      )}
                    </td>
                    <td className="w-[190px] space-y-1">
                      <p>
                        {t('memberKeys.amountLimit', {
                          value:
                            policy.money_month === null
                              ? unlimited
                              : `${policy.money_month} ${policy.currency}`,
                        })}
                      </p>
                      {(row.limits.stored.money_month !== policy.money_month ||
                        row.limits.stored.currency !== policy.currency) && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberKeys.storedMoney', {
                            value:
                              row.limits.stored.money_month === null
                                ? unlimited
                                : `${row.limits.stored.money_month} ${row.limits.stored.currency}`,
                          })}
                        </p>
                      )}
                      <p>{t('memberKeys.usedMoney')}</p>
                      <Amounts amounts={month?.money_used ?? null} />
                      <p>{t('memberKeys.heldMoney')}</p>
                      <Amounts amounts={month?.money_held ?? null} />
                      <p>{t('memberKeys.liveMoney')}</p>
                      <Amounts amounts={active?.money_held ?? null} />
                      <p>
                        {t('memberKeys.moneyUnknown', { value: month?.money_unknown ?? unknown })}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {t('memberKeys.currency', { value: row.limits.platform_currency })}
                      </p>
                    </td>
                    <td className="w-[300px] space-y-1">
                      <p>
                        {t('memberKeys.rateValues', {
                          rpm: maximum(policy.rpm),
                          tpm: maximum(policy.tpm),
                          concurrency: maximum(policy.concurrency),
                        })}
                      </p>
                      {(['rpm', 'tpm', 'concurrency'] as const).some(
                        (field) => row.limits.stored[field] !== policy[field],
                      ) && (
                        <p className="text-xs text-muted-foreground">
                          {t('memberKeys.storedRates', {
                            rpm: maximum(row.limits.stored.rpm),
                            tpm: maximum(row.limits.stored.tpm),
                            concurrency: maximum(row.limits.stored.concurrency),
                          })}
                        </p>
                      )}
                      <p>
                        {t('memberKeys.rateUsage', {
                          rpm: row.limits.rpm_used ?? unknown,
                          active: row.limits.active ?? unknown,
                        })}
                      </p>
                      <p>
                        {t(row.limits.enforced ? 'memberKeys.enforced' : 'memberKeys.notEnforced')}
                      </p>
                    </td>
                    <td className="w-[110px]">
                      {row.last_use_coverage === 'recorded'
                        ? date(row.last_used_at!)
                        : t(
                            row.last_use_coverage === 'unknown'
                              ? 'memberKeys.unknown'
                              : 'memberKeys.noUse',
                          )}
                    </td>
                    <td className="w-[95px]">{date(row.created_at)}</td>
                    <td className="w-[95px]">{date(row.updated_at)}</td>
                    <td className="w-[72px]">
                      {canWrite && (
                        <MemberKeyActions row={row} open={open} register={registerTrigger} />
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
          {!rows.length && <p>{t('memberKeys.empty')}</p>}
          {list.hasNextPage && (
            <div className="text-center">
              <Button
                variant="outline"
                onClick={() => void list.fetchNextPage()}
                disabled={list.isFetchingNextPage}
              >
                {t('memberKeys.loadMore')}
              </Button>
            </div>
          )}
          {!selected &&
            notice === 'confirmed' &&
            confirmed?.generation === generation &&
            rows.some(
              (row) =>
                row.id === confirmed.id && row.status === 'disabled' && row.etag === confirmed.etag,
            ) && <p role="status">{t('memberKeys.confirmed')}</p>}
        </>
      )}
      {selected && listReady && (
        <QueryState
          pending={detail.isFetching}
          error={detail.error}
          retry={() => void detail.refetch()}
        />
      )}
      <Dialog
        finalFocus={restoreRowFocus}
        open={!!selected && detailReady && canWrite}
        onOpenChange={(v) => {
          if (!v) close()
        }}
        busy={busy}
        title={t('memberKeys.disableTitle')}
        description={t('memberKeys.disableDescription')}
      >
        {detailReady && (
          <form
            className="space-y-5"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
          >
            <div className="rounded-lg border p-4 text-sm">
              <p className="font-medium">{detail.data.name}</p>
              <p className="break-all">{detail.data.id}</p>
            </div>
            <p className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
              {t('memberKeys.warning')}
            </p>
            <FormField label={t('memberKeys.reason')}>
              <Input
                autoComplete="off"
                value={reason}
                disabled={busy || uncertain || !canWrite}
                onChange={(e) => setReason(e.target.value)}
              />
            </FormField>
            {(conflict || (!!reviewed && reviewed.etag !== detail.data.etag)) && !uncertain && (
              <p role="alert">{t('memberKeys.conflict')}</p>
            )}
            {uncertain && !busy && <p role="status">{t('memberKeys.uncertain')}</p>}
            {!uncertain && notice && notice !== 'confirmed' && (
              <p role="alert">{t(`memberKeys.${notice}`)}</p>
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button type="button" variant="outline" disabled={busy} onClick={close}>
                {t('memberKeys.cancel')}
              </Button>
              {!uncertain && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => void review()}
                >
                  {t('memberKeys.review')}
                </Button>
              )}
              {uncertain ? (
                <Button
                  type="button"
                  className="bg-destructive text-white hover:bg-destructive/90"
                  disabled={busy || !canWrite}
                  onClick={() => void submit(true)}
                >
                  {t('memberKeys.retry')}
                </Button>
              ) : (
                <Button
                  type="submit"
                  className="bg-destructive text-white hover:bg-destructive/90"
                  disabled={
                    busy ||
                    !canWrite ||
                    conflict ||
                    !reviewed ||
                    reviewed.etag !== detail.data.etag ||
                    !detail.data.disable_eligible
                  }
                >
                  {t('memberKeys.confirm')}
                </Button>
              )}
            </div>
          </form>
        )}
      </Dialog>
    </section>
  )
}
function MemberKeyActions({
  row,
  open,
  register,
}: {
  row: MemberKeyRecord
  open: (row: MemberKeyRecord) => void
  register: (id: string, ref: RefObject<HTMLButtonElement | null>) => () => void
}) {
  const { t } = useTranslation('governance')
  const trigger = useRef<HTMLButtonElement | null>(null)
  useLayoutEffect(() => register(row.id, trigger), [register, row.id])
  return (
    <Menu
      side="bottom"
      align="end"
      trigger={<MoreHorizontal className="size-4" aria-hidden="true" />}
      label={t('memberKeys.actionsFor', { name: row.name })}
      triggerClassName="w-auto"
      triggerRef={trigger}
    >
      <MenuItem disabled={!row.disable_eligible} onClick={() => open(row)}>
        <Pause aria-hidden="true" className="size-4" />
        {t(row.disable_eligible ? 'memberKeys.disable' : 'memberKeys.unavailable')}
      </MenuItem>
    </Menu>
  )
}
function Amounts({ amounts }: { amounts: Record<string, string> | null }) {
  const { t } = useTranslation('governance')
  return amounts === null ? (
    <p>{t('memberKeys.unknown')}</p>
  ) : Object.keys(amounts).length ? (
    <div>
      {Object.entries(amounts).map(([currency, amount]) => (
        <p key={currency}>
          {amount} {currency}
        </p>
      ))}
    </div>
  ) : (
    <p>{t('memberKeys.noAmounts')}</p>
  )
}
