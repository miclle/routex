import { useLayoutEffect, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useApprovalCacheRevision } from '@/views/governance/approval-authority'
import { sessionKey } from '@/hooks/use-auth'
import { ReauthenticationContext, type ReauthenticationReceipt } from './reauthentication-state'
type RouteState = {
  location: { pathname: string }
  navigation?: { location?: { pathname: string } }
}
type RouteOwner = {
  state: RouteState
  subscribe: (listener: (state: RouteState) => void) => () => void
}
export function ReauthenticationProvider({
  children,
  router,
}: {
  children: ReactNode
  router: RouteOwner
}) {
  const [receipt, put] = useState<ReauthenticationReceipt | null>(null)
  const cache = useQueryClient()
  const { snapshot, revision } = useApprovalCacheRevision([sessionKey])
  const eligible =
    !!receipt &&
    (receipt.sessionRevision === '' ||
      (cache.getQueryData(sessionKey) === null && receipt.sessionRevision === revision))
  useLayoutEffect(() => {
    if (!receipt) return
    const holding = '/auth/reauthenticate'
    let entered = router.state.location.pathname === holding
    const stopRoute = router.subscribe((state) => {
      if (state.navigation?.location && state.navigation.location.pathname !== holding) {
        put(null)
        return
      }
      if (state.location.pathname === holding) entered = true
      else if (entered) put(null)
    })
    const timer = setTimeout(() => put(null), Math.max(0, receipt.expiresAt - Date.now()))
    const stopSession = cache.getQueryCache().subscribe((event) => {
      if (
        (event.type === 'updated' || event.type === 'removed') &&
        JSON.stringify(event.query.queryKey) === JSON.stringify(sessionKey) &&
        snapshot() !== receipt.sessionRevision
      )
        put(null)
    })
    return () => {
      clearTimeout(timer)
      stopRoute()
      stopSession()
    }
  }, [cache, receipt, router, snapshot])
  return (
    <ReauthenticationContext.Provider value={{ receipt: eligible ? receipt : null, put }}>
      {children}
    </ReauthenticationContext.Provider>
  )
}
