import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'
import { completeOIDC, readOIDCSession } from '@/api/oidc'
import { verifyMFALogin } from '@/api/mfa'
import { sessionKey, setupKey } from '@/hooks/use-auth'
import { LanguageSwitcher } from '@/components/app/LanguageSwitcher'
import { SiteLogo, SiteFooter } from '@/components/app/SiteBranding'
import { Button } from '@/components/ui/button'
import MFAChallengeForm from '@/views/auth/mfa-challenge'
import type { MFAChallenge, MFAProof } from '@/types/mfa'
import type { Session } from '@/types/auth'

export default function OIDCComplete() {
  const { t } = useTranslation('oidc')
  const cache = useQueryClient()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [started, setStarted] = useState(false)
  const [issue, setIssue] = useState('')
  const [challenge, setChallenge] = useState<MFAChallenge | null>(null)
  const owner = useRef({
    mounted: true,
    turn: 0,
    locked: false,
    used: false,
    phase: 'idle',
    session: '',
    revoked: false,
  })
  const controller = useRef<AbortController | null>(null)
  const snapshot = useCallback(() => {
    const state = cache.getQueryState(sessionKey)
    return `${state?.status}:${state?.fetchStatus}:${state?.isInvalidated}:${state?.dataUpdateCount}:${state?.errorUpdateCount}`
  }, [cache])
  useEffect(() => {
    const live = owner.current
    live.mounted = true
    function revoke() {
      live.revoked = true
      live.turn++
      live.locked = false
      controller.current?.abort()
      setChallenge(null)
      setBusy(false)
      setIssue('completionUnknown')
    }
    const unsubscribe = cache.getQueryCache().subscribe((event) => {
      if (
        JSON.stringify(event.query.queryKey) === JSON.stringify(sessionKey) &&
        (live.phase === 'read' || live.phase === 'complete' || live.phase === 'mfa') &&
        snapshot() !== live.session
      )
        revoke()
    })
    window.addEventListener('routex:session-expired', revoke)
    return () => {
      live.mounted = false
      live.turn++
      controller.current?.abort()
      unsubscribe()
      window.removeEventListener('routex:session-expired', revoke)
    }
  }, [cache, snapshot])
  function current(turn: number) {
    const live = owner.current
    return live.mounted && !live.revoked && live.turn === turn && snapshot() === live.session
  }
  async function install(session: Session, turn: number) {
    if (!current(turn)) return
    owner.current.phase = 'settle'
    await cache.cancelQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
    if (!current(turn)) return
    cache.removeQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
    cache.getMutationCache().clear()
    cache.setQueryData(setupKey, { initialized: true })
    cache.setQueryData(sessionKey, session)
    setChallenge(null)
    navigate('/', { replace: true })
  }
  async function complete() {
    const live = owner.current
    if (live.locked || live.used || live.revoked || !live.mounted) return
    live.locked = true
    live.used = true
    live.session = snapshot()
    live.phase = 'read'
    const turn = ++live.turn
    const request = new AbortController()
    controller.current = request
    setStarted(true)
    setBusy(true)
    setIssue('')
    try {
      const session = await readOIDCSession(request.signal)
      if (!current(turn) || request.signal.aborted) return
      live.phase = 'complete'
      const result = await completeOIDC(session?.csrf_token, request.signal)
      if (!current(turn)) return
      if (result.kind === 'session') {
        if (session) throw new Error('Account switch unavailable')
        await install(result.session, turn)
      } else if (result.kind === 'challenge') {
        if (session) throw new Error('Account switch unavailable')
        live.phase = 'mfa'
        setChallenge(result.challenge)
      } else {
        if (!session) throw new Error('Session unavailable')
        live.phase = 'settle'
        await cache.cancelQueries({ queryKey: ['oidc'] })
        if (!current(turn)) return
        cache.removeQueries({ queryKey: ['oidc'] })
        navigate(result.kind === 'bound' ? '/account/security' : '/admin/auth', { replace: true })
      }
    } catch {
      if (live.mounted && live.turn === turn) setIssue('completionUnknown')
    } finally {
      if (live.mounted && live.turn === turn) {
        live.locked = false
        setBusy(false)
      }
    }
  }
  async function verify(proof: MFAProof) {
    const live = owner.current
    if (!challenge || live.locked || !current(live.turn)) return
    if (Date.parse(challenge.expires_at) <= Date.now()) {
      restart()
      return
    }
    live.locked = true
    const turn = ++live.turn
    const request = new AbortController()
    controller.current = request
    setBusy(true)
    setIssue('')
    try {
      await install(await verifyMFALogin(challenge.challenge_token, proof, request.signal), turn)
    } catch {
      if (live.mounted && live.turn === turn) setIssue('proofFailed')
    } finally {
      if (live.mounted && live.turn === turn) {
        live.locked = false
        setBusy(false)
      }
    }
  }
  function restart() {
    owner.current.turn++
    controller.current?.abort()
    setChallenge(null)
    navigate('/login', { replace: true })
  }
  return (
    <main className="flex min-h-screen items-center justify-center bg-background p-6">
      <div className="absolute right-6 top-6">
        <LanguageSwitcher />
      </div>
      <section className="w-full max-w-[500px] space-y-6">
        <div className="flex justify-center">
          <SiteLogo />
        </div>
        <div className="space-y-6 rounded-lg border p-6">
          <h1 className="text-center text-[30px] font-semibold leading-[38px]">
            {t('completeTitle')}
          </h1>
          {challenge ? (
            <MFAChallengeForm
              challenge={challenge}
              busy={busy}
              error={issue ? t(issue) : ''}
              verify={(proof) => void verify(proof)}
              restart={restart}
            />
          ) : (
            <>
              <p className="text-sm text-muted-foreground">{t('completeHelp')}</p>
              {issue && <p role="alert">{t(issue)}</p>}
              {!started && (
                <Button className="w-full" disabled={busy} onClick={() => void complete()}>
                  {t('continue')}
                </Button>
              )}
              {busy && <p role="status">{t('working')}</p>}
              {started && !busy && (
                <Link className="block underline" to="/login" replace>
                  {t('restartLogin')}
                </Link>
              )}
            </>
          )}
        </div>
        <SiteFooter />
      </section>
    </main>
  )
}
