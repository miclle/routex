import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getCredentialStorageContext } from '@/api/provider-storage'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { Session } from '@/types/auth'
import { useConnectionQueryRevision } from './connection-authority'

export function useCredentialStorageContext() {
  const cache = useQueryClient()
  const session = useSession()
  const permissions = usePermissions()
  const actor = session.data?.user.id ?? ''
  const generation = useSessionGeneration()
  const permissionKey = ['permissions', actor] as const
  const key = ['admin', 'credential-storage-context', actor, generation] as const
  const freshAuthority = () => {
    const auth = cache.getQueryState<Session>(sessionKey)
    const access = cache.getQueryState<string[]>(permissionKey)
    return (
      !!actor &&
      auth?.data?.user.id === actor &&
      auth.status === 'success' &&
      auth.fetchStatus === 'idle' &&
      !auth.isInvalidated &&
      access?.status === 'success' &&
      access.fetchStatus === 'idle' &&
      !access.isInvalidated &&
      access.data?.includes('providers.write') === true
    )
  }
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getCredentialStorageContext(signal),
    enabled: freshAuthority(),
    retry: false,
    gcTime: 0,
    staleTime: 0,
  })
  const observed = useConnectionQueryRevision([sessionKey, permissionKey, key])
  const ready = () => {
    const state = cache.getQueryState(key)
    return (
      freshAuthority() &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated
    )
  }
  return {
    query,
    actor,
    session,
    permissions,
    ready: ready(),
    current: ready,
    stamp: observed.snapshot,
    csrf: () => cache.getQueryData<Session>(sessionKey)?.csrf_token,
    context: ready() ? query.data : undefined,
  }
}
