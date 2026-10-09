import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getProviderModelBindings } from '@/api/provider-model-bindings'
import { Button } from '@/components/ui/button'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { Provider } from '@/types/catalog'
import type { ProviderModelBindings } from '@/types/provider-model-bindings'
import { useConnectionQueryRevision } from './connection-authority'

function completeUnboundCount(provider: Provider, projection: ProviderModelBindings) {
  const rows = provider.connections.flatMap((connection) =>
    connection.provider_models.map((model) => ({ connection, model })),
  )
  const projected = new Map(projection.items.map((item) => [item.provider_model_id, item]))
  if (
    projection.provider_id !== provider.id ||
    projected.size !== projection.items.length ||
    projected.size !== rows.length ||
    new Set(rows.map(({ model }) => model.id)).size !== rows.length ||
    rows.some(({ connection, model }) => projected.get(model.id)?.connection_id !== connection.id)
  )
    return undefined
  return projection.items.filter((item) => item.binding_count === 0).length
}

export default function ProviderUnboundAttention({
  provider,
  onReview,
  showEmpty = false,
}: {
  provider: Provider
  onReview: () => void
  showEmpty?: boolean
}) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
  // Reuse the workspace's current reads; this row starts no Session or directory read.
  const sessionRevision = useConnectionQueryRevision([sessionKey])
  const actor = cache.getQueryData<Session | null>(sessionKey)?.user.id ?? ''
  const permissionsKey = ['permissions', actor] as const
  const catalogueKey = ['admin', 'providers'] as const
  const authority = useConnectionQueryRevision([sessionKey, permissionsKey, catalogueKey])
  const scope = `${actor}:${provider.id}:${sessionRevision.revision}:${authority.revision}`
  const owner = useRef<string | null>(null)
  const [expiredScope, setExpiredScope] = useState<string | null>(null)
  useLayoutEffect(() => {
    owner.current = scope
    const expire = () => {
      if (owner.current === scope) {
        owner.current = null
        setExpiredScope(scope)
      }
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      if (owner.current === scope) owner.current = null
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [scope])
  const current = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey)
    const rights = cache.getQueryState<string[]>(permissionsKey)
    const catalogue = cache.getQueryState<Provider[]>(catalogueKey)
    return (
      !!actor &&
      owner.current === scope &&
      authority.snapshot() === authority.revision &&
      auth?.data?.user.id === actor &&
      [auth, rights, catalogue].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.error &&
          !state.isInvalidated,
      ) &&
      rights?.data?.includes('providers.read') === true &&
      catalogue?.data?.find((item) => item.id === provider.id) === provider
    )
  }
  // Layout effects establish the mounted owner before any user interaction.
  const readable =
    !!actor &&
    expiredScope !== scope &&
    authority.snapshot() === authority.revision &&
    cache.getQueryData<string[]>(permissionsKey)?.includes('models.read_all') === true &&
    cache.getQueryData<string[]>(permissionsKey)?.includes('providers.read') === true &&
    [sessionKey, permissionsKey, catalogueKey].every((key) => {
      const state = cache.getQueryState(key)
      return (
        state?.status === 'success' &&
        state.fetchStatus === 'idle' &&
        !state.error &&
        !state.isInvalidated
      )
    }) &&
    cache.getQueryData<Provider[]>(catalogueKey)?.find((item) => item.id === provider.id) ===
      provider
  const queryKey = ['admin', 'provider-unbound-attention', actor, provider.id, scope] as const
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => {
      if (!current() || !cache.getQueryData<string[]>(permissionsKey)?.includes('models.read_all'))
        throw new Error('Provider Model binding authority changed')
      return getProviderModelBindings(provider.id, signal)
    },
    enabled: readable,
    retry: false,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  })
  const projectionRevision = useConnectionQueryRevision([queryKey])
  useEffect(() => {
    if (!readable)
      void cache.cancelQueries({
        queryKey: ['admin', 'provider-unbound-attention', actor, provider.id, scope],
        exact: true,
      })
  }, [cache, readable, actor, provider.id, scope])
  const count = () => {
    const state = cache.getQueryState<ProviderModelBindings>(queryKey)
    if (
      !current() ||
      !cache.getQueryData<string[]>(permissionsKey)?.includes('models.read_all') ||
      projectionRevision.snapshot() !== projectionRevision.revision ||
      state?.status !== 'success' ||
      state.fetchStatus !== 'idle' ||
      state.isInvalidated ||
      state.error ||
      !state.data
    )
      return undefined
    return completeUnboundCount(provider, state.data)
  }
  const projectionState = cache.getQueryState<ProviderModelBindings>(queryKey)
  const total =
    readable &&
    projectionRevision.snapshot() === projectionRevision.revision &&
    projectionState?.status === 'success' &&
    projectionState.fetchStatus === 'idle' &&
    !projectionState.isInvalidated &&
    !projectionState.error &&
    projectionState.data
      ? completeUnboundCount(provider, projectionState.data)
      : undefined
  if (total === 0)
    return showEmpty ? (
      <p className="text-sm text-muted-foreground">{t('providers.noAttention')}</p>
    ) : null
  return (
    <div className="flex items-center justify-between gap-3 text-sm">
      <span>
        {total === undefined
          ? t(
              readable
                ? query.isError
                  ? 'providerUnboundAttention.unavailable'
                  : 'providerUnboundAttention.unknown'
                : 'providerUnboundAttention.restricted',
            )
          : t('providerUnboundAttention.count', { count: total })}
      </span>
      {total !== undefined && (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            const latest = count()
            if (latest !== undefined && latest > 0) onReview()
          }}
        >
          {t('providerUnboundAttention.review')}
        </Button>
      )}
    </div>
  )
}
