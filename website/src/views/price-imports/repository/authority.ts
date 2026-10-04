import { useLayoutEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getPermissions } from '@/api/governance'
import { useSession, sessionKey } from '@/hooks/use-auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import type { Session } from '@/types/auth'
export type RepositorySession = ReturnType<typeof useSession>
export function useRepositoryAuthority(session: RepositorySession, target: string) {
  const cache = useQueryClient(),
    generation = useSessionGeneration(),
    actor = session.data?.user.id ?? '',
    role = session.data?.user.role
  const fresh =
    session.isSuccess && !session.isFetching && !session.error && !!session.data?.csrf_token
  const permissionKey = [
    'permissions',
    actor,
    'repository-prices',
    target,
    role,
    generation,
  ] as const
  const configKey = [
    'admin',
    'repository-prices',
    actor,
    target,
    role,
    generation,
    'config',
  ] as const
  const permissions = useQuery({
    queryKey: permissionKey,
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: fresh,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const readable =
    fresh &&
    permissions.isSuccess &&
    !permissions.isFetching &&
    !permissions.error &&
    permissions.data.includes('prices.read')
  const writable = readable && permissions.data?.includes('prices.write') === true
  const sessionVersion = useRef(0),
    networkGeneration = useRef(generation)
  const alive = useRef(true),
    version = useRef(0),
    controllers = useRef(new Set<AbortController>()),
    keys = useRef({ permissionKey, configKey })
  useLayoutEffect(() => {
    keys.current = { permissionKey, configKey }
  })
  useLayoutEffect(
    () =>
      cache.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated') return
        const key = JSON.stringify(event.query.queryKey),
          isSession = key === JSON.stringify(sessionKey)
        if (
          !isSession &&
          key !== JSON.stringify(keys.current.permissionKey) &&
          key !== JSON.stringify(keys.current.configKey)
        )
          return
        if (isSession && event.action.type === 'success' && event.action.manual) {
          const next = event.query.state.data as Session | null
          if (
            next?.user.id === actor &&
            next.csrf_token &&
            next.user.role === session.data?.user.role
          )
            return
        }
        if (['fetch', 'error', 'success'].includes(event.action.type)) {
          if (isSession) {
            sessionVersion.current++
            if (event.action.type === 'success' && !event.action.manual)
              networkGeneration.current = event.query.state.dataUpdateCount
          }
          version.current++
          for (const controller of controllers.current) controller.abort()
        }
      }),
    [cache, actor, session.data?.user.role],
  )
  useLayoutEffect(() => {
    alive.current = true
    const active = controllers.current,
      epoch = version
    return () => {
      alive.current = false
      epoch.current++
      for (const controller of active) controller.abort()
      active.clear()
    }
  }, [])
  function current(write = false) {
    const session = cache.getQueryState<Session | null>(sessionKey),
      permission = cache.getQueryState<string[]>(keys.current.permissionKey)
    if (
      !alive.current ||
      networkGeneration.current !== generation ||
      session?.status !== 'success' ||
      session.fetchStatus !== 'idle' ||
      session.data?.user.id !== actor ||
      session.data?.user.role !== role ||
      !session.data.csrf_token ||
      permission?.status !== 'success' ||
      permission.fetchStatus !== 'idle' ||
      !permission.data?.includes('prices.read') ||
      (write && !permission.data.includes('prices.write'))
    )
      return undefined
    return session.data
  }
  // Capture each operation generation synchronously before dispatch.
  function readGuard() {
    const captured = JSON.stringify(keys.current.configKey),
      sessionCaptured = sessionVersion.current
    return () =>
      alive.current &&
      sessionCaptured === sessionVersion.current &&
      captured === JSON.stringify(keys.current.configKey) &&
      !!current()
  }
  function begin() {
    const controller = new AbortController(),
      captured = version.current
    controllers.current.add(controller)
    return {
      controller,
      release: () => controllers.current.delete(controller),
      valid: () => !controller.signal.aborted && version.current === captured,
    }
  }
  return {
    actor,
    generation,
    fresh,
    readable,
    writable,
    permissions,
    configKey,
    current,
    begin,
    readGuard,
    version,
    cache,
  }
}
