import { useQuery } from '@tanstack/react-query'
import { getPermissions } from '@/api/governance'
import { useSession } from './use-auth'
export function usePermissions() {
  const session = useSession()
  const query = useQuery({
    queryKey: ['permissions', session.data?.user.id],
    queryFn: ({ signal }) => getPermissions(signal),
    enabled: !!session.data,
    retry: false,
    refetchOnWindowFocus: true,
    refetchInterval: 30_000,
  })
  return {
    ...query,
    can: (permission: string) => query.data?.includes(permission) === true,
    isAdmin: session.data?.user.role === 'admin',
  }
}
