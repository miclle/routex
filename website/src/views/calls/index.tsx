import { useState, type FormEvent } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { getCall, listCalls } from '@/api/calls'
import type { AdminCallDetail, CallFilters, CallRecord } from '@/types/calls'
import { Page, QueryState, FormField, ErrorNotice } from '@/components/app/CatalogUI'
import { PermissionGate } from '@/components/app/PermissionGate'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Drawer } from '@/components/ui/drawer'

const statuses = { success: '成功', error: '失败', canceled: '已取消' }
const formatTime = (value: string) => new Date(value).toLocaleString()
export default function CallsPage({ admin = false }: { admin?: boolean }) {
  return admin ? <PermissionGate permission="calls.read_all"><CallRecords admin /></PermissionGate> : <CallRecords admin={false} />
}
function CallRecords({ admin }: { admin: boolean }) {
  const [filters, setFilters] = useState<CallFilters>({})
  const [selected, setSelected] = useState<string | null>(null)
  const [validation, setValidation] = useState('')
  const calls = useInfiniteQuery({
    queryKey: ['calls', admin ? 'admin' : 'self', filters],
    queryFn: ({ pageParam, signal }) => listCalls(admin, filters, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  })
  const detail = useQuery({ queryKey: ['call', admin ? 'admin' : 'self', selected], queryFn: ({ signal }) => getCall(admin, selected!, signal), enabled: selected !== null })
  function filter(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const next: CallFilters = {}
    for (const name of ['status', 'model_id', 'key_id', ...(admin ? ['user_id'] : [])] as const) {
      const value = String(data.get(name) ?? '').trim()
      if (value) next[name as keyof CallFilters] = value
    }
    for (const name of ['from', 'to'] as const) {
      const value = String(data.get(name) ?? '')
      if (value) next[name] = new Date(value).toISOString()
    }
    if (next.from && next.to && next.from > next.to) { setValidation('开始时间不能晚于结束时间。'); return }
    setValidation('')
    setSelected(null)
    setFilters(next)
  }
  const items = calls.data?.pages.flatMap((page) => page.items) ?? []
  return <Page title={admin ? '全平台调用记录' : '我的调用记录'} description={admin ? '查看请求事实与上游尝试诊断。列表和详情不包含请求正文、响应正文或凭证明文。' : '查看归属于你的真实调用记录。用量缺失会明确标记，不以零代替。'} action={<Button variant="outline" disabled={calls.isFetching} onClick={() => void calls.refetch()}><RefreshCw className="size-4" aria-hidden="true" />刷新</Button>}>
    <form onSubmit={filter} aria-label="调用记录筛选" className="flex flex-wrap items-end gap-2 [&_label]:w-44 [&_label]:text-xs [&_input]:h-9 [&_select]:h-9"><FormField label="状态"><select name="status" className="h-11 w-full rounded-md border bg-background px-3 text-sm"><option value="">全部状态</option><option value="success">成功</option><option value="error">失败</option><option value="canceled">已取消</option></select></FormField><FormField label="模型 ID"><Input name="model_id" placeholder="可选" /></FormField><FormField label="Key ID"><Input name="key_id" placeholder="可选" /></FormField>{admin && <FormField label="用户 ID"><Input name="user_id" placeholder="可选" /></FormField>}<FormField label="开始时间"><Input name="from" type="datetime-local" /></FormField><FormField label="结束时间"><Input name="to" type="datetime-local" /></FormField><div className="flex items-end gap-2"><Button type="submit" disabled={calls.isFetching}>筛选</Button><Button type="reset" variant="ghost" onClick={() => { setFilters({}); setSelected(null); setValidation('') }}>重置</Button></div>{validation && <p role="alert" className="text-sm text-destructive sm:col-span-2">{validation}</p>}</form>
    <QueryState pending={calls.isPending} error={calls.isFetchNextPageError ? null : calls.error} retry={() => void calls.refetch()} empty={calls.isSuccess && items.length === 0} />
    {items.length > 0 && <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="border-b bg-muted/40 text-xs text-muted-foreground"><tr><th className="px-3 py-2 font-medium">模型 / 请求</th>{admin && <th className="px-3 py-2 font-medium">用户</th>}<th className="px-3 py-2 font-medium">状态</th><th className="px-3 py-2 font-medium">开始时间</th><th className="px-3 py-2 font-medium">耗时</th><th className="px-3 py-2 font-medium">输入 / 输出 Tokens</th><th className="px-3 py-2 font-medium"><span className="sr-only">详情</span></th></tr></thead><tbody className="divide-y">{items.map((call) => <tr key={call.request_id}><td className="max-w-72 px-3 py-2"><p className="break-all font-medium">{call.model_name}</p><p className="mt-1 break-all font-mono text-xs text-muted-foreground">{call.request_id}</p></td>{admin && <td className="px-3 py-2 font-mono text-xs">{'user_id' in call ? call.user_id : '—'}</td>}<td className="whitespace-nowrap px-3 py-2"><Badge variant="outline">{statuses[call.status]}</Badge></td><td className="whitespace-nowrap px-3 py-2 text-xs">{formatTime(call.started_at)}</td><td className="whitespace-nowrap px-3 py-2">{call.duration_ms} ms</td><td className="whitespace-nowrap px-3 py-2">{call.input_tokens ?? '未返回'} / {call.output_tokens ?? '未返回'}</td><td className="px-3 py-2"><Button size="sm" variant="ghost" aria-label={`查看 ${call.request_id}`} onClick={() => setSelected(call.request_id)}>详情</Button></td></tr>)}</tbody></table></div>}
    {calls.isFetchNextPageError && <ErrorNotice error={calls.error} />}
    {calls.hasNextPage && <div className="text-center"><Button variant="outline" disabled={calls.isFetchingNextPage} onClick={() => void calls.fetchNextPage()}>{calls.isFetchingNextPage ? '正在加载…' : calls.isFetchNextPageError ? '重试加载更多' : '加载更多'}</Button></div>}
    <Drawer open={selected !== null} onOpenChange={(open) => { if (!open) setSelected(null) }} title="调用详情" description={admin ? '仅显示必要的上游运行诊断，不包含凭证或请求内容。' : '此记录仅包含你的调用事实和用量，不包含上游敏感诊断。'}><QueryState pending={detail.isPending} error={detail.error} retry={() => void detail.refetch()} />{detail.data && <CallDetail call={detail.data} admin={admin} />}</Drawer>
  </Page>
}
function CallDetail({ call, admin }: { call: CallRecord | AdminCallDetail; admin: boolean }) {
  return <div className="space-y-5"><dl className="divide-y rounded-md border text-sm [&>div]:grid [&>div]:grid-cols-[140px_1fr] [&>div]:gap-3 [&>div]:p-3">{[
    ['Request ID', call.request_id], ['模型', call.model_name], ['模型 ID', call.model_id], ['Key ID', call.key_id], ['协议', call.protocol], ['状态', statuses[call.status]], ['响应方式', call.stream ? '流式' : '普通'], ['耗时', `${call.duration_ms} ms`], ['开始时间', formatTime(call.started_at)], ['完成时间', formatTime(call.completed_at)], ['输入 Tokens', call.input_tokens ?? '未返回'], ['输出 Tokens', call.output_tokens ?? '未返回'],
  ].map(([name, value]) => <div key={name}><dt className="text-xs text-muted-foreground">{name}</dt><dd className="mt-1 break-all">{value}</dd></div>)}</dl>{admin && 'attempts' in call && <section className="space-y-3 border-t pt-5"><h3 className="text-sm font-medium">上游诊断</h3><dl className="grid grid-cols-2 gap-3 text-xs"><div><dt className="text-muted-foreground">用户</dt><dd className="mt-1 break-all">{call.user_id}</dd></div><div><dt className="text-muted-foreground">错误代码</dt><dd className="mt-1 break-all">{call.error_code || '—'}</dd></div><div><dt className="text-muted-foreground">供应商模型</dt><dd className="mt-1 break-all">{call.provider_model_id || '—'}</dd></div><div><dt className="text-muted-foreground">接入</dt><dd className="mt-1 break-all">{call.connection_id || '—'}</dd></div></dl><h4 className="text-xs font-medium">上游尝试</h4>{call.attempts.length === 0 && <p className="text-xs text-muted-foreground">未发起上游尝试。</p>}{call.attempts.map((attempt) => <div key={attempt.id} className="space-y-1 rounded-md border p-3 text-xs"><p className="break-all font-mono">{attempt.id}</p><p>{attempt.status} · HTTP {attempt.http_status || '—'} · {attempt.error_code || '无错误代码'}</p><p className="break-all text-muted-foreground">{attempt.connection_id} / {attempt.provider_model_id}</p><p className="text-muted-foreground">{formatTime(attempt.started_at)} → {formatTime(attempt.completed_at)}</p></div>)}</section>}</div>
}
