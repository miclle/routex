import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, ShieldCheck } from 'lucide-react'
import { getRoles } from '@/api/governance'
import { writeCatalog } from '@/api/catalog'
import { useSession } from '@/hooks/use-auth'
import { usePermissions } from '@/hooks/use-permissions'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Page, QueryState, FormField, ErrorNotice, SaveButton } from '@/components/app/CatalogUI'
import { Table } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import type { PlatformRole } from '@/types/governance'

const resources: Record<string, string> = { members: '成员', roles: '角色与权限', registration: '成员注册', providers: '供应商接入', models: '模型', calls: '调用记录', audit: '审计日志', system: '系统', teams: '团队', projects: '项目' }
const actions: Record<string, string> = { read: '查看', read_all: '查看全部', write: '管理', 'models.write': '分配模型' }

export function PermissionRows({ permissions }: { permissions: string[] }) { return <Table><thead><tr><th>资源</th><th>允许动作</th></tr></thead><tbody>{[...new Set(permissions.map((p) => p.split('.')[0]))].map((resource) => <tr key={resource}><td>{resources[resource] ?? resource}</td><td className="space-x-2">{permissions.filter((p) => p.startsWith(`${resource}.`)).map((permission) => <Badge key={permission} variant="outline">{actions[permission.slice(resource.length + 1)] ?? permission.slice(resource.length + 1)}</Badge>)}</td></tr>)}</tbody></Table> }
export default function RolesPage() { return <PermissionGate permission="roles.read"><Roles /></PermissionGate> }
function Roles() {
  const session = useSession()
  const access = usePermissions()
  const cache = useQueryClient()
  const roles = useQuery({ queryKey: ['admin', 'roles'], queryFn: ({ signal }) => getRoles(signal) })
  const [editor, setEditor] = useState<PlatformRole | 'new' | null>(null)
  const [viewing, setViewing] = useState<PlatformRole | null>(null)
  const [deleting, setDeleting] = useState<PlatformRole | null>(null)
  const mutation = useMutation({ mutationFn: ({ method, path, data }: { method: 'post' | 'put' | 'delete'; path: string; data?: unknown }) => writeCatalog(method, path, data, session.data!.csrf_token), onSuccess: () => { setEditor(null); setDeleting(null); void cache.invalidateQueries({ queryKey: ['admin', 'roles'] }); void cache.invalidateQueries({ queryKey: ['permissions'] }) } })
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); if (!editor || mutation.isPending) return; const form = new FormData(event.currentTarget); mutation.mutate({ method: editor === 'new' ? 'post' : 'put', path: `/admin/roles${editor === 'new' ? '' : `/${editor.id}`}`, data: { name: String(form.get('name')).trim(), permissions: form.getAll('permissions').map(String) } }) }
  return <Page title="角色与权限" description="通过角色组合职责权限。"><div className="flex flex-wrap justify-between gap-4"><p className="max-w-3xl text-sm text-muted-foreground">通过角色组合职责权限。内置角色提供权限基线，自定义角色可按资源和动作细化授权。</p>{access.isAdmin && <Button onClick={() => { mutation.reset(); setEditor('new') }}><Plus className="size-4" />创建自定义角色</Button>}</div><QueryState pending={roles.isPending} error={roles.error} retry={() => void roles.refetch()} /><Table aria-label="角色列表"><thead><tr><th>角色</th><th>类型</th><th>权限</th><th>操作</th></tr></thead><tbody>{roles.data?.items.map((role) => <tr key={role.id}><td><span className="flex items-center gap-2"><ShieldCheck className="size-4" />{role.name}</span></td><td><Badge variant="outline">{role.builtin ? '内置角色' : '自定义角色'}</Badge></td><td>{role.permissions.length} 个动作</td><td><div className="flex gap-2"><Button variant="ghost" size="sm" onClick={() => setViewing(role)}>查看权限</Button>{access.isAdmin && !role.builtin && <><Button variant="ghost" size="sm" onClick={() => { mutation.reset(); setEditor(role) }}>编辑角色</Button><Button variant="ghost" size="sm" onClick={() => { mutation.reset(); setDeleting(role) }}>删除角色</Button></>}</div></td></tr>)}</tbody></Table><p className="text-sm text-muted-foreground">角色授权变更会记录到审计日志。</p>
    <Dialog open={editor !== null} onOpenChange={(open) => { if (!open) setEditor(null) }} busy={mutation.isPending} width={720} title={editor === 'new' ? '创建自定义角色' : '编辑自定义角色'} description="权限按资源与动作组合；模型调用授权独立管理。"><form onSubmit={submit} className="space-y-6"><fieldset disabled={mutation.isPending} className="space-y-6"><FormField label="角色名称"><Input name="name" defaultValue={editor && editor !== 'new' ? editor.name : ''} required maxLength={100} /></FormField><div className="divide-y">{[...new Set(roles.data?.available_permissions.map((p) => p.split('.')[0]))].map((resource) => <fieldset key={resource} className="space-y-3 py-4"><legend className="text-sm font-medium">{resources[resource] ?? resource}</legend><div className="flex flex-wrap gap-4">{roles.data?.available_permissions.filter((p) => p.startsWith(`${resource}.`)).map((permission) => <label key={permission} className="flex items-center gap-2 text-sm"><input type="checkbox" name="permissions" value={permission} defaultChecked={!!editor && editor !== 'new' && editor.permissions.includes(permission)} />{actions[permission.slice(resource.length + 1)] ?? permission}</label>)}</div></fieldset>)}</div></fieldset><ErrorNotice error={mutation.error} /><div className="flex justify-end"><SaveButton pending={mutation.isPending}>保存角色</SaveButton></div></form></Dialog>
    <Dialog open={!!viewing} onOpenChange={(open) => { if (!open) setViewing(null) }} width={640} title={`${viewing?.name ?? ''}权限`} description={viewing?.builtin ? '内置角色不可直接编辑。' : '此处展示该角色允许执行的动作。'}><PermissionRows permissions={viewing?.permissions ?? []} /></Dialog>
    <Dialog open={!!deleting} onOpenChange={(open) => { if (!open) setDeleting(null) }} busy={mutation.isPending} title="删除角色" description={`删除 ${deleting?.name ?? ''}。仍分配给成员的角色不能删除。`}><ErrorNotice error={mutation.error} /><Button disabled={mutation.isPending} onClick={() => deleting && mutation.mutate({ method: 'delete', path: `/admin/roles/${deleting.id}` })}>确认删除</Button></Dialog>
  </Page>
}
