import { CircleCheck, UserRound } from 'lucide-react'
import { useSession } from '@/hooks/use-auth'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export default function Home() {
  const { data: session } = useSession()
  if (!session) return null
  return (
    <div className="mx-auto max-w-6xl space-y-8 px-6 py-10 sm:py-14">
      <header className="space-y-3"><p className="text-xs font-medium tracking-widest text-muted-foreground">工作空间</p><h1 className="text-3xl font-semibold tracking-tight">你好，{session.user.name}</h1><p className="text-sm leading-6 text-muted-foreground">你已安全登录 RouteX。这里是你的企业 AI 控制台。</p></header>
      <Card className="max-w-2xl">
        <CardHeader><CardTitle className="flex items-center gap-2"><UserRound className="size-4" aria-hidden="true" />当前账户</CardTitle></CardHeader>
        <CardContent><dl className="grid gap-5 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">邮箱</dt><dd className="mt-1.5 break-all font-medium">{session.user.email}</dd></div><div><dt className="text-muted-foreground">平台角色</dt><dd className="mt-1.5"><Badge variant="outline">{session.user.role === 'admin' ? '管理员' : '成员'}</Badge></dd></div></dl><p className="mt-6 flex items-center gap-2 border-t pt-5 text-xs text-muted-foreground"><CircleCheck className="size-4" aria-hidden="true" />当前登录会话有效，退出后立即失效。</p></CardContent>
      </Card>
    </div>
  )
}
