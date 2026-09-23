import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { AdminOnly, Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Link, useParams, useSearchParams } from 'react-router'
import { Table } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'

type Action = { kind: 'provider' | 'connection' | 'credential' | 'model'; id?: string }
export default function ProvidersPage() { return <AdminOnly><Providers /></AdminOnly> }
function Providers() {
  const { data: session } = useSession()
  const cache = useQueryClient()
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const [action, setAction] = useState<Action | null>(null)
  const [notice, setNotice] = useState('')
  const { providerId } = useParams()
  const [params, setParams] = useSearchParams()
  const selected = providers.data?.find((provider) => provider.id === providerId)
  const tab = params.get('tab') || 'connections'
  const mutation = useMutation({
    mutationFn: ({ path, data, method = 'post' }: { path: string; data: unknown; method?: 'post' | 'patch' }) => writeCatalog<{ verified?: boolean; discovered_models?: number; message?: string }>(method, path, data, session!.csrf_token),
    onSuccess: (result) => {
      if (result.verified !== undefined) setNotice(result.verified ? `验证成功，发现 ${result.discovered_models ?? 0} 个上游模型。凭证仍需单独启用。` : '验证失败，请检查接入地址和凭证。')
      else { setNotice('配置已保存。'); setAction(null); mutation.reset() }
      void cache.invalidateQueries({ queryKey: ['admin', 'providers'] })
      void cache.invalidateQueries({ queryKey: ['admin', 'models'] })
    },
    gcTime: 0,
  })
  function open(next: Action) { mutation.reset(); setNotice(''); setAction(next) }
  function close() { mutation.reset(); setAction(null) }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!action || mutation.isPending) return
    const form = new FormData(event.currentTarget)
    const value = (name: string) => String(form.get(name) ?? '').trim()
    const secret = String(form.get('secret') ?? '')
    if (action.kind === 'provider') mutation.mutate({ path: '/admin/providers', data: { name: value('name'), connection_name: value('connection_name'), base_url: value('base_url'), protocol: 'openai_chat', credential_name: value('credential_name'), secret } })
    if (action.kind === 'connection') mutation.mutate({ path: `/admin/providers/${action.id}/connections`, data: { name: value('name'), base_url: value('base_url'), protocol: 'openai_chat', credential_name: value('credential_name'), secret } })
    if (action.kind === 'credential') mutation.mutate({ path: `/admin/connections/${action.id}/credentials`, data: { name: value('name'), secret, priority: Number(value('priority')) } })
    if (action.kind === 'model') mutation.mutate({ path: `/admin/connections/${action.id}/models`, data: { upstream_name: value('upstream_name') } })
  }
  const titles = { provider: '添加供应商', connection: '添加接入', credential: '添加凭证', model: '添加上游模型' }
  return <Page title="供应商" description="管理供应商、接入与凭证。新增凭证默认停用，真实验证通过后可单独启用。">
    {!providerId && <div className="flex flex-wrap items-center justify-between gap-4"><div className="flex gap-4 text-sm"><Badge variant="outline">{providers.data?.length ?? 0} 个供应商</Badge><span>{providers.data?.flatMap((p) => p.connections.flatMap((c) => c.credentials)).filter((c) => c.enabled && c.verification_status === 'verified').length ?? 0} 个有效凭证</span><span>{providers.data?.flatMap((p) => p.connections.flatMap((c) => c.provider_models)).length ?? 0} 个模型</span></div><Button onClick={() => open({ kind: 'provider' })}><Plus className="size-4" />添加供应商</Button></div>}
    {notice && <p role="status" className="rounded-md border bg-background p-3 text-sm">{notice}</p>}<ErrorNotice error={!action ? mutation.error : null} /><QueryState pending={providers.isPending} error={providers.error} retry={() => void providers.refetch()} empty={providers.data?.length === 0} />
    {!providerId && <Table aria-label="供应商列表视图"><thead><tr><th>供应商</th><th>接入配置</th><th>协议类型</th><th>有效凭证</th><th>模型</th></tr></thead><tbody>{providers.data?.map((provider) => <tr key={provider.id}><td><Link to={`/admin/providers/${provider.id}`} className="flex items-center gap-4 text-primary"><span className="flex size-8 items-center justify-center rounded-md bg-muted text-foreground">{provider.name.slice(0, 1)}</span>{provider.name}</Link></td><td>{provider.connections.length}</td><td>OpenAI Chat</td><td>{provider.connections.flatMap((c) => c.credentials).filter((c) => c.enabled && c.verification_status === 'verified').length}</td><td>{provider.connections.flatMap((c) => c.provider_models).length}</td></tr>)}</tbody></Table>}
    {providerId && !providers.isPending && !selected && <p role="alert">供应商不存在。<Link to="/admin/providers">返回列表</Link></p>}
    {selected && <><div className="flex items-center justify-between rounded-lg border p-6"><div className="flex items-center gap-4"><span className="flex size-10 items-center justify-center rounded-md bg-muted">{selected.name.slice(0, 1)}</span><h2 className="text-2xl font-semibold">{selected.name}</h2></div><Badge variant="outline">OpenAI Chat</Badge></div><Tabs value={tab} onValueChange={(value) => setParams({ tab: String(value) }, { replace: true })}><TabsList aria-label={`${selected.name} 管理视图`}><TabsTrigger value="connections">接入配置 {selected.connections.length}</TabsTrigger><TabsTrigger value="credentials">凭证 {selected.connections.flatMap((c) => c.credentials).length}</TabsTrigger><TabsTrigger value="models">模型 {selected.connections.flatMap((c) => c.provider_models).length}</TabsTrigger></TabsList><TabsContent value="connections"><div className="mb-4 flex justify-end"><Button onClick={() => open({ kind: 'connection', id: selected.id })}>添加接入</Button></div><Table><thead><tr><th>接入名称</th><th>协议类型</th><th>Base URL</th><th>凭证</th><th>模型</th></tr></thead><tbody>{selected.connections.map((c) => <tr key={c.id}><td>{c.name}</td><td>OpenAI Chat</td><td className="break-all">{c.base_url}</td><td>{c.credentials.length}</td><td>{c.provider_models.length}</td></tr>)}</tbody></Table></TabsContent><TabsContent value="credentials"><div className="mb-4 flex flex-wrap justify-end gap-2">{selected.connections.map((c) => <Button key={c.id} onClick={() => open({ kind: 'credential', id: c.id })}>添加凭证 · {c.name}</Button>)}</div><Table><thead><tr><th>凭证名称</th><th>所属接入</th><th>业务验证</th><th>启用状态</th><th>优先级</th><th>操作</th></tr></thead><tbody>{selected.connections.flatMap((c) => c.credentials.map((credential) => <tr key={credential.id}><td>{credential.name}</td><td>{c.name}</td><td><Badge variant="outline">{credential.verification_status === 'verified' ? '已验证' : credential.verification_status === 'failed' ? '验证失败' : '待验证'}</Badge></td><td>{credential.enabled ? '已启用' : '已停用'}</td><td>{credential.priority}</td><td><div className="flex gap-2"><Button size="sm" variant="ghost" disabled={mutation.isPending} onClick={() => { setNotice(''); mutation.mutate({ path: `/admin/credentials/${credential.id}/verify`, data: {} }) }}>验证</Button><Button size="sm" variant="ghost" disabled={mutation.isPending || (!credential.enabled && credential.verification_status !== 'verified')} onClick={() => mutation.mutate({ path: `/admin/credentials/${credential.id}`, method: 'patch', data: { enabled: !credential.enabled } })}>{credential.enabled ? '停用' : '启用'}</Button></div></td></tr>))}</tbody></Table></TabsContent><TabsContent value="models"><div className="mb-4 flex flex-wrap justify-end gap-2">{selected.connections.map((c) => <Button key={c.id} onClick={() => open({ kind: 'model', id: c.id })}>手工添加 · {c.name}</Button>)}</div><Table><thead><tr><th>模型标识</th><th>所属接入</th><th>协议类型</th></tr></thead><tbody>{selected.connections.flatMap((c) => c.provider_models.map((m) => <tr key={m.id}><td>{m.upstream_name}</td><td>{c.name}</td><td>OpenAI Chat</td></tr>))}</tbody></Table></TabsContent></Tabs></>}
    <Dialog width={640} open={!!action} onOpenChange={(open) => { if (!open) close() }} busy={mutation.isPending} title={action ? titles[action.kind] : ''} description="接入使用 OpenAI Chat Completions 协议。凭证只作为写入输入，不会返回到列表。"><form className="space-y-5" onSubmit={submit}><fieldset disabled={mutation.isPending} className="space-y-5">{action?.kind !== 'model' && <FormField label={action?.kind === 'provider' ? '供应商名称' : '名称'}><Input name="name" required maxLength={100} /></FormField>}{action?.kind === 'provider' && <FormField label="接入名称"><Input name="connection_name" required maxLength={100} /></FormField>}{(action?.kind === 'provider' || action?.kind === 'connection') && <><FormField label="Base URL"><Input name="base_url" type="url" required placeholder="https://api.example.com/v1" /></FormField><FormField label="凭证名称"><Input name="credential_name" required maxLength={100} /></FormField></>}{action?.kind !== 'model' && <FormField label="上游 API Key"><Input name="secret" type="password" autoComplete="off" required /></FormField>}{action?.kind === 'credential' && <FormField label="优先级"><Input name="priority" type="number" min={0} step={1} defaultValue={0} required /></FormField>}{action?.kind === 'model' && <FormField label="上游模型名称"><Input name="upstream_name" required maxLength={200} /></FormField>}</fieldset><ErrorNotice error={mutation.error} /><SaveButton pending={mutation.isPending}>保存</SaveButton></form></Dialog>
  </Page>
}
