import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Mail } from 'lucide-react'
import { getRegistration } from '@/api/governance'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/ui/drawer'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
export default function RegistrationPage() { return <PermissionGate permission="registration.write"><Registration /></PermissionGate> }
function Registration() {
  const session = useSession()
  const cache = useQueryClient()
  const settings = useQuery({ queryKey: ['admin', 'registration'], queryFn: () => getRegistration(true) })
  const [open, setOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const mutation = useMutation({ mutationFn: (enabled: boolean) => writeCatalog<{ enabled: boolean }>('patch', '/admin/registration', { enabled }, session.data!.csrf_token), onSuccess: (result) => { cache.setQueryData(['admin', 'registration'], result); void cache.invalidateQueries({ queryKey: ['auth', 'registration'] }); setNotice('成员注册设置已保存。'); setOpen(false) } })
  return <Page title="身份认证" description="管理成员邮箱注册方式。"><div><h2 className="text-base font-semibold">认证方式</h2><p className="mt-2 text-sm text-muted-foreground">查看平台支持的登录方式与启用状态，需要调整时进入单项配置。</p></div>{notice && <p role="status" className="text-sm">{notice}</p>}<QueryState pending={settings.isPending} error={settings.error} retry={() => void settings.refetch()} />{settings.data && <section className="flex items-center justify-between gap-6 rounded-lg border p-4"><div className="flex items-center gap-4"><Mail className="size-6" /><div><p className="flex items-center gap-2 font-medium">邮箱注册<Badge variant="outline">{settings.data.enabled ? '已启用' : '未启用'}</Badge></p><p className="mt-2 text-sm text-muted-foreground">允许用户通过工作邮箱创建成员账户。</p></div></div><Button variant="outline" aria-label="配置 邮箱注册" onClick={() => { mutation.reset(); setOpen(true) }}>配置</Button></section>}<Drawer open={open} onOpenChange={setOpen} title="配置 邮箱注册" busy={mutation.isPending}><form className="space-y-6" aria-label="成员注册设置" onSubmit={(event) => { event.preventDefault(); if (mutation.isPending) return; mutation.mutate(new FormData(event.currentTarget).get('enabled') === 'on') }}><label className="flex items-center justify-between gap-4"><span><span className="block text-sm font-medium">开放邮箱注册</span><span className="text-sm text-muted-foreground">新账户默认为成员，不会自动获得模型授权。</span></span><Switch name="enabled" aria-label="开放邮箱注册" defaultChecked={settings.data?.enabled} disabled={mutation.isPending} /></label><ErrorNotice error={mutation.error} /><div className="flex justify-end"><SaveButton pending={mutation.isPending}>保存成员注册设置</SaveButton></div></form></Drawer></Page>
}
