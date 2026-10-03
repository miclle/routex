import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import {
  getOwnActiveTeams,
  getTeamModels,
  runTeamChat,
  runTeamResponses,
  runTeamMessages,
  runTeamGemini,
} from './playground-team'
import { nativeRequest } from './playground-transport'
import type { ChatRequest } from '@/types/playground'

const adapter = client.defaults.adapter
const model = {
  id: 'public-name',
  model_id: 'mdl_one',
  protocols: ['openai_chat'],
  input_capabilities: { openai_chat: [] },
  attachment_scope: 'team',
  personal_attachments: false,
}
const request: ChatRequest = {
  model: 'public-name',
  messages: [{ role: 'user', content: 'Hello' }],
  stream: false,
  temperature: 1,
  top_p: 1,
  max_completion_tokens: 32,
}
beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
afterEach(() => {
  client.defaults.adapter = adapter
  vi.unstubAllGlobals()
})
function reply(data: unknown) {
  client.defaults.adapter = async (config) => ({
    data,
    config,
    status: 200,
    statusText: 'OK',
    headers: new AxiosHeaders(),
  })
}
describe('Team Session transport', () => {
  it('rejects attachment parts before dispatch and expires a denied Session independently of Key authentication', async () => {
    await expect(
      runTeamChat(
        'tea_one',
        'csrf',
        {
          ...request,
          messages: [
            {
              role: 'user',
              content: [
                { type: 'image_url', image_url: { url: 'routex://attachments/obj_private' } },
              ],
            },
          ],
        },
        new AbortController().signal,
        vi.fn(),
      ),
    ).rejects.toThrow()
    expect(fetch).not.toHaveBeenCalled()
    const expired = vi.fn()
    window.addEventListener('routex:session-expired', expired)
    vi.mocked(fetch).mockResolvedValue(new Response('{}', { status: 401 }))
    await expect(
      runTeamChat('tea_one', 'csrf', request, new AbortController().signal, vi.fn()),
    ).rejects.toMatchObject({ status: 401 })
    expect(expired).toHaveBeenCalledOnce()
    window.removeEventListener('routex:session-expired', expired)
  })
  it('uses the explicit Team resource and native parser without an API Key', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          choices: [{ index: 0, message: { content: 'Native text' }, finish_reason: 'stop' }],
          usage: { prompt_tokens: 1, completion_tokens: 2, total_tokens: 3 },
        }),
        { headers: { 'X-Request-ID': 'req_team' } },
      ),
    )
    const result = await runTeamChat(
      'tea_one',
      'current-csrf',
      request,
      new AbortController().signal,
      vi.fn(),
    )
    const [path, options] = vi.mocked(fetch).mock.calls[0]
    expect(path).toBe('/api/v1/teams/tea_one/chat/completions')
    expect(options).toMatchObject({
      credentials: 'same-origin',
      redirect: 'error',
      headers: { 'X-CSRF-Token': 'current-csrf' },
    })
    expect(options!.headers).not.toHaveProperty('Authorization')
    expect(result).toMatchObject({
      text: 'Native text',
      requestId: 'req_team',
      finishReason: 'stop',
    })
    expect(JSON.parse(options!.body as string)).toEqual(request)
  })
  it('retains native SSE terminal and usage semantics', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        'data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}\n\ndata: [DONE]\n\n',
        { headers: { 'Content-Type': 'text/event-stream' } },
      ),
    )
    expect(
      await runTeamChat(
        'tea_one',
        'csrf',
        { ...request, stream: true },
        new AbortController().signal,
        vi.fn(),
      ),
    ).toMatchObject({ text: 'Hello', usage: { total_tokens: 3 } })
    vi.mocked(fetch).mockResolvedValue(
      new Response('data: {"choices":[]}\n\n', {
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    )
    await expect(
      runTeamChat(
        'tea_one',
        'csrf',
        { ...request, stream: true },
        new AbortController().signal,
        vi.fn(),
      ),
    ).rejects.toThrow()
  })
  it('observes a native refusal without promoting it to usable conversation history', async () => {
    vi.mocked(fetch).mockResolvedValue(
      new Response(
        JSON.stringify({
          choices: [{ index: 0, message: { refusal: 'Refused' }, finish_reason: 'stop' }],
        }),
      ),
    )
    expect(
      await runTeamChat('tea_one', 'csrf', request, new AbortController().signal, vi.fn()),
    ).toMatchObject({ text: 'Refused', refused: true, finishReason: 'stop' })
  })
  it('keeps Key requests cookie-free and does not retry a denied Team request', async () => {
    vi.mocked(fetch).mockResolvedValue(new Response('{}', { status: 403 }))
    await expect(
      runTeamChat('tea_one', 'csrf', request, new AbortController().signal, vi.fn()),
    ).rejects.toMatchObject({ status: 403 })
    expect(fetch).toHaveBeenCalledTimes(1)
    vi.mocked(fetch).mockResolvedValue(new Response('{}'))
    await nativeRequest('/v1/models', 'transient-key', new AbortController().signal)
    expect(vi.mocked(fetch).mock.calls[1][1]).toMatchObject({
      credentials: 'omit',
      headers: { Authorization: 'Bearer transient-key' },
    })
    expect(vi.mocked(fetch).mock.calls[1][1]!.headers).not.toHaveProperty('X-CSRF-Token')
  })
  it('validates exact text-only Team discovery and stable model identity', async () => {
    reply({ object: 'list', data: [model] })
    expect(await getTeamModels('tea_one', new AbortController().signal)).toEqual([model])
  })
  it.each([
    {},
    { object: 'list', data: [undefined] },
    { object: 'list', data: [{ ...model, model_id: undefined }] },
    { object: 'list', data: [{ ...model, protocols: ['openai_responses'] }] },
    { object: 'list', data: [{ ...model, input_capabilities: { openai_chat: ['image'] } }] },
    { object: 'list', data: [{ ...model, attachment_scope: 'user' }] },
    { object: 'list', data: [model, model] },
  ])('rejects malformed, media, other-protocol or duplicate discovery: %j', async (body) => {
    reply(body)
    await expect(getTeamModels('tea_one', new AbortController().signal)).rejects.toThrow()
  })
  it('reads only scoped active Teams and rejects a repeated cursor', async () => {
    const seen: unknown[] = []
    client.defaults.adapter = async (config) => {
      seen.push([config.url, config.params])
      return {
        data: { items: [], next_cursor: null },
        config,
        status: 200,
        statusText: 'OK',
        headers: new AxiosHeaders(),
      }
    }
    await getOwnActiveTeams(null, new AbortController().signal)
    expect(seen).toEqual([['/teams', { status: 'active', cursor: undefined }]])
    reply({ items: [], next_cursor: 'same' })
    await expect(getOwnActiveTeams('same', new AbortController().signal)).rejects.toThrow()
  })
})

