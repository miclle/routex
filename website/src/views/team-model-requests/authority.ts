import { useQuery } from '@tanstack/react-query'
import { getTeamModelWorkspace } from '@/api/team-model-requests'
import { useSession } from '@/hooks/use-auth'

export function useTeamModelReview(team: string | undefined, visible = true) {
  const session = useSession()
  const actor = session.isError ? '' : (session.data?.user.id ?? '')
  const query = useQuery({
    queryKey: ['team-model-workspace', actor, team],
    queryFn: ({ signal }) => getTeamModelWorkspace(team!, signal),
    enabled: !!actor && !!team,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const current =
    actor && !session.isFetching && visible && query.isSuccess && !query.isFetching
      ? query.data
      : undefined
  return { ...query, actor, session, current }
}
