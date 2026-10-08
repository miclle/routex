import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { getPermissions } from '@/api/governance'
import {
  createModelBatch,
  getModelCreationContext,
  getModelCreationReceipt,
  listModelCreationConnections,
  listModelCreationProviderModels,
  listModelCreationTargets,
  modelCreationOutcomeUnknown,
  modelCreationItemKey,
  modelCreationSource,
  validManualModelName,
  validModelCreationName,
  previewModelCreation,
  validateModelCreationItems,
  validModelCreationReason,
} from '@/api/model-creation'
import { sessionKey, useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { Session } from '@/types/auth'
import type {
  ModelCreationInput,
  ModelCreationItem,
  ModelCreationPreview,
  ModelCreationProviderModel,
  ModelCreationResult,
} from '@/types/model-creation'
import { Page } from '@/components/app/CatalogUI'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Dialog } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { PublicModelName } from './public-model-name'
import { ModelAccessForm, ModelAccessSummary } from './model-inline-access'
import type { ModelAccessSaved } from '@/types/model-access'

function useReadCount(key: readonly unknown[]) {
  const cache = useQueryClient(),
    serialized = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === serialized) notify()
      }),
    [cache, serialized],
  )
  const snapshot = useCallback(
    () => cache.getQueryState(JSON.parse(serialized))?.dataUpdateCount ?? 0,
    [cache, serialized],
  )
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}

function useReadFresh(key: readonly unknown[]) {
  const cache = useQueryClient(),
    serialized = JSON.stringify(key)
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === serialized) notify()
      }),
    [cache, serialized],
  )
  const snapshot = useCallback(() => {
    const state = cache.getQueryState(JSON.parse(serialized))
    return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
  }, [cache, serialized])
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}

