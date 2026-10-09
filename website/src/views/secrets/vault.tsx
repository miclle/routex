import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient, type QueryKey, type Query } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getPermissions } from '@/api/governance'
import {
  getVaultIntegration,
  getVaultIntegrations,
  getVaultProbe,
  runVaultProbe,
  validVaultReason,
  VaultError,
} from '@/api/vault-integrations'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  VaultIntegration,
  VaultPage,
  VaultProbe,
  VaultProbeIntent,
} from '@/types/vault-integrations'
import { Page } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { Menu, MenuItem } from '@/components/ui/menu'
import { MoreHorizontal, Plus } from 'lucide-react'
import { VaultDrawer } from './vault-drawer'
import { ProviderOrphans } from './provider-orphans'

const same = (a: QueryKey, b: QueryKey) => JSON.stringify(a) === JSON.stringify(b)
export function VaultWorkspace({
  actor,
  admin,
  fresh,
  generation,
}: {
  actor: string
  admin: boolean
  fresh: boolean
  generation: number
}) {
  const { t, i18n } = useTranslation('secrets'),
    cache = useQueryClient()
  const expired = useRef(false)
  const [suspended, setSuspended] = useState(false)
  const nextSelection = useRef(0)
  const detailCandidate = useRef<{ nonce: number; query: Query; value: VaultIntegration } | null>(
    null,
  )
  const [orphanID, setOrphanID] = useState('')
  const [history, setHistory] = useState<Record<string, VaultProbe>>({})
  const [cursor, setCursor] = useState(''),
    [selection, setSelection] = useState<{
      id?: string
      action: 'edit' | 'view' | 'write' | 'read' | 'cleanup'
      nonce: number
      seed?: VaultIntegration
      initialETag?: string
    }>()
  const [reason, setReason] = useState(''),
    [intent, setIntent] = useState<VaultProbeIntent>(),
    [uncertain, setUncertain] = useState(false),
    [confirm, setConfirm] = useState(false),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState(''),
    [receipt, setReceipt] = useState<VaultProbe>()
  const permissionsKey = ['permissions', actor, 'vault', generation] as const
  const listKey = ['admin', 'vault', actor, generation, cursor] as const
  const detailKey = ['admin', 'vault-detail', actor, generation, selection?.id ?? ''] as const
  const probeKey = [
    'admin',
    'vault-probe',
    actor,
    generation,
    selection?.id ?? '',
    receipt?.id ?? '',
  ] as const
  const permissions = useQuery({
    queryKey: permissionsKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: fresh && admin,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const canRead =
    fresh &&
    !suspended &&
    admin &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    !permissions.error &&
    permissions.data.includes('secrets.read')
  const list = useQuery({
    queryKey: listKey,
    queryFn: ({ signal }) => getVaultIntegrations(cursor, signal),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getVaultIntegration(selection!.id!, signal),
    enabled: canRead && !!selection?.id,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const probe = useQuery({
    queryKey: probeKey,
    queryFn: ({ signal }) => getVaultProbe(selection!.id!, receipt!.id, signal),
    enabled:
      canRead &&
      !!selection?.id &&
      receipt?.integration_id === selection.id &&
      (selection.action === 'view' ||
        selection.action === 'read' ||
        selection.action === 'cleanup'),
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const visible = canRead && list.isSuccess && !list.isFetching && !list.error
  const selected =
    visible && detail.isSuccess && !detail.isFetching && !detail.error ? detail.data : undefined
  const currentProbe =
    visible && probe.isSuccess && !probe.isFetching && !probe.error ? probe.data : undefined
  const ready =
    visible &&
    (!selection?.id || !!selected) &&
    (!(
      selection?.action === 'view' ||
      selection?.action === 'read' ||
      selection?.action === 'cleanup'
    ) ||
      !!currentProbe)
  const controller = useRef<AbortController | null>(null),
    epoch = useRef(0),
    mounted = useRef(true),
    lock = useRef(false)
  const current = useRef({ permissionsKey, listKey, detailKey, probeKey, selection })
  useLayoutEffect(() => {
    current.current = { permissionsKey, listKey, detailKey, probeKey, selection }
  })
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      controller.current?.abort()
    }
  }, [])
  useLayoutEffect(() => {
    const expire = () => {
      expired.current = true
      setSuspended(true)
      epoch.current++
      controller.current?.abort()
      detailCandidate.current = null
      setSelection(undefined)
      setIntent(undefined)
      setReason('')
      setHistory({})
      setOrphanID('')
    }
    window.addEventListener('routex:session-expired', expire)
    return () => window.removeEventListener('routex:session-expired', expire)
  }, [])
  useLayoutEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        const keys = current.current
        const liveSession = cache.getQueryCache().find({ queryKey: sessionKey, exact: true })
        if (same(event.query.queryKey, sessionKey) && liveSession && liveSession !== event.query)
          return
        if (
          ![sessionKey, keys.permissionsKey, keys.listKey, keys.detailKey, keys.probeKey].some(
            (key) => same(key, event.query.queryKey),
          )
        )
          return
        if (
          event.type === 'updated' &&
          same(event.query.queryKey, keys.detailKey) &&
          event.action.type === 'success' &&
          !event.action.manual &&
          keys.selection?.action === 'edit' &&
          keys.selection.id &&
          !keys.selection.seed
        ) {
          const value = event.query.state.data as VaultIntegration | undefined
          if (value?.id === keys.selection.id)
            detailCandidate.current = { nonce: keys.selection.nonce, query: event.query, value }
        }

        if (
          event.type === 'removed' ||
          (event.type === 'updated' &&
            ['fetch', 'error', 'success', 'invalidate'].includes(event.action.type))
        ) {
          if (
            event.type === 'updated' &&
            same(event.query.queryKey, sessionKey) &&
            event.action.type === 'success' &&
            event.action.manual
          ) {
            const session = event.query.state.data as Session | undefined
            if (
              session?.user.id === actor &&
              session.user.role === 'admin' &&
              /^[a-f0-9]{64}$/.test(session.csrf_token)
            )
              return
          }
          epoch.current++
          if (
            event.type === 'updated' &&
            same(event.query.queryKey, sessionKey) &&
            event.action.type === 'success' &&
            !event.action.manual
          ) {
            const next = event.query.state.data as Session | undefined
            if (next?.user.id === actor && next.user.role === 'admin') {
              expired.current = false
              setSuspended(false)
            }
          }
          if (same(event.query.queryKey, sessionKey)) {
            const next = event.query.state.data as Session | undefined
            if (
              event.type === 'removed' ||
              !next ||
              next.user.id !== actor ||
              next.user.role !== 'admin'
            ) {
              expired.current = true
              setSuspended(true)
              detailCandidate.current = null
              setSelection(undefined)
              setIntent(undefined)
              setReason('')
              setHistory({})
              setOrphanID('')
            }
          }
          if (
            same(event.query.queryKey, current.current.permissionsKey) &&
            event.query.state.status === 'success' &&
            event.query.state.fetchStatus === 'idle' &&
            !(event.query.state.data as string[] | undefined)?.includes('secrets.read')
          ) {
            detailCandidate.current = null
            setSelection(undefined)
            setIntent(undefined)
            setReason('')
          }
          controller.current?.abort()
          if (lock.current) {
            setUncertain(true)
            setNotice('unknown')
          }
          setConfirm(false)
        }
        const candidate = detailCandidate.current
        if (
          candidate &&
          keys.selection?.action === 'edit' &&
          !keys.selection.seed &&
          keys.selection.nonce === candidate.nonce
        ) {
          const session = cache.getQueryState<Session>(sessionKey),
            perms = cache.getQueryState<string[]>(keys.permissionsKey),
            page = cache.getQueryState(keys.listKey),
            exactDetail = cache.getQueryCache().find({ queryKey: keys.detailKey, exact: true })
          if (
            !expired.current &&
            candidate.value.id === keys.selection.id &&
            exactDetail === candidate.query &&
            exactDetail.state.data === candidate.value &&
            exactDetail.state.status === 'success' &&
            exactDetail.state.fetchStatus === 'idle' &&
            !exactDetail.state.isInvalidated &&
            session?.status === 'success' &&
            session.fetchStatus === 'idle' &&
            !session.isInvalidated &&
            session.data?.user.id === actor &&
            session.data.user.role === 'admin' &&
            /^[a-f0-9]{64}$/.test(session.data.csrf_token) &&
            perms?.status === 'success' &&
            perms.fetchStatus === 'idle' &&
            !perms.isInvalidated &&
            perms.data?.includes('secrets.read') &&
            page?.status === 'success' &&
            page.fetchStatus === 'idle' &&
            !page.isInvalidated
          ) {
            detailCandidate.current = null
            setSelection((previous) =>
              previous?.nonce === candidate.nonce && !previous.seed
                ? {
                    ...previous,
                    seed: structuredClone(candidate.value),
                    initialETag: candidate.value.review_etag,
                  }
                : previous,
            )
          }
        }
      }),
    [cache, actor],
  )
  function authority(kind: 'write' | 'test') {
    const state = cache.getQueryState<Session>(sessionKey),
      session = state?.data
    const allowed = cache.getQueryState<string[]>(current.current.permissionsKey),
      page = cache.getQueryState(current.current.listKey),
      reviewedProbe =
        current.current.selection?.action === 'read' ||
        current.current.selection?.action === 'cleanup'
          ? cache.getQueryState(current.current.probeKey)
          : undefined,
      target = current.current.selection?.id
        ? cache.getQueryState(current.current.detailKey)
        : undefined
    if (
      expired.current ||
      state?.status !== 'success' ||
      state.fetchStatus !== 'idle' ||
      state.isInvalidated ||
      session?.user.id !== actor ||
      session.user.role !== 'admin' ||
      !/^[a-f0-9]{64}$/.test(session.csrf_token) ||
      allowed?.status !== 'success' ||
      allowed.fetchStatus !== 'idle' ||
      allowed.isInvalidated ||
      !allowed.data?.includes('secrets.read') ||
      !allowed.data.includes(`secrets.${kind}`) ||
      page?.status !== 'success' ||
      page.fetchStatus !== 'idle' ||
      page.isInvalidated ||
      ((target?.data as VaultIntegration | undefined) ?? (page.data as VaultPage | undefined))?.[
        kind === 'write' ? 'can_write' : 'can_test'
      ] !== true ||
      (reviewedProbe &&
        (reviewedProbe.status !== 'success' ||
          reviewedProbe.fetchStatus !== 'idle' ||
          reviewedProbe.isInvalidated)) ||
      (target &&
        (target.status !== 'success' || target.fetchStatus !== 'idle' || target.isInvalidated))
    )
      return null
    return { csrf: session.csrf_token, epoch: epoch.current }
  }
  function chooseOrphans(id: string) {
    const session = cache.getQueryState<Session>(sessionKey),
      allowed = cache.getQueryState<string[]>(current.current.permissionsKey),
      page = cache.getQueryState<VaultPage>(current.current.listKey)
    if (
      expired.current ||
      !fresh ||
      !admin ||
      session?.status !== 'success' ||
      session.fetchStatus !== 'idle' ||
      session.isInvalidated ||
      session.data?.user.id !== actor ||
      session.data.user.role !== 'admin' ||
      allowed?.status !== 'success' ||
      allowed.fetchStatus !== 'idle' ||
      allowed.isInvalidated ||
      !allowed.data?.includes('secrets.read') ||
      page?.status !== 'success' ||
      page.fetchStatus !== 'idle' ||
      page.isInvalidated ||
      !page.data?.items.some((row) => row.id === id)
    )
      return
    setOrphanID(id)
  }
  const canWrite =
    visible &&
    permissions.data?.includes('secrets.write') === true &&
    (selected?.can_write ?? list.data?.can_write) === true
  const canTest =
    ready && permissions.data?.includes('secrets.test') === true && selected?.can_test === true
  function choose(
    id: string | undefined,
    action: 'edit' | 'view' | 'write' | 'read' | 'cleanup',
    savedProbe?: VaultProbe,
  ) {
    if (action === 'edit' && !id && !authority('write')) return
    detailCandidate.current = null
    setSelection({
      id,
      action,
      nonce: ++nextSelection.current,
      ...(!id ? { initialETag: list.data!.review_etag } : {}),
    })
    setReason('')
    setIntent(undefined)
    setUncertain(false)
    setNotice('')
    setReceipt(savedProbe)
    setConfirm(false)
  }
  function close() {
    controller.current?.abort()
    detailCandidate.current = null
    setSelection(undefined)
    setIntent(undefined)
    setUncertain(false)
    setReason('')
    setConfirm(false)
  }
  function capture() {
    if (
      !canTest ||
      !selected ||
      !selection ||
      !validVaultReason(reason) ||
      uncertain ||
      !authority('test')
    )
      return
    const action = selection.action
    if (action === 'edit' || action === 'view') return
    setIntent({
      id: selected.id,
      action,
      ...(action === 'write' ? {} : { probe_id: currentProbe!.id }),
      etag: action === 'write' ? selected.review_etag : currentProbe!.review_etag,
      input: { request_id: crypto.randomUUID(), reason },
    })
    setConfirm(true)
  }
  async function dispatch(retry = false) {
    const gate = authority('test')
    if (!intent || !gate || !canTest || lock.current) return
    lock.current = true
    setBusy(true)
    setConfirm(false)
    const abort = new AbortController()
    controller.current = abort
    let refreshList = false
    try {
      const result = await runVaultProbe(intent, gate.csrf, abort.signal)
      if (!mounted.current) return
      if (abort.signal.aborted || authority('test')?.epoch !== gate.epoch) {
        setUncertain(true)
        setNotice('unknown')
        return
      }
      setReceipt(result)
      setHistory((previous) => ({ ...previous, [result.integration_id]: result }))
      setIntent(undefined)
      setUncertain(false)
      setNotice('recorded')
      refreshList = true
    } catch (error) {
      if (!mounted.current) return
      const status = error instanceof VaultError ? error.status : 0
      if (
        retry ||
        abort.signal.aborted ||
        authority('test')?.epoch !== gate.epoch ||
        ![400, 401, 403, 404, 409, 428].includes(status)
      ) {
        setUncertain(true)
        setNotice('unknown')
      } else {
        setIntent(undefined)
        setNotice(status === 409 ? 'conflict' : 'rejected')
      }
    } finally {
      lock.current = false
      if (controller.current === abort) controller.current = null
      if (mounted.current) setBusy(false)
    }
    // A completed command must settle before its own list fetch revokes read freshness.
    if (
      refreshList &&
      mounted.current &&
      !abort.signal.aborted &&
      authority('test')?.epoch === gate.epoch
    )
      void list.refetch()
  }
  const formatDate = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  const receiptView = (receipt: VaultProbe) => (
    <section className="space-y-2">
      <h3 className="font-medium">{t('vault.savedFacts')}</h3>
      {selected && receipt.revision_id !== selected.revision_id && (
        <p>{t('vault.historicalProbe')}</p>
      )}
      <p>
        {receipt.id} · {t(`vault.states.${receipt.state}`)} · {formatDate(receipt.created_at)}
      </p>
      <p>
        {t('vault.version')}: {receipt.version ?? t('unavailable')}
      </p>
      {(['write', 'read'] as const).map((stage) => (
        <p key={stage}>
          {t(`vault.${stage}`)}:{' '}
          {t(
            receipt[stage].succeeded
              ? 'vault.succeeded'
              : receipt[stage].attempted
                ? 'vault.failed'
                : 'vault.notAttempted',
          )}{' '}
          · {receipt[stage].duration_ms} {t('vault.ms')}
          {receipt[stage].failure && (
            <>
              {' '}
              · {t('vault.failure')} ({receipt[stage].failure!.http_status || t('unavailable')})
            </>
          )}
        </p>
      ))}
      <p>
        {t('vault.cleanup')}: {t(`vault.cleanupStates.${receipt.cleanup.state}`)} ·{' '}
        {receipt.cleanup.observation.duration_ms} {t('vault.ms')}
      </p>
      <p className="text-xs text-muted-foreground">{t('vault.factGuidance')}</p>
    </section>
  )
  return (
    <Page title={t('title')} description={t('vault.description')}>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">{t('tabs.vault')}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('vault.description')}</p>
        </div>
      </div>
      {!fresh || permissions.isFetching || list.isFetching ? (
        <p role="status">{t('loading')}</p>
      ) : !admin || (permissions.isSuccess && !canRead) ? (
        <p role="alert">{t('denied')}</p>
      ) : permissions.error || list.error ? (
        <p role="alert">{t('vault.loadError')}</p>
      ) : null}
      <Button
        variant="outline"
        disabled={!fresh || !admin || busy || permissions.isFetching || list.isFetching}
        onClick={() => {
          if (canRead) void list.refetch()
          else void permissions.refetch()
        }}
      >
        {t('refresh')}
      </Button>
      {visible && (
        <>
          <Table aria-label={t('tabs.vault')}>
            <thead>
              <tr>
                {['integration', 'location', 'authentication', 'usage', 'actions'].map((key) => (
                  <th key={key} className={key === 'actions' ? 'w-16' : undefined}>
                    {t(`vault.${key}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.data.items.map((row) => {
                const last = row.last_probe ?? history[row.id]
                return (
                  <tr key={row.id}>
                    <td>
                      <div className="font-medium">{row.name}</div>
                      <div className="text-xs text-muted-foreground">{row.id}</div>
                      <div className="mt-2 text-sm">
                        {t('vault.endpoint')}: {row.descriptor.endpoint}
                      </div>
                      <div className="text-sm text-muted-foreground">
                        {t('vault.namespace')}: {row.descriptor.namespace || t('vault.notSet')}
                      </div>
                    </td>
                    <td className="space-y-2 text-sm">
                      <div>
                        {t('vault.mount')}: {row.descriptor.mount}
                      </div>
                      <div className="text-muted-foreground">
                        {t('vault.prefix')}: {row.descriptor.prefix}
                      </div>
                      <div className="text-muted-foreground">
                        {t('vault.data_field')}: {row.descriptor.data_field}
                      </div>
                    </td>
                    <td className="space-y-2 text-sm">
                      {(['writer', 'reader'] as const).map((kind, index) => (
                        <div key={kind} className="flex flex-wrap items-center gap-2">
                          <span className="text-muted-foreground">{t(`vault.${kind}`)}</span>
                          <span>{t(`vault.${row[`${kind}_auth`].method}`)}</span>
                          <span>
                            {t(
                              row[`${kind}_auth`].configured
                                ? 'vault.configured'
                                : 'vault.notConfigured',
                            )}
                          </span>
                          <span>
                            {last
                              ? t(
                                  last[index === 0 ? 'write' : 'read'].succeeded
                                    ? 'vault.succeeded'
                                    : last[index === 0 ? 'write' : 'read'].attempted
                                      ? 'vault.failed'
                                      : 'vault.notAttempted',
                                )
                              : t('vault.noProbe')}
                          </span>
                        </div>
                      ))}
                      {last && (
                        <div className="text-xs text-muted-foreground">
                          {last.revision_id !== row.revision_id && (
                            <div>{t('vault.historicalProbe')}</div>
                          )}
                          {t(`vault.states.${last.state}`)}
                        </div>
                      )}
                    </td>
                    <td className="text-sm text-muted-foreground">{t('unavailable')}</td>
                    <td>
                      <Menu
                        label={t('vault.rowActions', { name: row.name })}
                        trigger={<MoreHorizontal className="size-4" aria-hidden="true" />}
                        triggerClassName="size-8 justify-center"
                        side="bottom"
                        align="end"
                      >
                        <MenuItem onClick={() => chooseOrphans(row.id)}>
                          {t('orphans.title')}
                        </MenuItem>
                        <MenuItem onClick={() => choose(row.id, 'edit')}>
                          {t('vault.edit')}
                        </MenuItem>
                        <MenuItem
                          disabled={
                            !permissions.data?.includes('secrets.test') ||
                            !row.can_test ||
                            !row.writer_auth.configured ||
                            !row.reader_auth.configured
                          }
                          onClick={() => choose(row.id, 'write')}
                        >
                          {t('vault.write')}
                        </MenuItem>
                        {last && (
                          <>
                            <MenuItem onClick={() => choose(row.id, 'view', last)}>
                              {t('vault.view')}
                            </MenuItem>
                            <MenuItem
                              disabled={
                                !permissions.data?.includes('secrets.test') ||
                                !row.can_test ||
                                last.revision_id !== row.revision_id
                              }
                              onClick={() => choose(row.id, 'read', last)}
                            >
                              {t('vault.read')}
                            </MenuItem>
                            <MenuItem
                              disabled={
                                !permissions.data?.includes('secrets.test') || !row.can_test
                              }
                              onClick={() => choose(row.id, 'cleanup', last)}
                            >
                              {t('vault.cleanup')}
                            </MenuItem>
                          </>
                        )}
                      </Menu>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </Table>
          <div className="flex justify-center">
            <Button disabled={!canWrite} onClick={() => choose(undefined, 'edit')}>
              <Plus className="size-4" aria-hidden="true" />
              {t('vault.add')}
            </Button>
          </div>
          {!list.data.items.length && <p>{t('vault.empty')}</p>}
          <div className="flex gap-2">
            <Button variant="outline" disabled={!cursor} onClick={() => setCursor('')}>
              {t('vault.first')}
            </Button>
            <Button
              variant="outline"
              disabled={!list.data.next_cursor}
              onClick={() => setCursor(list.data.next_cursor!)}
            >
              {t('vault.more')}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">{t('vault.pageGuidance')}</p>
        </>
      )}
      {orphanID && (
        <ProviderOrphans
          key={`${actor}:${orphanID}`}
          actor={actor}
          integration={orphanID}
          admin={admin}
          fresh={fresh}
          generation={generation}
          close={() => setOrphanID('')}
        />
      )}
      {selection?.action === 'edit' &&
        (!!selection.seed || (!selection.id && !!selection.initialETag)) && (
          <VaultDrawer
            key={selection.nonce}
            open
            visible={ready}
            id={selection.id}
            integration={selection.seed}
            initialETag={selection.initialETag}
            etag={selected?.review_etag ?? list.data?.review_etag ?? ''}
            canSave={canWrite}
            authority={() => authority('write')}
            close={close}
            review={async () => {
              const result = selection.id ? await detail.refetch() : await list.refetch()
              return !result.error && authority('write') ? result.data?.review_etag : undefined
            }}
            saved={() => {
              void list.refetch()
            }}
          />
        )}
      {selection && selection.action !== 'edit' && (
        <Dialog
          open={ready}
          onOpenChange={(value) => {
            if (!value) close()
          }}
          title={t(`vault.${selection.action}`)}
          description={t('vault.probeDescription')}
          busy={busy}
        >
          <div className="space-y-4">
            <p>{selected?.name}</p>
            {selection.action !== 'view' && (
              <label className="block space-y-1">
                <span>{t('reason')}</span>
                <Input
                  value={reason}
                  disabled={busy || uncertain}
                  onChange={(event) => setReason(event.target.value)}
                />
              </label>
            )}
            {notice && <p role="status">{t(`vault.${notice}`)}</p>}
            {(currentProbe ?? receipt) && receiptView(currentProbe ?? receipt!)}
            <div className="flex justify-end gap-2">
              <Button variant="outline" disabled={busy} onClick={close}>
                {t('close')}
              </Button>
              <Button
                variant="outline"
                disabled={busy || uncertain}
                onClick={() => {
                  void detail.refetch()
                  if (receipt) void probe.refetch()
                }}
              >
                {t('review')}
              </Button>
              {selection.action !== 'view' && (
                <Button
                  disabled={busy || !canTest || (!uncertain && !validVaultReason(reason))}
                  onClick={() => (uncertain ? void dispatch(true) : capture())}
                >
                  {t(uncertain ? 'retry' : 'vault.prepare')}
                </Button>
              )}
            </div>
          </div>
          <Dialog
            open={confirm && ready}
            onOpenChange={setConfirm}
            title={t('vault.confirmProbe')}
            description={t(
              selection.action === 'write'
                ? 'vault.writeDescription'
                : selection.action === 'cleanup'
                  ? 'vault.cleanupDescription'
                  : 'vault.readDescription',
            )}
            busy={busy}
          >
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setConfirm(false)}>
                {t('cancel')}
              </Button>
              <Button onClick={() => void dispatch()}>{t('confirm')}</Button>
            </div>
          </Dialog>
        </Dialog>
      )}
    </Page>
  )
}
