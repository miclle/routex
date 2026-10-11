import { createContext, useCallback, useContext } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { sessionKey, setupKey } from '@/hooks/use-auth'
export type ReauthenticationReceipt = {
  recoveryCodes: string[]
  expiresAt: number
  sessionRevision: string
  runtimeApplied?: boolean
}
export const ReauthenticationContext = createContext<{
  receipt: ReauthenticationReceipt | null
  put: (v: ReauthenticationReceipt | null) => void
} | null>(null)
export function useReauthentication() {
  const context = useContext(ReauthenticationContext),
    cache = useQueryClient(),
    navigate = useNavigate()
  return useCallback(
    (codes: string[] = [], runtimeApplied?: boolean) => {
      // Never put this one-time receipt in browser history, storage or server-state caches.
      const receipt = {
        recoveryCodes: [...codes],
        expiresAt: Date.now() + 5 * 60 * 1000,
        sessionRevision: '',
        runtimeApplied,
      }
      context?.put(receipt)
      // Keep the observed Session query: removing it can recreate an authenticated
      // read during route teardown. Cancellation reverts before the final null write.
      void cache.cancelQueries({ predicate: (q) => q.queryKey[0] !== 'site' }, { revert: true })
      cache.removeQueries({
        predicate: (q) =>
          q.queryKey[0] !== 'site' && JSON.stringify(q.queryKey) !== JSON.stringify(sessionKey),
      })
      cache.getMutationCache().clear()
      cache.setQueryData(setupKey, { initialized: true })
      cache.setQueryData(sessionKey, null)
      const state = cache.getQueryState(sessionKey)
      context?.put({
        ...receipt,
        sessionRevision: `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`,
      })
      navigate(context ? '/auth/reauthenticate' : '/login', { replace: true })
    },
    [context, cache, navigate],
  )
}
