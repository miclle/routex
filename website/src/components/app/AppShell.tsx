import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Route, LogOut } from 'lucide-react'
import { NavLink, Outlet, useNavigate } from 'react-router'
import { authError, logout } from '@/api/auth'
import { useSession, sessionKey, setupKey } from '@/hooks/use-auth'
import { Button } from '@/components/ui/button'

export default function AppShell() {
  const session = useSession()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const mutation = useMutation({
    mutationFn: () => logout(session.data!.csrf_token),
    onSuccess: () => {
      queryClient.clear()
      queryClient.setQueryData(setupKey, { initialized: true })
      queryClient.setQueryData(sessionKey, null)
      navigate('/login', { replace: true })
    },
  })
  return (
    <div className="min-h-screen bg-muted/20 text-foreground">
      <header className="border-b bg-background">
        <div className="mx-auto flex min-h-16 max-w-6xl flex-wrap items-center justify-between gap-3 px-6 py-3">
          <NavLink to="/" className="flex items-center gap-2 text-base font-semibold"><Route className="size-5" aria-hidden="true" />RouteX<span className="ml-3 border-l pl-3 text-sm font-normal text-muted-foreground">控制台</span></NavLink>
          <div className="flex items-center gap-3"><span className="max-w-40 truncate text-sm text-muted-foreground">{session.data?.user.name}</span><Button variant="outline" size="sm" disabled={mutation.isPending} onClick={() => mutation.mutate()}><LogOut className="size-3.5" aria-hidden="true" />{mutation.isPending ? '正在退出…' : '退出登录'}</Button></div>
        </div>
        <nav aria-label="主导航" className="mx-auto flex max-w-6xl flex-wrap gap-1 px-6 pb-3">
          {[{ to: '/', label: '概览' }, { to: '/models', label: '我的模型' }, { to: '/keys', label: '我的 Key' }, ...(session.data?.user.role === 'admin' ? [{ to: '/admin/providers', label: '供应商' }, { to: '/admin/models', label: '模型管理' }] : [])].map((item) => <NavLink key={item.to} to={item.to} end className={({ isActive }) => `rounded-md px-3 py-2 text-sm ${isActive ? 'bg-secondary font-medium text-foreground' : 'text-muted-foreground hover:bg-accent'}`}>{item.label}</NavLink>)}
        </nav>
      </header>
      {mutation.isError && <p role="alert" className="mx-auto max-w-6xl px-6 pt-4 text-sm text-destructive">退出失败。{authError(mutation.error)}</p>}
      <main><Outlet /></main>
    </div>
  )
}
