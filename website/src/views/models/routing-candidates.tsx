import { useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { isAxiosError } from 'axios'
import { listRoutingProviders, listRoutingCandidates, addRoutingBinding } from '@/api/model-routing'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import { routingProtocols } from '@/types/model-routing'
import type {
  RoutingCandidate,
  RoutingProtocol,
  RoutingProvider,
  RoutingBindingInput,
} from '@/types/model-routing'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { ErrorNotice, FormField, QueryState, SaveButton } from '@/components/app/CatalogUI'
import { protocolLabel } from '@/lib/protocols'

export default function RoutingCandidates({
  actor,
  modelID,
  protocol,
  generation,
  resourceGeneration,
  readable,
  canWrite,
  readReady,
  writeReady,
  onProtocolChange,
  onClose,
  onSaved,
}: {
  actor: string
  modelID: string
  protocol: RoutingProtocol
  generation: number
  resourceGeneration: number
  readable: boolean
  canWrite: boolean
  onProtocolChange: (protocol: RoutingProtocol) => void
  readReady: () => boolean
  writeReady: () => boolean
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const currentRead = useSyncExternalStore(
    (notify) => cache.getQueryCache().subscribe(() => notify()),
    readReady,
    () => false,
  )
  const [provider, setProvider] = useState<RoutingProvider | null>(null)
  const [providerQuery, setProviderQuery] = useState('')
  const [providerCursor, setProviderCursor] = useState<string | undefined>()
  const [query, setQuery] = useState('')
  const [cursor, setCursor] = useState<string | undefined>()
  const [selected, setSelected] = useState<RoutingCandidate | null>(null)
  const [intent, setIntent] = useState<RoutingBindingInput | null>(null)
  const [unknown, setUnknown] = useState(false)
  const [conflict, setConflict] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const mounted = useRef(false)
  const owner = useRef(generation)
  const resourceOwner = useRef(resourceGeneration)
  const busy = useRef(false)
  const sequence = useRef({ value: 0 })
  useLayoutEffect(() => {
    mounted.current = true
    const lifetime = sequence.current
    return () => {
      mounted.current = false
      lifetime.value++
    }
  }, [])
  useLayoutEffect(() => {
    if (owner.current !== generation || resourceOwner.current !== resourceGeneration) {
      owner.current = generation
      resourceOwner.current = resourceGeneration
      if (busy.current) {
        sequence.current.value++
        busy.current = false
        setPending(false)
        setUnknown(true)
      }
    }
  }, [generation, resourceGeneration])
  const providersKey = [
    'admin',
    'model-routing-providers',
    actor,
    modelID,
    protocol,
    generation,
    resourceGeneration,
    providerQuery,
    providerCursor,
  ]
  const providers = useQuery({
    queryKey: providersKey,
    queryFn: ({ signal }) =>
      listRoutingProviders(modelID, protocol, { q: providerQuery, cursor: providerCursor }, signal),
    enabled: readable && currentRead,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const candidatesKey = [
    'admin',
    'model-routing-candidates',
    actor,
    modelID,
    protocol,
    generation,
    resourceGeneration,
    provider?.id,
    query,
    cursor,
  ]
  const candidates = useQuery({
    queryKey: candidatesKey,
    queryFn: ({ signal }) =>
      listRoutingCandidates(modelID, protocol, provider!.id, { q: query, cursor }, signal),
    enabled: readable && currentRead && !!provider,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh = (key: readonly unknown[]) => {
    const state = cache.getQueryState(key)
    return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
  }
  // staleTime is deliberately zero; freshness here is the exact active completed read, not a time estimate.
  const candidatesFresh =
    readable &&
    currentRead &&
    candidates.isSuccess &&
    !candidates.isFetching &&
    fresh(candidatesKey)
  const providersVisible =
    readable && currentRead && providers.isSuccess && !providers.isFetching && fresh(providersKey)
  const authority = () =>
    mounted.current &&
    owner.current === generation &&
    resourceOwner.current === resourceGeneration &&
    readReady() &&
    fresh(providersKey) &&
    (!provider || fresh(candidatesKey))
  const lockDraft = pending || unknown || !!intent
  async function add() {
    if (busy.current || !authority() || !writeReady() || !canWrite) return
    const original =
      intent ??
      (selected
        ? { provider_model_id: selected.id, protocol, review_etag: selected.review_etag }
        : null)
    if (!original || (!intent && !selected?.selectable)) return
    if (!intent) {
      const active = cache
        .getQueryData<{ items: RoutingCandidate[] }>(candidatesKey)
        ?.items.find((v) => v.id === selected?.id)
      if (active && active.review_etag !== original.review_etag) return
    }
    setIntent(original)
    busy.current = true
    setPending(true)
    setError(null)
    const attempt = ++sequence.current.value
    const capturedGeneration = owner.current
    const providerRead = cache.getQueryState(providersKey)?.dataUpdateCount
    const candidateRead = cache.getQueryState(candidatesKey)?.dataUpdateCount
    try {
      await addRoutingBinding(
        modelID,
        original,
        cache.getQueryData<Session>(sessionKey)!.csrf_token,
      )
      if (!mounted.current || attempt !== sequence.current.value) return
      if (
        owner.current !== capturedGeneration ||
        !authority() ||
        !writeReady() ||
        cache.getQueryState(providersKey)?.dataUpdateCount !== providerRead ||
        cache.getQueryState(candidatesKey)?.dataUpdateCount !== candidateRead
      ) {
        setUnknown(true)
        return
      }
      // Only this current authorized insertion response closes the dialog. A later GET never does.
      onSaved()
      onClose()
    } catch (err) {
      if (!mounted.current || attempt !== sequence.current.value) return
      setError(err)
      setUnknown(true)
      setConflict(isAxiosError(err) && err.response?.status === 409)
    } finally {
      if (mounted.current && attempt === sequence.current.value) {
        busy.current = false
        setPending(false)
      }
    }
  }
  function review() {
    if (!authority() || pending) return
    setConflict(false)
    void candidates.refetch()
  }
  return (
    <Dialog
      open={readable && currentRead}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
      title={t('routingCandidates.title', { protocol: protocolLabel(protocol) })}
      description={t('routingCandidates.description')}
      busy={pending}
      width={880}
    >
      {unknown && (
        <p role="alert" className="mb-4 text-sm text-destructive">
          {t('routingCandidates.unknown')}
        </p>
      )}
      {conflict && (
        <Button type="button" variant="outline" disabled={pending} onClick={review}>
          {t('routingCandidates.review')}
        </Button>
      )}
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          void add()
        }}
      >
        <FormField label={t('common.protocol')}>
          <select
            aria-label={t('common.protocol')}
            value={protocol}
            disabled={lockDraft}
            onChange={(e) => {
              if (authority()) onProtocolChange(e.target.value as RoutingProtocol)
            }}
            className="h-11 w-full rounded-md border bg-background px-3"
          >
            {routingProtocols.map((p) => (
              <option key={p} value={p}>
                {protocolLabel(p)}
              </option>
            ))}
          </select>
        </FormField>
        <FormField label={t('routingCandidates.providerSearch')}>
          <Input
            aria-label={t('routingCandidates.providerSearch')}
            value={providerQuery}
            disabled={lockDraft}
            onValueChange={(v) => {
              if (!readReady()) return
              setProviderQuery(v)
              setProviderCursor(undefined)
            }}
            maxLength={200}
          />
        </FormField>
        <QueryState
          pending={providers.isFetching}
          error={providers.error}
          retry={() => void providers.refetch()}
          empty={providersVisible && !providers.data.items.length}
        />
        <FormField label={t('common.provider')}>
          <select
            aria-label={t('common.provider')}
            className="h-11 w-full rounded-md border bg-background px-3"
            value={provider?.id ?? ''}
            disabled={!providersVisible || lockDraft}
            onChange={(e) => {
              if (!authority()) return
              const row = providers.data?.items.find((p) => p.id === e.target.value)
              if (!row) return
              setProvider(row)
              setSelected(null)
              setQuery('')
              setCursor(undefined)
            }}
          >
            <option value="">{t('routingCandidates.chooseProvider')}</option>
            {provider && !providers.data?.items.some((p) => p.id === provider.id) && (
              <option value={provider.id}>{provider.name}</option>
            )}
            {providersVisible &&
              providers.data.items.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
          </select>
        </FormField>
        {providersVisible && providers.data.next_cursor && (
          <Button
            type="button"
            variant="outline"
            disabled={lockDraft}
            onClick={() => {
              if (authority()) setProviderCursor(providers.data.next_cursor!)
            }}
          >
            {t('routingCandidates.nextProviders')}
          </Button>
        )}
        {provider && (
          <>
            <FormField label={t('routingCandidates.search')}>
              <Input
                aria-label={t('routingCandidates.search')}
                value={query}
                maxLength={200}
                disabled={lockDraft}
                onValueChange={(v) => {
                  if (readReady()) {
                    setQuery(v)
                    setCursor(undefined)
                  }
                }}
              />
            </FormField>
            <QueryState
              pending={candidates.isFetching}
              error={candidates.error}
              retry={() => void candidates.refetch()}
              empty={candidatesFresh && !candidates.data.items.length}
            />
            {candidatesFresh && (
              <Table aria-label={t('routingCandidates.table')}>
                <thead>
                  <tr>
                    <th>{t('routingCandidates.select')}</th>
                    <th>{t('routingCandidates.connection')}</th>
                    <th>{t('adminModels.providerModel')}</th>
                    <th>{t('routingCandidates.verification')}</th>
                    <th>{t('routingCandidates.availability')}</th>
                    <th>{t('adminModels.inputBasePrice')}</th>
                    <th>{t('adminModels.outputBasePrice')}</th>
                  </tr>
                </thead>
                <tbody>
                  {candidates.data.items.map((row) => (
                    <tr key={row.id}>
                      <td>
                        <input
                          type="radio"
                          name="routing-candidate"
                          aria-label={t('routingCandidates.selectModel', {
                            name: row.upstream_name,
                          })}
                          checked={selected?.id === row.id}
                          disabled={lockDraft || !row.selectable}
                          onChange={() => {
                            if (authority() && row.selectable) setSelected(row)
                          }}
                        />
                      </td>
                      <td>{row.connection_name}</td>
                      <td>{row.upstream_name}</td>
                      <td>
                        {t(
                          row.verification_covered
                            ? 'routingCandidates.covered'
                            : 'routingCandidates.uncovered',
                        )}
                      </td>
                      <td>
                        {t(
                          row.configured_available
                            ? 'routingCandidates.available'
                            : 'routingCandidates.unavailable',
                        )}
                      </td>
                      {(['input', 'output'] as const).map((side) => (
                        <td key={side}>
                          {row.prices === null
                            ? t('routingCandidates.pricesDenied')
                            : row.prices[side].length
                              ? row.prices[side].map((rate) => (
                                  <p key={rate.currency}>
                                    {rate.amount} {rate.currency} /{' '}
                                    {t('routingCandidates.tokenUnit')}
                                    {!rate.enabled && ` (${t('common.disabled')})`}
                                  </p>
                                ))
                              : t('routingCandidates.priceMissing')}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
            {candidatesFresh && candidates.data.next_cursor && (
              <Button
                type="button"
                variant="outline"
                disabled={lockDraft}
                onClick={() => {
                  if (authority()) setCursor(candidates.data.next_cursor!)
                }}
              >
                {t('routingCandidates.nextModels')}
              </Button>
            )}
            {selected && candidatesFresh && (
              <p className="text-sm">
                {t('routingCandidates.selected', {
                  name: selected.upstream_name,
                  connection: selected.connection_name,
                })}
              </p>
            )}
          </>
        )}
        <p className="text-xs text-muted-foreground">{t('routingCandidates.factsHelp')}</p>
        <ErrorNotice error={error} />
        <div className="flex justify-end">
          <SaveButton
            pending={pending}
            disabled={
              !canWrite || !candidatesFresh || (!intent && !selected?.selectable) || conflict
            }
          >
            {t(intent ? 'routingCandidates.retry' : 'routingCandidates.confirm')}
          </SaveButton>
        </div>
      </form>
    </Dialog>
  )
}
