import { useSession } from '@/hooks/use-auth'
import { Badge } from '@/components/ui/badge'

export default function Home() {
  const { data: session } = useSession()
  if (!session) return null
  return <section className="space-y-6"><h1 className="sr-only">你好，{session.user.name}</h1><section aria-label={`${session.user.name}成员信息`} className="flex flex-wrap items-center gap-4 rounded-lg border p-3"><span className="flex size-12 items-center justify-center rounded-full bg-muted">{session.user.name.slice(0, 2).toUpperCase()}</span><div className="min-w-0 flex-1"><h2 className="text-[30px] leading-[38px] font-semibold">{session.user.name}</h2><p className="text-sm leading-6 text-muted-foreground">{session.user.email}</p><p className="text-sm leading-6 text-muted-foreground">角色：{session.user.role === 'admin' ? '管理员' : '成员'}</p></div><Badge variant="outline">正常</Badge></section></section>
}
