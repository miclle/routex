import { afterEach, describe, expect, it, vi } from 'vitest'
import { GatewayError, runResponses } from './playground'
import type { ResponsesRequest } from '@/types/playground'
const key = 'rx_ephemeral'
const request: ResponsesRequest = {
  model: 'native',
  input: [{ role: 'user', content: 'Hello' }],
  instructions: 'Be concise',
  stream: false,
  temperature: 0.7,
  top_p: 1,
  max_output_tokens: 2048,
}
function body(status = 'completed', extra = {}) {
  return {
    status,
    output: [{ type: 'message', content: [{ type: 'output_text', text: 'Native answer' }] }],
    usage: { input_tokens: 3, output_tokens: 4, total_tokens: 7 },
    ...extra,
  }
}
function event(type: string, extra = {}) {
  return `event: ${type}\r\ndata: ${JSON.stringify({ type, ...extra })}\r\n\r\n`
}
function stream(text: string) {
  const bytes = new TextEncoder().encode(text)
  return new Response(
    new ReadableStream({
      start(controller) {
        for (let i = 0; i < bytes.length; i += 3) controller.enqueue(bytes.slice(i, i + 3))
        controller.close()
      },
    }),
    { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'req_native' } },
  )
}
const signal = () => new AbortController().signal
const delta = event('response.output_text.delta', {
  output_index: 0,
  content_index: 0,
  delta: '你好',
})
afterEach(() => vi.unstubAllGlobals())
describe('native Responses client', () => {
  it('sends native input and maps exact usage without Chat fields or cookies', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(Response.json(body(), { headers: { 'X-Request-ID': 'req_normal' } }))
    vi.stubGlobal('fetch', fetch)
    expect(await runResponses(key, request, signal(), vi.fn())).toMatchObject({
      text: 'Native answer',
      responseStatus: 'completed',
      requestId: 'req_normal',
      usage: { prompt_tokens: 3, completion_tokens: 4, total_tokens: 7 },
    })
    expect(fetch.mock.calls[0][0]).toBe('/v1/responses')
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(request)
    expect(fetch.mock.calls[0][1]).toMatchObject({
      credentials: 'omit',
      redirect: 'error',
      headers: { Authorization: `Bearer ${key}` },
    })
  })
  it('does not treat HTTP202 acceptance as completion or terminal usage', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(Response.json(body('queued'), { status: 202 })),
    )
    expect(await runResponses(key, request, signal(), vi.fn())).toMatchObject({
      responseStatus: 'queued',
      usage: null,
    })
  })
  it.each(['completed', 'failed', 'incomplete'])(
    'uses authoritative %s event and complete final usage',
    async (status) => {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          stream(
            event('response.created', { response: { usage: { input_tokens: 999 } } }) +
              delta +
              event('response.refusal.delta', {
                output_index: 1,
                content_index: 0,
                delta: 'Refusal',
              }) +
              event(`response.${status}`, {
                response: body(status, {
                  output: [
                    { type: 'message', content: [{ type: 'refusal', refusal: 'Final refusal' }] },
                  ],
                }),
              }),
          ),
        ),
      )
      const update = vi.fn()
      const result = await runResponses(key, { ...request, stream: true }, signal(), update)
      expect(update.mock.calls.some(([r]) => r.text === '你好\nRefusal' && r.usage === null)).toBe(
        true,
      )
      expect(result).toMatchObject({
        text: 'Final refusal',
        responseStatus: status,
        usage: { prompt_tokens: 3, completion_tokens: 4, total_tokens: 7 },
      })
    },
  )
  it.each([
    '',
    'data: [DONE]\n\n',
    'data: invalid\n\n',
    event('response.completed', { response: body('incomplete') }),
  ])('rejects missing or inconsistent terminal evidence', async (ending) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(stream(delta + ending)))
    const update = vi.fn()
    await expect(
      runResponses(key, { ...request, stream: true }, signal(), update),
    ).rejects.toBeInstanceOf(GatewayError)
    expect(update.mock.calls.at(-1)?.[0]).toMatchObject({
      text: '你好',
      usage: null,
      requestId: 'req_native',
    })
  })
  it('identifies non-text output and leaves missing counters unknown', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json(
          body('completed', {
            output: [{ type: 'reasoning', summary: [] }],
            usage: { input_tokens: 2, output_tokens: 1 },
          }),
        ),
      ),
    )
    expect(await runResponses(key, request, signal(), vi.fn())).toMatchObject({
      text: '',
      nonTextOutput: true,
      usage: null,
    })
  })
  it('redacts typed bearer secrets from native error events', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(stream(event('error', { message: `Rejected ${key}` }))),
    )
    await expect(
      runResponses(key, { ...request, stream: true }, signal(), vi.fn()),
    ).rejects.toMatchObject({ message: 'Rejected [REDACTED]', requestId: 'req_native' })
  })
  it('passes cancellation to the active native reader', async () => {
    const abort = new AbortController()
    let writer!: ReadableStreamDefaultController<Uint8Array>
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_path, options: RequestInit) => {
        options.signal!.addEventListener('abort', () =>
          writer.error(new DOMException('Aborted', 'AbortError')),
        )
        return new Response(
          new ReadableStream<Uint8Array>({
            start(value) {
              writer = value
            },
          }),
          { headers: { 'Content-Type': 'text/event-stream' } },
        )
      }),
    )
    const pending = runResponses(key, { ...request, stream: true }, abort.signal, vi.fn())
    await Promise.resolve()
    await Promise.resolve()
    abort.abort()
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  })
})
