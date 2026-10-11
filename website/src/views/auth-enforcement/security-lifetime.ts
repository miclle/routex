import { useCallback, useLayoutEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
// Sensitive security requests are transient and belong to one fresh Session generation.
export function useSecurityLifetime(reset: () => void) {
  const cache = useQueryClient()
  const latest = useRef(reset)
  const owner = useRef({
    mounted: true,
    turn: 0,
    locked: false,
    controller: null as AbortController | null,
  })
  useLayoutEffect(() => {
    latest.current = reset
  }, [reset])
  useLayoutEffect(() => {
    const cell = owner.current
    cell.mounted = true
    const snapshot = () => {
      const s = cache.getQueryState(sessionKey)
      return `${s?.status}:${s?.fetchStatus}:${s?.isInvalidated}:${s?.dataUpdateCount}:${s?.errorUpdateCount}`
    }
    let prior = snapshot()
    const unsubscribe = cache.getQueryCache().subscribe((event) => {
      if (
        (event.type !== 'updated' && event.type !== 'removed') ||
        JSON.stringify(event.query.queryKey) !== JSON.stringify(sessionKey)
      )
        return
      const next = snapshot()
      if (next === prior) return
      prior = next
      cell.turn++
      cell.controller?.abort()
      cell.controller = null
      cell.locked = false
      latest.current()
    })
    return () => {
      cell.mounted = false
      cell.turn++
      cell.controller?.abort()
      cell.locked = false
      unsubscribe()
    }
  }, [cache])
  function fresh() {
    const s = cache.getQueryState<Session | null>(sessionKey)
    return (
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.isInvalidated &&
      !s.error &&
      !!s.data
    )
  }
  function start() {
    const cell = owner.current
    if (!cell.mounted || cell.locked || !fresh()) return null
    const session = cache.getQueryData<Session>(sessionKey)!
    const turn = ++cell.turn
    const controller = new AbortController()
    cell.locked = true
    cell.controller = controller
    return {
      session,
      signal: controller.signal,
      current: () =>
        cell.mounted &&
        cell.turn === turn &&
        !controller.signal.aborted &&
        fresh() &&
        cache.getQueryData(sessionKey) === session,
      settle: () => {
        if (cell.turn === turn) {
          cell.locked = false
          cell.controller = null
        }
      },
    }
  }
  const cancel = useCallback(() => {
    const cell = owner.current
    cell.turn++
    cell.controller?.abort()
    cell.controller = null
    cell.locked = false
  }, [])
  return { fresh, start, cancel }
}
