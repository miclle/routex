import { useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getPermissions } from '@/api/governance'
import {
  cleanupProviderOrphan,
  getProviderCleanupReceipt,
  getProviderOrphan,
  getProviderOrphans,
} from '@/api/provider-credential-cleanup'
import { validVaultReason, validVaultToken } from '@/api/vault-integrations'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type {
  ProviderCleanupIntent,
  ProviderCleanupReceipt,
  ProviderOrphan,
} from '@/types/provider-credential-cleanup'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Drawer } from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'

const sameKey = (a: QueryKey, b: QueryKey) => JSON.stringify(a) === JSON.stringify(b)
const knownBlockers = new Set([
  'unconfirmed_ownership',
  'creation_unresolved',
  'previously_published',
  'live_credential',
  'retained_reference',
  'active_creation',
  'cleanup_claimed',
  'source_unavailable',
  'fleet_ambiguous',
  'process_ownership_unknown',
  'published_process_unproven',
  'published_source_unavailable',
  'published_drain_unproven',
])
export function ProviderOrphans({
  actor,
  integration,
  admin,
  fresh,
  generation,
  close,
}: {
  actor: string
  integration: string
  admin: boolean
  fresh: boolean
  generation: number
  close: () => void
}) {
  const { t, i18n } = useTranslation('secrets')
  const cache = useQueryClient()
  const [cursor, setCursor] = useState(''),
    [selected, setSelected] = useState('')
  const [reason, setReason] = useState(''),
    [token, setToken] = useState('')
  const [reviewed, setReviewed] = useState<ProviderOrphan>(),
    [intent, setIntent] = useState<ProviderCleanupIntent>()
  const [receipt, setReceipt] = useState<ProviderCleanupReceipt>(),
    [notice, setNotice] = useState('')
  const [newAttempt, setNewAttempt] = useState(false),
    [newReason, setNewReason] = useState('')
  const [previousCommands, setPreviousCommands] = useState<
    { intent: ProviderCleanupIntent; receipt: ProviderCleanupReceipt }[]
  >([])
  const commandRef = useRef({ intent, receipt })
  const newReviewNonce = useRef(0)
  const [authorityExpired, setAuthorityExpired] = useState(false)
  const [busy, setBusy] = useState(false),
    [confirm, setConfirm] = useState(false)
  const mounted = useRef(true),
    epoch = useRef(0),
    locked = useRef(false),
    expired = useRef(false)
  const controller = useRef<AbortController | null>(null)
  const owner = `${actor}:${integration}`
  const ownerRef = useRef(owner)
  const live = useRef({ fresh, admin, owner })
  useLayoutEffect(() => {
    live.current = { fresh, admin, owner }
    commandRef.current = { intent, receipt }
  })
  const permissionsKey = ['permissions', actor, 'provider-orphans', generation] as const
  const listKey = ['admin', 'provider-orphans', actor, integration, generation, cursor] as const
  const detailKey = ['admin', 'provider-orphan', actor, integration, generation, selected] as const
  const keys = useRef({ permissionsKey, listKey, detailKey })
  useLayoutEffect(() => {
    keys.current = { permissionsKey, listKey, detailKey }
  })
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
    admin &&
    !authorityExpired &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    !permissions.error &&
    permissions.data.includes('secrets.read')
  const list = useQuery({
    queryKey: listKey,
    queryFn: ({ signal }) => getProviderOrphans(integration, cursor, signal),
    enabled: canRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: ({ signal }) => getProviderOrphan(integration, selected, signal),
    enabled: canRead && !!selected,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const visible = canRead && list.isSuccess && !list.isFetching && !list.error
  const current =
    visible && detail.isSuccess && !detail.isFetching && !detail.error ? detail.data : undefined
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      controller.current?.abort()
      ownerRef.current = ''
    }
  }, [])
  useLayoutEffect(() => {
    if (ownerRef.current !== owner) {
      ownerRef.current = owner
      newReviewNonce.current++
      epoch.current++
      controller.current?.abort()
      setIntent(undefined)
      setReviewed(undefined)
      setReceipt(undefined)
      setPreviousCommands([])
      setNewAttempt(false)
      setNewReason('')
      setSelected('')
      setReason('')
      setToken('')
      setConfirm(false)
      setNotice('')
    }
  }, [owner])
  useLayoutEffect(() => {
    const hide = () => {
      epoch.current++
      controller.current?.abort()
      setToken('')
      setConfirm(false)
      setReviewed(undefined)
      setNewAttempt(false)
      setNewReason('')
    }
    const expire = () => {
      newReviewNonce.current++
      expired.current = true
      setAuthorityExpired(true)
      hide()
    }
    window.addEventListener('routex:session-expired', expire)
    const unsubscribe = cache.getQueryCache().subscribe((event) => {
      if (
        ![
          sessionKey,
          keys.current.permissionsKey,
          keys.current.listKey,
          keys.current.detailKey,
        ].some((key) => sameKey(key, event.query.queryKey))
      )
        return
      if (
        event.type === 'removed' ||
        (event.type === 'updated' &&
          ['fetch', 'invalidate', 'error', 'success'].includes(event.action.type))
      ) {
        if (!sameKey(event.query.queryKey, keys.current.detailKey)) newReviewNonce.current++
        // Observe synchronous cache transitions before a queued confirmation runs.
        hide()
        if (sameKey(event.query.queryKey, sessionKey)) {
          const session = event.query.state.data as Session | undefined
          if (
            !session ||
            session.user.id !== actor ||
            session.user.role !== 'admin' ||
            event.type === 'removed'
          ) {
            expired.current = true
            setAuthorityExpired(true)
            setIntent(undefined)
            setReceipt(undefined)
            setPreviousCommands([])
            setReason('')
            setSelected('')
          } else if (
            event.type === 'updated' &&
            event.action.type === 'success' &&
            !event.action.manual
          ) {
            expired.current = false
            setAuthorityExpired(false)
          }
        }
      }
    })
    return () => {
      unsubscribe()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [cache, actor, integration])
  const [readAuthority, setReadAuthority] = useState(canRead)
  if (readAuthority !== canRead) {
    setReadAuthority(canRead)
    if (!canRead) {
      setToken('')
      setConfirm(false)
      setReviewed(undefined)
      setNewAttempt(false)
    }
  }
  if (current && intent && current.review_etag.split('.')[0] !== intent.etag.split('.')[0]) {
    setIntent(undefined)
    setReceipt(undefined)
    setReason('')
    setToken('')
    setNotice('identityChanged')
    setPreviousCommands([])
    setNewAttempt(false)
    setNewReason('')
  }
  useLayoutEffect(() => {
    if (!fresh || !admin || !canRead) {
      newReviewNonce.current++
      controller.current?.abort()
    }
  }, [fresh, admin, canRead])
  function authority(write: boolean, requireDetail = true) {
    const session = cache.getQueryState<Session>(sessionKey),
      allowed = cache.getQueryState<string[]>(keys.current.permissionsKey),
      page = cache.getQueryState(keys.current.listKey),
      row = cache.getQueryState<ProviderOrphan>(keys.current.detailKey)
    if (
      !mounted.current ||
      ownerRef.current !== owner ||
      expired.current ||
      !live.current.fresh ||
      !live.current.admin ||
      live.current.owner !== owner ||
      session?.status !== 'success' ||
      session.fetchStatus !== 'idle' ||
      session.isInvalidated ||
      session.data?.user.id !== actor ||
      session.data.user.role !== 'admin' ||
      !/^[a-f0-9]{64}$/.test(session.data.csrf_token) ||
      allowed?.status !== 'success' ||
      allowed.fetchStatus !== 'idle' ||
      allowed.isInvalidated ||
      !allowed.data?.includes('secrets.read') ||
      page?.status !== 'success' ||
      page.fetchStatus !== 'idle' ||
      page.isInvalidated
    )
      return null
    if (
      requireDetail &&
      selected &&
      (row?.status !== 'success' ||
        row.fetchStatus !== 'idle' ||
        row.isInvalidated ||
        row.data?.integration_id !== integration ||
        row.data.creation_request_id !== selected)
    )
      return null
    if (
      write &&
      (!allowed.data.includes('secrets.write') ||
        !allowed.data.includes('providers.write') ||
        !row?.data?.can_cleanup)
    )
      return null
    return { csrf: session.data.csrf_token, epoch: epoch.current, row: row?.data }
  }
  const writable =
    !!current &&
    current.can_cleanup &&
    permissions.data?.includes('secrets.write') &&
    permissions.data.includes('providers.write')
  const eligible =
    current?.eligible &&
    (current.state === 'orphan' || current.state === 'committed') &&
    current.ownership_recorded &&
    !current.blocker_codes.length
  const canReviewNew =
    current?.state === 'committed' && receipt?.state === 'failed' && !!receipt.finished_at
  function choose(row: ProviderOrphan) {
    if (!authority(false, false) || locked.current) return
    // Revisiting the same object must not discard a dispatched command's identity.
    if (intent?.creation_request_id === row.creation_request_id) {
      setSelected(row.creation_request_id)
      if (selected === row.creation_request_id) void detail.refetch()
      return
    }
    setSelected(row.creation_request_id)
    setReason('')
    setToken('')
    setIntent(undefined)
    setReceipt(undefined)
    setNotice('')
    setReviewed(undefined)
    setConfirm(false)
    setNewAttempt(false)
    setNewReason('')
    setPreviousCommands([])
  }
  async function prepare() {
    if (locked.current || intent || !authority(true) || !validVaultReason(reason)) return
    setToken('')
    setReviewed(undefined)
    setNotice('')
    setConfirm(false)
    const capturedOwner = owner
    const result = await detail.refetch()
    if (!mounted.current || ownerRef.current !== capturedOwner) return
    const auth = authority(true)
    if (
      !auth ||
      result.error ||
      !result.data ||
      auth.row !== result.data ||
      !result.data.eligible ||
      result.data.blocker_codes.length
    ) {
      if (mounted.current) setNotice('reviewFailed')
      return
    }
    setReviewed(result.data)
    setConfirm(true)
  }
  async function prepareNew() {
    const initial = authority(true)
    if (locked.current || !initial || !intent || !receipt || !canReviewNew) return
    const original = intent,
      originalReceipt = receipt,
      capturedOwner = owner,
      capturedKey = keys.current.detailKey,
      sessionGeneration = cache.getQueryState(sessionKey)?.dataUpdatedAt,
      permissionGeneration = cache.getQueryState(keys.current.permissionsKey)?.dataUpdatedAt
    const nonce = ++newReviewNonce.current
    setToken('')
    setReviewed(undefined)
    setConfirm(false)
    setNewAttempt(false)
    locked.current = true
    setBusy(true)
    try {
      const result = await detail.refetch()
      if (!mounted.current || ownerRef.current !== capturedOwner) return
      const auth = authority(true)
      if (
        !auth ||
        nonce !== newReviewNonce.current ||
        result.error ||
        !result.data ||
        auth.row !== result.data ||
        !sameKey(capturedKey, keys.current.detailKey) ||
        sessionGeneration !== cache.getQueryState(sessionKey)?.dataUpdatedAt ||
        permissionGeneration !== cache.getQueryState(keys.current.permissionsKey)?.dataUpdatedAt ||
        commandRef.current.intent !== original ||
        commandRef.current.receipt !== originalReceipt ||
        originalReceipt.state !== 'failed' ||
        !originalReceipt.finished_at ||
        result.data.state !== 'committed' ||
        !result.data.eligible ||
        !result.data.ownership_recorded ||
        result.data.blocker_codes.length ||
        result.data.integration_id !== original.integration_id ||
        result.data.creation_request_id !== original.creation_request_id ||
        result.data.revision_id !== original.revision_id ||
        result.data.review_etag.split('.')[0] !== original.etag.split('.')[0]
      ) {
        setNotice('newReviewBlocked')
        return
      }
      setNewReason('')
      setReviewed(result.data)
      setNewAttempt(true)
      setConfirm(true)
      setNotice('newReviewReady')
    } catch {
      if (mounted.current && ownerRef.current === capturedOwner) setNotice('newReviewBlocked')
    } finally {
      locked.current = false
      if (mounted.current && ownerRef.current === capturedOwner) setBusy(false)
    }
  }
  async function dispatch(retry = false) {
    const auth = authority(true)
    if (locked.current || !auth || !auth.row) return
    let command = intent
    if (retry) {
      if (
        !command ||
        command.creation_request_id !== selected ||
        command.integration_id !== integration ||
        command.revision_id !== auth.row.revision_id ||
        command.etag.split('.')[0] !== auth.row.review_etag.split('.')[0]
      )
        return
    } else {
      if (
        (intent && !newAttempt) ||
        !confirm ||
        !reviewed ||
        reviewed !== auth.row ||
        !reviewed.eligible ||
        reviewed.blocker_codes.length ||
        !validVaultReason(newAttempt ? newReason : reason) ||
        !validVaultToken(token)
      )
        return
      if (
        newAttempt &&
        (!intent ||
          !canReviewNew ||
          reviewed.state !== 'committed' ||
          !reviewed.ownership_recorded ||
          intent.revision_id !== reviewed.revision_id ||
          intent.etag.split('.')[0] !== reviewed.review_etag.split('.')[0])
      )
        return
      command = {
        integration_id: integration,
        creation_request_id: selected,
        revision_id: reviewed.revision_id,
        etag: reviewed.review_etag,
        input: { request_id: crypto.randomUUID(), reason: newAttempt ? newReason : reason },
      }
      if (newAttempt && intent && receipt)
        setPreviousCommands((previous) => [...previous, { intent, receipt }])
      setIntent(command)
      setReceipt(undefined)
    }
    if (!command) return
    const transient = retry ? undefined : token
    // Clear authentication before the network await. It never enters retained intent.
    setToken('')
    setConfirm(false)
    setReviewed(undefined)
    setNewAttempt(false)
    setNewReason('')
    setBusy(true)
    setNotice('pending')
    locked.current = true
    const abort = new AbortController()
    controller.current = abort
    const capturedOwner = owner
    try {
      const result = await cleanupProviderOrphan(command, auth.csrf, abort.signal, transient)
      if (!mounted.current || ownerRef.current !== capturedOwner) return
      if (epoch.current !== auth.epoch) {
        setNotice('uncertain')
        return
      }
      setReceipt(result.receipt)
      setNotice(result.receipt.state)
    } catch {
      if (mounted.current && ownerRef.current === capturedOwner) setNotice('uncertain')
    } finally {
      if (controller.current === abort) controller.current = null
      locked.current = false
      if (mounted.current && ownerRef.current === capturedOwner) setBusy(false)
    }
  }
  async function reconcile(command = intent) {
    if (!command || locked.current || !authority(false)) return
    const auth = authority(false)!
    const capturedOwner = owner
    const abort = new AbortController()
    controller.current = abort
    locked.current = true
    setBusy(true)
    try {
      const result = await getProviderCleanupReceipt(command, abort.signal)
      if (!mounted.current || ownerRef.current !== capturedOwner) return
      if (epoch.current !== auth.epoch) {
        setNotice('uncertain')
        return
      }
      if (commandRef.current.intent === command) {
        setReceipt(result)
        setNotice(result.state)
      } else {
        setPreviousCommands((previous) =>
          previous.map((entry) =>
            entry.intent === command ? { ...entry, receipt: result } : entry,
          ),
        )
      }
    } catch {
      if (mounted.current && ownerRef.current === capturedOwner) setNotice('uncertain')
    } finally {
      locked.current = false
      if (controller.current === abort) controller.current = null
      if (mounted.current && ownerRef.current === capturedOwner) setBusy(false)
    }
  }
  function cancelWaiting() {
    setToken('')
    newReviewNonce.current++
    setNotice(controller.current ? 'uncertain' : 'newReviewBlocked')
    if (!controller.current)
      void cache.cancelQueries({ queryKey: keys.current.detailKey, exact: true })
    controller.current?.abort()
  }
  const date = (value: string) =>
    new Date(value).toLocaleString(i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US')
  return (
    <Drawer
      open
      onOpenChange={(open) => {
        if (!open) {
          setToken('')
          controller.current?.abort()
          close()
        }
      }}
      title={t('orphans.title')}
      description={t('orphans.description')}
    >
      {!visible ? (
        <div>
          <p role={permissions.error || list.error ? 'alert' : 'status'}>
            {t(
              !fresh || permissions.isFetching || list.isFetching
                ? 'loading'
                : !canRead
                  ? 'denied'
                  : 'vault.loadError',
            )}
          </p>
          <Button
            variant="outline"
            disabled={!fresh || !admin || busy}
            onClick={() => {
              if (canRead) void list.refetch()
              else void permissions.refetch()
            }}
          >
            {t('refresh')}
          </Button>
        </div>
      ) : (
        <div className="space-y-4">
          <Table aria-label={t('orphans.title')}>
            <thead>
              <tr>
                {['creation', 'credential', 'state', 'ownership', 'actions'].map((key) => (
                  <th key={key}>{t(`orphans.${key}`)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {list.data.items.map((row) => (
                <tr key={row.creation_request_id}>
                  <td>
                    {row.creation_request_id}
                    <div className="text-xs text-muted-foreground">{date(row.created_at)}</div>
                  </td>
                  <td>{row.credential_id}</td>
                  <td>{t(`orphans.states.${row.state}`)}</td>
                  <td>{t(row.ownership_recorded ? 'orphans.recorded' : 'orphans.unconfirmed')}</td>
                  <td>
                    <Button variant="ghost" disabled={busy} onClick={() => choose(row)}>
                      {t('review')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
          {!list.data.items.length && <p>{t('orphans.empty')}</p>}
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={!cursor || busy}
              onClick={() => {
                setSelected('')
                setCursor('')
              }}
            >
              {t('vault.first')}
            </Button>
            <Button
              variant="outline"
              disabled={!list.data.next_cursor || busy}
              onClick={() => {
                setSelected('')
                setCursor(list.data.next_cursor!)
              }}
            >
              {t('vault.more')}
            </Button>
          </div>
          {selected && !current && (
            <div>
              <p role="status">{t(detail.error ? 'vault.loadError' : 'loading')}</p>
              {busy && (
                <Button variant="outline" onClick={cancelWaiting}>
                  {t('orphans.cancel')}
                </Button>
              )}
            </div>
          )}
          {current && (
            <section className="space-y-3">
              <h3 className="font-medium">{t('orphans.reviewTitle')}</h3>
              <p>
                {current.creation_request_id} · {current.credential_id}
              </p>
              <p>
                {t('orphans.revision')}: {current.revision_id}
              </p>
              <p>{t('orphans.recordedGuidance')}</p>
              <p>
                {t(
                  current.state === 'committed'
                    ? 'orphans.publishedGuidance'
                    : 'orphans.neverCommittedGuidance',
                )}
              </p>
              {!!current.blocker_codes.length && (
                <ul>
                  {current.blocker_codes.map((code) => (
                    <li key={code}>
                      {t(knownBlockers.has(code) ? `orphans.blockers.${code}` : 'orphans.blocked')}
                    </li>
                  ))}
                </ul>
              )}
              <label className="block space-y-1">
                <span>{t('reason')}</span>
                <Input
                  value={intent?.input.reason ?? reason}
                  disabled={busy || !!intent || !writable}
                  onChange={(event) => setReason(event.target.value)}
                />
              </label>
              {notice && <p role="status">{t(`orphans.notices.${notice}`)}</p>}
              {previousCommands.map((previous) => (
                <section key={previous.intent.input.request_id} className="space-y-1">
                  <h4 className="font-medium">{t('orphans.previousCommand')}</h4>
                  <p>
                    {previous.intent.input.request_id} ·{' '}
                    {t(`orphans.notices.${previous.receipt.state}`)}
                  </p>
                  <p>{previous.intent.input.reason}</p>
                  <p>{t('orphans.originalReceiptGuidance')}</p>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => void reconcile(previous.intent)}
                  >
                    {t('orphans.reviewPreviousCommand')}
                  </Button>
                </section>
              ))}
              {receipt && (
                <section className="space-y-1">
                  <h4 className="font-medium">{t('orphans.receipt')}</h4>
                  <p>
                    {receipt.request_id} · {t(`orphans.notices.${receipt.state}`)}
                  </p>
                  <p>
                    {t('orphans.ownership')}:{' '}
                    {t(
                      receipt.ownership.succeeded
                        ? 'vault.succeeded'
                        : receipt.ownership.attempted
                          ? 'vault.failed'
                          : 'vault.notAttempted',
                    )}
                  </p>
                  <p>
                    {t('vault.cleanup')}: {t(`vault.cleanupStates.${receipt.cleanup.state}`)}
                  </p>
                  <p>
                    {date(receipt.started_at)}
                    {receipt.finished_at && <> · {date(receipt.finished_at)}</>}
                  </p>
                  <p>{t('orphans.receiptGuidance')}</p>
                </section>
              )}
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" disabled={busy} onClick={() => void detail.refetch()}>
                  {t('review')}
                </Button>
                {intent ? (
                  <>
                    <Button variant="outline" disabled={busy} onClick={() => void reconcile()}>
                      {t('orphans.reviewReceipt')}
                    </Button>
                    {canReviewNew && (
                      <Button
                        variant="outline"
                        disabled={busy || !writable}
                        onClick={() => void prepareNew()}
                      >
                        {t('orphans.reviewNewAttempt')}
                      </Button>
                    )}
                    <Button
                      disabled={
                        busy ||
                        !writable ||
                        intent.etag.split('.')[0] !== current.review_etag.split('.')[0]
                      }
                      onClick={() => void dispatch(true)}
                    >
                      {t('orphans.retry')}
                    </Button>
                  </>
                ) : (
                  <Button
                    className="bg-destructive text-white hover:bg-destructive/90"
                    disabled={busy || !writable || !eligible || !validVaultReason(reason)}
                    onClick={() => void prepare()}
                  >
                    {t('orphans.prepare')}
                  </Button>
                )}
                {busy && (
                  <Button variant="outline" onClick={cancelWaiting}>
                    {t('orphans.cancel')}
                  </Button>
                )}
              </div>
            </section>
          )}
        </div>
      )}
      <Dialog
        open={confirm && visible && !!current && !!reviewed && reviewed === current && !!writable}
        onOpenChange={(open) => {
          if (!open) {
            setConfirm(false)
            setToken('')
            setReviewed(undefined)
            setNewAttempt(false)
            setNewReason('')
          }
        }}
        title={t(newAttempt ? 'orphans.confirmNewTitle' : 'orphans.confirmTitle')}
        description={t(
          newAttempt
            ? 'orphans.confirmNewDescription'
            : reviewed?.state === 'committed'
              ? 'orphans.confirmPublishedDescription'
              : 'orphans.confirmDescription',
        )}
      >
        <div className="space-y-4">
          <p>{reviewed?.credential_id}</p>
          {newAttempt ? (
            <>
              <p>{t('orphans.originalReceiptGuidance')}</p>
              <p>{intent?.input.request_id}</p>
              <label className="block space-y-1">
                <span>{t('orphans.newReason')}</span>
                <Input value={newReason} onChange={(event) => setNewReason(event.target.value)} />
              </label>
            </>
          ) : (
            <p>{reason}</p>
          )}
          <label className="block space-y-1">
            <span>{t('orphans.token')}</span>
            <Input
              type="password"
              autoComplete="off"
              value={token}
              onChange={(event) => setToken(event.target.value)}
            />
          </label>
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => {
                setToken('')
                setConfirm(false)
                setReviewed(undefined)
                setNewAttempt(false)
                setNewReason('')
              }}
            >
              {t('close')}
            </Button>
            <Button
              className="bg-destructive text-white hover:bg-destructive/90"
              disabled={
                busy || !validVaultToken(token) || (newAttempt && !validVaultReason(newReason))
              }
              onClick={() => void dispatch()}
            >
              {t(newAttempt ? 'orphans.confirmNew' : 'orphans.confirm')}
            </Button>
          </div>
        </div>
      </Dialog>
    </Drawer>
  )
}
