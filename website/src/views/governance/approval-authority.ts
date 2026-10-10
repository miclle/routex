import { useCallback, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { Session } from '@/types/auth'
export function useApprovalCacheRevision(keys: readonly (readonly unknown[])[]) {
  const cache = useQueryClient(),
    serialized = JSON.stringify(keys)
  const snapshot = useCallback(
    () =>
      JSON.parse(serialized)
        .map((key: unknown[]) => {
          const s = cache.getQueryState(key)
          return `${s?.status}:${s?.fetchStatus}:${s?.isInvalidated}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
        })
        .join('|'),
    [cache, serialized],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (event.type === 'updated' || event.type === 'removed') &&
          JSON.parse(serialized).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, serialized],
  )
  return { snapshot, revision: useSyncExternalStore(subscribe, snapshot, snapshot) }
}
export function approvalActorCurrent(
  cache: ReturnType<typeof useQueryClient>,
  actor: string,
  permission: string,
  admin = false,
) {
  const auth = cache.getQueryState<Session>(['auth', 'session']),
    grants = cache.getQueryState<string[]>(['permissions', actor])
  return (
    !!actor &&
    [auth, grants].every(
      (s) => s?.status === 'success' && s.fetchStatus === 'idle' && !s.isInvalidated && !s.error,
    ) &&
    auth?.data?.user.id === actor &&
    (!admin || auth.data.user.role === 'admin') &&
    grants?.data?.includes(permission) === true
  )
}