const nativeResponses = {
  model: 'public-name',
  input: [{ role: 'user' as const, content: 'Hello' }],
  stream: false,
  temperature: 1,
  top_p: 1,
  max_output_tokens: 32,
}
const nativeMessages = {
  model: 'public-name',
  messages: [{ role: 'user' as const, content: 'Hello' }],
  stream: false,
  temperature: 1,
  top_p: 1,
  max_tokens: 32,
}
const nativeGemini = {
  model: 'public-name',
  contents: [{ role: 'user' as const, parts: [{ text: 'Hello' }] }],
  stream: false,
  generationConfig: { temperature: 1, topP: 1, maxOutputTokens: 32, candidateCount: 1 as const },
}
const sig = () => new AbortController().signal
const sse = (text: string) =>
  new Response(text, { headers: { 'Content-Type': 'text/event-stream' } })
const event = (type: string, body: unknown) =>
  `event: ${type}\ndata: ${JSON.stringify({ type, ...(body as object) })}\n\n`
const responseBody = (
  status = 'completed',
  content: unknown = [
    { type: 'message', content: [{ type: 'output_text', text: 'Native answer' }] },
  ],
) => ({ status, output: content, usage: { input_tokens: 2, output_tokens: 3, total_tokens: 5 } })
const messageBody = (reason = 'end_turn') => ({
  type: 'message',
  content: [{ type: 'text', text: 'Native answer' }],
  stop_reason: reason,
  usage: {
    input_tokens: 2,
    output_tokens: 3,
    cache_read_input_tokens: 0,
    cache_creation_input_tokens: 0,
  },
})
const geminiBody = (reason = 'STOP') => ({
  candidates: [
    {
      index: 0,
      content: { role: 'model', parts: [{ text: 'Native answer' }] },
      finishReason: reason,
    },
  ],
  usageMetadata: {
    promptTokenCount: 2,
    candidatesTokenCount: 3,
    totalTokenCount: 5,
    cachedContentTokenCount: 0,
    thoughtsTokenCount: 0,
  },
})
describe('native Team text protocols', () => {
  it.each(['openai_responses', 'anthropic_messages', 'gemini_generate_content'])(
    'discovers %s independently without Chat fallback or media',
    async (protocol) => {
      reply({
        object: 'list',
        data: [{ ...model, protocols: [protocol], input_capabilities: { [protocol]: [] } }],
      })
      expect((await getTeamModels('tea_one', sig()))[0].protocols).toEqual([protocol])
    },
  )
  it('preserves each native ordinary body, path and session-only headers', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(Response.json(responseBody()))
      .mockResolvedValueOnce(Response.json(messageBody()))
      .mockResolvedValueOnce(Response.json(geminiBody()))
    expect(
      await runTeamResponses('tea_one', 'csrf', nativeResponses, sig(), vi.fn()),
    ).toMatchObject({ responseStatus: 'completed', usage: { total_tokens: 5 } })
    expect(await runTeamMessages('tea_one', 'csrf', nativeMessages, sig(), vi.fn())).toMatchObject({
      messageStatus: 'completed',
      usage: { total_tokens: 5 },
    })
    expect(await runTeamGemini('tea_one', 'csrf', nativeGemini, sig(), vi.fn())).toMatchObject({
      generationStatus: 'completed',
      usage: { total_tokens: 5 },
    })
    expect(vi.mocked(fetch).mock.calls.map((call) => call[0])).toEqual([
      '/api/v1/teams/tea_one/responses',
      '/api/v1/teams/tea_one/messages',
      '/api/v1/teams/tea_one/models/public-name:generateContent',
    ])
    const bodies = vi.mocked(fetch).mock.calls.map((call) => JSON.parse(call[1]!.body as string))
    expect(bodies[0]).toEqual(nativeResponses)
    expect(bodies[1]).toEqual(nativeMessages)
    expect(bodies[2]).toEqual({
      contents: nativeGemini.contents,
      generationConfig: nativeGemini.generationConfig,
    })
    for (const [, options] of vi.mocked(fetch).mock.calls) {
      expect(options).toMatchObject({
        credentials: 'same-origin',
        redirect: 'error',
        headers: { 'X-CSRF-Token': 'csrf' },
      })
      expect(options!.headers).not.toHaveProperty('Authorization')
      expect(options!.headers).not.toHaveProperty('x-api-key')
      expect(options!.headers).not.toHaveProperty('x-goog-api-key')
    }
    expect(vi.mocked(fetch).mock.calls[1][1]!.headers).toHaveProperty(
      'anthropic-version',
      '2023-06-01',
    )
  })
  it('requires native Responses terminals and distinguishes refusal from completed history', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      sse(
        event('response.output_text.delta', {
          output_index: 0,
          content_index: 0,
          delta: 'Partial',
        }) + event('response.completed', { response: responseBody() }),
      ),
    )
    expect(
      await runTeamResponses(
        'tea_one',
        'csrf',
        { ...nativeResponses, stream: true },
        sig(),
        vi.fn(),
      ),
    ).toMatchObject({
      text: 'Native answer',
      responseStatus: 'completed',
      usage: { total_tokens: 5 },
    })
    vi.mocked(fetch).mockResolvedValueOnce(sse('data: [DONE]\n\n'))
    await expect(
      runTeamResponses('tea_one', 'csrf', { ...nativeResponses, stream: true }, sig(), vi.fn()),
    ).rejects.toThrow()
    vi.mocked(fetch).mockResolvedValueOnce(
      Response.json(
        responseBody('completed', [
          { type: 'message', content: [{ type: 'refusal', refusal: 'Blocked' }] },
        ]),
      ),
    )
    expect(
      await runTeamResponses('tea_one', 'csrf', nativeResponses, sig(), vi.fn()),
    ).toMatchObject({ refused: true, text: 'Blocked' })
  })
  it('requires Messages message_stop and preserves handoff/refusal/incomplete native facts', async () => {
    const prefix =
      event('message_start', {
        message: {
          type: 'message',
          content: [],
          usage: {
            input_tokens: 2,
            output_tokens: 0,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
          },
        },
      }) +
      event('content_block_start', { index: 0, content_block: { type: 'text', text: '' } }) +
      event('content_block_delta', { index: 0, delta: { type: 'text_delta', text: 'Partial' } }) +
      event('content_block_stop', { index: 0 }) +
      event('message_delta', { delta: { stop_reason: 'end_turn' }, usage: { output_tokens: 3 } })
    vi.mocked(fetch).mockResolvedValueOnce(sse(prefix + event('message_stop', {})))
    expect(
      await runTeamMessages('tea_one', 'csrf', { ...nativeMessages, stream: true }, sig(), vi.fn()),
    ).toMatchObject({ messageStatus: 'completed', text: 'Partial', usage: { total_tokens: 5 } })
    vi.mocked(fetch).mockResolvedValueOnce(sse(prefix))
    await expect(
      runTeamMessages('tea_one', 'csrf', { ...nativeMessages, stream: true }, sig(), vi.fn()),
    ).rejects.toThrow()
    for (const [reason, status] of [
      ['tool_use', 'handoff'],
      ['refusal', 'refused'],
      ['max_tokens', 'incomplete'],
    ]) {
      vi.mocked(fetch).mockResolvedValueOnce(Response.json(messageBody(reason)))
      expect(
        await runTeamMessages('tea_one', 'csrf', nativeMessages, sig(), vi.fn()),
      ).toHaveProperty('messageStatus', status)
    }
  })
  it('uses native Gemini stream action, waits for terminal EOF and preserves prompt blocks', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(sse(`data: ${JSON.stringify(geminiBody())}\n\n`))
    expect(
      await runTeamGemini('tea_one', 'csrf', { ...nativeGemini, stream: true }, sig(), vi.fn()),
    ).toMatchObject({ generationStatus: 'completed', usage: { total_tokens: 5 } })
    expect(vi.mocked(fetch).mock.calls[0][0]).toBe(
      '/api/v1/teams/tea_one/models/public-name:streamGenerateContent?alt=sse',
    )
    vi.mocked(fetch).mockResolvedValueOnce(sse(`data: ${JSON.stringify(geminiBody(''))}\n\n`))
    await expect(
      runTeamGemini('tea_one', 'csrf', { ...nativeGemini, stream: true }, sig(), vi.fn()),
    ).rejects.toThrow()
    vi.mocked(fetch).mockResolvedValueOnce(
      Response.json({
        promptFeedback: { blockReason: 'SAFETY' },
        usageMetadata: { promptTokenCount: 2, candidatesTokenCount: 0, totalTokenCount: 2 },
      }),
    )
    expect(await runTeamGemini('tea_one', 'csrf', nativeGemini, sig(), vi.fn())).toMatchObject({
      generationStatus: 'refused',
      finishReason: 'SAFETY',
    })
  })
  it('rejects all media forms/unsafe aliases and missing session proof before dispatch', async () => {
    await expect(
      runTeamResponses(
        'tea_one',
        'csrf',
        {
          ...nativeResponses,
          input: [
            {
              role: 'user',
              content: [{ type: 'input_image', image_url: 'routex://attachments/obj_private' }],
            },
          ],
        },
        sig(),
        vi.fn(),
      ),
    ).rejects.toThrow()
    await expect(
      runTeamMessages(
        'tea_one',
        'csrf',
        {
          ...nativeMessages,
          messages: [
            {
              role: 'user',
              content: [
                {
                  type: 'image',
                  source: { type: 'base64', media_type: 'image/png', data: 'private' },
                },
              ],
            },
          ],
        },
        sig(),
        vi.fn(),
      ),
    ).rejects.toThrow()
    await expect(
      runTeamGemini(
        'tea_one',
        'csrf',
        {
          ...nativeGemini,
          contents: [
            { role: 'user', parts: [{ inlineData: { mimeType: 'image/png', data: 'private' } }] },
          ],
        },
        sig(),
        vi.fn(),
      ),
    ).rejects.toThrow()
    await expect(
      runTeamGemini('tea_one', 'csrf', { ...nativeGemini, model: 'bad/name' }, sig(), vi.fn()),
    ).rejects.toThrow()
    await expect(runTeamResponses('tea_one', '', nativeResponses, sig(), vi.fn())).rejects.toThrow()
    const abort = new AbortController()
    abort.abort()
    await expect(
      runTeamGemini('tea_one', 'csrf', nativeGemini, abort.signal, vi.fn()),
    ).rejects.toThrow()
    expect(fetch).not.toHaveBeenCalled()
  })
})
