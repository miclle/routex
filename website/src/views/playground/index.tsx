import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Copy, LoaderCircle, Send, Square, Trash2 } from 'lucide-react'
import { GatewayError, getGatewayModels, runChat } from '@/api/playground'
import type { ChatMessage, ChatResult, GatewayModel } from '@/types/playground'
import { Page, FormField } from '@/components/app/CatalogUI'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'

type Exchange = ChatResult & { id: string; prompt: string; model: string; status: 'running' | 'completed' | 'cancelled' | 'failed'; error: string }
export default function PlaygroundPage() {
  const [key, setKey] = useState('')
  const [models, setModels] = useState<GatewayModel[]>([])
  const [model, setModel] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [keyChecked, setKeyChecked] = useState(false)
  const [exchanges, setExchanges] = useState<Exchange[]>([])
  const [prompt, setPrompt] = useState('')
  const [copied, setCopied] = useState('')
  const controller = useRef<AbortController | null>(null)
  const mounted = useRef(true)
  const busy = loading || exchanges.some((exchange) => exchange.status === 'running')
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort() } }, [])
  function changeKey(value: string) { setKey(value); setModels([]); setModel(''); setKeyChecked(false); setError(''); setExchanges([]) }
  async function loadModels() {
    if (!key.trim() || busy) return
    const abort = new AbortController()
    controller.current = abort
    setLoading(true)
    setError('')
    try {
      const items = await getGatewayModels(key.trim(), abort.signal)
      if (mounted.current) { setModels(items); setModel(items[0]?.id ?? ''); setKeyChecked(true) }
    } catch (failure) {
      if (mounted.current && !abort.signal.aborted) setError(failure instanceof GatewayError ? failure.message : '无法连接网关，请检查网络后重试。')
    } finally { if (mounted.current) setLoading(false); if (controller.current === abort) controller.current = null }
  }
  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || !prompt.trim() || !model || !key.trim()) return
    const form = new FormData(event.currentTarget)
    const text = prompt.trim()
    const system = String(form.get('system') ?? '').trim()
    const stream = form.get('stream') === 'on'
    const messages: ChatMessage[] = [
      ...(system ? [{ role: 'system' as const, content: system }] : []),
      ...exchanges.filter((exchange) => exchange.status === 'completed').flatMap((exchange): ChatMessage[] => [{ role: 'user', content: exchange.prompt }, { role: 'assistant', content: exchange.text }]),
      { role: 'user', content: text },
    ]
    const id = crypto.randomUUID()
    const abort = new AbortController()
    controller.current = abort
    setCopied('')
    setError('')
    setPrompt('')
    setExchanges((current) => [...current, { id, prompt: text, model, status: 'running', text: '', requestId: '', usage: null, finishReason: null, error: '' }])
    const update = (patch: Partial<Exchange>) => { if (mounted.current) setExchanges((current) => current.map((exchange) => exchange.id === id ? { ...exchange, ...patch } : exchange)) }
    try {
      const result = await runChat(key.trim(), { model, messages, stream, temperature: Number(form.get('temperature')), top_p: Number(form.get('top_p')), max_tokens: Number(form.get('max_tokens')), ...(stream ? { stream_options: { include_usage: true } } : {}) }, abort.signal, update)
      update({ ...result, status: 'completed' })
    } catch (failure) {
      if (abort.signal.aborted) update({ status: 'cancelled', error: '' })
      else update({ status: 'failed', error: failure instanceof GatewayError ? failure.message : '请求失败，已保留收到的内容。请检查连接后重试。', ...(failure instanceof GatewayError && failure.requestId ? { requestId: failure.requestId } : {}) })
    } finally { if (controller.current === abort) controller.current = null }
  }
  async function copy(exchange: Exchange) {
    try { await navigator.clipboard.writeText(exchange.text); setCopied(exchange.id) } catch { setError('复制失败，请手动选择输出文本复制。') }
  }
  return <Page title="Playground" description="使用个人 API Key 发起真实的 OpenAI Chat 调用。模型范围由该 Key 与用户授权共同决定；密钥和对话仅保留在当前页面内存中。">
    {error && <p role="alert" className="rounded-md border border-destructive/30 p-3 text-sm text-destructive">{error}</p>}
    <form onSubmit={(event) => void send(event)} className="flex min-h-[640px] min-w-[980px] overflow-hidden rounded-lg border" style={{ height: 'calc(100vh - 190px)' }}>
      <aside className="w-80 shrink-0 space-y-5 overflow-auto border-r bg-muted/30 p-5">
        <fieldset disabled={busy} className="space-y-5"><FormField label="个人 API Key"><Input name="api_key" type="password" autoComplete="off" value={key} onValueChange={changeKey} placeholder="粘贴已启用的个人 Key" /></FormField><div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={!key.trim() || busy} onClick={() => void loadModels()}>{loading ? '正在验证…' : '验证并加载模型'}</Button><Button size="sm" variant="ghost" disabled={!key || busy} onClick={() => changeKey('')}>清除密钥</Button></div>
          <FormField label="选择模型"><select name="model" aria-label="选择模型" value={model} onChange={(e) => { setModel(e.target.value); setExchanges([]) }} className="h-11 w-full rounded-md border bg-background px-3 text-sm" disabled={!models.length}><option value="">{models.length ? '选择模型' : keyChecked ? '没有可调用模型' : '请先验证 Key'}</option>{models.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}</select></FormField>
          {keyChecked && models.length === 0 && <p role="status" className="text-xs leading-5 text-muted-foreground">此 Key 没有有效模型，请检查其范围及用户授权。</p>}
          <div className="border-t pt-4"><Badge variant="outline">OpenAI Chat</Badge><p className="mt-2 break-all font-mono text-xs text-muted-foreground">POST /v1/chat/completions</p></div>
          <FormField label="Temperature"><Input name="temperature" type="number" min={0} max={2} step={0.1} defaultValue={0.7} required /></FormField><FormField label="Top P"><Input name="top_p" type="number" min={0} max={1} step={0.05} defaultValue={1} required /></FormField><FormField label="最大输出 Token"><Input name="max_tokens" type="number" min={1} max={32768} step={1} defaultValue={2048} required /></FormField><FormField label="System Prompt"><Textarea name="system" placeholder="可选：说明回答风格或任务背景" /></FormField><label className="flex items-center gap-2 text-sm"><input type="checkbox" name="stream" defaultChecked />流式输出</label>
        </fieldset>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden bg-card">
        <div className="flex items-center justify-between border-b px-5 py-3"><h2 className="text-sm font-medium">模型对话</h2><Button variant="ghost" size="sm" disabled={busy || !exchanges.length} onClick={() => { setExchanges([]); setCopied('') }}><Trash2 className="size-3.5" aria-hidden="true" />清空对话</Button></div>
        <div role="log" aria-label="模型对话" aria-live="polite" className="min-h-0 flex-1 space-y-6 overflow-auto p-5">{exchanges.length === 0 && <div className="flex min-h-60 items-center justify-center text-center text-sm leading-7 text-muted-foreground">验证 Key、选择模型，开始一次真实对话。<br />成功完成的消息会作为后续对话上下文发送。</div>}{exchanges.map((exchange) => <article key={exchange.id} className="space-y-3"><div className="ml-auto max-w-[90%] whitespace-pre-wrap rounded-lg bg-secondary px-4 py-3 text-sm leading-6">{exchange.prompt}</div><div className="space-y-3 border-l-2 pl-4"><div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><span>{exchange.model}</span><span>{exchange.status === 'running' ? '生成中…' : exchange.status === 'cancelled' ? '已停止' : exchange.status === 'failed' ? '调用失败' : '已完成'}</span></div><p className="whitespace-pre-wrap break-words text-sm leading-7">{exchange.text || (exchange.status === 'running' ? '等待响应…' : '未返回文本内容。')}</p>{exchange.error && <p role="alert" className="text-sm text-destructive">{exchange.error}</p>}<div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">{exchange.requestId && <span className="break-all">Request ID：{exchange.requestId}</span>}{exchange.usage ? <span>输入 {exchange.usage.prompt_tokens} · 输出 {exchange.usage.completion_tokens} · 总计 {exchange.usage.total_tokens} Tokens</span> : exchange.status !== 'running' && <span>上游未返回用量</span>}{exchange.text && <Button size="sm" variant="ghost" onClick={() => void copy(exchange)}><Copy className="size-3" aria-hidden="true" />{copied === exchange.id ? '已复制' : '复制输出'}</Button>}</div></div></article>)}</div>
        <div className="space-y-3 border-t p-5"><Textarea aria-label="消息" name="prompt" value={prompt} onChange={(e) => setPrompt(e.target.value)} disabled={busy} placeholder="输入消息；Ctrl / ⌘ + Enter 发送" onKeyDown={(event) => { if (event.key === 'Enter' && (event.ctrlKey || event.metaKey) && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit() } }} /><div className="flex justify-end gap-2">{busy && !loading && <Button variant="outline" onClick={() => controller.current?.abort()}><Square className="size-4" aria-hidden="true" />停止生成</Button>}<Button type="submit" disabled={busy || !model || !prompt.trim()}>{busy ? <LoaderCircle className="size-4 animate-spin" aria-hidden="true" /> : <Send className="size-4" aria-hidden="true" />}{busy ? '请求进行中…' : '发送消息'}</Button></div></div>
      </div>
    </form>
  </Page>
}
