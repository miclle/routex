import { useCallback, useLayoutEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSession } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { Session } from '@/types/auth'

// One Session observer owns the catalogue. Cache events close the interval
// between a completed network read and React rendering its new generation.
export function useCatalogueAuthority() {
  const cache = useQueryClient()
  const session = useSession()
  const generation = useSessionGeneration()
  const networkGeneration = useRef(generation)
  useLayoutEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        if (
          event.query.queryKey.length === 2 &&
          event.query.queryKey[0] === 'auth' &&
          event.query.queryKey[1] === 'session' &&
          event.type === 'updated' &&
          event.action.type === 'success' &&
          !event.action.manual
        )
          networkGeneration.current = event.query.state.dataUpdateCount
      }),
    [cache],
  )
  const actorID = session.data?.user.id
  const fresh = session.isSuccess && !session.isFetching && !session.isError
  const isCurrent = useCallback(() => {
    const current = cache.getQueryState<Session>(['auth', 'session'])
    return (
      fresh &&
      current?.status === 'success' &&
      current.fetchStatus === 'idle' &&
      current.data?.user.id === actorID &&
      networkGeneration.current === generation
    )
  }, [cache, actorID, fresh, generation])
  return { session, actorID, generation, fresh, isCurrent }
}
