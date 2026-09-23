import { useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { changePassword, listAccountSessions, revokeAccountSession, updateAccount } from '@/api/account'
import { sessionKey, setupKey, useSession } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { AccountSession } from '@/types/account'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Dialog } from '@/components/ui/dialog'

export default function AccountPage({ security = false }: { security?: boolean }) {
  const { data: session } = useSession()
  const cache = useQueryClient()
  const navigate = useNavigate()
  const sessions = useQuery({ queryKey: ['account', 'sessions'], queryFn: ({ signal }) => listAccountSessions(signal) })
  const [notice, setNotice] = useState('')
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [validation, setValidation] = useState('')
  const passwordForm = useRef<HTMLFormElement>(null)
  const [selected, setSelected] = useState<AccountSession | null>(null)
  const profile = useMutation({
    mutationFn: (name: string) => updateAccount(name, session!.csrf_token),
    onSuccess: (user) => { cache.setQueryData(sessionKey, (current: Session | undefined) => current ? { ...current, user } : current); setNotice('个人资料已更新。') },
  })
  const password = useMutation({
    mutationFn: (input: { current_password: string; new_password: string }) => changePassword(input, session!.csrf_token),
    onSuccess: async (nextSession) => {
      await cache.cancelQueries({ queryKey: sessionKey })
      cache.setQueryData(sessionKey, nextSession)
      await cache.invalidateQueries({ queryKey: ['account', 'sessions'] })
      setNotice('密码已更新，其他旧会话均已撤销。当前页面使用新会话。')
      passwordForm.current?.reset()
      password.reset()
    },
    gcTime: 0,
  })
  const revoke = useMutation({
    mutationFn: (target: AccountSession) => revokeAccountSession(target.id, session!.csrf_token),
    onSuccess: async (_result, target) => {
      setSelected(null)
      if (target.current) {
        await cache.cancelQueries()
        cache.clear()
        cache.setQueryData(setupKey, { initialized: true })
        cache.setQueryData(sessionKey, null)
        navigate('/login', { replace: true })
      } else { await cache.invalidateQueries({ queryKey: ['account', 'sessions'] }); setNotice('会话已撤销。') }
    },
  })
  function submitPassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (password.isPending) return
    const form = event.currentTarget
    const values = new FormData(form)
    const newPassword = String(values.get('new_password') ?? '')
    const bytes = new TextEncoder().encode(newPassword).length
    if (bytes < 12 || bytes > 72) { setValidation('新密码须为 12–72 个 UTF-8 字节。'); return }
    if (newPassword !== values.get('confirm_password')) { setValidation('两次输入的新密码不一致。'); return }
    setValidation('')
    setNotice('')
    password.mutate({ current_password: String(values.get('current_password') ?? ''), new_password: newPassword })
  }
  if (!session) return null
  return <Page title={security ? "安全设置" : "个人资料"} description="管理个人资料、密码和登录会话。敏感修改会在服务端验证身份并立即更新会话状态。">
    {notice && <p role="status" className="rounded-md border bg-card p-3 text-sm">{notice}</p>}
    {!security && <><div className="flex flex-wrap items-center gap-4 rounded-lg border p-6"><span className="flex size-[72px] items-center justify-center rounded-full bg-muted text-2xl">{session.user.name.slice(0, 2).toUpperCase()}</span><div className="min-w-0 flex-1"><h2 className="text-2xl font-semibold">{session.user.name}</h2><p className="mt-2 break-all text-sm text-muted-foreground">{session.user.email}</p></div><Badge variant="outline">{session.user.role === 'admin' ? '管理员' : '成员'}</Badge></div><Card><CardHeader><CardTitle>基本信息</CardTitle><CardDescription>工作邮箱用于登录，本页可修改显示名称。</CardDescription></CardHeader><CardContent><form aria-label="个人资料" className="space-y-5 [&_label]:grid [&_label]:grid-cols-[minmax(80px,1fr)_5fr] [&_label]:items-center [&_label]:gap-4" onSubmit={(e) => { e.preventDefault(); if (profile.isPending) return; const name = String(new FormData(e.currentTarget).get('name') ?? '').trim(); if (!name) return; setNotice(''); profile.mutate(name) }}><fieldset disabled={profile.isPending} className="space-y-5"><FormField label="姓名"><Input name="name" defaultValue={session.user.name} maxLength={100} required /></FormField><FormField label="工作邮箱"><Input value={session.user.email} disabled /></FormField></fieldset><ErrorNotice error={profile.error} /><SaveButton pending={profile.isPending}>保存资料</SaveButton></form></CardContent></Card>
</>}
    {security && <><Card><CardHeader><CardTitle>账户密码</CardTitle></CardHeader><CardContent className="flex items-center justify-between gap-4"><div className="space-y-1"><p className="text-sm font-medium">登录密码已设置</p><p className="text-sm text-muted-foreground">密码更新后全部旧会话将失效。</p></div><Button variant="outline" onClick={() => setPasswordOpen(true)}>修改密码</Button></CardContent></Card>
    <Card><CardHeader><CardTitle>登录会话</CardTitle><CardDescription>撤销会话后，该浏览器下次请求将需要重新登录。</CardDescription></CardHeader><CardContent><QueryState pending={sessions.isPending} error={sessions.error} retry={() => void sessions.refetch()} empty={sessions.data?.length === 0} /><div className="divide-y">{sessions.data?.map((item) => <div key={item.id} className="flex flex-wrap items-center justify-between gap-4 py-4 first:pt-0"><div className="space-y-2"><p className="break-all font-mono text-xs">{item.id} {item.current && <Badge variant="outline">当前会话</Badge>}</p><p className="text-xs leading-6 text-muted-foreground">创建于 {new Date(item.created_at).toLocaleString()} · 到期 {new Date(item.expires_at).toLocaleString()}</p></div><Button variant="outline" size="sm" onClick={() => { revoke.reset(); setSelected(item) }}>撤销会话</Button></div>)}</div></CardContent></Card>
    </>}
    <Dialog open={passwordOpen} onOpenChange={setPasswordOpen} busy={password.isPending} title="修改账户密码" description="成功后撤销全部旧会话，并为当前浏览器创建新会话。"><form ref={passwordForm} aria-label="修改密码" onSubmit={submitPassword} className="space-y-5"><fieldset disabled={password.isPending} className="space-y-5"><FormField label="当前密码"><Input name="current_password" type="password" autoComplete="current-password" required /></FormField><FormField label="新密码"><Input name="new_password" type="password" autoComplete="new-password" required /></FormField><FormField label="确认新密码"><Input name="confirm_password" type="password" autoComplete="new-password" required /></FormField></fieldset>{validation && <p role="alert" className="text-sm text-destructive">{validation}</p>}<ErrorNotice error={password.error} /><SaveButton pending={password.isPending}>更新密码</SaveButton></form></Dialog>
    <Dialog open={selected !== null} onOpenChange={(open) => { if (!open) setSelected(null) }} busy={revoke.isPending} title="撤销登录会话" description={selected?.current ? '这是当前会话。确认后你将退出登录，且此操作无法恢复。' : '该会话将立即失效，对方需要重新输入密码登录。'}><div className="space-y-4"><ErrorNotice error={revoke.error} /><Button disabled={revoke.isPending} onClick={() => { if (selected) revoke.mutate(selected) }}>{revoke.isPending ? '正在撤销…' : '确认撤销'}</Button></div></Dialog>
  </Page>
}
