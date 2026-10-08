import { useCallback, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { Session } from '@/types/auth'
import { sessionKey } from '@/hooks/use-auth'
import { systemJobsKey } from '@/api/system-status'

// Existing workspace observers perform network reads. Cache events also close
// the interval before React renders a renewed or revoked read generation.
export function useRoutingReadAuthority() {
  const cache = useQueryClient()
  const snapshot = useCallback(() => {
    const session = cache.getQueryState<Session>(sessionKey)
    const actorID = session?.data?.user.id
    const permissions = cache.getQueryState<string[]>(['permissions', actorID])
    const jobs = cache.getQueryState(systemJobsKey)
    const states = [session, permissions, jobs].map((state) => ({
      status: state?.status ?? 'pending',
      fetching: state?.fetchStatus ?? 'idle',
      invalidated: state?.isInvalidated ?? true,
      generation: state?.dataUpdateCount ?? 0,
    }))
    const ready =
      !!actorID &&
      permissions?.data?.includes('system.read') === true &&
      states.every(
        (state) => state.status === 'success' && state.fetching === 'idle' && !state.invalidated,
      )
    return JSON.stringify({ actorID: actorID ?? null, ready, states })
  }, [cache])
  const subscribe = useCallback(
    (changed: () => void) => cache.getQueryCache().subscribe(changed),
    [cache],
  )
  const basis = useSyncExternalStore(subscribe, snapshot, snapshot)
  const { actorID, ready } = JSON.parse(basis) as { actorID: string | null; ready: boolean }
  return { actorID, basis, ready, isCurrent: () => ready && snapshot() === basis }
}
