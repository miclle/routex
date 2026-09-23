import { t } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Navigate, Outlet } from 'react-router'
import { LoaderCircle } from 'lucide-react'
import { useSession, useSetup, sessionKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'

export default function AuthGate({ mode }: { mode: 'private' | 'login' | 'setup' }) {
  useTranslation()

  const queryClient = useQueryClient()
  const setup = useSetup()
  const session = useSession(setup.data?.initialized === true)

  useEffect(() => {
    const expire = () => {
      void queryClient
        .cancelQueries({ predicate: (query) => query.queryKey[0] !== 'site' })
        .then(() => {
          queryClient.setQueryData(sessionKey, null)
          queryClient.removeQueries({
            predicate: (query) => query.queryKey[0] !== 'auth' && query.queryKey[0] !== 'site',
          })
          queryClient.getMutationCache().clear()
        })
    }
    window.addEventListener('routex:session-expired', expire)
    return () => window.removeEventListener('routex:session-expired', expire)
  }, [queryClient])

  useEffect(() => {
    if (session.data === null) {
      queryClient.removeQueries({
        predicate: (query) => query.queryKey[0] !== 'auth' && query.queryKey[0] !== 'site',
      })
      queryClient.getMutationCache().clear()
    }
  }, [queryClient, session.data])

  if (setup.isPending || (setup.data?.initialized && session.isPending)) {
    return (
      <GateState>
        <LoaderCircle className="mx-auto size-5 animate-spin" aria-hidden="true" />
        <p role="status">{t('checking_your_session_f9b65')}</p>
      </GateState>
    )
  }
  if (setup.isError || (setup.data?.initialized && session.isError)) {
    return (
      <GateState>
        <p role="alert">{t('unable_to_check_your_session_check_the_service_76313')}</p>
        <Button
          onClick={() => {
            void setup.refetch()
            if (setup.data?.initialized) void session.refetch()
          }}
        >
          {t('retry_e2d53')}
        </Button>
      </GateState>
    )
  }
  if (!setup.data?.initialized)
    return mode === 'setup' ? <Outlet /> : <Navigate to="/setup" replace />
  if (session.data) return mode === 'private' ? <Outlet /> : <Navigate to="/" replace />
  if (mode === 'setup')
    return (
      <Navigate
        to="/login"
        replace
        state={{ messageKey: 'this_site_is_already_set_up_please_sign_e2a95' }}
      />
    )
  if (mode === 'private')
    return (
      <Navigate
        to="/login"
        replace
        state={{ messageKey: 'sign_in_to_continue_if_your_session_expired_10a92' }}
      />
    )
  return <Outlet />
}

function GateState({ children }: { children: ReactNode }) {
  useTranslation()

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/30 px-6">
      <div className="space-y-4 text-center text-sm text-muted-foreground">{children}</div>
    </main>
  )
}
