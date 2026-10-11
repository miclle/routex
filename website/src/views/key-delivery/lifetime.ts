import { useCallback, useLayoutEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { Session } from '@/types/auth'
import type { KeyDeliveryOwner } from '@/types/key-delivery'

export class ObsoleteOwnerOperation extends Error {
  constructor() {
    super('Owner operation is no longer current')
  }
}
export interface OwnerTicket {
  version: string
  actor: string
  csrf: string
  controller: AbortController
}
export interface OwnerAuthority {
  fresh: boolean
  version: string
  sessionVersion: string
  actor: string
  capture: () => OwnerTicket
  current: (ticket: OwnerTicket) => boolean
  finish: (ticket: OwnerTicket) => void
}

// QueryCache notifications fence dispatch and completion synchronously, including
// the interval before React commits the renewed Session/list render. Session has
// no public ID: freshness uses dataUpdatedAt, fetch generation and exact CSRF.
export function useOwnerAuthority(
  scope: KeyDeliveryOwner,
  allowed: boolean,
  resource = '',
): OwnerAuthority {
  const cache = useQueryClient()
  const projectId = scope.kind === 'project' ? scope.projectId : ''
  const owner = useRef({
    mounted: false,
    allowed: false,
    resource: '',
    tickets: new Set<OwnerTicket>(),
  })
  const relevant = useCallback(
    (key: readonly unknown[]) =>
      (key[0] === 'auth' && key[1] === 'session') ||
      (projectId
        ? (key[0] === 'project-keys' && key[1] === projectId) ||
          (key[0] === 'project-key-replacements' && key[1] === projectId) ||
          key[0] === 'permissions' ||
          (key[0] === 'resources' && key[1] === 'projects' && key[3] === projectId)
        : key[0] === 'keys' || key[0] === 'models'),
    [projectId],
  )
  const readFreshness = useCallback(() => {
    const session = cache.getQueryState<Session | null>(['auth', 'session'])
    const good = (state: {
      status: string
      fetchStatus: string
      error: unknown
      isInvalidated: boolean
    }) =>
      state.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.error &&
      !state.isInvalidated
    const queries = cache.getQueryCache().getAll()
    const rows = queries.filter(
      (query) =>
        query.isActive() &&
        (projectId
          ? (query.queryKey[0] === 'project-keys' ||
              query.queryKey[0] === 'project-key-replacements') &&
            query.queryKey[1] === projectId
          : query.queryKey[0] === 'keys' || query.queryKey[0] === 'models'),
    )
    const permissions = projectId
      ? cache.getQueryState<string[]>(['permissions', session?.data?.user.id])
      : undefined
    const resources = queries.filter(
      (query) =>
        query.isActive() &&
        query.queryKey[0] === 'resources' &&
        query.queryKey[1] === 'projects' &&
        query.queryKey[3] === projectId,
    )
    return !!(
      session &&
      good(session) &&
      session.data &&
      rows.length &&
      rows.every((query) => good(query.state)) &&
      (!projectId || (permissions && good(permissions))) &&
      resources.every((query) => good(query.state))
    )
  }, [cache, projectId])
  const snapshot = useCallback(
    () =>
      JSON.stringify([
        readFreshness(),
        cache
          .getQueryCache()
          .getAll()
          .filter((query) => relevant(query.queryKey))
          .sort((a, b) => a.queryHash.localeCompare(b.queryHash))
          .map((query) => [
            query.queryHash,
            query.state.status,
            query.state.fetchStatus,
            query.state.fetchFailureCount,
            query.state.dataUpdatedAt,
            query.state.errorUpdatedAt,
            query.state.isInvalidated,
            query.state.dataUpdateCount,
            query.state.errorUpdateCount,
            query.queryKey[0] === 'auth' ? (query.state.data as Session | null)?.csrf_token : null,
          ]),
      ]),
    [cache, readFreshness, relevant],
  )
  const subscribe = useCallback(
    (notify: () => void) => {
      let subscribed = true
      let queued = false
      const unsubscribe = cache.getQueryCache().subscribe((event) => {
        if (!relevant(event.query.queryKey)) return
        const version = snapshot()
        // Admission and I/O cancellation never wait for React or the bookkeeping queue.
        for (const ticket of owner.current.tickets)
          if (ticket.version !== version) ticket.controller.abort()
        if (event.type === 'updated' || event.type === 'removed') {
          notify()
          return
        }
        // Cached observers can make authority fresh without changing query state.
        // Defer only their React notification, since options may change during render.
        if (queued) return
        queued = true
        queueMicrotask(() => {
          queued = false
          if (subscribed) notify()
        })
      })
      return () => {
        subscribed = false
        unsubscribe()
      }
    },
    [cache, owner, relevant, snapshot],
  )
  const version = useSyncExternalStore(subscribe, snapshot, snapshot)
  const session = cache.getQueryState<Session | null>(['auth', 'session'])
  const fresh = allowed && readFreshness()
  useLayoutEffect(() => {
    const cell = owner.current
    cell.mounted = true
    cell.allowed = fresh
    if (cell.resource !== resource) for (const ticket of cell.tickets) ticket.controller.abort()
    cell.resource = resource
    return () => {
      cell.allowed = false
    }
  }, [owner, fresh, resource])
  useLayoutEffect(() => {
    const cell = owner.current
    return () => {
      cell.mounted = false
      for (const ticket of cell.tickets) ticket.controller.abort()
      cell.tickets.clear()
    }
  }, [owner])
  const capture = useCallback((): OwnerTicket => {
    const auth = cache.getQueryState<Session | null>(['auth', 'session'])
    if (
      !owner.current.mounted ||
      !owner.current.allowed ||
      snapshot() !== version ||
      !readFreshness() ||
      owner.current.resource !== resource ||
      auth?.status !== 'success' ||
      auth.fetchStatus !== 'idle' ||
      !auth.data ||
      auth.error ||
      auth.isInvalidated
    )
      throw new ObsoleteOwnerOperation()
    const ticket = {
      version: snapshot(),
      actor: auth.data.user.id,
      csrf: auth.data.csrf_token,
      controller: new AbortController(),
    }
    owner.current.tickets.add(ticket)
    return ticket
  }, [cache, owner, readFreshness, resource, snapshot, version])
  const current = useCallback(
    (ticket: OwnerTicket) => {
      const auth = cache.getQueryData<Session | null>(['auth', 'session'])
      return (
        owner.current.mounted &&
        owner.current.allowed &&
        readFreshness() &&
        owner.current.resource === resource &&
        !ticket.controller.signal.aborted &&
        ticket.version === snapshot() &&
        ticket.actor === auth?.user.id &&
        ticket.csrf === auth?.csrf_token
      )
    },
    [cache, owner, readFreshness, resource, snapshot],
  )
  const finish = useCallback(
    (ticket: OwnerTicket) => {
      owner.current.tickets.delete(ticket)
    },
    [owner],
  )
  const sessionVersion = JSON.stringify([
    session?.status,
    session?.fetchStatus,
    session?.dataUpdatedAt,
    session?.dataUpdateCount,
    session?.errorUpdateCount,
    session?.isInvalidated,
    session?.data?.csrf_token,
  ])
  const actor = session?.data?.user.id ?? ''
  return useMemo(
    () => ({ fresh, version, sessionVersion, actor, capture, current, finish }),
    [fresh, version, sessionVersion, actor, capture, current, finish],
  )
}
