import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { listAdminModels, listGrantees, listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { AdminOnly, Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import type { Model } from '@/types/catalog'

type Action = { kind: 'create' } | { kind: 'rename' | 'binding' | 'weights' | 'grants'; model: Model }
export default function AdminModelsPage() { return <AdminOnly><AdminModels /></AdminOnly> }
function AdminModels() {
  const { data: session } = useSession()
  const cache = useQueryClient()
  const models = useQuery({ queryKey: ['admin', 'models'], queryFn: listAdminModels })
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const grantees = useQuery({ queryKey: ['admin', 'model-grantees'], queryFn: listGrantees })
  const [action, setAction] = useState<Action | null>(null)
  const mutation = useMutation({
    mutationFn: ({ path, data, method = 'post' }: { path: string; data: unknown; method?: 'post' | 'put' }) => writeCatalog(method, path, data, session!.csrf_token),
    onSuccess: () => { setAction(null); void cache.invalidateQueries({ queryKey: ['admin', 'models'] }); void cache.invalidateQueries({ queryKey: ['models'] }) },
  })
  function open(next: Action) { mutation.reset(); setAction(next) }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!action || mutation.isPending) return
    const form = new FormData(event.currentTarget)
    const name = String(form.get('name') ?? '').trim()
    const provider_model_id = String(form.get('provider_model_id') ?? '')
    if (action.kind === 'create') mutation.mutate({ path: '/admin/models', data: { name, provider_model_id } })
    else {
      const path = `/admin/models/${action.model.id}`
      if (action.kind === 'binding') mutation.mutate({ path: `${path}/bindings`, data: { provider_model_id } })
      if (action.kind === 'rename') { const expiration = String(form.get('alias_expires_at') ?? ''); mutation.mutate({ path: `${path}/rename`, data: { name, ...(expiration ? { alias_expires_at: new Date(expiration).toISOString() } : {}) } }) }
      if (action.kind === 'weights') mutation.mutate({ path: `${path}/weights`, method: 'put', data: { weights: action.model.bindings.map((binding) => ({ binding_id: binding.id, weight: Number(form.get(binding.id)) })) } })
      if (action.kind === 'grants') mutation.mutate({ path: `${path}/grants`, method: 'put', data: { user_ids: form.getAll('user_ids').map(String) } })
    }
  }
  const titles = { create: '添加模型', rename: '修改模型名称', binding: '添加供应关系', weights: '调整供应权重', grants: '模型授权' }
  const upstreamModels = providers.data?.flatMap((provider) => provider.connections.flatMap((connection) => connection.provider_models.map((model) => ({ id: model.id, label: `${provider.name} / ${connection.name} / ${model.upstream_name}` })))) ?? []
  return <Page title="模型管理" description="使用稳定的模型身份管理名称、供应关系和用户授权。新增供应关系权重为 0，只有发布有效权重后才接收流量。" action={<Button onClick={() => open({ kind: 'create' })}><Plus className="size-4" aria-hidden="true" />添加模型</Button>}>
    <QueryState pending={models.isPending} error={models.error} retry={() => void models.refetch()} empty={models.data?.length === 0} />
    {models.data?.map((model) => <article key={model.id} className="space-y-4 rounded-lg border bg-card p-5"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="font-semibold">{model.name} <Badge variant="outline">{model.status === 'active' ? '正常' : model.status === 'disabled' ? '已停用' : '已归档'}</Badge></h2><p className="mt-2 text-xs text-muted-foreground">已授权 {model.granted_user_ids.length} 位用户 · {model.bindings.length} 个供应关系</p></div><div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" onClick={() => open({ kind: 'rename', model })}>修改名称</Button><Button size="sm" variant="outline" onClick={() => open({ kind: 'binding', model })}>添加供应关系</Button><Button size="sm" variant="outline" onClick={() => open({ kind: 'weights', model })}>调整权重</Button><Button size="sm" variant="outline" onClick={() => open({ kind: 'grants', model })}>授权</Button></div></div><ul className="divide-y rounded-md border">{model.bindings.map((binding) => <li key={binding.id} className="flex flex-wrap justify-between gap-2 px-3 py-2 text-sm"><span>{binding.upstream_name} <span className="text-muted-foreground">{providers.data?.find((p) => p.id === binding.provider_id)?.name}</span></span><span>{binding.weight}% · {binding.ready ? '接入就绪' : '接入未就绪'}</span></li>)}</ul>{model.names.some((name) => !name.is_current) && <p className="text-xs leading-6 text-muted-foreground">历史名称：{model.names.filter((name) => !name.is_current).map((name) => `${name.name}（${name.expires_at ? new Date(name.expires_at).toLocaleString() : '不再可用'}）`).join('、')}</p>}</article>)}
    <Dialog open={!!action} onOpenChange={(open) => { if (!open) setAction(null) }} busy={mutation.isPending} title={action ? titles[action.kind] : ''} description={action?.kind === 'grants' ? '仅被选中的用户获得该模型授权。管理员也需要明确授权；取消授权会影响其已有 Key 的调用。' : action?.kind === 'weights' ? '同一协议的权重合计必须为 100。正权重供应关系需要已启用且验证通过的凭证。' : action?.kind === 'rename' ? '模型 ID 和已有授权保持不变。历史名称不能再次使用；不设置兼容期限时，旧名称立即失效。' : '选择已登记的上游模型。新模型将明确授权给创建它的管理员。'}>
      <form onSubmit={submit} className="space-y-5"><fieldset disabled={mutation.isPending} className="space-y-5">{(action?.kind === 'create' || action?.kind === 'rename') && <FormField label="模型名称"><Input name="name" required maxLength={200} defaultValue={action.kind === 'rename' ? action.model.name : ''} /></FormField>}{action?.kind === 'rename' && <FormField label="旧名称兼容截止时间（可选）"><Input name="alias_expires_at" type="datetime-local" /></FormField>}{(action?.kind === 'create' || action?.kind === 'binding') && <><QueryState pending={providers.isPending} error={providers.error} retry={() => void providers.refetch()} empty={upstreamModels.length === 0} /><FormField label="上游模型"><select name="provider_model_id" required className="h-11 w-full rounded-md border bg-background px-3 text-sm"><option value="">选择供应商 / 接入 / 模型</option>{upstreamModels.filter((upstream) => action.kind === 'create' || !action.model.bindings.some((binding) => binding.provider_model_id === upstream.id)).map((model) => <option key={model.id} value={model.id}>{model.label}</option>)}</select></FormField></>}{action?.kind === 'weights' && action.model.bindings.map((binding) => <FormField key={binding.id} label={`${binding.upstream_name}（${binding.ready ? '就绪' : '未就绪'}）`}><Input name={binding.id} type="number" min={0} max={100} step={1} required defaultValue={binding.weight} /></FormField>)}{action?.kind === 'grants' && <><QueryState pending={grantees.isPending} error={grantees.error} retry={() => void grantees.refetch()} empty={grantees.data?.length === 0} />{grantees.data?.map((user) => <label key={user.id} className="flex items-center gap-2 text-sm"><input type="checkbox" name="user_ids" value={user.id} defaultChecked={action.model.granted_user_ids.includes(user.id)} />{user.name} <span className="text-muted-foreground">{user.email}</span></label>)}</>}</fieldset><ErrorNotice error={mutation.error} /><SaveButton pending={mutation.isPending || (action?.kind === 'grants' && !grantees.isSuccess)}>保存</SaveButton></form>
    </Dialog>
  </Page>
}
