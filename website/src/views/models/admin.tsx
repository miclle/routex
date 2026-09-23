import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { listAdminModels, listGrantees, listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { AdminOnly, Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button, buttonVariants } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Link, useParams } from 'react-router'
import { Table } from '@/components/ui/table'
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
  const [search, setSearch] = useState('')
  const { modelId } = useParams()
  const selected = models.data?.find((model) => model.id === modelId)
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
  return <Page title="模型管理" description="使用稳定的模型身份管理名称、供应关系和用户授权。新增供应关系权重为 0，只有发布有效权重后才接收流量。">
    <QueryState pending={models.isPending} error={models.error} retry={() => void models.refetch()} empty={models.data?.length === 0} />
    {!modelId && <><div className="flex items-center justify-between gap-4"><Input aria-label="搜索模型" placeholder="搜索对外模型名、协议或供应商" value={search} onValueChange={setSearch} className="max-w-[420px]" /><Link className={buttonVariants()} to="/admin/models/new"><Plus className="size-4" aria-hidden="true" />添加模型</Link></div><div className="rounded-lg border"><Table aria-label="模型列表"><thead><tr><th>对外模型名</th><th>协议类型</th><th>供应商</th><th>状态</th><th>使用成员</th><th>操作</th></tr></thead><tbody>{models.data?.filter((model) => `${model.name} ${model.bindings.map((b) => providers.data?.find((p) => p.id === b.provider_id)?.name).join(' ')}`.toLowerCase().includes(search.toLowerCase())).map((model) => <tr key={model.id}><td><Link className="text-primary" to={`/admin/models/${model.id}`}>{model.name}</Link></td><td><Badge variant="outline">OpenAI Chat</Badge></td><td>{[...new Set(model.bindings.map((b) => providers.data?.find((p) => p.id === b.provider_id)?.name ?? b.provider_id))].join('、')}</td><td>{model.status === 'active' ? model.bindings.some((b) => b.ready && b.weight > 0) ? '正常' : '待配置' : '已停用'}</td><td>{model.granted_user_ids.length}</td><td><Link to={`/admin/models/${model.id}`}>详情</Link></td></tr>)}</tbody></Table></div></>}
    {selected && <><section aria-label="模型信息" className="rounded-lg border"><header className="flex items-center justify-between gap-3 border-b px-6 py-4"><h2 className="font-semibold">{selected.name} <Badge variant="outline">{selected.status === 'active' ? selected.bindings.some((binding) => binding.ready && binding.weight > 0) ? '正常' : '待配置' : '已停用'}</Badge></h2><Button variant="outline" onClick={() => open({ kind: 'rename', model: selected })}>修改名称</Button></header><div className="space-y-4 p-6"><dl className="grid grid-cols-2 gap-4 text-sm lg:grid-cols-4"><div><dt className="text-muted-foreground">模型 ID</dt><dd className="break-all">{selected.id}</dd></div><div><dt className="text-muted-foreground">协议类型</dt><dd>OpenAI Chat</dd></div><div><dt className="text-muted-foreground">供应关系</dt><dd>{selected.bindings.length} 个</dd></div><div><dt className="text-muted-foreground">使用成员</dt><dd>{selected.granted_user_ids.length}</dd></div></dl>{selected.names.some((name) => !name.is_current) && <p className="text-xs leading-6 text-muted-foreground">历史名称：{selected.names.filter((name) => !name.is_current).map((name) => `${name.name}（${name.expires_at ? new Date(name.expires_at).toLocaleString() : '不再可用'}）`).join('、')}</p>}</div></section><form key={JSON.stringify(selected.bindings)} aria-label="供应商路由权重" className="space-y-4" onSubmit={(event) => { event.preventDefault(); if (mutation.isPending) return; const values = new FormData(event.currentTarget); mutation.mutate({ path: `/admin/models/${selected.id}/weights`, method: 'put', data: { weights: selected.bindings.map((binding) => ({ binding_id: binding.id, weight: Number(values.get(binding.id)) })) } }) }}><section className="rounded-lg border"><h3 className="border-b px-6 py-4 font-semibold">供应商路由</h3><Table><thead><tr><th>供应商</th><th>供应商模型</th><th>协议</th><th>状态</th><th>路由权重</th></tr></thead><tbody>{selected.bindings.map((binding) => <tr key={binding.id}><td>{providers.data?.find((p) => p.id === binding.provider_id)?.name ?? binding.provider_id}</td><td>{binding.upstream_name}</td><td>OpenAI Chat</td><td>{binding.ready ? '接入就绪' : '接入未就绪'}</td><td><div className="flex items-center gap-2"><Input aria-label={`${binding.upstream_name} 路由权重`} name={binding.id} type="number" min={0} max={100} step={1} required defaultValue={binding.weight} disabled={mutation.isPending} className="w-24" />%</div></td></tr>)}</tbody></Table><div className="flex items-center justify-between gap-4 p-4"><Button variant="outline" onClick={() => open({ kind: 'binding', model: selected })}>添加供应关系</Button><p className="text-sm text-muted-foreground">同一协议权重合计必须为 100%</p></div></section><ErrorNotice error={!action ? mutation.error : null} /><div className="flex justify-end gap-3"><Button variant="outline" onClick={() => open({ kind: 'grants', model: selected })}>授权</Button><SaveButton pending={mutation.isPending}>保存路由权重</SaveButton></div></form></>}
    <Dialog open={!!action} onOpenChange={(open) => { if (!open) setAction(null) }} busy={mutation.isPending} title={action ? titles[action.kind] : ''} description={action?.kind === 'grants' ? '仅被选中的用户获得该模型授权。管理员也需要明确授权；取消授权会影响其已有 Key 的调用。' : action?.kind === 'weights' ? '同一协议的权重合计必须为 100。正权重供应关系需要已启用且验证通过的凭证。' : action?.kind === 'rename' ? '模型 ID 和已有授权保持不变。历史名称不能再次使用；不设置兼容期限时，旧名称立即失效。' : '选择已登记的上游模型。新模型将明确授权给创建它的管理员。'}>
      <form onSubmit={submit} className="space-y-5"><fieldset disabled={mutation.isPending} className="space-y-5">{(action?.kind === 'create' || action?.kind === 'rename') && <FormField label="模型名称"><Input name="name" required maxLength={200} defaultValue={action.kind === 'rename' ? action.model.name : ''} /></FormField>}{action?.kind === 'rename' && <FormField label="旧名称兼容截止时间（可选）"><Input name="alias_expires_at" type="datetime-local" /></FormField>}{(action?.kind === 'create' || action?.kind === 'binding') && <><QueryState pending={providers.isPending} error={providers.error} retry={() => void providers.refetch()} empty={upstreamModels.length === 0} /><FormField label="上游模型"><select name="provider_model_id" required className="h-11 w-full rounded-md border bg-background px-3 text-sm"><option value="">选择供应商 / 接入 / 模型</option>{upstreamModels.filter((upstream) => action.kind === 'create' || !action.model.bindings.some((binding) => binding.provider_model_id === upstream.id)).map((model) => <option key={model.id} value={model.id}>{model.label}</option>)}</select></FormField></>}{action?.kind === 'weights' && action.model.bindings.map((binding) => <FormField key={binding.id} label={`${binding.upstream_name}（${binding.ready ? '就绪' : '未就绪'}）`}><Input name={binding.id} type="number" min={0} max={100} step={1} required defaultValue={binding.weight} /></FormField>)}{action?.kind === 'grants' && <><QueryState pending={grantees.isPending} error={grantees.error} retry={() => void grantees.refetch()} empty={grantees.data?.length === 0} />{grantees.data?.map((user) => <label key={user.id} className="flex items-center gap-2 text-sm"><input type="checkbox" name="user_ids" value={user.id} defaultChecked={action.model.granted_user_ids.includes(user.id)} />{user.name} <span className="text-muted-foreground">{user.email}</span></label>)}</>}</fieldset><ErrorNotice error={mutation.error} /><SaveButton pending={mutation.isPending} disabled={action?.kind === 'grants' && !grantees.isSuccess}>保存</SaveButton></form>
    </Dialog>
  </Page>
}
