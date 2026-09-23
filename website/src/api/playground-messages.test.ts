import { afterEach, describe, expect, it, vi } from 'vitest'
import { GatewayError, runMessages } from './playground'
import type { MessagesRequest } from '@/types/playground'
const key = 'rx_messages_secret'
const request: MessagesRequest = {
  model: 'native',
  messages: [{ role: 'user', content: 'Hello' }],
  system: 'Be concise',
  stream: false,
  temperature: 0.7,
  top_p: 1,
  max_tokens: 2048,
}
const usage = {
  input_tokens: 3,
  cache_read_input_tokens: 2,
  cache_creation_input_tokens: 1,
  output_tokens: 4,
}
const body = (reason = 'end_turn', counters: unknown = usage) => ({
  type: 'message',
  role: 'assistant',
  content: [{ type: 'text', text: 'Native text' }],
  stop_reason: reason,
  usage: counters,
})
const ev = (type: string, extra = {}) =>
  `event: ${type}\r\ndata: ${JSON.stringify({ type, ...extra })}\r\n\r\n`
const start = ev('message_start', {
  message: { type: 'message', content: [], usage: { ...usage, output_tokens: 1 } },
})
const block = ev('content_block_start', { index: 0, content_block: { type: 'text', text: '' } })
const delta = ev('content_block_delta', { index: 0, delta: { type: 'text_delta', text: '你好' } })
const close = ev('content_block_stop', { index: 0 })
const final = (reason = 'end_turn', counters: unknown = { output_tokens: 4 }) =>
  ev('message_delta', { delta: { stop_reason: reason }, usage: counters })
