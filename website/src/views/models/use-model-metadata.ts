import { useCallback, useLayoutEffect, useRef, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getModelMonthlyRequests } from '@/api/model-metadata'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { ModelMonthlyRequests } from '@/types/model-metadata'

export interface ModelMonthlyView {
  state: 'unknown' | 'unavailable' | 'available'
  report?: ModelMonthlyRequests
  retry: () => void
}
export function useModelMonthlyRequests({
  actor,
  generation,
  permissionKey,
  catalogKey,
  ids,
  visible,
}: {
  actor: string
  generation: number
  permissionKey: unknown[]
  catalogKey: unknown[]
  ids: string[]
  visible: boolean
}): ModelMonthlyView {
  const cache = useQueryClient()
  const keys = JSON.stringify([sessionKey, permissionKey, catalogKey])
  const subscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (
          JSON.parse(keys).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, keys],
  )
  const snapshot = useCallback(
    () =>
      JSON.stringify(
        JSON.parse(keys).map((key: unknown[]) => {
          const s = cache.getQueryState(key)
          return [s?.status, s?.fetchStatus, s?.isInvalidated, s?.dataUpdateCount]
        }),
      ),
    [cache, keys],
  )
  const proof = useSyncExternalStore(subscribe, snapshot, snapshot)
  const ready = () =>
    visible &&
    cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
    !!cache.getQueryData<Session>(sessionKey)?.csrf_token &&
    [sessionKey, permissionKey, catalogKey].every((key) => {
      const s = cache.getQueryState(key)
      return s?.status === 'success' && s.fetchStatus !== 'fetching' && !s.isInvalidated
    }) &&
    cache.getQueryData<string[]>(permissionKey)?.includes('models.read_all') === true &&
    cache.getQueryData<string[]>(permissionKey)?.includes('calls.read_all') === true
  const bounded = ids.length > 0 && ids.length <= 500
  const queryKey = [
    'admin',
    'model-monthly-requests',
    actor,
    generation,
    JSON.stringify(permissionKey),
    JSON.stringify(catalogKey),
    proof,
    ids,
  ]
  const hash = JSON.stringify(queryKey),
    owner = useRef<string | null>(null)
  useLayoutEffect(() => {
    owner.current = hash
    return () => {
      owner.current = null
      void cache.cancelQueries({ queryKey: JSON.parse(hash), exact: true })
    }
  }, [cache, hash])
  const query = useQuery({
    queryKey,
    enabled: ready() && bounded,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    queryFn: async ({ signal }) => {
      if (!ready() || snapshot() !== proof || owner.current !== hash)
        throw new Error('Current Model statistics authority unavailable')
      const result = await getModelMonthlyRequests([...ids], signal)
      if (signal.aborted || !ready() || snapshot() !== proof || owner.current !== hash)
        throw new Error('Obsolete Model statistics response')
      return result
    },
  })
  const subscribeOwn = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === hash) notify()
      }),
    [cache, hash],
  )
  const ownSnapshot = useCallback(
    () => cache.getQueryState(JSON.parse(hash))?.isInvalidated ?? false,
    [cache, hash],
  )
  const invalidated = useSyncExternalStore(subscribeOwn, ownSnapshot, ownSnapshot)
  const current = ready()
  useLayoutEffect(() => {
    if (!current) void cache.cancelQueries({ queryKey: JSON.parse(hash), exact: true })
  }, [cache, hash, current])
  const state =
    !current || ids.length === 0
      ? 'unknown'
      : !bounded || query.isError
        ? 'unavailable'
        : query.isSuccess && !query.isFetching && !invalidated
          ? 'available'
          : 'unknown'
  return {
    state,
    ...(state === 'available' ? { report: query.data } : {}),
    retry: () => {
      if (ready() && bounded && owner.current === hash) void query.refetch()
    },
  }
}
