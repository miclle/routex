import { useCallback, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'

// Observe invalidation as well as data changes, so private cached facts hide
// before a renewed read can establish current authority.
export function useConnectionQueryRevision(keys: readonly (readonly unknown[])[]) {
  const cache = useQueryClient()
  const serialized = JSON.stringify(keys)
  const snapshot = useCallback(
    () =>
      (JSON.parse(serialized) as unknown[][])
        .map((key) => {
          const state = cache.getQueryState(key)
          return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
        })
        .join('|'),
    [cache, serialized],
  )
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          (JSON.parse(serialized) as unknown[][]).some(
            (key) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, serialized],
  )
  return { snapshot, revision: useSyncExternalStore(subscribe, snapshot, snapshot) }
}