const stop = ev('message_stop')
function stream(text: string) {
  const bytes = new TextEncoder().encode(text)
  return new Response(
    new ReadableStream({
      start(controller) {
        for (let i = 0; i < bytes.length; i += 5) controller.enqueue(bytes.slice(i, i + 5))
        controller.close()
      },
    }),
    { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'req_messages' } },
  )
}
const signal = () => new AbortController().signal
afterEach(() => vi.unstubAllGlobals())
describe('native Messages playground transport', () => {
  it('uses exclusive x-api-key and version headers with native system/max_tokens including zero', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(Response.json(body(), { headers: { 'request-id': 'req_header' } }))
    vi.stubGlobal('fetch', fetch)
    expect(await runMessages(key, { ...request, max_tokens: 0 }, signal(), vi.fn())).toMatchObject({
      text: 'Native text',
      requestId: 'req_header',
      messageStatus: 'completed',
      usage: { prompt_tokens: 6, completion_tokens: 4, total_tokens: 10 },
    })
    expect(fetch.mock.calls[0][0]).toBe('/v1/messages')
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ ...request, max_tokens: 0 })
    expect(fetch.mock.calls[0][1]).toMatchObject({
      credentials: 'omit',
      redirect: 'error',
      headers: { 'x-api-key': key, 'anthropic-version': '2023-06-01' },
    })
    expect(fetch.mock.calls[0][1].headers).not.toHaveProperty('Authorization')
  })
  it('streams split UTF-8 text and replaces cumulative usage only after message_stop', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          stream(
            ev('ping') +
              start +
              block +
              delta +
              close +
              ev('message_delta', { delta: {}, usage: { output_tokens: 2 } }) +
              final() +
              stop,
          ),
        ),
    )
    const update = vi.fn()
    const result = await runMessages(key, { ...request, stream: true }, signal(), update)
    expect(update.mock.calls.some(([value]) => value.text === '你好' && value.usage === null)).toBe(
      true,
    )
    expect(result).toMatchObject({
      text: '你好',
      messageStatus: 'completed',
      finishReason: 'end_turn',
      usage: { prompt_tokens: 6, completion_tokens: 4, total_tokens: 10 },
    })
  })
  it.each([
    ['max_tokens', 'incomplete'],
    ['tool_use', 'handoff'],
    ['pause_turn', 'handoff'],
    ['refusal', 'refused'],
    ['stop_sequence', 'completed'],
  ])('represents %s without inventing success', async (reason, status) => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(stream(start + block + delta + close + final(reason) + stop)),
    )
    expect(await runMessages(key, { ...request, stream: true }, signal(), vi.fn())).toMatchObject({
      messageStatus: status,
      finishReason: reason,
      usage: { total_tokens: 10 },
    })
  })
  it.each([
    { input_tokens: 3, output_tokens: 4 },
    { ...usage, cache_read_input_tokens: null },
    { ...usage, input_tokens: Number.MAX_SAFE_INTEGER },
  ])('keeps omitted, invalid and unsafe usage unknown', async (counters) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(body('end_turn', counters))))
    expect((await runMessages(key, request, signal(), vi.fn())).usage).toBeNull()
  })
  it.each([
    {},
    { output_tokens: 'invalid' },
    { output_tokens: 4, cache_creation_input_tokens: null },
  ])('does not inherit invalid or omitted final counters', async (counters) => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          stream(start + block + delta + close + final('end_turn', counters) + stop),
        ),
    )
    expect(
      (await runMessages(key, { ...request, stream: true }, signal(), vi.fn())).usage,
    ).toBeNull()
  })
  it.each([
    start + block + delta + close + final(),
    start + block + delta + close + stop,
    start + start,
    start + block + block,
    start + delta,
    start + block + delta + final() + stop,
    start + block + delta + close + final('end_turn', { output_tokens: 0 }) + stop,
    start + 'data: [DONE]\n\n',
  ])('rejects invalid lifecycle or truncated native finality', async (events) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(stream(events)))
    await expect(
      runMessages(key, { ...request, stream: true }, signal(), vi.fn()),
    ).rejects.toBeInstanceOf(GatewayError)
  })
  it('does not render or execute tool/thinking output and preserves known text', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        stream(
          start +
            ev('content_block_start', {
              index: 0,
              content_block: { type: 'thinking', thinking: '' },
            }) +
            ev('content_block_delta', {
              index: 0,
              delta: { type: 'thinking_delta', thinking: 'private reasoning' },
            }) +
            close +
            ev('future_event', { opaque: true }) +
            final() +
            stop,
        ),
      ),
    )
    expect(await runMessages(key, { ...request, stream: true }, signal(), vi.fn())).toMatchObject({
      text: '',
      nonTextOutput: true,
    })
  })
  it('keeps native HTTP529 and redacts secrets from HTTP and SSE errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          Response.json(
            { error: { type: 'overloaded_error', message: `Overloaded ${key}` } },
            { status: 529, headers: { 'X-Request-ID': 'req_529' } },
          ),
        )
        .mockResolvedValueOnce(
          stream(
            start +
              block +
              delta +
              ev('error', { error: { type: 'overloaded_error', message: `Failed ${key}` } }),
          ),
        ),
    )
    await expect(runMessages(key, request, signal(), vi.fn())).rejects.toMatchObject({
      status: 529,
      message: 'Overloaded [REDACTED]',
      requestId: 'req_529',
    })
    const update = vi.fn()
    await expect(
      runMessages(key, { ...request, stream: true }, signal(), update),
    ).rejects.toMatchObject({ message: 'Failed [REDACTED]', requestId: 'req_messages' })
    expect(update.mock.calls.at(-1)?.[0]).toMatchObject({ text: '你好', usage: null })
  })
  it('aborts native fetch and stops reading without final usage', async () => {
    const abort = new AbortController()
    let controller!: ReadableStreamDefaultController<Uint8Array>
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_path, options: RequestInit) => {
        options.signal!.addEventListener('abort', () =>
          controller.error(new DOMException('Aborted', 'AbortError')),
        )
        return new Response(
          new ReadableStream<Uint8Array>({
            start(value) {
              controller = value
            },
          }),
          { headers: { 'Content-Type': 'text/event-stream' } },
        )
      }),
    )
    const pending = runMessages(key, { ...request, stream: true }, abort.signal, vi.fn())
    await Promise.resolve()
    await Promise.resolve()
    abort.abort()
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  })
})
