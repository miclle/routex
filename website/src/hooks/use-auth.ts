import { useQuery } from '@tanstack/react-query'
import { getSession, getSetup } from '@/api/auth'

export const setupKey = ['auth', 'setup'] as const
export const sessionKey = ['auth', 'session'] as const

export function useSetup() {
  return useQuery({ queryKey: setupKey, queryFn: getSetup, retry: false })
}

export function useSession(enabled = true) {
  return useQuery({
    queryKey: sessionKey,
    queryFn: getSession,
    enabled,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
  })
}
