import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AxiosHeaders } from 'axios'
import client from './client'
import { getOwnActiveTeams, getTeamModels, runTeamChat } from './playground-team'
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
