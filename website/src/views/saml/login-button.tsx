import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getSAMLMethod, startSAML } from '@/api/saml'
import { Button } from '@/components/ui/button'
import { useApprovalCacheRevision } from '@/views/governance/approval-authority'
import type { Session } from '@/types/auth'

export default function SAMLLoginButton({
  disabled = false,
  acquire,
}: {
  disabled?: boolean
  acquire: () => { current: () => boolean; release: () => void } | null
}) {
  const { t } = useTranslation('saml')
  const cache = useQueryClient()
  const boundary = useApprovalCacheRevision([
    ['auth', 'session'],
    ['saml', 'public'],
  ])
  const method = useQuery({
    queryKey: ['saml', 'public'],
    queryFn: ({ signal }) => getSAMLMethod(signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  const [busy, setBusy] = useState(false)
  const [issue, setIssue] = useState(false)
  const owner = useRef({ mounted: true, turn: 0, locked: false, used: false, revoked: false })
  const request = useRef<AbortController | null>(null)
  function readable() {
    const state = cache.getQueryState(['saml', 'public'])
    const session = cache.getQueryState<Session | null>(['auth', 'session'])
    return (
      boundary.snapshot() === boundary.revision &&
      method.isSuccess &&
      !method.isFetching &&
      state?.status === 'success' &&
      state.fetchStatus === 'idle' &&
      !state.isInvalidated &&
      !state.error &&
      state.data === method.data &&
      (!session ||
        (session.status === 'success' &&
          session.fetchStatus === 'idle' &&
          !session.isInvalidated &&
          !session.error &&
          session.data === null))
    )
  }
  useEffect(() => {
    const live = owner.current
    live.mounted = true
    const expire = () => {
      live.revoked = true
      live.turn++
      request.current?.abort()
      setBusy(false)
      setIssue(true)
    }
    window.addEventListener('routex:session-expired', expire)
    return () => {
      live.mounted = false
      live.turn++
      request.current?.abort()
      window.removeEventListener('routex:session-expired', expire)
    }
  }, [])
  const authoritySnapshot = boundary.snapshot
  useLayoutEffect(() => {
    let previous = authoritySnapshot()
    return cache.getQueryCache().subscribe((event) => {
      if (event.type !== 'updated' && event.type !== 'removed') return
      const key = event.query.queryKey
      if (
        key.length !== 2 ||
        !((key[0] === 'auth' && key[1] === 'session') || (key[0] === 'saml' && key[1] === 'public'))
      )
        return
      const next = authoritySnapshot()
      if (previous === next) return
      previous = next
      const live = owner.current
      if (!live.locked) return
      live.revoked = true
      live.turn++
      live.locked = false
      request.current?.abort()
      setBusy(false)
      setIssue(true)
    })
  }, [cache, authoritySnapshot])
  async function start() {
    const live = owner.current
    if (
      !live.mounted ||
      disabled ||
      live.locked ||
      live.used ||
      live.revoked ||
      !readable() ||
      !method.data?.available
    )
      return
    const lease = acquire()
    if (!lease) return
    live.locked = true
    live.used = true
    setBusy(true)
    const turn = ++live.turn
    request.current = new AbortController()
    try {
      const url = await startSAML(request.current.signal)
      if (live.mounted && live.turn === turn && !live.revoked && readable() && lease.current())
        window.location.assign(url)
      else if (live.mounted) setIssue(true)
    } catch {
      if (live.mounted && live.turn === turn) setIssue(true)
    } finally {
      lease.release()
      if (live.mounted && live.turn === turn) {
        live.locked = false
        setBusy(false)
      }
    }
  }
  if (!readable() || !method.data?.available) return null
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <span className="h-px flex-1 bg-border" />
        <span>{t('alternative')}</span>
        <span className="h-px flex-1 bg-border" />
      </div>
      <Button
        type="button"
        variant="outline"
        className="w-full"
        disabled={disabled || busy || issue}
        onClick={() => void start()}
      >
        {t('signIn', { name: method.data.name })}
      </Button>
      {issue && <p role="alert">{t('startUnknown')}</p>}
    </div>
  )
}
