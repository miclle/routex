import { afterEach, describe, expect, it, vi } from 'vitest'
import { GatewayError, getGatewayModels, runChat } from './playground'
import type { ChatRequest, ChatResult } from '@/types/playground'

const key = 'rx_ephemeral_secret'
const request: ChatRequest = { model: 'public-model', messages: [{ role: 'user', content: 'Hello' }], stream: false, temperature: 0.7, top_p: 1, max_tokens: 2048 }
const controller = () => new AbortController()
function sse(text: string) {
  const data = new TextEncoder().encode(text)
  return new Response(new ReadableStream({ start(stream) { for (let i = 0; i < data.length; i += 3) stream.enqueue(data.slice(i, i + 3)); stream.close() } }), { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'req_1' } })
}
afterEach(() => vi.unstubAllGlobals())

describe('native playground client', () => {
  it('loads models with only the typed bearer Key and parses a normal response', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(Response.json({ data: [{ id: 'public-model' }] })).mockResolvedValueOnce(Response.json({ choices: [{ index: 0, message: { content: 'Hello back' }, finish_reason: 'stop' }], usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 } }, { headers: { 'X-Request-ID': 'req_normal' } }))
    vi.stubGlobal('fetch', fetch)
    expect(await getGatewayModels(key, controller().signal)).toEqual([{ id: 'public-model' }])
    const result = await runChat(key, request, controller().signal, vi.fn())
    expect(result).toEqual({ text: 'Hello back', requestId: 'req_normal', finishReason: 'stop', usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 } })
    const options = fetch.mock.calls[1][1]
    expect(options.credentials).toBe('omit')
    expect(options.redirect).toBe('error')
    expect(options.headers.Authorization).toBe(`Bearer ${key}`)
    expect(JSON.parse(options.body)).toEqual(request)
  })
  it('decodes split UTF-8 and CRLF stream frames, incrementally reports text, and preserves usage', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sse(': keepalive\r\n\r\ndata: {"choices":[{"index":0,"delta":{"content":"你好"}}]}\r\n\r\ndata: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}\r\n\r\ndata: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}\r\n\r\ndata: [DONE]\r\n\r\n')))
    const update = vi.fn()
    const result = await runChat(key, { ...request, stream: true }, controller().signal, update)
    expect(update.mock.calls.some(([value]) => value.text === '你好')).toBe(true)
    expect(result.text).toBe('你好 world')
    expect(result.usage?.total_tokens).toBe(7)
    expect(result.requestId).toBe('req_1')
  })
  it('retains partial output and the request ID when a native error arrives mid-stream', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sse('data: {"choices":[{"delta":{"content":"partial"}}]}\n\ndata: {"error":{"message":"Upstream unavailable","type":"server_error"}}\n\n')))
    let latest: ChatResult | undefined
    await expect(runChat(key, { ...request, stream: true }, controller().signal, (result) => { latest = result })).rejects.toMatchObject({ message: 'Upstream unavailable', requestId: 'req_1' })
    expect(latest?.text).toBe('partial')
  })
  it('does not treat a truncated response as successfully completed', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sse('data: {"choices":[{"delta":{"content":"partial"}}]}\n\n')))
    await expect(runChat(key, { ...request, stream: true }, controller().signal, vi.fn())).rejects.toThrow('响应流提前结束')
  })
  it('rejects malformed events and unexpected content types', async () => {
    const fetch = vi.fn().mockResolvedValueOnce(sse('data: not-json\n\n')).mockResolvedValueOnce(Response.json({ choices: [] }))
    vi.stubGlobal('fetch', fetch)
    await expect(runChat(key, { ...request, stream: true }, controller().signal, vi.fn())).rejects.toThrow('无效的数据')
    await expect(runChat(key, { ...request, stream: true }, controller().signal, vi.fn())).rejects.toThrow('有效的流式响应')
  })
  it('reports HTTP errors and removes the supplied secret from error messages', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ error: { message: `Invalid Key ${key}` } }, { status: 401, headers: { 'X-Request-ID': 'req_error' } })))
    await expect(runChat(key, request, controller().signal, vi.fn())).rejects.toEqual(new GatewayError('Invalid Key [REDACTED]', 'req_error', 401))
  })
  it('passes cancellation to the request and releases the response reader', async () => {
    const abort = controller()
    const cancel = vi.fn()
    let streamController!: ReadableStreamDefaultController<Uint8Array>
    vi.stubGlobal('fetch', vi.fn(async (_path, options: RequestInit) => {
      options.signal!.addEventListener('abort', () => streamController.error(new DOMException('Aborted', 'AbortError')))
      return new Response(new ReadableStream<Uint8Array>({ start(value) { streamController = value }, cancel }), { headers: { 'Content-Type': 'text/event-stream' } })
    }))
    const pending = runChat(key, { ...request, stream: true }, abort.signal, vi.fn())
    await Promise.resolve()
    await Promise.resolve()
    abort.abort()
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  })
})
