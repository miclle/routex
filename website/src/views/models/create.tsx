import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router'
import { listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import type { Model } from '@/types/catalog'
import { Page, QueryState, FormField, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Input } from '@/components/ui/input'
import { buttonVariants } from '@/components/ui/button'

export default function CreateModelPage() { return <PermissionGate permission="models.write"><PermissionGate permission="providers.read"><CreateModel /></PermissionGate></PermissionGate> }
function CreateModel() {
  const { data: session } = useSession()
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const cache = useQueryClient()
  const navigate = useNavigate()
  const [connectionId, setConnectionId] = useState('')
  const connection = providers.data?.flatMap((p) => p.connections).find((c) => c.id === connectionId)
  const provider = providers.data?.find((p) => p.connections.some((c) => c.id === connectionId))
  const mutation = useMutation({ mutationFn: (data: { name: string; provider_model_id: string }) => writeCatalog<Model>('post', '/admin/models', data, session!.csrf_token), onSuccess: (model) => { void cache.invalidateQueries({ queryKey: ['admin', 'models'] }); void cache.invalidateQueries({ queryKey: ['models'] }); navigate(`/admin/models/${model.id}`) } })
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); if (mutation.isPending) return; const data = new FormData(event.currentTarget); mutation.mutate({ name: String(data.get('name')).trim(), provider_model_id: String(data.get('provider_model_id')) }) }
  return <Page title="添加模型" description="选择已有供应商接入，登记对外模型名称。新增供应关系权重为 0，保存后可配置路由。"><QueryState pending={providers.isPending} error={providers.error} retry={() => void providers.refetch()} /><section className="rounded-lg border p-6"><form aria-label="添加模型" onSubmit={submit} className="space-y-6 [&_label]:grid [&_label]:grid-cols-1 sm:[&_label]:grid-cols-[180px_1fr] [&_label]:items-center [&_label]:gap-4"><fieldset disabled={mutation.isPending} className="space-y-6"><FormField label="接入配置"><span className="text-sm">已有接入</span></FormField><FormField label="供应商接入"><select className="h-10 rounded-md border bg-background px-3" value={connectionId} onChange={(event) => setConnectionId(event.target.value)} required><option value="">选择供应商接入</option>{providers.data?.flatMap((p) => p.connections.map((c) => <option key={c.id} value={c.id}>{p.name} / {c.name}</option>))}</select></FormField>{connection && <><FormField label="供应商"><Input value={provider?.name ?? ''} disabled /></FormField><FormField label="协议类型"><Input value="OpenAI Chat" disabled /></FormField><FormField label="Upstream Base URL"><Input value={connection.base_url} disabled /></FormField><FormField label="可添加模型"><select name="provider_model_id" required className="h-10 rounded-md border bg-background px-3"><option value="">选择已发现的上游模型</option>{connection.provider_models.map((model) => <option key={model.id} value={model.id}>{model.upstream_name}</option>)}</select></FormField></>}<FormField label="对外模型名称"><Input name="name" required maxLength={200} placeholder="调用时使用的模型名称" /></FormField></fieldset><ErrorNotice error={mutation.error} /><div className="flex justify-center gap-3"><Link className={buttonVariants({ variant: 'outline' })} to="/admin/models">取消</Link><SaveButton pending={mutation.isPending} disabled={!connection}>添加模型</SaveButton></div></form></section></Page>
}
