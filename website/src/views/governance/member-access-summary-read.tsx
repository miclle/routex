import { useCallback, useSyncExternalStore } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getMemberAccessSummary, memberAccessSummaryKey } from '@/api/member-access-summary'
import type { Session } from '@/types/auth'
import type { MemberDetail } from '@/types/member-recent-login'

export function useMemberAccessSummary({
  actor,
  target,
  generation,
  ready,
  targetQueryKey,
}: {
  actor: string
  target: string
  generation: number
  ready: boolean
  targetQueryKey: readonly unknown[]
}) {
  const cache = useQueryClient()
  const serialized = JSON.stringify([['auth', 'session'], ['permissions', actor], targetQueryKey])
  const snapshot = useCallback(
    () =>
      JSON.parse(serialized)
        .map((key: unknown[]) => {
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
          JSON.parse(serialized).some(
            (key: unknown[]) => JSON.stringify(key) === JSON.stringify(event.query.queryKey),
          )
        )
          notify()
      }),
    [cache, serialized],
  )
  const revision = useSyncExternalStore(subscribe, snapshot, snapshot)
  function authority() {
    const session = cache.getQueryState<Session>(['auth', 'session'])
    const grants = cache.getQueryState<string[]>(['permissions', actor])
    const subject = cache.getQueryState<MemberDetail>(targetQueryKey)
    return (
      ready &&
      !!actor &&
      !!target &&
      snapshot() === revision &&
      [session, grants, subject].every(
        (state) =>
          state?.status === 'success' &&
          state.fetchStatus === 'idle' &&
          !state.isInvalidated &&
          !state.error,
      ) &&
      session?.data?.user.id === actor &&
      subject?.data?.id === target &&
      grants?.data?.includes('members.read') === true
    )
  }
  const grants = cache.getQueryData<string[]>(['permissions', actor])
  const flags = {
    roles: grants?.includes('roles.read') === true,
    teams: grants?.includes('teams.read_all') === true,
  }
  const ownKey = JSON.stringify([
    'admin',
    'member-access-summary',
    actor,
    target,
    generation,
    revision,
  ])
  const key = memberAccessSummaryKey(actor, target, generation, revision)
  const query = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      if (!authority()) throw new Error('Member access read is unavailable')
      const page = await getMemberAccessSummary(target, flags, signal)
      const subject = cache.getQueryData<MemberDetail>(targetQueryKey)
      if (signal.aborted || !authority() || page.identity_role !== subject?.role)
        throw new Error('Member access read is unavailable')
      return page
    },
    enabled: authority(),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const readSnapshot = useCallback(() => {
    const state = cache.getQueryState(JSON.parse(ownKey))
    return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
  }, [cache, ownKey])
  const readSubscribe = useCallback(
    (notify: () => void) =>
      cache.getQueryCache().subscribe((event) => {
        if (JSON.stringify(event.query.queryKey) === ownKey) notify()
      }),
    [cache, ownKey],
  )
  const readRevision = useSyncExternalStore(readSubscribe, readSnapshot, readSnapshot)
  function current() {
    const state = cache.getQueryState(key)
    return (
      authority() &&
      readSnapshot() === readRevision &&
      query.isSuccess &&
      !query.isFetching &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === query.data
    )
  }
  return {
    data: current() ? query.data : undefined,
    pending: authority() && (query.isPending || query.isFetching),
    error: query.error,
    retry: () => {
      if (authority()) void query.refetch()
    },
    current,
  }
}
export type MemberAccessRead = ReturnType<typeof useMemberAccessSummary>
