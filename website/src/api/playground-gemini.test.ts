import { afterEach, describe, expect, it, vi } from 'vitest'
import { runGemini } from './playground'
import type { GeminiRequest } from '@/types/playground'
const key = 'rx_gemini_secret'
const request: GeminiRequest = {
  model: 'native_2.5',
  stream: false,
  contents: [{ role: 'user', parts: [{ text: 'Hello' }] }],
  systemInstruction: { parts: [{ text: 'Be concise' }] },
  generationConfig: { temperature: 0.7, topP: 1, maxOutputTokens: 2048, candidateCount: 1 },
}
const usage = {
  promptTokenCount: 5,
  cachedContentTokenCount: 2,
  candidatesTokenCount: 3,
  thoughtsTokenCount: 2,
  totalTokenCount: 10,
}
const candidate = (finishReason?: string, text = 'Hello') => ({
  candidates: [
    {
      index: 0,
      content: { role: 'model', parts: [{ text }] },
      ...(finishReason ? { finishReason } : {}),
    },
  ],
})
const body = (reason = 'STOP', counters: unknown = usage) => ({
  ...candidate(reason),
  usageMetadata: counters,
  responseId: 'native_opaque',
  modelVersion: 'native_2.5',
})
const frame = (value: unknown) => `data: ${JSON.stringify(value)}\r\n\r\n`
const signal = () => new AbortController().signal
function sse(text: string) {
  const bytes = new TextEncoder().encode(text)
  return new Response(
    new ReadableStream({
      start(controller) {
        for (let i = 0; i < bytes.length; i += 3) controller.enqueue(bytes.slice(i, i + 3))
        controller.close()
      },
    }),
    { headers: { 'Content-Type': 'text/event-stream', 'X-Request-ID': 'req_gemini' } },
  )
}
afterEach(() => vi.unstubAllGlobals())
describe('native Gemini Playground', () => {
  it('uses native path/header and exact body without model, stream, Bearer, query credentials or cookies', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue(Response.json(body(), { headers: { 'X-Request-ID': 'req_route' } }))
    vi.stubGlobal('fetch', fetch)
    expect(await runGemini(key, request, signal(), vi.fn())).toMatchObject({
      text: 'Hello',
      requestId: 'req_route',
      generationStatus: 'completed',
      usage: { prompt_tokens: 5, completion_tokens: 5, total_tokens: 10 },
    })
    const [path, options] = fetch.mock.calls[0]
    expect(path).toBe('/v1beta/models/native_2.5:generateContent')
    expect(options.headers).toEqual({ 'x-goog-api-key': key, 'Content-Type': 'application/json' })
    expect(options.credentials).toBe('omit')
    expect(options.redirect).toBe('error')
    expect(JSON.parse(options.body)).toEqual({
      contents: request.contents,
      systemInstruction: request.systemInstruction,
      generationConfig: request.generationConfig,
    })
  })
  it.each(['bad/name', 'bad:name', 'bad name', '-bad', 'a'.repeat(129)])(
    'rejects unsafe model %s without fetch',
    async (model) => {
      const fetch = vi.fn()
      vi.stubGlobal('fetch', fetch)
      await expect(runGemini(key, { ...request, model }, signal(), vi.fn())).rejects.toThrow(
        'compatible public name',
      )
      expect(fetch).not.toHaveBeenCalled()
    },
  )
  it('decodes split UTF-8, waits for clean EOF, and uses the last complete usage snapshot', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          sse(
            frame(candidate(undefined, '你')) +
              frame({ ...candidate('STOP', '好'), usageMetadata: usage }) +
              frame({ usageMetadata: { ...usage, candidatesTokenCount: 4, totalTokenCount: 11 } }),
          ),
        ),
    )
    const update = vi.fn()
    const result = await runGemini(key, { ...request, stream: true }, signal(), update)
    expect(result).toMatchObject({
      text: '你好',
      usage: { prompt_tokens: 5, completion_tokens: 6, total_tokens: 11 },
      generationStatus: 'completed',
    })
    expect(
      update.mock.calls
        .slice(0, -1)
        .every(([value]) => value.usage === null && value.generationStatus === 'incomplete'),
    ).toBe(true)
    expect(vi.mocked(fetch).mock.calls[0][0]).toBe(
      '/v1beta/models/native_2.5:streamGenerateContent?alt=sse',
    )
  })
  it('retains null usage when only a preliminary snapshot precedes candidate completion', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(sse(frame({ usageMetadata: usage }) + frame(candidate('STOP')))),
    )
    expect((await runGemini(key, { ...request, stream: true }, signal(), vi.fn())).usage).toBeNull()
  })
  it.each([
    {},
    { ...usage, thoughtsTokenCount: undefined },
    { ...usage, thoughtsTokenCount: -1 },
    { ...usage, totalTokenCount: 99 },
    { ...usage, promptTokenCount: Number.MAX_SAFE_INTEGER },
    { ...usage, prompt_token_count: 5 },
  ])('never estimates missing or invalid aggregate counters %#', async (counters) => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(sse(frame(body()) + frame({ usageMetadata: counters }))),
    )
    expect((await runGemini(key, { ...request, stream: true }, signal(), vi.fn())).usage).toBeNull()
  })
  it('accepts snake_case usage aliases without double counting cached input', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json({
          ...candidate('STOP'),
          usage_metadata: {
            prompt_token_count: 5,
            cached_content_token_count: 2,
            candidates_token_count: 3,
            thoughts_token_count: 2,
            total_token_count: 10,
          },
        }),
      ),
    )
    expect((await runGemini(key, request, signal(), vi.fn())).usage).toEqual({
      prompt_tokens: 5,
      completion_tokens: 5,
      total_tokens: 10,
    })
  })
  it.each([
    ['MAX_TOKENS', 'incomplete'],
    ['SAFETY', 'refused'],
    ['OTHER', 'incomplete'],
  ])('keeps %s outcome distinct', async (reason, status) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(body(reason))))
    expect((await runGemini(key, request, signal(), vi.fn())).generationStatus).toBe(status)
  })
  it('shows explicit prompt blocks without inventing usage', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(sse(frame({ promptFeedback: { blockReason: 'SAFETY' } }))),
    )
    expect(await runGemini(key, { ...request, stream: true }, signal(), vi.fn())).toMatchObject({
      text: '',
      generationStatus: 'refused',
      finishReason: 'SAFETY',
      usage: null,
    })
  })
  it('does not display thoughts or execute/replay native function calls', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        Response.json({
          candidates: [
            {
              finishReason: 'STOP',
              content: {
                parts: [
                  { text: 'hidden thought', thought: true },
                  { text: 'Visible' },
                  { functionCall: { name: 'tool', args: {} } },
                ],
              },
            },
          ],
          usageMetadata: usage,
        }),
      ),
    )
    expect(await runGemini(key, request, signal(), vi.fn())).toMatchObject({
      text: 'Visible',
      nonTextOutput: true,
      generationStatus: 'handoff',
    })
  })
  it.each([
    frame(candidate()),
    frame(body()) + frame(candidate('STOP')),
    frame({ candidates: [{ index: 1, finishReason: 'STOP' }] }),
    'data: [DONE]\n\n',
    'data: {invalid}\n\n',
  ])(
    'rejects missing terminal, duplicates, unexpected candidate or malformed framing %#',
    async (text) => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sse(text)))
      await expect(runGemini(key, { ...request, stream: true }, signal(), vi.fn())).rejects.toThrow(
        'invalid or unfinished',
      )
    },
  )
  it.each(['cancel', 'failure'])(
    'does not publish completion or usage after %s before EOF',
    async (mode) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>
      const abort = new AbortController()
      const update = vi.fn()
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          new Response(
            new ReadableStream({
              start(value) {
                controller = value
                controller.enqueue(new TextEncoder().encode(frame(body())))
              },
            }),
            { headers: { 'Content-Type': 'text/event-stream' } },
          ),
        ),
      )
      const pending = runGemini(key, { ...request, stream: true }, abort.signal, update)
      const rejected = expect(pending).rejects.toThrow()
      await vi.waitFor(() => expect(update.mock.calls.length).toBeGreaterThan(1))
      if (mode === 'cancel') {
        abort.abort()
        controller.close()
      } else controller.error(new Error('transport interrupted'))
      await rejected
      expect(
        update.mock.calls.every(
          ([value]) => value.usage === null && value.generationStatus === 'incomplete',
        ),
      ).toBe(true)
    },
  )
  it('sanitizes native HTTP and SSE error messages without exposing the typed key', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          Response.json(
            { error: { message: `Denied ${key}` } },
            { status: 403, headers: { 'X-Request-ID': 'req_denied' } },
          ),
        ),
    )
    await expect(runGemini(key, request, signal(), vi.fn())).rejects.toMatchObject({
      message: 'Denied [REDACTED]',
      status: 403,
      requestId: 'req_denied',
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(sse(frame({ error: { message: `Failed ${key}` } }))),
    )
    await expect(runGemini(key, { ...request, stream: true }, signal(), vi.fn())).rejects.toThrow(
      'Failed [REDACTED]',
    )
  })
})
