import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, PlugZap } from 'lucide-react'
import { listProviders, writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { AdminOnly, Page, QueryState, ErrorNotice, FormField, SaveButton } from '@/components/app/CatalogUI'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'

type Action = { kind: 'provider' | 'connection' | 'credential' | 'model'; id?: string }
export default function ProvidersPage() { return <AdminOnly><Providers /></AdminOnly> }
function Providers() {
  const { data: session } = useSession()
  const cache = useQueryClient()
  const providers = useQuery({ queryKey: ['admin', 'providers'], queryFn: listProviders })
  const [action, setAction] = useState<Action | null>(null)
  const [notice, setNotice] = useState('')
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
  return <Page title="供应商" description="管理供应商、接入与凭证。新增凭证默认停用，真实验证通过后可单独启用。" action={<Button onClick={() => open({ kind: 'provider' })}><Plus className="size-4" aria-hidden="true" />添加供应商</Button>}>
    {notice && <p role="status" className="rounded-md border bg-background p-3 text-sm">{notice}</p>}<ErrorNotice error={!action ? mutation.error : null} /><QueryState pending={providers.isPending} error={providers.error} retry={() => void providers.refetch()} empty={providers.data?.length === 0} />
    {providers.data?.map((provider) => <article key={provider.id} className="overflow-hidden rounded-lg border bg-card"><header className="flex items-center justify-between gap-4 border-b p-5"><h2 className="flex items-center gap-2 font-semibold"><PlugZap className="size-4" aria-hidden="true" />{provider.name}</h2><Button size="sm" variant="outline" onClick={() => open({ kind: 'connection', id: provider.id })}>添加接入</Button></header><div className="divide-y">{provider.connections.map((connection) => <section key={connection.id} className="space-y-5 p-5"><div className="space-y-1"><h3 className="font-medium">{connection.name} <Badge variant="outline">OpenAI Chat</Badge></h3><p className="break-all text-sm text-muted-foreground">{connection.base_url}</p></div><div className="space-y-3"><div className="flex items-center justify-between"><h4 className="text-sm font-medium">凭证</h4><Button size="sm" variant="ghost" onClick={() => open({ kind: 'credential', id: connection.id })}>添加凭证</Button></div>{connection.credentials.length === 0 && <p className="text-sm text-muted-foreground">暂无凭证。</p>}{connection.credentials.map((credential) => <div key={credential.id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border px-3 py-2"><div className="space-x-2 text-sm"><span>{credential.name}</span><Badge variant="outline">{credential.verification_status === 'verified' ? '已验证' : credential.verification_status === 'failed' ? '验证失败' : '待验证'}</Badge><span className="text-xs text-muted-foreground">{credential.enabled ? '已启用' : '已停用'} · 优先级 {credential.priority}</span></div><div className="flex gap-2"><Button variant="outline" size="sm" disabled={mutation.isPending} onClick={() => { setNotice(''); mutation.mutate({ path: `/admin/credentials/${credential.id}/verify`, data: {} }) }}>验证</Button><Button variant="outline" size="sm" disabled={mutation.isPending || (!credential.enabled && credential.verification_status !== 'verified')} onClick={() => mutation.mutate({ path: `/admin/credentials/${credential.id}`, method: 'patch', data: { enabled: !credential.enabled } })}>{credential.enabled ? '停用' : '启用'}</Button></div></div>)}</div><div className="space-y-3"><div className="flex items-center justify-between"><h4 className="text-sm font-medium">上游模型</h4><Button size="sm" variant="ghost" onClick={() => open({ kind: 'model', id: connection.id })}>手工添加</Button></div><div className="flex flex-wrap gap-2">{connection.provider_models.map((model) => <Badge variant="outline" key={model.id}>{model.upstream_name}</Badge>)}{connection.provider_models.length === 0 && <p className="text-sm text-muted-foreground">验证凭证以发现模型，或手工添加模型名称。</p>}</div></div></section>)}</div></article>)}
    <Dialog open={!!action} onOpenChange={(open) => { if (!open) close() }} busy={mutation.isPending} title={action ? titles[action.kind] : ''} description="接入使用 OpenAI Chat Completions 协议。凭证只作为写入输入，不会返回到列表。"><form className="space-y-5" onSubmit={submit}><fieldset disabled={mutation.isPending} className="space-y-5">{action?.kind !== 'model' && <FormField label={action?.kind === 'provider' ? '供应商名称' : '名称'}><Input name="name" required maxLength={100} /></FormField>}{action?.kind === 'provider' && <FormField label="接入名称"><Input name="connection_name" required maxLength={100} /></FormField>}{(action?.kind === 'provider' || action?.kind === 'connection') && <><FormField label="Base URL"><Input name="base_url" type="url" required placeholder="https://api.example.com/v1" /></FormField><FormField label="凭证名称"><Input name="credential_name" required maxLength={100} /></FormField></>}{action?.kind !== 'model' && <FormField label="上游 API Key"><Input name="secret" type="password" autoComplete="off" required /></FormField>}{action?.kind === 'credential' && <FormField label="优先级"><Input name="priority" type="number" min={0} step={1} defaultValue={0} required /></FormField>}{action?.kind === 'model' && <FormField label="上游模型名称"><Input name="upstream_name" required maxLength={200} /></FormField>}</fieldset><ErrorNotice error={mutation.error} /><SaveButton pending={mutation.isPending}>保存</SaveButton></form></Dialog>
  </Page>
}
