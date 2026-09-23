import { useEffect, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Navigate, Outlet } from 'react-router'
import { LoaderCircle } from 'lucide-react'
import { useSession, useSetup, sessionKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'

export default function AuthGate({ mode }: { mode: 'private' | 'login' | 'setup' }) {
  const queryClient = useQueryClient()
  const setup = useSetup()
  const session = useSession(setup.data?.initialized === true)

  useEffect(() => {
    const expire = () => {
      void queryClient.cancelQueries().then(() => {
        queryClient.setQueryData(sessionKey, null)
        queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'auth' })
        queryClient.getMutationCache().clear()
      })
    }
    window.addEventListener('routex:session-expired', expire)
    return () => window.removeEventListener('routex:session-expired', expire)
  }, [queryClient])

  useEffect(() => {
    if (session.data === null) {
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'auth' })
      queryClient.getMutationCache().clear()
    }
  }, [queryClient, session.data])

  if (setup.isPending || (setup.data?.initialized && session.isPending)) {
    return <GateState><LoaderCircle className="mx-auto size-5 animate-spin" aria-hidden="true" /><p role="status">正在确认登录状态…</p></GateState>
  }
  if (setup.isError || (setup.data?.initialized && session.isError)) {
    return <GateState><p role="alert">无法确认登录状态，请检查服务连接。</p><Button onClick={() => { void setup.refetch(); if (setup.data?.initialized) void session.refetch() }}>重试</Button></GateState>
  }
  if (!setup.data?.initialized) return mode === 'setup' ? <Outlet /> : <Navigate to="/setup" replace />
  if (session.data) return mode === 'private' ? <Outlet /> : <Navigate to="/" replace />
  if (mode === 'setup') return <Navigate to="/login" replace state={{ message: '站点已完成初始化，请登录。' }} />
  if (mode === 'private') return <Navigate to="/login" replace state={{ message: '请登录后继续；如果会话已过期，请重新登录。' }} />
  return <Outlet />
}

function GateState({ children }: { children: ReactNode }) {
  return <main className="flex min-h-screen items-center justify-center bg-muted/30 px-6"><div className="space-y-4 text-center text-sm text-muted-foreground">{children}</div></main>
}
