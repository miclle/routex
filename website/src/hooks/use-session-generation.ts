import { useCallback, useContext, useMemo, useSyncExternalStore } from 'react'
import { QueryClientContext } from '@tanstack/react-query'

// Observe completed Session reads without mounting another network observer.
// Manual CSRF cache replacement is not a renewal of Team authority.
export function useSessionGeneration() {
  const client = useContext(QueryClientContext)
  const state = useMemo(
    () => ({ generation: client?.getQueryState(['auth', 'session'])?.dataUpdateCount ?? 0 }),
    [client],
  )
  const subscribe = useCallback(
    (changed: () => void) =>
      client?.getQueryCache().subscribe((event) => {
        const key = event.query.queryKey
        if (
          key.length === 2 &&
          key[0] === 'auth' &&
          key[1] === 'session' &&
          event.type === 'updated' &&
          event.action.type === 'success' &&
          !event.action.manual
        ) {
          state.generation = event.query.state.dataUpdateCount
          changed()
        }
      }) ?? (() => undefined),
    [client, state],
  )
  const snapshot = useCallback(() => state.generation, [state])
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}
