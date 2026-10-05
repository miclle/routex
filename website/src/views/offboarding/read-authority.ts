import { useCallback, useSyncExternalStore } from 'react'
import type { QueryClient, QueryKey } from '@tanstack/react-query'

export function freshQuery(cache: QueryClient, key: QueryKey) {
  const state = cache.getQueryState(key)
  return state?.status === 'success' && state.fetchStatus === 'idle' && !state.isInvalidated
}
export function offboardingVersion(cache: QueryClient, actor: string, target: string) {
  return JSON.stringify(
    cache
      .getQueryCache()
      .getAll()
      .filter((query) => {
        const key = query.queryKey
        return (
          (key[0] === 'auth' && key[1] === 'session') ||
          (key[0] === 'permissions' && key[1] === actor && key[3] === 'offboarding') ||
          (key[0] === 'offboarding' &&
            key[1] === actor &&
            key[2] === target &&
            (key[4] === 'inventory' || key[4] === 'member'))
        )
      })
      .sort((a, b) => a.queryHash.localeCompare(b.queryHash))
      .map((query) => [
        query.queryHash,
        query.state.dataUpdateCount,
        query.state.errorUpdateCount,
        query.state.status,
        query.state.fetchStatus,
        query.state.isInvalidated,
      ]),
  )
}
export function useOffboardingVersion(cache: QueryClient, actor: string, target: string) {
  const subscribe = useCallback(
    (changed: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type === 'updated') changed()
      }),
    [cache],
  )
  const snapshot = useCallback(
    () => offboardingVersion(cache, actor, target),
    [cache, actor, target],
  )
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}
