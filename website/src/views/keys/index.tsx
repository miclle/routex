import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, KeyRound } from 'lucide-react'
import { createKey, listKeys, listModels, rotateKey, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import type { KeyDelivery, PersonalKey } from '@/types/catalog'

const statuses = { pending: '待确认交付', active: '已启用', disabled: '已停用', revoked: '已撤销' }
export default function KeysPage() {
  const { data: session } = useSession()
  const cache = useQueryClient()
  const keys = useQuery({ queryKey: ['keys'], queryFn: listKeys })
  const models = useQuery({ queryKey: ['models'], queryFn: listModels })
  const [creating, setCreating] = useState(false)
  const [delivery, setDelivery] = useState<KeyDelivery | null>(null)
  const [saved, setSaved] = useState(false)
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
  const finish = useMutation({
    mutationFn: (confirm: boolean) => writeCatalog(confirm ? 'post' : 'delete', `/keys/${delivery!.key.id}${confirm ? '/confirm' : ''}`, confirm ? {} : undefined, session!.csrf_token),
    onSuccess: () => { setDelivery(null); setSaved(false); void refresh() },
  })
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (create.isPending) return
    const form = new FormData(event.currentTarget)
    const expiration = String(form.get('expires_at') || '')
    create.mutate({ name: String(form.get('name')).trim(), model_ids: form.getAll('models').map(String), expires_at: expiration ? new Date(expiration).toISOString() : null })
  }
  return <Page title="我的 API Key" description="Key 仅用于你的个人调用。密钥只展示一次，确认保存后才启用；模型范围不会超出你的有效授权。" action={<Button onClick={() => { create.reset(); setCreating(true) }}><Plus className="size-4" aria-hidden="true" />创建 Key</Button>}>
    <ErrorNotice error={!creating && !delivery ? create.error || update.error : null} />
    <QueryState pending={keys.isPending} error={keys.error} retry={() => void keys.refetch()} empty={keys.data?.length === 0} />
    <div className="space-y-3">{keys.data?.map((key) => <article key={key.id} className="space-y-4 rounded-lg border bg-card p-5"><div className="flex flex-wrap items-start justify-between gap-4"><div><h2 className="flex items-center gap-2 font-medium"><KeyRound className="size-4" aria-hidden="true" />{key.name}<Badge variant="outline">{statuses[key.status]}</Badge></h2><p className="mt-2 font-mono text-sm text-muted-foreground">{key.prefix}…</p></div><div className="flex flex-wrap gap-2">{(key.status === 'active' || key.status === 'disabled') && <><Button size="sm" variant="outline" disabled={update.isPending} onClick={() => { update.reset(); setAction({ kind: 'rename', key }) }}>重命名</Button><Button size="sm" variant="outline" disabled={update.isPending} onClick={() => update.mutate({ id: key.id, method: 'patch', data: { enabled: key.status !== 'active' } })}>{key.status === 'active' ? '停用' : '启用'}</Button><Button size="sm" variant="outline" disabled={create.isPending} onClick={() => { create.reset(); finish.reset(); create.mutate(key) }}>轮换</Button></>}{key.status !== 'revoked' && <Button size="sm" variant="outline" onClick={() => { update.reset(); setAction({ kind: 'revoke', key }) }}>撤销</Button>}</div></div><p className="text-xs leading-6 text-muted-foreground">模型：{key.model_ids.map((id) => models.data?.find((m) => m.id === id)?.name ?? id).join('、')} · 到期：{key.expires_at ? new Date(key.expires_at).toLocaleString() : '无固定期限'}{key.status === 'pending' && ' · 未确认交付的 Key 不可调用'}</p></article>)}</div>
    <Dialog open={creating} onOpenChange={setCreating} busy={create.isPending} title="创建个人 Key" description="选择允许调用的模型。创建后有 10 分钟确认交付，逾期将无法启用。"><form onSubmit={submit} className="space-y-5"><fieldset disabled={create.isPending} className="space-y-5"><FormField label="名称"><Input name="name" required maxLength={100} /></FormField><fieldset className="space-y-2"><legend className="mb-2 text-sm font-medium">授权模型</legend><QueryState pending={models.isPending} error={models.error} retry={() => void models.refetch()} empty={models.data?.length === 0} />{models.data?.filter((m) => m.status === 'active').map((m) => <label key={m.id} className="flex items-center gap-2 text-sm"><input type="checkbox" name="models" value={m.id} />{m.name}</label>)}</fieldset><FormField label="到期时间（可选）"><Input name="expires_at" type="datetime-local" /></FormField></fieldset><ErrorNotice error={create.error} /><SaveButton pending={create.isPending || !models.data?.length}>创建并查看密钥</SaveButton></form></Dialog>
    <Dialog open={!!delivery} onOpenChange={(open) => { if (!open && !finish.isPending) finish.mutate(false) }} busy={finish.isPending} title="保存你的密钥" description="此密钥仅展示一次。确认已保存后才可使用；关闭或取消会撤销这次创建的 Key。轮换时旧 Key 在确认成功后才撤销。">
      {delivery && <div className="space-y-5"><code className="block break-all rounded-md border bg-muted p-4 text-sm" aria-label="一次性密钥">{delivery.secret}</code><p className="text-xs text-muted-foreground">请存入你的密钥管理工具，不要发送给他人。</p><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />我已安全保存密钥</label><ErrorNotice error={finish.error} /><div className="flex gap-2"><Button disabled={!saved || finish.isPending} onClick={() => finish.mutate(true)}>确认并启用</Button><Button variant="outline" disabled={finish.isPending} onClick={() => finish.mutate(false)}>取消并撤销</Button></div></div>}
    </Dialog>
    <Dialog open={!!action} onOpenChange={(open) => { if (!open) setAction(null) }} busy={update.isPending} title={action?.kind === 'rename' ? '重命名 Key' : '撤销 Key'} description={action?.kind === 'rename' ? '名称仅用于识别，不改变密钥和模型权限。' : '撤销后立即拒绝新请求，无法恢复。'}><form className="space-y-5" onSubmit={(e) => { e.preventDefault(); if (!action || update.isPending) return; update.mutate({ id: action.key.id, method: action.kind === 'rename' ? 'patch' : 'delete', data: action.kind === 'rename' ? { name: String(new FormData(e.currentTarget).get('name')).trim() } : undefined }) }}>{action?.kind === 'rename' && <FormField label="名称"><Input name="name" defaultValue={action.key.name} required maxLength={100} /></FormField>}<ErrorNotice error={update.error} /><SaveButton pending={update.isPending}>{action?.kind === 'rename' ? '保存' : '确认撤销'}</SaveButton></form></Dialog>
  </Page>
}