export default function CreateModelPage() {
  const { t } = useTranslation('modelCreation')
  const session = useSession(),
    generation = useSessionGeneration(),
    cache = useQueryClient()
  const actor = session.data?.user.id ?? ''
  const networkGeneration = useRef(generation)
  useEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        if (
          event.query.queryKey.join('/') === 'auth/session' &&
          event.type === 'updated' &&
          event.action.type === 'success' &&
          !event.action.manual
        )
          networkGeneration.current = event.query.state.dataUpdateCount
      }),
    [cache],
  )
  const sessionCurrent = useReadFresh(sessionKey)
  const sessionFresh =
    sessionCurrent &&
    !!actor &&
    session.isSuccess &&
    !session.isFetching &&
    !session.isError &&
    session.data?.user.role === 'admin' &&
    !!session.data.csrf_token
  const permissionKey = ['model-creation', 'permissions', actor, generation] as const
  const access = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: sessionFresh,
    retry: false,
    refetchOnMount: 'always',
  })
  const accessCount = useReadCount(permissionKey)
  const permissionsCurrent = useReadFresh(permissionKey)
  const canRead =
    permissionsCurrent &&
    sessionFresh &&
    access.isSuccess &&
    !access.isFetching &&
    !access.isError &&
    access.data.includes('models.read_all') &&
    access.data.includes('providers.read')
  const canWrite = canRead && access.data?.includes('models.write') === true
  const basis = `${generation}:${accessCount}`
  const [params] = useSearchParams()
  const initialConnection = params.get('connectionId') ?? ''
  const [selected, setSelected] = useState(initialConnection),
    [q, setQ] = useState('')
  const [mode, setMode] = useState<'new' | 'existing'>(initialConnection ? 'existing' : 'new')
  const [accessSaved, setAccessSaved] = useState<{ actor: string; saved: ModelAccessSaved } | null>(
    null,
  )
  const [accessLocked, setAccessLocked] = useState(false),
    [batchLocked, setBatchLocked] = useState(false)
  const accessLock = useRef(false),
    batchLock = useRef(false)
  const reportAccessLock = useCallback((locked: boolean) => {
    accessLock.current = locked
    setAccessLocked(locked)
  }, [])
  const reportBatchLock = useCallback((locked: boolean) => {
    batchLock.current = locked
    setBatchLocked(locked)
  }, [])
  const [savedActor, setSavedActor] = useState(actor)
  if (savedActor !== actor) {
    setSavedActor(actor)
    setAccessSaved(null)
    if (mode === 'new') setSelected('')
    setAccessLocked(false)
    setBatchLocked(false)
  }
  const onAccessSaved = useCallback(
    (saved: ModelAccessSaved) => {
      setAccessSaved({ actor, saved })
      setSelected(saved.connection_id)
      void cache.invalidateQueries({ queryKey: ['model-creation', 'connections', actor] })
    },
    [actor, cache],
  )
  const connectionKey = ['model-creation', 'connections', actor, basis, q] as const
  const connections = useInfiniteQuery({
    queryKey: connectionKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      listModelCreationConnections(
        { ...(q ? { q } : {}), ...(pageParam ? { cursor: pageParam } : {}) },
        signal,
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled: canRead,
    retry: false,
  })
  const connectionsCurrent = useReadFresh(connectionKey)
  const visible =
    connectionsCurrent &&
    canRead &&
    connections.isSuccess &&
    !connections.isFetching &&
    !connections.isError
  const fresh = () => {
    const current = cache.getQueryData<Session | null>(sessionKey),
      state = cache.getQueryState(sessionKey),
      permissions = cache.getQueryState<string[]>(permissionKey),
      connectionState = cache.getQueryState(connectionKey)
    return (
      current?.user.id === actor &&
      current.user.role === 'admin' &&
      !!current.csrf_token &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      connectionState?.status === 'success' &&
      connectionState.fetchStatus === 'idle' &&
      !connectionState.isInvalidated &&
      networkGeneration.current === generation &&
      permissions?.status === 'success' &&
      permissions.fetchStatus === 'idle' &&
      !permissions.isInvalidated &&
      permissions.dataUpdateCount === accessCount &&
      permissions.data?.includes('models.read_all') === true &&
      permissions.data?.includes('providers.read') === true
    )
  }
  // The keyed child owns only this actor/Connection's captured intent.
  return (
    <Page title={t('title')} description={t('description')}>
      <section className="space-y-6 rounded-lg border p-6">
        <div role="group" aria-label={t('access.mode')} className="flex flex-wrap gap-2">
          {(['new', 'existing'] as const).map((value) => (
            <Button
              key={value}
              variant={mode === value ? 'default' : 'outline'}
              aria-pressed={mode === value}
              disabled={!canRead || accessLocked || batchLocked}
              onClick={() => {
                if (fresh() && !accessLock.current && !batchLock.current) {
                  setMode(value)
                  setSelected('')
                  setAccessSaved(null)
                }
              }}
            >
              {t(value === 'new' ? 'access.newConnection' : 'existingConnection')}
            </Button>
          ))}
        </div>
        <p className="text-sm text-muted-foreground">
          {t('separateSetup')}{' '}
          <Link className="underline" to="/admin/providers">
            {t('providerWorkspace')}
          </Link>
        </p>
        {mode === 'new' ? (
          <ModelAccessForm
            key={actor}
            actor={actor}
            basis={basis}
            readable={canRead}
            writable={access.data?.includes('providers.write') === true}
            fresh={fresh}
            writeFresh={() =>
              fresh() &&
              cache.getQueryState<string[]>(permissionKey)?.data?.includes('providers.write') ===
                true
            }
            onSaved={onAccessSaved}
            onInteractionLockChange={reportAccessLock}
          />
        ) : !canRead ? (
          <div role="status">
            <p>
              {session.isFetching || access.isFetching || access.isPending
                ? t('loading')
                : t('denied')}
            </p>
            <Button
              variant="outline"
              onClick={() => {
                void session.refetch()
                void access.refetch()
              }}
            >
              {t('retryAuthority')}
            </Button>
          </div>
        ) : (
          <>
            <label className="grid gap-3 text-sm sm:grid-cols-[180px_1fr]">
              <span>{t('connectionSearch')}</span>
              <Input value={q} onChange={(e) => setQ(e.target.value)} />
            </label>
            <label className="grid gap-3 text-sm sm:grid-cols-[180px_1fr]">
              <span>{t('connection')}</span>
              <select
                aria-label={t('connection')}
                className="h-11 rounded-md border px-3"
                value={selected}
                disabled={!visible || batchLocked}
                onChange={(e) => {
                  if (fresh() && !accessLock.current && !batchLock.current)
                    setSelected(e.target.value)
                }}
              >
                <option value="">{t('chooseConnection')}</option>
                {visible &&
                  connections.data.pages
                    .flatMap((p) => p.items)
                    .map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.provider_name} · {c.name} · {c.protocol}
                      </option>
                    ))}
                {selected &&
                  !connections.data?.pages.some((p) => p.items.some((c) => c.id === selected)) && (
                    <option value={selected}>{selected}</option>
                  )}
              </select>
            </label>
            <div className="flex gap-2">
              <Button
                variant="outline"
                disabled={connections.isFetching}
                onClick={() => {
                  if (fresh()) void connections.refetch()
                }}
              >
                {t('refresh')}
              </Button>
              {connections.hasNextPage && (
                <Button
                  variant="outline"
                  disabled={connections.isFetching}
                  onClick={() => {
                    if (fresh()) void connections.fetchNextPage()
                  }}
                >
                  {t('loadMore')}
                </Button>
              )}
            </div>
            {connections.isError && <p role="alert">{t('error')}</p>}
          </>
        )}
        {selected && (
          <BatchForm
            key={`${actor}:${selected}`}
            actor={actor}
            connectionId={selected}
            basis={basis}
            readable={visible}
            writable={canWrite}
            manualWritable={canWrite && access.data?.includes('providers.write') === true}
            writeFresh={(manual) =>
              fresh() &&
              cache.getQueryData<string[]>(permissionKey)?.includes('models.write') === true &&
              (!manual ||
                cache.getQueryData<string[]>(permissionKey)?.includes('providers.write') === true)
            }
            fresh={fresh}
            onInteractionLockChange={reportBatchLock}
            accessSaved={accessSaved?.actor === actor ? accessSaved.saved : null}
          />
        )}
      </section>
    </Page>
  )
}
interface Draft {
  metadata: ModelCreationProviderModel
  item: ModelCreationItem
}
function BatchForm({
  actor,
  connectionId,
  basis,
  readable,
  writable,
  manualWritable,
  writeFresh,
  fresh,
  onInteractionLockChange,
  accessSaved,
}: {
  actor: string
  connectionId: string
  basis: string
  readable: boolean
  writable: boolean
  manualWritable: boolean
  writeFresh: (manual: boolean) => boolean
  fresh: () => boolean
  onInteractionLockChange?: (locked: boolean) => void
  accessSaved?: ModelAccessSaved | null
}) {
  const { t, i18n } = useTranslation('modelCreation'),
    cache = useQueryClient()
  const prefix = ['model-creation', actor, connectionId, basis] as const
  const context = useQuery({
    queryKey: [...prefix, 'context'],
    queryFn: ({ signal }) => getModelCreationContext(connectionId, signal),
    enabled: readable,
    retry: false,
    refetchOnMount: 'always',
  })
  const contextCount = useReadCount([...prefix, 'context'])
  const [q, setQ] = useState(''),
    [targetQ, setTargetQ] = useState(''),
    [draft, setDraft] = useState<Draft[]>([]),
    [reason, setReason] = useState(''),
    [manualOpen, setManualOpen] = useState(false),
    [manualName, setManualName] = useState('')
  const models = useInfiniteQuery({
    queryKey: [...prefix, 'provider-models', q],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      listModelCreationProviderModels(
        connectionId,
        { ...(q ? { q } : {}), ...(pageParam ? { cursor: pageParam } : {}) },
        signal,
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled: readable && context.isSuccess,
    retry: false,
  })
  const targets = useInfiniteQuery({
    queryKey: [...prefix, 'targets', targetQ],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ signal, pageParam }) =>
      listModelCreationTargets(
        connectionId,
        { ...(targetQ ? { q: targetQ } : {}), ...(pageParam ? { cursor: pageParam } : {}) },
        signal,
      ),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    enabled: readable && context.isSuccess,
    retry: false,
  })
  const modelCount = useReadCount([...prefix, 'provider-models', q]),
    targetCount = useReadCount([...prefix, 'targets', targetQ])
  const contextFresh = useReadFresh([...prefix, 'context'])
  const modelsFresh = useReadFresh([...prefix, 'provider-models', q])
  const targetsFresh = useReadFresh([...prefix, 'targets', targetQ])
  const visible =
    contextFresh &&
    modelsFresh &&
    targetsFresh &&
    readable &&
    context.isSuccess &&
    !context.isFetching &&
    !context.isError &&
    models.isSuccess &&
    !models.isFetching &&
    !models.isError &&
    targets.isSuccess &&
    !targets.isFetching &&
    !targets.isError
  const [review, setReview] = useState<{
      value: ModelCreationPreview
      basis: string
      contextCount: number
      fingerprint: string
    } | null>(null),
    [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState('')
  const [intent, setIntent] = useState<{ input: ModelCreationInput; etag: string } | null>(null),
    [unknown, setUnknown] = useState(false),
    [result, setResult] = useState<ModelCreationResult | null>(null)
  useLayoutEffect(() => {
    onInteractionLockChange?.(busy || unknown)
    return () => onInteractionLockChange?.(false)
  }, [busy, unknown, onInteractionLockChange])
  const lock = useRef(false),
    controller = useRef<AbortController | null>(null),
    live = useRef(true)
  const items = draft
      .map((x) => x.item)
      .sort((a, b) =>
        modelCreationItemKey(a) < modelCreationItemKey(b)
          ? -1
          : modelCreationItemKey(a) > modelCreationItemKey(b)
            ? 1
            : 0,
      ),
    fingerprint = JSON.stringify(items)
  const ready = () =>
    fresh() &&
    readable &&
    [
      [[...prefix, 'context'], contextCount],
      [[...prefix, 'provider-models', q], modelCount],
      [[...prefix, 'targets', targetQ], targetCount],
    ].every(([key, count]) => {
      const state = cache.getQueryState(key as readonly unknown[])
      return (
        state?.status === 'success' &&
        state.fetchStatus === 'idle' &&
        !state.isInvalidated &&
        state.dataUpdateCount === count
      )
    })
  const readyRef = useRef(ready)
  useLayoutEffect(() => {
    readyRef.current = ready
  })
  const freshRef = useRef(fresh)
  useLayoutEffect(() => {
    freshRef.current = fresh
  })
  const authority = `${basis}:${contextCount}:${modelCount}:${targetCount}`
  const authorityRef = useRef(authority)
  useLayoutEffect(() => {
    authorityRef.current = authority
    return () => controller.current?.abort()
  }, [authority])
  useEffect(() => {
    live.current = true
    return () => {
      live.current = false
      controller.current?.abort()
    }
  }, [])
  useEffect(() => {
    if (!visible) controller.current?.abort()
  }, [visible, basis])
  const readAction = (work: () => unknown) => {
    if (fresh()) void work()
  }
  const blockers = (codes: string[]) =>
    codes.map((code) => t(`blockers.${code}`, { defaultValue: t('blockers.unknown') })).join(' ')
  const update = (id: string, item: ModelCreationItem) => {
    setDraft((old) => old.map((x) => (modelCreationItemKey(x.item) === id ? { ...x, item } : x)))
    setReview(null)
  }
  async function preview() {
    if (lock.current || !visible || !ready()) return
    try {
      validateModelCreationItems(items)
      if (!validModelCreationReason(reason)) throw new Error()
    } catch {
      setNotice('invalid')
      return
    }
    lock.current = true
    onInteractionLockChange?.(true)
    setBusy(true)
    setNotice('')
    const capturedAuthority = authority
    const abort = new AbortController()
    controller.current = abort
    try {
      const value = await previewModelCreation(
        connectionId,
        items,
        cache.getQueryData<Session>(sessionKey)!.csrf_token,
        abort.signal,
      )
      if (
        !live.current ||
        abort.signal.aborted ||
        !readyRef.current() ||
        authorityRef.current !== capturedAuthority
      )
        return
      setReview({ value, basis: authority, contextCount, fingerprint })
      setOpen(true)
    } catch {
      if (live.current && !abort.signal.aborted) setNotice('error')
    } finally {
      lock.current = false
      if (live.current) {
        setBusy(false)
        onInteractionLockChange?.(false)
      }
    }
  }
  const reviewed =
    visible &&
    review?.basis === authority &&
    review.contextCount === contextCount &&
    review.fingerprint === fingerprint
  async function dispatch(original = false, lookup = false) {
    if (
      lock.current ||
      !(original || lookup ? readable && fresh() : visible && ready()) ||
      (!lookup &&
        (!writable ||
          !writeFresh(
            (original ? intent?.input.items : items)?.some((x) => x.upstream_name !== undefined) ===
              true,
          ) ||
          (!original && !context.data?.can_create)))
    )
      return
    const captured =
      original || lookup
        ? intent
        : reviewed && review?.value.can_commit && validModelCreationReason(reason)
          ? {
              input: { request_id: crypto.randomUUID(), reason, items: structuredClone(items) },
              etag: review.value.review_etag,
            }
          : null
    if (!captured) return
    let retainLock = true
    setIntent(captured)
    setUnknown(true)
    setNotice('unknown')
    lock.current = true
    onInteractionLockChange?.(true)
    setBusy(true)
    const capturedAuthority = authority
    const abort = new AbortController()
    controller.current = abort
    try {
      const value = lookup
        ? await getModelCreationReceipt(
            captured.input.request_id,
            abort.signal,
            connectionId,
            captured.input,
          )
        : await createModelBatch(
            connectionId,
            captured.input,
            captured.etag,
            cache.getQueryData<Session>(sessionKey)!.csrf_token,
            abort.signal,
          )
      if (
        !live.current ||
        abort.signal.aborted ||
        !freshRef.current() ||
        authorityRef.current !== capturedAuthority
      )
        return
      if (value.receipt.connection_id !== connectionId) throw new Error()
      setResult(value)
      retainLock = false
      setUnknown(false)
      setNotice('')
      setOpen(false)
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
      void cache.invalidateQueries({ queryKey: ['models'] })
    } catch (error) {
      if (live.current) {
        if (original || lookup || abort.signal.aborted || modelCreationOutcomeUnknown(error)) {
          setUnknown(true)
          setNotice('unknown')
        } else {
          retainLock = false
          setUnknown(false)
          setIntent(null)
          setReview(null)
          setOpen(false)
          setNotice('conflict')
        }
      }
    } finally {
      lock.current = false
      if (live.current) {
        setBusy(false)
        onInteractionLockChange?.(retainLock)
      }
    }
  }
  if (!visible)
    return (
      <div role="status">
        <p>{context.isError || models.isError || targets.isError ? t('error') : t('loading')}</p>
        {intent && <p>{t('retained')}</p>}
        {readable && intent && (
          <>
            <p role="alert">{t(unknown ? 'unknown' : 'committed')}</p>
            <Button disabled={busy || !writable} onClick={() => void dispatch(true)}>
              {t('retryOriginal')}
            </Button>
            <Button variant="outline" disabled={busy} onClick={() => void dispatch(true, true)}>
              {t('receiptLookup')}
            </Button>
          </>
        )}
        <Button
          variant="outline"
          disabled={!readable || busy}
          onClick={() => {
            if (!fresh()) return
            void context.refetch()
            void models.refetch()
            void targets.refetch()
          }}
        >
          {t('refresh')}
        </Button>
      </div>
    )
  const c = context.data.connection
  const allTargets = targets.data.pages.flatMap((p) => p.items)
  const locked = busy || unknown || !!result
  const pageRows = models.data.pages.flatMap((p) => p.items)
  const displayRows = [
    ...pageRows,
    ...draft.filter((d) => !pageRows.some((p) => p.id === d.metadata.id)).map((d) => d.metadata),
  ]
  return (
    <div className="space-y-6">
      <dl className="grid gap-3 text-sm sm:grid-cols-[180px_1fr]">
        {[
          [t('provider'), c.provider_name],
          [t('protocol'), c.protocol],
          [t('baseURL'), c.base_url],
        ].map(([label, value]) => (
          <div key={label} className="contents">
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <div className="border-t pt-6">
        <h2 className="mb-4 text-sm font-medium">{t('models')}</h2>
        <div className="mb-3 flex items-end gap-3">
          <label className="flex-1 text-sm">
            {t('modelSearch')}
            <Input value={q} disabled={locked} onChange={(e) => setQ(e.target.value)} />
          </label>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => readAction(() => models.refetch())}
          >
            {t('refresh')}
          </Button>
          <Button
            variant="outline"
            disabled={locked || !manualWritable || draft.length >= 50}
            onClick={() => {
              if (live.current && readyRef.current() && writeFresh(true)) setManualOpen(true)
            }}
          >
            {t('manualAdd')}
          </Button>
        </div>
        <Table aria-label={t('models')}>
          <thead>
            <tr>
              <th></th>
              <th>{t('upstream')}</th>
              <th>{t('capabilities')}</th>
              <th>{t('target')}</th>
            </tr>
          </thead>
          <tbody>
            {displayRows.map((row) => {
              const selected = draft.find((x) => modelCreationItemKey(x.item) === row.id)
              const chosen = !!selected
              return (
                <tr key={row.id}>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={t('selection', { name: row.upstream_name })}
                      checked={chosen}
                      disabled={locked || (!chosen && (!row.selectable || draft.length >= 50))}
                      onChange={() => {
                        if (
                          !live.current ||
                          lock.current ||
                          locked ||
                          !visible ||
                          !readyRef.current() ||
                          authorityRef.current !== authority
                        )
                          return
                        for (const [key, count] of [
                          [[...prefix, 'provider-models', q], modelCount],
                          [[...prefix, 'targets', targetQ], targetCount],
                        ] as const) {
                          const state = cache.getQueryState(key)
                          if (
                            state?.status !== 'success' ||
                            state.fetchStatus !== 'idle' ||
                            state.isInvalidated ||
                            state.dataUpdateCount !== count
                          )
                            return
                        }
                        const initial = row.initial_target
                        const item: ModelCreationItem =
                          initial?.target === 'existing'
                            ? {
                                provider_model_id: row.id,
                                target: 'existing',
                                model_id: initial.model_id,
                              }
                            : {
                                provider_model_id: row.id,
                                target: 'new',
                                name: initial?.name ?? '',
                              }
                        setDraft((old) =>
                          chosen
                            ? old.filter((x) => modelCreationItemKey(x.item) !== row.id)
                            : [...old, { metadata: row, item }],
                        )
                        setReview(null)
                      }}
                    />
                  </td>
                  <td>
                    {row.upstream_name}
                    {selected?.item.upstream_name !== undefined && (
                      <p className="text-xs text-muted-foreground">{t('manualCoverage')}</p>
                    )}
                  </td>
                  <td>
                    {selected?.item.upstream_name !== undefined
                      ? t('unknownCapabilities')
                      : row.input_capabilities.length
                        ? row.input_capabilities
                            .map((cap) => t(cap === 'image' ? 'imageInput' : 'pdfInput'))
                            .join(', ')
                        : t('text')}
                  </td>
                  <td>
                    {selected ? (
                      <div className="flex items-center gap-3">
                        {' '}
                        <select
                          className="rounded-md border p-2"
                          aria-label={`${row.upstream_name} ${t('target')}`}
                          disabled={locked || !row.selectable}
                          value={selected.item.target}
                          onChange={(e) =>
                            update(
                              modelCreationItemKey(selected.item),
                              e.target.value === 'new'
                                ? {
                                    ...modelCreationSource(selected.item),
                                    target: 'new',
                                    name: '',
                                  }
                                : {
                                    ...modelCreationSource(selected.item),
                                    target: 'existing',
                                    model_id: '',
                                  },
                            )
                          }
                        >
                          <option value="new">{t('new')}</option>
                          <option value="existing">{t('existing')}</option>
                        </select>
                        {selected.item.target === 'new' ? (
                          <PublicModelName
                            key={`${authority}:${row.id}`}
                            actor={actor}
                            connectionId={connectionId}
                            authority={`${authority}:${row.id}`}
                            fresh={() =>
                              live.current &&
                              !lock.current &&
                              !locked &&
                              visible &&
                              row.selectable &&
                              readyRef.current() &&
                              authorityRef.current === authority
                            }
                            label={t('publicName', { name: row.upstream_name })}
                            disabled={locked || !row.selectable}
                            value={selected.item.name}
                            excludedNames={draft.flatMap((d) =>
                              modelCreationItemKey(d.item) !== row.id && d.item.target === 'new'
                                ? [d.item.name]
                                : [],
                            )}
                            onValueChange={(name) => {
                              const modelState = cache.getQueryState([
                                ...prefix,
                                'provider-models',
                                q,
                              ])
                              const targetState = cache.getQueryState([
                                ...prefix,
                                'targets',
                                targetQ,
                              ])
                              if (
                                !live.current ||
                                lock.current ||
                                locked ||
                                !row.selectable ||
                                !visible ||
                                !readyRef.current() ||
                                authorityRef.current !== authority ||
                                modelState?.status !== 'success' ||
                                modelState.fetchStatus !== 'idle' ||
                                modelState.dataUpdateCount !== modelCount ||
                                targetState?.status !== 'success' ||
                                targetState.fetchStatus !== 'idle' ||
                                targetState.dataUpdateCount !== targetCount
                              )
                                return
                              setDraft((old) =>
                                old.map((d) =>
                                  modelCreationItemKey(d.item) === row.id && d.item.target === 'new'
                                    ? { ...d, item: { ...d.item, name } }
                                    : d,
                                ),
                              )
                              setReview(null)
                            }}
                          />
                        ) : (
                          <select
                            className="rounded-md border p-2"
                            aria-label={t('targetModel', { name: row.upstream_name })}
                            disabled={locked || !row.selectable}
                            value={selected.item.model_id}
                            onChange={(e) =>
                              update(modelCreationItemKey(selected.item), {
                                ...modelCreationSource(selected.item),
                                target: 'existing',
                                model_id: e.target.value,
                              })
                            }
                          >
                            <option value="">{t('chooseTarget')}</option>
                            {allTargets.map((x) => (
                              <option key={x.id} value={x.id} disabled={!x.selectable}>
                                {x.name}
                                {!x.selectable ? ` · ${blockers(x.blocker_codes)}` : ''}
                              </option>
                            ))}
                            {selected.item.model_id &&
                              !allTargets.some(
                                (x) =>
                                  x.id ===
                                  (selected.item.target === 'existing'
                                    ? selected.item.model_id
                                    : ''),
                              ) && (
                                <option value={selected.item.model_id}>
                                  {selected.metadata.initial_target?.target === 'existing' &&
                                  selected.metadata.initial_target.model_id ===
                                    selected.item.model_id
                                    ? selected.metadata.initial_target.name
                                    : selected.item.model_id}
                                </option>
                              )}
                          </select>
                        )}
                      </div>
                    ) : row.selectable ? (
                      '—'
                    ) : (
                      blockers(row.blocker_codes) || t('blocked')
                    )}
                    {selected && !selected.metadata.initial_target && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        {t('initialTargetUnknown')}
                      </p>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </Table>
        {!displayRows.length && <p>{t('empty')}</p>}
        {models.hasNextPage && (
          <Button
            variant="outline"
            disabled={locked}
            onClick={() => readAction(() => models.fetchNextPage())}
          >
            {t('loadMore')}
          </Button>
        )}
      </div>
      {draft.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-medium">{t('selected')}</h2>
          <p className="text-sm">{t('count', { count: draft.length })}</p>
          <label className="block text-sm">
            {t('targetSearch')}
            <Input value={targetQ} disabled={locked} onChange={(e) => setTargetQ(e.target.value)} />
          </label>
          {targets.hasNextPage && (
            <Button
              variant="outline"
              disabled={locked}
              onClick={() => readAction(() => targets.fetchNextPage())}
            >
              {t('loadMore')}
            </Button>
          )}

          <p className="text-sm text-muted-foreground">{t('nameRule')}</p>
        </div>
      )}
      <p className="text-sm text-muted-foreground">{t('weights')}</p>
      <p className="text-sm text-muted-foreground">{t('noGrants')}</p>
      <label className="block space-y-2 text-sm">
        {t('reason')}
        <Textarea value={reason} disabled={locked} onChange={(e) => setReason(e.target.value)} />
      </label>
      <p className="text-sm text-muted-foreground">{t('reasonRule')}</p>
      {notice && <p role="alert">{t(notice)}</p>}
      {unknown && (
        <div className="flex gap-3">
          <Button disabled={busy || !writable} onClick={() => void dispatch(true)}>
            {t('retryOriginal')}
          </Button>
          <Button variant="outline" disabled={busy} onClick={() => void dispatch(true, true)}>
            {t('receiptLookup')}
          </Button>
        </div>
      )}
      {result && (
        <section className="space-y-3 rounded-lg border p-4">
          <h2>{t('receipt')}</h2>
          <p>{t('committed')}</p>
          <p>
            {t('requestID')}: {result.receipt.request_id}
          </p>
          <p>
            {t('responseTime', {
              time: new Date(result.receipt.created_at).toLocaleString(
                i18n.resolvedLanguage === 'zh' ? 'zh-CN' : 'en-US',
              ),
            })}
          </p>
          <p>
            {t('application')}: {t(result.application_status)}
          </p>
          {result.receipt.items.map((row) => (
            <p key={row.binding_id}>
              <Link className="underline" to={`/admin/models/${row.model_id}`}>
                {t('viewModel', { name: row.name })}
              </Link>{' '}
              · {row.protocol} · {row.weight}
            </p>
          ))}
          <Button variant="outline" disabled={busy} onClick={() => void dispatch(true, true)}>
            {t('currentRefresh')}
          </Button>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              if (!fresh()) return
              setResult(null)
              setIntent(null)
              setReview(null)
              setDraft([])
              setReason('')
              void models.refetch()
            }}
          >
            {t('startAnother')}
          </Button>
        </section>
      )}
      {!unknown && !result && (
        <div className="flex justify-center gap-3">
          <Link className={buttonVariants({ variant: 'outline' })} to="/admin/models">
            {t('back')}
          </Link>
          <Button disabled={busy || !draft.length} onClick={() => void preview()}>
            {busy ? t('busy') : t('review')}
          </Button>
        </div>
      )}
      <Dialog
        open={manualOpen && visible && manualWritable}
        onOpenChange={(value) => {
          setManualOpen(value)
          if (!value) setManualName('')
        }}
        title={t('manualTitle')}
        description={t('manualDescription')}
      >
        <label className="block space-y-2 text-sm">
          {t('manualIdentifier')}
          <Input value={manualName} onChange={(e) => setManualName(e.target.value)} />
        </label>
        <p className="mt-3 text-sm text-muted-foreground">{t('manualCoverage')}</p>
        <Button
          className="mt-4"
          disabled={
            locked ||
            !validManualModelName(manualName, c) ||
            draft.length >= 50 ||
            displayRows.some((row) => row.upstream_name === manualName)
          }
          onClick={() => {
            if (
              !live.current ||
              lock.current ||
              locked ||
              !readyRef.current() ||
              !writeFresh(true) ||
              authorityRef.current !== authority ||
              !validManualModelName(manualName, c) ||
              draft.length >= 50 ||
              displayRows.some((row) => row.upstream_name === manualName)
            )
              return
            const exact = allTargets.find(
              (target) => target.name === manualName && target.selectable,
            )
            const item: ModelCreationItem = exact
              ? { upstream_name: manualName, target: 'existing', model_id: exact.id }
              : {
                  upstream_name: manualName,
                  target: 'new',
                  name: validModelCreationName(manualName) ? manualName : '',
                }
            const metadata: ModelCreationProviderModel = {
              id: modelCreationItemKey(item),
              upstream_name: manualName,
              disabled: false,
              input_capabilities: [],
              credential_ready: false,
              selectable: true,
              blocker_codes: [],
              initial_target: exact
                ? {
                    target: 'existing',
                    name: exact.name,
                    model_id: exact.id,
                    initial_weight: exact.initial_weight as 0 | 100,
                  }
                : null,
            }
            setDraft((old) => [...old, { metadata, item }])
            setReview(null)
            setManualOpen(false)
            setManualName('')
          }}
        >
          {t('manualToList')}
        </Button>
      </Dialog>
      <Dialog
        open={open && visible && !unknown}
        onOpenChange={setOpen}
        title={t('confirmation')}
        description={t('confirmationDescription')}
        busy={busy}
        width={960}
      >
        {review && (
          <>
            <dl className="mb-4 grid gap-3 text-sm sm:grid-cols-3">
              {[
                [t('provider'), review.value.connection.provider_name],
                [t('connection'), review.value.connection.name],
                [t('protocol'), review.value.connection.protocol],
              ].map(([label, value]) => (
                <div key={label}>
                  <dt className="text-muted-foreground">{label}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
            {accessSaved && (
              <div className="mb-4">
                <ModelAccessSummary saved={accessSaved} />
                <p className="mt-2 text-sm">{t('access.retained')}</p>
              </div>
            )}
            <Table aria-label={t('summary')}>
              <thead>
                <tr>
                  <th>{t('upstream')}</th>
                  <th>{t('target')}</th>
                  <th>{t('protocol')}</th>
                  <th>{t('weight')}</th>
                </tr>
              </thead>
              <tbody>
                {review.value.items.map((row) => (
                  <tr key={row.provider_model_id || `manual:${row.upstream_name}`}>
                    <td>
                      {row.upstream_name}
                      {row.warning_codes?.includes('credential_coverage_unproven') && (
                        <p className="text-xs text-muted-foreground">{t('manualCoverage')}</p>
                      )}
                    </td>
                    <td>
                      {row.name}
                      <p className="text-xs">{t(row.target)}</p>
                      {row.blocker_codes.length > 0 && (
                        <p role="alert">{blockers(row.blocker_codes)}</p>
                      )}
                    </td>
                    <td>{row.protocol}</td>
                    <td>{row.initial_weight}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {!reviewed && <p role="alert">{t('stale')}</p>}
            {!review.value.can_commit && <p role="alert">{t('previewBlocked')}</p>}
            <p className="my-4 text-sm">{t('noGrants')}</p>
            <div className="flex justify-end gap-3">
              <Button variant="outline" disabled={busy} onClick={() => setOpen(false)}>
                {t('back')}
              </Button>
              <Button
                disabled={
                  busy ||
                  !reviewed ||
                  !review.value.can_commit ||
                  !writable ||
                  !context.data.can_create
                }
                onClick={() => void dispatch()}
              >
                {busy ? t('busy') : t('confirm')}
              </Button>
            </div>
          </>
        )}
      </Dialog>
    </div>
  )
}
