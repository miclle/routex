import { useQuery } from '@tanstack/react-query'
import { getSite, siteKey } from '@/api/site'
export function useSite() {
  return useQuery({
    queryKey: siteKey,
    queryFn: ({ signal }) => getSite(signal),
    retry: false,
    staleTime: 60_000,
  })
}
