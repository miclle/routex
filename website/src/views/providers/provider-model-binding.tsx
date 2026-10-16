import { useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { listModelCreationTargets } from '@/api/model-creation'
import { addRoutingBinding, getRoutingCandidate } from '@/api/model-routing'
import type { ModelCreationTarget } from '@/types/model-creation'
import type { RoutingBindingInput, RoutingProtocol } from '@/types/model-routing'
import type { Session } from '@/types/auth'
import { sessionKey } from '@/hooks/use-auth'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { QueryState } from '@/components/app/CatalogUI'
import { protocolLabel } from '@/lib/protocols'
import useProviderModelBindingWeights from './provider-model-binding-weights'

export interface ProviderModelBindingSource {
  providerID: string
  providerName: string
  connectionID: string
  connectionName: string
  modelID: string
  upstreamName: string
  protocol: RoutingProtocol
}

export default function ProviderModelBinding({
  actor,
  source,
  revision,
  readReady,
  writeReady,
  onClose,
  onSaved,
  onActivity,
  finalFocus,
}: {
  actor: string
  source: ProviderModelBindingSource
  revision: string
  readReady: () => boolean
  writeReady: () => boolean
  onClose: () => void
  onSaved: () => void
  onActivity: (locked: boolean) => void
  finalFocus: () => HTMLButtonElement | false
}) {
  const { t } = useTranslation('catalog')
  const navigate = useNavigate()
  const cache = useQueryClient()
  const currentRead = useSyncExternalStore(
    (notify) => cache.getQueryCache().subscribe(() => notify()),
    readReady,
    () => false,
  )
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState<string>()
  const [target, setTarget] = useState<ModelCreationTarget | null>(null)
  const [intent, setIntent] = useState<{ modelID: string; input: RoutingBindingInput } | null>(null)
  const [uncertain, setUncertain] = useState(false)
  const [pending, setPending] = useState(false)
  const [saved, setSaved] = useState<{ modelID: string; bindingID: string } | null>(null)
  const [explicitReview, setExplicitReview] = useState(0)
  const mounted = useRef(false)
  const operation = useRef<AbortController | null>(null)
  const capturedIntent = useRef<typeof intent>(null)
  const ownerRevision = useRef(revision)
  const lifetime = useRef(0)
  const discarded = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      operation.current?.abort()
    }
  }, [])
  useLayoutEffect(() => {
    if (ownerRevision.current !== revision || !currentRead) {
      ownerRevision.current = revision
      lifetime.current++
      if (operation.current) {
        operation.current.abort()
        operation.current = null
        setPending(false)
        setUncertain(true)
      }
    }
  }, [revision, currentRead])
  const targetsKey = [
    'admin',
    'provider-model-binding-targets',
    actor,
    source.providerID,
    source.connectionID,
    source.modelID,
    revision,
    query,
    cursor,
  ]
  const targets = useQuery({
    queryKey: targetsKey,
    queryFn: ({ signal }) =>
      listModelCreationTargets(
        source.connectionID,
        { ...(query.trim() ? { q: query.trim() } : {}), ...(cursor ? { cursor } : {}) },
        signal,
      ),
    enabled: currentRead && !intent && !saved,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const reviewKey = [
    'admin',
    'provider-model-binding-review',
    actor,
    source.providerID,
    source.connectionID,
    source.modelID,
    source.protocol,
    target?.id,
    revision,
    explicitReview,
  ]
  const review = useQuery({
    queryKey: reviewKey,
    queryFn: ({ signal }) =>
      getRoutingCandidate(target!.id, source.protocol, source.providerID, source.modelID, signal),
    enabled: currentRead && !!target && !intent && !saved,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh = (key: readonly unknown[]) => {
    const state = cache.getQueryState(key)
    return (
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error
    )
  }
  const currentReview =
    currentRead &&
    fresh(reviewKey) &&
    review.data?.model_id === target?.id &&
    review.data?.candidate.connection_id === source.connectionID &&
    review.data.candidate.upstream_name === source.upstreamName
      ? review.data
      : undefined
  const locked = pending || !!intent || !!saved
  const ready = () =>
    mounted.current &&
    !discarded.current &&
    readReady() &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor
  async function save() {
    if (!ready() || !writeReady() || operation.current || saved) return
    const retry = !!capturedIntent.current
    const original =
      capturedIntent.current ??
      (currentReview?.candidate.selectable && target
        ? {
            modelID: target.id,
            input: {
              provider_model_id: source.modelID,
              protocol: source.protocol,
              review_etag: currentReview.candidate.review_etag,
            },
          }
        : null)
    if (!original || (!retry && (!fresh(reviewKey) || !fresh(targetsKey)))) return
    const csrf = cache.getQueryData<Session>(sessionKey)?.csrf_token
    if (!csrf) return
    capturedIntent.current = original
    onActivity(true)
    setIntent(original)
    setUncertain(true)
    setPending(true)
    const controller = new AbortController()
    operation.current = controller
    const version = lifetime.current
    const capturedRevision = ownerRevision.current
    const reviewCount = cache.getQueryState(reviewKey)?.dataUpdateCount
    const targetCount = cache.getQueryState(targetsKey)?.dataUpdateCount
    const valid = () =>
      ready() &&
      writeReady() &&
      operation.current === controller &&
      !controller.signal.aborted &&
      version === lifetime.current &&
      ownerRevision.current === capturedRevision
    try {
      const result = await addRoutingBinding(
        original.modelID,
        original.input,
        csrf,
        controller.signal,
      )
      if (!valid()) return
      if (
        !retry &&
        (!fresh(reviewKey) ||
          !fresh(targetsKey) ||
          cache.getQueryState(reviewKey)?.dataUpdateCount !== reviewCount ||
          cache.getQueryState(targetsKey)?.dataUpdateCount !== targetCount)
      ) {
        setUncertain(true)
        return
      }
      const inserted = result.bindings.find(
        (binding) => binding.provider_model_id === source.modelID,
      )
      if (
        !inserted ||
        inserted.provider_id !== source.providerID ||
        inserted.connection_id !== source.connectionID ||
        inserted.protocol !== source.protocol ||
        inserted.weight !== 0
      ) {
        setUncertain(true)
        return
      }
      capturedIntent.current = null
      setIntent(null)
      setUncertain(false)
      setSaved({ modelID: original.modelID, bindingID: inserted.id })
    } catch {
      if (valid()) setUncertain(true)
    } finally {
      if (mounted.current && operation.current === controller) {
        operation.current = null
        setPending(false)
      }
    }
  }
  const close = (): void => {
    if (operation.current || weights.pending) return
    discarded.current = true
    capturedIntent.current = null
    onActivity(false)
    if (saved) onSaved()
    else onClose()
  }
  const weights = useProviderModelBindingWeights({
    actor,
    modelID: saved?.modelID ?? null,
    bindingID: saved?.bindingID ?? null,
    source,
    revision,
    readReady,
    writeReady,
    onActivity,
    onClose: close,
  })
  return (
    <Dialog
      open={currentRead}
      onOpenChange={(open) => {
        if (!open) close()
      }}
      title={t('providerModelBinding.title', { name: source.upstreamName })}
      description={t('providerModelBinding.description')}
      busy={pending || weights.pending}
      width={960}
      finalFocus={finalFocus}
    >
      <div className="space-y-5">
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          {[
            [t('common.provider'), source.providerName],
            [t('providers.connection'), source.connectionName],
            [t('common.upstreamModel'), source.upstreamName],
            [t('common.protocol'), protocolLabel(source.protocol)],
          ].map(([label, value]) => (
            <div key={label}>
              <dt className="text-muted-foreground">{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
        {!saved && (
          <>
            {uncertain && <p role="alert">{t('providerModelBinding.uncertain')}</p>}
            {!intent && (
              <>
                <label className="block space-y-2">
                  <span>{t('providerModelBinding.search')}</span>
                  <Input
                    type="search"
                    autoComplete="off"
                    value={query}
                    disabled={locked}
                    onValueChange={(value) => {
                      setQuery(value)
                      setCursor(undefined)
                    }}
                  />
                </label>
                <QueryState
                  pending={targets.isFetching}
                  error={targets.error}
                  retry={() => void targets.refetch()}
                />
                {currentRead && fresh(targetsKey) && (
                  <>
                    <Table aria-label={t('providerModelBinding.targets')}>
                      <thead>
                        <tr>
                          <th>{t('common.modelName')}</th>
                          <th>{t('common.actions')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {targets.data?.items.map((item) => (
                          <tr key={item.id}>
                            <td>
                              {item.name}
                              <span className="block text-xs text-muted-foreground">{item.id}</span>
                            </td>
                            <td>
                              <Button
                                variant="outline"
                                disabled={locked || !item.selectable}
                                aria-pressed={target?.id === item.id}
                                onClick={() => {
                                  if (ready() && fresh(targetsKey) && !capturedIntent.current)
                                    setTarget(item)
                                }}
                              >
                                {t(
                                  target?.id === item.id
                                    ? 'providerModelBinding.selected'
                                    : 'providerModelBinding.select',
                                )}
                              </Button>
                              {!item.selectable && <p>{t('providerModelBinding.ineligible')}</p>}
                            </td>
                          </tr>
                        ))}
                        {targets.data?.items.length === 0 && (
                          <tr>
                            <td colSpan={2}>{t('providerModelBinding.empty')}</td>
                          </tr>
                        )}
                      </tbody>
                    </Table>
                    <div className="flex gap-2">
                      {cursor && (
                        <Button
                          variant="outline"
                          onClick={() => {
                            if (ready()) setCursor(undefined)
                          }}
                        >
                          {t('providerModelBinding.first')}
                        </Button>
                      )}
                      {targets.data?.next_cursor && (
                        <Button
                          variant="outline"
                          onClick={() => {
                            if (ready() && fresh(targetsKey)) setCursor(targets.data!.next_cursor!)
                          }}
                        >
                          {t('providerModelBinding.next')}
                        </Button>
                      )}
                    </div>
                    {!query.trim() &&
                      !cursor &&
                      !targets.data?.next_cursor &&
                      targets.data?.items.every((item) => !item.selectable) && (
                        <div className="space-y-2">
                          <p className="text-sm text-muted-foreground">
                            {t('providerModelBinding.createHelp')}
                          </p>
                          <Button
                            variant="outline"
                            disabled={locked}
                            onClick={() => {
                              if (!ready() || !fresh(targetsKey) || capturedIntent.current) return
                              close()
                              void navigate(
                                `/admin/models/new?connectionId=${encodeURIComponent(source.connectionID)}`,
                              )
                            }}
                          >
                            {t('providerModelBinding.createFull')}
                          </Button>
                        </div>
                      )}
                  </>
                )}
                {target && (
                  <QueryState
                    pending={review.isFetching}
                    error={review.error}
                    retry={() => setExplicitReview((v) => v + 1)}
                  />
                )}
              </>
            )}
            {currentReview && !intent && (
              <section className="space-y-3">
                <h3 className="font-semibold">
                  {t('providerModelBinding.preview', { name: currentReview.model_name })}
                </h3>
                <Table aria-label={t('providerModelBinding.routes')}>
                  <thead>
                    <tr>
                      <th>{t('common.provider')}</th>
                      <th>{t('providers.connection')}</th>
                      <th>{t('common.upstreamModel')}</th>
                      <th>{t('providerModelBinding.weight')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {currentReview.routes
                      .filter((route) => route.protocol === source.protocol)
                      .map((route) => (
                        <tr key={route.id}>
                          <td>{route.provider_name}</td>
                          <td>{route.connection_name}</td>
                          <td>{route.upstream_name}</td>
                          <td>{route.weight}%</td>
                        </tr>
                      ))}
                    <tr>
                      <td>{source.providerName}</td>
                      <td>{source.connectionName}</td>
                      <td>
                        {source.upstreamName}
                        <span className="block text-xs">{t('providerModelBinding.newRoute')}</span>
                      </td>
                      <td>0%</td>
                    </tr>
                  </tbody>
                </Table>
                <p className="text-sm text-muted-foreground">
                  {t('providerModelBinding.weightHelp')}
                </p>
                {!currentReview.candidate.selectable && (
                  <p role="alert">{t('providerModelBinding.notReady')}</p>
                )}
              </section>
            )}
            <div className="flex flex-wrap justify-between gap-3">
              <div className="flex gap-2">
                <Button variant="outline" disabled={pending} onClick={close}>
                  {t('common.cancel')}
                </Button>
                <Button
                  disabled={
                    !writeReady() ||
                    pending ||
                    (!intent && (!fresh(targetsKey) || !currentReview?.candidate.selectable))
                  }
                  onClick={() => void save()}
                >
                  {t(
                    pending
                      ? 'providerModelBinding.saving'
                      : intent
                        ? 'providerModelBinding.retry'
                        : 'providerModelBinding.add',
                  )}
                </Button>
              </div>
            </div>
          </>
        )}
        {saved && weights.content}
      </div>
    </Dialog>
  )
}
