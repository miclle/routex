import { useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  getRoutingWeightReview,
  saveReviewedRoutingWeights,
  type RoutingWeightReview,
} from '@/api/model-routing'
import { QueryState } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import RoutingWeights from '@/views/models/routing-weights'
import type { ProviderModelBindingSource } from './provider-model-binding'

type Weights = { binding_id: string; weight: number }[]
type Intent = { etag: string; weights: Weights }

export default function useProviderModelBindingWeights({
  actor,
  modelID,
  bindingID,
  source,
  revision,
  readReady,
  writeReady,
  onActivity,
  onClose,
}: {
  actor: string
  modelID: string | null
  bindingID: string | null
  source: ProviderModelBindingSource
  revision: string
  readReady: () => boolean
  writeReady: () => boolean
  onActivity: (locked: boolean) => void
  onClose: () => void
}) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  const [generation, setGeneration] = useState(0)
  const [draft, setDraft] = useState<{ etag: string; values: Record<string, string> } | null>(null)
  const [intent, setIntent] = useState<Intent | null>(null)
  const retained = useRef<Intent | null>(null)
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<RoutingWeightReview | null>(null)
  const mounted = useRef(false)
  const operation = useRef<AbortController | null>(null)
  const owner = useRef(revision)
  const lifetime = useRef(0)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      operation.current?.abort()
    }
  }, [])
  const authority = useSyncExternalStore(
    (notify) => cache.getQueryCache().subscribe(() => notify()),
    () => {
      if (
        !modelID ||
        !bindingID ||
        !readReady() ||
        cache.getQueryData<Session>(sessionKey)?.user.id !== actor
      )
        return 0
      return writeReady() ? 2 : 1
    },
    () => 0,
  )
  const readable = authority > 0
  const writable = authority === 2
  useLayoutEffect(() => {
    if (owner.current !== revision || !readable) {
      owner.current = revision
      lifetime.current++
      setResult(null)
      if (operation.current) {
        operation.current.abort()
        operation.current = null
        setPending(false)
      }
    }
  }, [revision, readable])
  const key = [
    'admin',
    'provider-model-binding-weights',
    actor,
    modelID,
    bindingID,
    revision,
    generation,
  ]
  const review = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getRoutingWeightReview(modelID!, signal),
    enabled: readable && !!modelID && !!bindingID && !intent && !result,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const fresh = () => {
    const state = cache.getQueryState(key)
    return (
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error
    )
  }
  const exactSource = (value: RoutingWeightReview) =>
    value.model.bindings.some(
      (binding) =>
        binding.id === bindingID &&
        binding.provider_model_id === source.modelID &&
        binding.provider_id === source.providerID &&
        binding.connection_id === source.connectionID &&
        binding.protocol === source.protocol &&
        binding.upstream_name === source.upstreamName,
    )
  const current =
    readable && fresh() && review.data && exactSource(review.data) ? review.data : undefined
  const values =
    draft?.values ??
    Object.fromEntries(
      current?.model.bindings.map((binding) => [binding.id, String(binding.weight)]) ?? [],
    )
  const reviewRequired = !!current && !!draft && draft.etag !== current.review_etag
  const readAuthorized = () =>
    !!modelID &&
    !!bindingID &&
    mounted.current &&
    readReady() &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor
  const ready = () => readAuthorized() && writeReady()
  async function save(weights?: Weights) {
    if (!ready() || operation.current || result) return
    const retry = !!retained.current
    const captured =
      retained.current ??
      (weights && current && !reviewRequired ? { etag: current.review_etag, weights } : null)
    if (!captured || (!retry && !fresh())) return
    const csrf = cache.getQueryData<Session>(sessionKey)?.csrf_token
    if (!csrf) return
    retained.current = captured
    setIntent(captured)
    setPending(true)
    onActivity(true)
    const controller = new AbortController()
    operation.current = controller
    const version = lifetime.current
    const readCount = cache.getQueryState(key)?.dataUpdateCount
    const valid = () =>
      ready() &&
      operation.current === controller &&
      !controller.signal.aborted &&
      version === lifetime.current
    try {
      const response = await saveReviewedRoutingWeights(
        modelID!,
        captured.weights,
        captured.etag,
        csrf,
        controller.signal,
      )
      if (
        !valid() ||
        !exactSource(response) ||
        (!retry && (!fresh() || cache.getQueryState(key)?.dataUpdateCount !== readCount))
      )
        return
      retained.current = null
      setIntent(null)
      setResult(response)
    } catch {
      // Keep the exact reviewed write separate from the insertion intent.
    } finally {
      if (mounted.current && operation.current === controller) {
        operation.current = null
        setPending(false)
      }
    }
  }
  function reviewCurrent() {
    if (!readAuthorized() || operation.current) return
    // This explicit choice abandons the original retry, never acknowledges it.
    retained.current = null
    setIntent(null)
    setResult(null)
    setDraft(null)
    setGeneration((value) => value + 1)
  }
  const content = (
    <section className="space-y-4">
      <p role="status">{t('providerModelBinding.saved')}</p>
      <p className="text-sm text-muted-foreground">{t('providerModelBinding.weightsHelp')}</p>
      {intent && (
        <div className="space-y-3">
          <p role="alert">{t('providerModelBinding.weightsUncertain')}</p>
          <Button disabled={pending || !writable} onClick={() => void save()}>
            {t(
              pending ? 'providerModelBinding.weightsSaving' : 'providerModelBinding.retryWeights',
            )}
          </Button>
          <Button variant="outline" disabled={pending || !readable} onClick={reviewCurrent}>
            {t('providerModelBinding.reviewWeights')}
          </Button>
        </div>
      )}
      {result && (
        <div className="space-y-3">
          <p role="status">
            {t(
              result.runtime_applied
                ? 'providerModelBinding.weightsApplied'
                : 'providerModelBinding.weightsSaved',
            )}
          </p>
          <Button variant="outline" disabled={pending || !readable} onClick={reviewCurrent}>
            {t('providerModelBinding.reviewWeights')}
          </Button>
        </div>
      )}
      {!intent && !result && (
        <>
          <QueryState
            pending={review.isPending || review.isFetching}
            error={review.error}
            retry={reviewCurrent}
          />
          {review.isError && (
            <Button variant="outline" onClick={reviewCurrent}>
              {t('providerModelBinding.reviewWeights')}
            </Button>
          )}
          {current ? (
            <>
              <p className="text-sm text-muted-foreground">
                {t(
                  current.runtime_applied
                    ? 'providerModelBinding.currentApplied'
                    : 'providerModelBinding.currentConfigured',
                )}
              </p>
              <RoutingWeights
                model={current.model}
                draft={values}
                reviewRequired={reviewRequired}
                onDraftChange={(id, value) => {
                  if (!ready() || !fresh() || reviewRequired) return
                  const binding = current.model.bindings.find((row) => row.id === id)
                  if (binding?.protocol !== source.protocol) return
                  setDraft({ etag: current.review_etag, values: { ...values, [id]: value } })
                }}
                onReviewCurrent={reviewCurrent}
                actor={actor}
                generation={generation}
                providers={undefined}
                canWrite={writable}
                canAdd={false}
                pricesReadable={false}
                editableProtocol={source.protocol}
                showPrices={false}
                pending={pending}
                error={null}
                onSave={(weights) => void save(weights)}
                refreshDetail={reviewCurrent}
              />
            </>
          ) : (
            !review.isPending &&
            !review.isFetching &&
            !review.isError && <p role="alert">{t('providerModelBinding.weightsSourceChanged')}</p>
          )}
        </>
      )}
      <div className="flex justify-end">
        <Button variant="outline" disabled={pending} onClick={onClose}>
          {t('providerModelBinding.done')}
        </Button>
      </div>
    </section>
  )
  return { content, pending }
}
