import { useLayoutEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { Provider } from '@/types/catalog'
import { useConnectionQueryRevision } from './connection-authority'

export default function ProviderDisabledConnectionAttention({
  provider,
  onReview,
}: {
  provider: Provider
  onReview: () => void
}) {
  const { t } = useTranslation('catalog')
  const cache = useQueryClient()
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
  // Rendering reads current query state, never the event-time owner ref.
  const readable =
    !!actor &&
    expiredScope !== scope &&
    authority.snapshot() === authority.revision &&
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
  const current = () =>
    owner.current === scope &&
    authority.snapshot() === authority.revision &&
    cache.getQueryData<Session | null>(sessionKey)?.user.id === actor &&
    readable &&
    [sessionKey, permissionsKey, catalogueKey].every((key) => {
      const state = cache.getQueryState(key)
      return (
        state?.status === 'success' &&
        state.fetchStatus === 'idle' &&
        !state.error &&
        !state.isInvalidated
      )
    }) &&
    cache.getQueryData<string[]>(permissionsKey)?.includes('providers.read') === true &&
    cache.getQueryData<Provider[]>(catalogueKey)?.find((item) => item.id === provider.id) ===
      provider
  const disabled = readable
    ? provider.connections.filter((connection) => connection.enabled === false).length
    : 0
  const unknown =
    !readable || provider.connections.some((connection) => typeof connection.enabled !== 'boolean')
  return (
    <>
      {disabled > 0 && (
        <div className="flex items-center justify-between gap-3 text-sm">
          <span>{t('providerDisabledAttention.count', { count: disabled })}</span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              if (
                current() &&
                provider.connections.some((connection) => connection.enabled === false)
              )
                onReview()
            }}
          >
            {t('providerDisabledAttention.review')}
          </Button>
        </div>
      )}
      {unknown && (
        <p className="text-sm text-muted-foreground">{t('providerDisabledAttention.unknown')}</p>
      )}
    </>
  )
}
