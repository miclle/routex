import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Bot, Grid2X2, List, Copy } from 'lucide-react'
import { listModels } from '@/api/catalog'
import { Page, QueryState } from '@/components/app/CatalogUI'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table } from '@/components/ui/table'
import { Drawer } from '@/components/ui/drawer'
import type { CallableModel } from '@/types/catalog'

export default function ModelsPage() {
  const models = useQuery({ queryKey: ['models'], queryFn: listModels })
  const [query, setQuery] = useState('')
  const [view, setView] = useState('card')
  const [selected, setSelected] = useState<CallableModel | null>(null)
  const [notice, setNotice] = useState('')
  const items = models.data?.filter((model) => model.name.toLowerCase().includes(query.toLowerCase())) ?? []
  const endpoint = `${window.location.origin}/v1`
  const example = `curl ${endpoint}/chat/completions \\\n  -H "Authorization: Bearer $ROUTEX_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '${JSON.stringify({ model: selected?.name, messages: [{ role: 'user', content: 'Hello' }] })}'`
  async function copy(value: string) { try { await navigator.clipboard.writeText(value); setNotice('已复制。') } catch { setNotice('复制失败，请手动选择文本。') } }
  return <Page title="模型广场" description="查看已授权模型的协议与 API 接入配置。"><div aria-label="模型广场统计" className="grid grid-cols-3 gap-6 rounded-lg border px-6 py-3">{[['模型总数', models.data?.length], ['可接入模型', models.data?.length], ['协议类型', new Set(models.data?.map((m) => m.protocol)).size]].map(([label, value]) => <div key={label}><p className="text-sm text-muted-foreground">{label}</p><p className="mt-1 text-2xl">{models.isSuccess ? value : '—'}</p></div>)}</div><Input type="search" aria-label="搜索模型" placeholder="按模型名称搜索" value={query} onValueChange={setQuery} /><div className="flex items-center justify-between gap-4"><p className="text-sm text-muted-foreground">已筛选出 {items.length} 个模型</p><div aria-label="模型展示方式" className="flex rounded-md bg-muted p-1"><Button size="sm" variant={view === 'card' ? 'outline' : 'ghost'} onClick={() => setView('card')} aria-pressed={view === 'card'}><Grid2X2 className="size-4" />卡片</Button><Button size="sm" variant={view === 'table' ? 'outline' : 'ghost'} onClick={() => setView('table')} aria-pressed={view === 'table'}><List className="size-4" />表格</Button></div></div><QueryState pending={models.isPending} error={models.error} retry={() => void models.refetch()} empty={models.isSuccess && items.length === 0} />{view === 'card' ? <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{items.map((model) => <button key={model.id} onClick={() => { setSelected(model); setNotice('') }} aria-label={`打开 ${model.name} API 接入`} className="space-y-4 rounded-lg border border-foreground p-3 text-left"><div className="flex items-center gap-2"><span className="rounded bg-muted p-2"><Bot className="size-4" /></span><h2 className="truncate text-sm font-semibold">{model.name}</h2></div><div className="flex gap-2"><Badge variant="outline">OpenAI Chat</Badge><Badge variant="secondary">个人授权</Badge></div></button>)}</div> : <Table aria-label="模型广场列表"><thead><tr><th>模型</th><th>可用来源</th><th>协议类型</th><th>操作</th></tr></thead><tbody>{items.map((model) => <tr key={model.id}><td>{model.name}</td><td>个人授权</td><td>OpenAI Chat</td><td><Button size="sm" variant="ghost" onClick={() => setSelected(model)}>API 接入</Button></td></tr>)}</tbody></Table>}
    <Drawer open={!!selected} onOpenChange={(open) => { if (!open) setSelected(null) }} title={`${selected?.name ?? ''} API 接入`} width={520}><div className="space-y-6"><div className="flex items-center gap-3 rounded-lg border p-4"><Bot className="size-5" /><h2 className="font-semibold">{selected?.name}</h2><Badge variant="outline">OpenAI Chat</Badge></div><section className="rounded-lg border"><h3 className="border-b p-4 text-sm font-semibold">接入配置</h3><div className="space-y-4 p-4"><div><p className="text-sm text-muted-foreground">Base URL</p><code className="break-all text-sm">{endpoint}</code></div><div><p className="text-sm text-muted-foreground">Model</p><code>{selected?.name}</code></div><Link to="/keys" className="text-sm underline">管理 API Key</Link></div></section><section className="rounded-lg border"><div className="flex items-center justify-between border-b px-4 py-2"><h3 className="text-sm font-semibold">请求示例</h3><Button size="sm" variant="ghost" onClick={() => void copy(example)}><Copy className="size-3" />复制</Button></div><pre className="overflow-auto p-4 text-xs leading-6">{example}</pre></section>{notice && <p role="status" className="text-sm">{notice}</p>}</div></Drawer>
  </Page>
}
