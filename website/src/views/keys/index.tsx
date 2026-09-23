import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { createKey, listKeys, listModels, rotateKey, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Table } from '@/components/ui/table'
import { Drawer } from '@/components/ui/drawer'
import { Badge } from '@/components/ui/badge'
import type { KeyDelivery, PersonalKey } from '@/types/catalog'

const statuses = { pending: '待确认交付', active: '已启用', disabled: '已停用', revoked: '已撤销' }
export default function KeysPage() {
  const { data: session } = useSession()
  const cache = useQueryClient()
  const keys = useQuery({ queryKey: ['keys'], queryFn: listKeys })
  const models = useQuery({ queryKey: ['models'], queryFn: listModels })
  const [creating, setCreating] = useState(false)
  const [statusFilter, setStatusFilter] = useState('all')
  const [viewing, setViewing] = useState<PersonalKey | null>(null)
  const [delivery, setDelivery] = useState<KeyDelivery | null>(null)
  const [saved, setSaved] = useState(false)
  const [retiring, setRetiring] = useState<PersonalKey | null>(null)
  const [notice, setNotice] = useState('')
  const [action, setAction] = useState<{ kind: 'rename' | 'revoke'; key: PersonalKey } | null>(null)
  const refresh = () => cache.invalidateQueries({ queryKey: ['keys'] })
  const create = useMutation({
    mutationFn: async (input: { name: string; model_ids: string[]; expires_at: string | null } | PersonalKey) => {
      // Only component state holds the one-time secret; never put it in the query/mutation cache.
      const result = 'id' in input ? await rotateKey(input.id, session!.csrf_token) : await createKey(input, session!.csrf_token)
      setDelivery(result)
      setSaved(false)
      setCreating(false)
    },
    onSuccess: refresh,
  })
  const update = useMutation({
    mutationFn: ({ id, method, data }: { id: string; method: 'patch' | 'delete' | 'post'; data?: unknown }) => writeCatalog(method, `/keys/${id}`, data, session!.csrf_token),
    onSuccess: () => { setAction(null); void refresh() },
  })
  const retirement = useMutation({
    mutationFn: (replacementID: string) => writeCatalog('post', `/keys/${retiring!.id}/complete-rotation`, { replacement_key_id: replacementID }, session!.csrf_token),
    onSuccess: () => { setRetiring(null); setNotice('轮换已完成，旧 Key 已撤销。'); void refresh() },
  })
  const finish = useMutation({
    mutationFn: (confirm: boolean) => writeCatalog(confirm ? 'post' : 'delete', `/keys/${delivery!.key.id}${confirm ? '/confirm' : ''}`, confirm ? {} : undefined, session!.csrf_token),
    onSuccess: (_, confirmed) => { if (confirmed && delivery?.key.replaces_key_id) setNotice('替代 Key 已确认交付，旧 Key 仍保留。请切换应用并完成一次真实调用，再单独完成轮换。'); setDelivery(null); setSaved(false); void refresh() },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (create.isPending) return
    const form = new FormData(event.currentTarget)
    const expiration = String(form.get('expires_at') || '')
    create.mutate({ name: String(form.get('name')).trim(), model_ids: form.getAll('models').map(String), expires_at: expiration ? new Date(expiration).toISOString() : null })
  }
  return <Page title="我的 API Key" description="Key 仅用于你的个人调用。密钥只展示一次，确认保存后才启用；模型范围不会超出你的有效授权。">
    <p className="rounded-lg border bg-muted/30 px-4 py-3 text-sm">完整 API Key 仅在创建后展示一次，请妥善保管。确认已保存后才启用。</p>
    {notice && <p role="status" className="text-sm">{notice}</p>}
    <ErrorNotice error={!creating && !delivery ? create.error || update.error : null} />
    <QueryState pending={keys.isPending} error={keys.error} retry={() => void keys.refetch()} empty={keys.data?.length === 0} />
    <div className="flex flex-wrap items-center justify-between gap-4"><div className="flex items-center gap-2" aria-label="Key 状态筛选">{[{ id: 'active', label: '正常' }, { id: 'all', label: '全部' }, { id: 'disabled', label: '已停用' }].map((item) => <Button key={item.id} size="sm" variant={statusFilter === item.id ? 'secondary' : 'ghost'} aria-pressed={statusFilter === item.id} onClick={() => setStatusFilter(item.id)}>{item.label}</Button>)}</div><Button onClick={() => { create.reset(); setCreating(true) }}><Plus className="size-4" />创建 Key</Button></div>
    <Table aria-label="API Key 列表"><thead><tr><th>名称</th><th>API Key</th><th>交付方式</th><th>模型范围</th><th>状态</th><th>有效期</th><th>操作</th></tr></thead><tbody>{keys.data?.filter((key) => statusFilter === 'all' || key.status === statusFilter).map((key) => <tr key={key.id}><td>{key.name}{key.replaces_key_id && <p className="mt-1 text-xs text-muted-foreground">替代 {keys.data?.find((source) => source.id === key.replaces_key_id)?.name ?? key.replaces_key_id} · {keys.data?.find((source) => source.id === key.replaces_key_id)?.status === 'revoked' ? '旧 Key 已撤销' : '旧 Key 尚未撤销'}</p>}</td><td className="font-mono">{key.prefix}…</td><td><Badge variant="outline">浏览器一次性显示</Badge></td><td><Button variant="ghost" size="sm" onClick={() => setViewing(key)}>{key.model_ids.length} 个模型</Button></td><td><Badge variant="outline">{statuses[key.status]}</Badge></td><td className="whitespace-nowrap">{key.expires_at ? new Date(key.expires_at).toLocaleString() : '无固定期限'}</td><td><div className="flex gap-1">{(key.status === 'active' || key.status === 'disabled') && <><Button size="sm" variant="ghost" disabled={update.isPending} onClick={() => { update.reset(); setAction({ kind: 'rename', key }) }}>重命名</Button><Button size="sm" variant="ghost" disabled={update.isPending} onClick={() => update.mutate({ id: key.id, method: 'patch', data: { enabled: key.status !== 'active' } })}>{key.status === 'active' ? '停用' : '启用'}</Button><Button size="sm" variant="ghost" disabled={create.isPending} onClick={() => { create.reset(); finish.reset(); create.mutate(key) }}>轮换</Button>{keys.data?.some((candidate) => candidate.replaces_key_id === key.id && candidate.status === 'active') && <Button size="sm" variant="ghost" onClick={() => { retirement.reset(); setRetiring(key) }}>完成轮换</Button>}</>}{key.status !== 'revoked' && <Button size="sm" variant="ghost" onClick={() => { update.reset(); setAction({ kind: 'revoke', key }) }}>撤销</Button>}</div></td></tr>)}</tbody></Table>
    <Drawer open={!!viewing} onOpenChange={(open) => { if (!open) setViewing(null) }} title={`${viewing?.name ?? ''} 模型范围`} width={720}><Table><thead><tr><th>模型</th><th>协议类型</th><th>状态</th></tr></thead><tbody>{viewing?.model_ids.map((id) => { const model = models.data?.find((m) => m.id === id); return <tr key={id}><td>{model?.name ?? id}</td><td>OpenAI Chat</td><td>{model ? '已授权' : '授权已失效'}</td></tr> })}</tbody></Table></Drawer>
    <Dialog width={800} open={creating} onOpenChange={setCreating} busy={create.isPending} title="创建个人 Key" description="选择允许调用的模型。创建后有 10 分钟确认交付，逾期将无法启用。"><form onSubmit={submit} className="space-y-5"><fieldset disabled={create.isPending} className="space-y-5"><FormField label="名称"><Input name="name" required maxLength={100} /></FormField><fieldset className="space-y-2"><legend className="mb-2 text-sm font-medium">授权模型</legend><QueryState pending={models.isPending} error={models.error} retry={() => void models.refetch()} empty={models.data?.length === 0} />{models.data?.filter((m) => m.status === 'active').map((m) => <label key={m.id} className="flex items-center gap-2 text-sm"><input type="checkbox" name="models" value={m.id} />{m.name}</label>)}</fieldset><FormField label="到期时间（可选）"><Input name="expires_at" type="datetime-local" /></FormField></fieldset><ErrorNotice error={create.error} /><SaveButton pending={create.isPending} disabled={!models.data?.length}>创建并查看密钥</SaveButton></form></Dialog>
    <Dialog width={560} open={!!delivery} onOpenChange={(open) => { if (!open && !finish.isPending) finish.mutate(false) }} busy={finish.isPending} title="保存你的密钥" description="此密钥仅展示一次。确认已保存后才可使用；关闭或取消会撤销这次创建的 Key。轮换时旧 Key 继续保留，需在新 Key 真实调用成功后单独完成轮换。">
      {delivery && <div className="space-y-5"><code className="block break-all rounded-md border bg-muted p-4 text-sm" aria-label="一次性密钥">{delivery.secret}</code><p className="text-xs text-muted-foreground">请存入你的密钥管理工具，不要发送给他人。</p><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />我已安全保存密钥</label>{delivery.key.replaces_key_id && <p className="text-sm text-muted-foreground">确认交付不代表调用验证通过。若原 Key 已停用，替代 Key 也会保持停用，需单独启用后验证。</p>}<ErrorNotice error={finish.error} /><div className="flex gap-2"><Button disabled={!saved || finish.isPending} onClick={() => finish.mutate(true)}>{delivery.key.replaces_key_id ? '确认交付' : '确认并启用'}</Button><Button variant="outline" disabled={finish.isPending} onClick={() => finish.mutate(false)}>取消并撤销</Button></div></div>}
    </Dialog>
    <Dialog open={!!retiring} onOpenChange={(open) => { if (!open) setRetiring(null) }} busy={retirement.isPending} title="完成 Key 轮换" description="请先将应用切换到替代 Key，并用它完成一次真实模型调用。服务器确认成功调用已记录后才会撤销旧 Key；确认保存密钥不等于调用验证。"><form className="space-y-5" aria-label="完成 Key 轮换" onSubmit={(event) => { event.preventDefault(); if (!retiring || retirement.isPending) return; retirement.mutate(String(new FormData(event.currentTarget).get('replacement'))) }}><FormField label="替代 Key"><select name="replacement" required className="h-9 w-full rounded-md border bg-background px-3 text-sm" disabled={retirement.isPending}>{keys.data?.filter((key) => key.replaces_key_id === retiring?.id && key.status === 'active').map((key) => <option key={key.id} value={key.id}>{key.name} · {key.prefix}…</option>)}</select></FormField><p className="text-sm text-muted-foreground">旧 Key：{retiring?.name} · {retiring?.prefix}…。成功记录尚未落库或验证不满足时，旧 Key 保持原状态，可稍后重试。紧急泄露请直接使用撤销操作。</p><ErrorNotice error={retirement.error} /><SaveButton pending={retirement.isPending}>验证调用并撤销旧 Key</SaveButton></form></Dialog>
    <Dialog open={!!action} onOpenChange={(open) => { if (!open) setAction(null) }} busy={update.isPending} title={action?.kind === 'rename' ? '重命名 Key' : '撤销 Key'} description={action?.kind === 'rename' ? '名称仅用于识别，不改变密钥和模型权限。' : '撤销后立即拒绝新请求，无法恢复。'}><form className="space-y-5" onSubmit={(e) => { e.preventDefault(); if (!action || update.isPending) return; update.mutate({ id: action.key.id, method: action.kind === 'rename' ? 'patch' : 'delete', data: action.kind === 'rename' ? { name: String(new FormData(e.currentTarget).get('name')).trim() } : undefined }) }}>{action?.kind === 'rename' && <FormField label="名称"><Input name="name" defaultValue={action.key.name} required maxLength={100} /></FormField>}<ErrorNotice error={update.error} /><SaveButton pending={update.isPending}>{action?.kind === 'rename' ? '保存' : '确认撤销'}</SaveButton></form></Dialog>
  </Page>
}
