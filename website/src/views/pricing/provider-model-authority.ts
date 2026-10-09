import { useLayoutEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { Session } from '@/types/auth'
import { sessionKey } from '@/hooks/use-auth'
import { useConnectionQueryRevision } from '@/views/providers/connection-authority'

export interface ProviderModelAuthority {
  actor: string
  providerId: string
  connectionId: string
  generation: number
  permissionsKey: readonly unknown[]
  catalogueKey: readonly unknown[]
  readable: () => boolean
  writable: () => boolean
}
// Reuse the detail page's exact selected-resource reads without another Session or catalogue observer.
export function useProviderModelAuthority(
  modelId: string,
  visible: boolean,
  authority: ProviderModelAuthority,
) {
  const cache = useQueryClient()
  const revision = useConnectionQueryRevision([
    sessionKey,
    authority.permissionsKey,
    authority.catalogueKey,
  ])
  const ownerKey = JSON.stringify([
    authority.actor,
    authority.providerId,
    authority.connectionId,
    authority.generation,
    modelId,
  ])
  const [revokedOwner, setRevokedOwner] = useState<string | null>(null)
  const lifetime = useRef<object | null>({})
  const expired = useRef(false)
  useLayoutEffect(() => {
    lifetime.current = {}
    expired.current = false
    const revoke = () => {
      expired.current = true
      lifetime.current = null
      setRevokedOwner(ownerKey)
    }
    window.addEventListener('routex:session-expired', revoke)
    return () => {
      lifetime.current = null
      window.removeEventListener('routex:session-expired', revoke)
    }
  }, [ownerKey])
  // Rendering follows reactive query revisions and revocation state; handlers still
  // enforce the synchronous lifetime refs below, including captured callbacks.
  const auth = cache.getQueryState<Session | null>(sessionKey)
  const renderReadable =
    revokedOwner !== ownerKey &&
    visible &&
    authority.readable() &&
    revision.snapshot() === revision.revision &&
    auth?.status === 'success' &&
    auth.fetchStatus === 'idle' &&
    !auth.error &&
    !auth.isInvalidated &&
    auth.data?.user.id === authority.actor &&
    auth.dataUpdateCount === authority.generation
  const renderWritable = renderReadable && authority.writable() && !!auth?.data?.csrf_token
  const readable = () => {
    const auth = cache.getQueryState<Session | null>(sessionKey)
    return (
      !!lifetime.current &&
      !expired.current &&
      visible &&
      authority.readable() &&
      revision.snapshot() === revision.revision &&
      auth?.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.error &&
      !auth.isInvalidated &&
      auth.data?.user.id === authority.actor &&
      auth.dataUpdateCount === authority.generation
    )
  }
  const writable = () =>
    readable() && authority.writable() && !!cache.getQueryData<Session>(sessionKey)?.csrf_token
  const capture = () => ({ identity: lifetime.current, revision: revision.snapshot() })
  const current = (token: ReturnType<typeof capture>) =>
    readable() &&
    token.identity !== null &&
    token.identity === lifetime.current &&
    token.revision === revision.snapshot()
  return {
    renderReadable,
    renderWritable,
    readable,
    writable,
    capture,
    current,
    revision: revision.revision,
    csrf: () => cache.getQueryData<Session>(sessionKey)?.csrf_token,
    cache,
  }
}
