import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getAuthenticationPolicy } from '@/api/auth-enforcement'
import { useApprovalCacheRevision } from '@/views/governance/approval-authority'
import type { AuthenticationPolicy } from '@/types/auth-enforcement'
export const authenticationPolicyKey = ['auth', 'enforcement'] as const
export function useAuthenticationPolicy(enabled = true) {
  const cache = useQueryClient()
  const query = useQuery({
    queryKey: authenticationPolicyKey,
    queryFn: ({ signal }) => getAuthenticationPolicy(signal),
    enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
  })
  const generation = useApprovalCacheRevision([authenticationPolicyKey])
  function current() {
    const s = cache.getQueryState<AuthenticationPolicy>(authenticationPolicyKey)
    return (
      enabled &&
      generation.snapshot() === generation.revision &&
      s?.status === 'success' &&
      s.fetchStatus === 'idle' &&
      !s.isInvalidated &&
      !s.error &&
      s.data === query.data
    )
  }
  return {
    query,
    current,
    revision: generation.revision,
    policy: current() ? query.data : undefined,
  }
}
