import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useLayoutEffect, useRef } from 'react'
import type { Session } from '@/types/auth'
import { getTeamRoles } from '@/api/team-roles'
import { sessionKey, useSession } from './use-auth'
export function useTeamAccess(team: string, enabled: boolean) {
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const query = useQuery({
    queryKey: ['team-roles', actor, team],
    queryFn: ({ signal }) => getTeamRoles(team, signal),
    enabled: !!actor && enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const fresh = !!actor && enabled && query.isSuccess && !query.isFetching && !query.isError
  return { ...query, actor, fresh, current: fresh ? query.data : undefined }
}

export function useTeamMutationGuard(team: boolean, canEdit: boolean) {
  const session = useSession(),
    cache = useQueryClient()
  const actor = session.data?.user.id
  const mounted = useRef(false),
    allowed = useRef(false)
  useLayoutEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useLayoutEffect(() => {
    allowed.current = canEdit
  })
  const active = () =>
    !team ||
    (mounted.current &&
      allowed.current &&
      !!actor &&
      cache.getQueryData<Session>(sessionKey)?.user.id === actor &&
      cache.getQueryState(sessionKey)?.status !== 'error')
  const csrf = () => {
    if (!active()) throw new Error('Team authority is unavailable')
    return team ? cache.getQueryData<Session>(sessionKey)!.csrf_token : session.data!.csrf_token
  }
  return { active, csrf }
}
