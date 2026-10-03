import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import { getSession } from '@/api/auth'
import {
  getOwnActiveTeams,
  getTeamModels,
  runTeamChat,
  runTeamResponses,
  runTeamMessages,
  runTeamGemini,
} from '@/api/playground-team'
import { GatewayError, getGatewayModels, runChat, runResponses } from '@/api/playground'
import type { ChatResult, GatewayModel, ResponsesResult } from '@/types/playground'
import CompareWorkbench from './compare'
import PlaygroundPage from './index'

let actor = 'usr_one',
  csrf = 'csrf-one',
  refreshing = false,
  sessionError = false,
  realSession = false
vi.mock('@/hooks/use-auth', async (original) => {
  const actual = await original<typeof import('@/hooks/use-auth')>()
  return {
    ...actual,
    useSession: () =>
      realSession
        ? actual.useSession()
        : {
            isFetching: refreshing,
            isError: sessionError,
            data: { user: { id: actor, role: 'member' }, csrf_token: csrf },
          },
  }
})
vi.mock('@/api/auth', async (original) => ({
  ...(await original<typeof import('@/api/auth')>()),
  getSession: vi.fn(),
}))
vi.mock('@/api/playground-team', async (original) => {
  const actual = await original<typeof import('@/api/playground-team')>()
  return {
    ...actual,
    getOwnActiveTeams: vi.fn(),
    getTeamModels: vi.fn(),
    runTeamChat: vi.fn(actual.runTeamChat),
    runTeamResponses: vi.fn(actual.runTeamResponses),
    runTeamMessages: vi.fn(actual.runTeamMessages),
    runTeamGemini: vi.fn(actual.runTeamGemini),
  }
})
vi.mock('@/api/playground', async (original) => ({
  ...(await original<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
  runResponses: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, cache: QueryClient
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const
const models: GatewayModel[] = protocols.map((protocol, index) => ({
  id: ['chat-a', 'responses-b', 'messages-c', 'gemini-d'][index],
  model_id: `mdl_${index}`,
  protocols: [protocol],
  attachment_scope: 'team',
  personal_attachments: false,
  input_capabilities: { [protocol]: [] },
}))
const result: ChatResult = {
  text: 'Chat text',
  requestId: 'req_chat',
  usage: null,
  finishReason: 'stop',
}
function response(path: string): Response {
  const event = (type: string, data: unknown) =>
    `event: ${type}\ndata: ${JSON.stringify({ ...(data as object), type })}\n\n`
  const sse = (data: string) =>
    new Response(data, { headers: { 'Content-Type': 'text/event-stream' } })
  if (path.includes('/responses'))
    return sse(
      event('response.output_text.delta', {
        output_index: 0,
        content_index: 0,
        delta: 'Responses text',
      }) +
        event('response.completed', {
          response: {
            status: 'completed',
            output: [
              {
                type: 'message',
                role: 'assistant',
                content: [{ type: 'output_text', text: 'Responses text' }],
              },
            ],
            usage: { input_tokens: 2, output_tokens: 3, total_tokens: 5 },
          },
        }),
    )
  if (path.includes('/messages'))
    return sse(
      event('message_start', {
        type: 'message_start',
        message: {
          id: 'msg_one',
          type: 'message',
          role: 'assistant',
          content: [],
          usage: {
            input_tokens: 3,
            output_tokens: 0,
            cache_read_input_tokens: 0,
            cache_creation_input_tokens: 0,
          },
        },
      }) +
        event('content_block_start', {
          type: 'content_block_start',
          index: 0,
          content_block: { type: 'text', text: '' },
        }) +
        event('content_block_delta', {
          type: 'content_block_delta',
          index: 0,
          delta: { type: 'text_delta', text: 'Messages text' },
        }) +
        event('content_block_stop', { type: 'content_block_stop', index: 0 }) +
        event('message_delta', {
          type: 'message_delta',
          delta: { stop_reason: 'end_turn' },
          usage: { output_tokens: 4 },
        }) +
        event('message_stop', { type: 'message_stop' }),
    )
  if (path.includes('/models/'))
    return sse(
      `data: ${JSON.stringify({
        candidates: [
          {
            index: 0,
            content: { role: 'model', parts: [{ text: 'Gemini text' }] },
            finishReason: 'STOP',
          },
        ],
        usageMetadata: {
          promptTokenCount: 4,
          candidatesTokenCount: 5,
          totalTokenCount: 9,
          cachedContentTokenCount: 0,
          thoughtsTokenCount: 0,
        },
      })}\n\n`,
    )
  return sse(
    `data: ${JSON.stringify({
      choices: [{ index: 0, delta: { content: 'Chat text' }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 1, completion_tokens: 2, total_tokens: 3 },
    })}\n\ndata: [DONE]\n\n`,
  )
}

beforeEach(async () => {
  actor = 'usr_one'
  csrf = 'csrf-one'
  refreshing = false
  sessionError = false
  realSession = false
  vi.mocked(getSession).mockResolvedValue({
    user: { id: actor, role: 'member', name: 'Applicant', email: 'applicant@example.test' },
    csrf_token: csrf,
  })
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.mocked(getOwnActiveTeams).mockResolvedValue({
    items: [
      {
        id: 'tea_one',
        name: 'Research Team',
        status: 'active',
        description: '',
        created_at: '',
        model_ids: [],
      },
      {
        id: 'tea_two',
        name: 'Second Team',
        status: 'active',
        description: '',
        created_at: '',
        model_ids: [],
      },
    ],
    next_cursor: null,
  })
  vi.mocked(getTeamModels).mockResolvedValue(models)
  vi.mocked(getGatewayModels).mockResolvedValue(models)
  vi.mocked(runChat).mockResolvedValue(result)
  vi.mocked(runResponses).mockResolvedValue({
    ...result,
    responseStatus: 'completed',
    nonTextOutput: false,
  })
  vi.stubGlobal(
    'fetch',
    vi.fn(async (path: RequestInfo | URL) => response(String(path))),
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  const actual =
    await vi.importActual<typeof import('@/api/playground-team')>('@/api/playground-team')
  vi.mocked(runTeamChat).mockImplementation(actual.runTeamChat)
  vi.mocked(runTeamResponses).mockImplementation(actual.runTeamResponses)
  vi.mocked(runTeamMessages).mockImplementation(actual.runTeamMessages)
  vi.mocked(runTeamGemini).mockImplementation(actual.runTeamGemini)
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5))
  })
}
async function render(source: 'key' | 'team' = 'team', teamId = 'tea_one') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CompareWorkbench source={source} teamId={teamId} onSource={vi.fn()} onTeam={vi.fn()} />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
function button(label: string) {
  const found = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await settle()
}
async function fill(name: string, value: string) {
  const input = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
  await act(async () => {
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function send(text = 'Same prompt') {
  await fill('comparison_prompt', text)
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
}
async function select(label: string, value: string) {
  const input = host.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function ready() {
  await render()
  await settle()
  expect(button('Load Team models').disabled).toBe(false)
  await click('Load Team models')
}
function lane(index: number) {
  return host.querySelector<HTMLElement>(`section[aria-label="Comparison ${index}"]`)!
}

describe('Team native comparison', () => {
  it('uses four independent native endpoints and Session CSRF without Keys or media', async () => {
    await ready()
    await click('Add comparison')
    await click('Add comparison')
    await send()
    expect(fetch).toHaveBeenCalledTimes(4)
    expect(vi.mocked(fetch).mock.calls.map(([path]) => path)).toEqual([
      '/api/v1/teams/tea_one/chat/completions',
      '/api/v1/teams/tea_one/responses',
      '/api/v1/teams/tea_one/messages',
      '/api/v1/teams/tea_one/models/gemini-d:streamGenerateContent?alt=sse',
    ])
    for (const [, options] of vi.mocked(fetch).mock.calls) {
      expect(options).toMatchObject({
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': csrf },
      })
      expect(options!.headers).not.toHaveProperty('Authorization')
      expect(options!.headers).not.toHaveProperty('x-api-key')
      expect(options!.headers).not.toHaveProperty('x-goog-api-key')
    }
    expect(vi.mocked(fetch).mock.calls[2][1]!.headers).toHaveProperty(
      'anthropic-version',
      '2023-06-01',
    )
    expect(host.querySelector('input[type="password"]')).toBeNull()
    expect(host.querySelector('input[type="file"]')).toBeNull()
    expect(button('Get code for comparison 1').disabled).toBe(false)
    expect(getGatewayModels).not.toHaveBeenCalled()
    expect(runChat).not.toHaveBeenCalled()
    expect(host.textContent).toContain('Responses text')
    expect(host.textContent).toContain('Messages text')
    expect(host.textContent).toContain('Gemini text')
    expect(lane(1).textContent).toContain('Input 1 · Output 2 · Total 3 Tokens')
    expect(lane(2).textContent).toContain('Input 2 · Output 3 · Total 5 Tokens')
    expect(lane(3).textContent).toContain('Input 3 · Output 4 · Total 7 Tokens')
    expect(lane(4).textContent).toContain('Input 4 · Output 5 · Total 9 Tokens')
    await send('Follow up')
    const requests = vi
      .mocked(fetch)
      .mock.calls.slice(4)
      .map(([, options]) => JSON.parse(options!.body as string))
    expect(requests[0].messages).toHaveLength(3)
    expect(requests[1].input).toHaveLength(3)
    expect(requests[2].messages).toHaveLength(3)
    expect(requests[3].contents).toHaveLength(3)
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })
  it('requires explicit Team selection even with zero prior grants and keeps live bilingual copy', async () => {
    await render('team', '')
    expect(button('Load Team models').disabled).toBe(true)
    expect(getTeamModels).not.toHaveBeenCalled()
    await render('team', 'tea_one')
    await click('Load Team models')
    await fill('comparison_prompt', 'Preserved draft')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('文本对话和模型比较')
    expect(host.textContent).not.toContain('暂不支持附件、模型比较')
    expect(host.querySelector<HTMLTextAreaElement>('textarea')!.value).toBe('Preserved draft')
  })
  it('rotates same-actor CSRF only for a subsequent explicit send', async () => {
    await ready()
    await send()
    csrf = 'csrf-rotated'
    await render()
    expect(fetch).toHaveBeenCalledTimes(2)
    await send('Next')
    expect(vi.mocked(runTeamChat).mock.calls[1][1]).toBe('csrf-rotated')
    expect(vi.mocked(runTeamChat).mock.calls[1][2].messages).toHaveLength(3)
  })
  it.each([401, 403, 404])(
    'clears all Team lanes and aborts siblings on unavailable authority (%i)',
    async (status) => {
      let signal: AbortSignal | undefined, late: ((value: ResponsesResult) => void) | undefined
      vi.mocked(runTeamResponses).mockImplementation((_team, _csrf, _body, abort, update) => {
        signal = abort
        late = update
        return new Promise(() => {})
      })
      vi.mocked(runTeamChat).mockRejectedValue(new GatewayError('Unavailable', '', status))
      await ready()
      await send()
      expect(signal!.aborted).toBe(true)
      await act(async () =>
        late?.({
          ...result,
          text: 'Old private response',
          responseStatus: 'completed',
          nonTextOutput: false,
        }),
      )
      expect(host.textContent).not.toContain('Old private response')
      expect(host.textContent).not.toContain('Same prompt')
      expect(host.querySelector('option[value="chat-a"]')).toBeNull()
      expect(button('Load Team models').disabled).toBe(true)
      expect(host.textContent).toContain('The call could not complete')
      expect(fetch).not.toHaveBeenCalled()
      await click('Refresh Teams')
      expect(button('Load Team models').disabled).toBe(false)
    },
  )
  it.each([new GatewayError('Limited', '', 429), new GatewayError('Native parser rejected')])(
    'keeps native failures local to a lane',
    async (failure) => {
      vi.mocked(runTeamChat).mockRejectedValue(failure)
      await ready()
      await send()
      expect(lane(1).textContent).toContain('Call failed')
      expect(lane(2).textContent).toContain('Completed')
      expect(host.querySelector('option[value="chat-a"]')).not.toBeNull()
      await send('Next')
      expect(vi.mocked(runTeamChat).mock.calls[1][2].messages).toHaveLength(1)
      expect(vi.mocked(runTeamResponses).mock.calls[1][2].input).toHaveLength(3)
    },
  )
  it.each(['actor', 'source', 'team', 'refresh', 'error'] as const)(
    'aborts and forgets private histories on %s changes',
    async (change) => {
      let signal: AbortSignal | undefined, late: ((value: ChatResult) => void) | undefined
      vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _body, abort, update) => {
        signal = abort
        late = update
        return new Promise(() => {})
      })
      await ready()
      await send()
      if (change === 'actor') actor = 'usr_two'
      if (change === 'refresh') refreshing = true
      if (change === 'error') sessionError = true
      await render(change === 'source' ? 'key' : 'team', change === 'team' ? 'tea_two' : 'tea_one')
      expect(signal!.aborted).toBe(true)
      await act(async () => late?.({ ...result, text: 'Old private response' }))
      expect(host.textContent).not.toContain('Old private response')
      expect(host.textContent).not.toContain('Responses text')
      expect(host.querySelector('option[value="chat-a"]')).toBeNull()
      expect(fetch).toHaveBeenCalledTimes(1)
    },
  )
  it('ignores discovery that settles after authority renewal, and refresh failure cannot restore old models', async () => {
    await ready()
    await send()
    let resolve!: (items: GatewayModel[]) => void
    vi.mocked(getTeamModels).mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      }),
    )
    await click('Load Team models')
    expect(host.querySelector('option[value="chat-a"]')).toBeNull()
    refreshing = true
    await render()
    await act(async () => resolve(models))
    expect(host.querySelector('option[value="chat-a"]')).toBeNull()
    refreshing = false
    await render()
    await settle()
    vi.mocked(getTeamModels).mockRejectedValueOnce(new Error('Discovery unavailable'))
    await click('Load Team models')
    expect(host.querySelector('option[value="chat-a"]')).toBeNull()
    expect(host.textContent).not.toContain('Responses text')
  })
  it('aborts inference during renewed membership and rejects an obsolete membership response', async () => {
    let signal: AbortSignal | undefined
    vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _body, abort) => {
      signal = abort
      return new Promise(() => {})
    })
    await ready()
    await send()
    vi.mocked(getOwnActiveTeams).mockRejectedValueOnce(new Error('No longer a member'))
    await click('Refresh Teams')
    expect(signal!.aborted).toBe(true)
    expect(host.querySelector('option[value="chat-a"]')).toBeNull()
    expect(host.textContent).not.toContain('Responses text')
    expect(button('Load Team models').disabled).toBe(true)
  })
  it('blocks duplicate sends and ignores updates/results after a lane cancellation', async () => {
    let resolve!: (value: ChatResult) => void, late!: (value: ChatResult) => void
    vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _body, _abort, update) => {
      late = update
      return new Promise((done) => {
        resolve = done
      })
    })
    await ready()
    await fill('comparison_prompt', 'Once')
    await act(async () => {
      const form = host.querySelector('form')!
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(runTeamChat).toHaveBeenCalledOnce()
    await click('Stop comparison 1')
    await act(async () => {
      late({ ...result, text: 'Late text' })
      resolve({ ...result, text: 'Late final' })
    })
    expect(lane(1).textContent).toContain('Stopped')
    expect(lane(1).textContent).not.toContain('Late')
    expect(lane(2).textContent).toContain('Completed')
  })
  it('independently removes or changes a running lane without late completion', async () => {
    let late!: (value: ChatResult) => void
    vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _body, _abort, update) => {
      late = update
      return new Promise(() => {})
    })
    await ready()
    await click('Add comparison')
    await send()
    await select('Comparison model 1', 'responses-b')
    await act(async () => late({ ...result, text: 'Old target reply' }))
    expect(host.textContent).not.toContain('Old target reply')
    expect(lane(1).querySelector('article')).toBeNull()
    expect(lane(2).textContent).toContain('Completed')
    await click('Remove comparison 3')
    expect(host.querySelectorAll('section')).toHaveLength(2)
  })
  it.each(['key', 'team'] as const)(
    'keeps truncation, handoff, blocking and non-text Responses outside %s history',
    async (source) => {
      if (source === 'team') await ready()
      else {
        await render('key')
        await fill('comparison_key', 'transient-key')
        await click('Verify and load models')
      }
      const chat = source === 'team' ? vi.mocked(runTeamChat) : vi.mocked(runChat)
      const responses = source === 'team' ? vi.mocked(runTeamResponses) : vi.mocked(runResponses)
      for (const finishReason of ['length', 'tool_calls', 'content_filter']) {
        chat.mockResolvedValueOnce({ ...result, finishReason })
        responses.mockResolvedValueOnce({
          ...result,
          responseStatus: 'completed',
          nonTextOutput: true,
        })
        await send(finishReason)
      }
      chat.mockResolvedValueOnce(result)
      responses.mockResolvedValueOnce({
        ...result,
        responseStatus: 'completed',
        nonTextOutput: false,
        refused: true,
      })
      await send('Refusal')
      const latestChat = chat.mock.calls.at(-1)!,
        latestResponses = responses.mock.calls.at(-1)!
      expect((source === 'team' ? latestChat[2] : latestChat[1]) as unknown).toHaveProperty(
        'messages',
        [{ role: 'user', content: 'Refusal' }],
      )
      expect(
        (source === 'team' ? latestResponses[2] : latestResponses[1]) as unknown,
      ).toHaveProperty('input', [{ role: 'user', content: 'Refusal' }])
      expect(lane(1).textContent).toContain('Incomplete')
      expect(lane(1).textContent).toContain('Action required')
      expect(lane(1).textContent).toContain('Refused')
      expect(lane(2).textContent).toContain('Action required')
      expect(lane(2).textContent).toContain('Refused')
    },
  )
  it('reaches Team comparison through the normal tabs and cancels when leaving the tab', async () => {
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter initialEntries={['/playground?team=tea_one']}>
            <PlaygroundPage />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    )
    await settle()
    await click('Model comparison')
    await click('Load Team models')
    let signal: AbortSignal | undefined
    vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _body, abort) => {
      signal = abort
      return new Promise(() => {})
    })
    await send()
    await click('Model conversation')
    expect(signal!.aborted).toBe(true)
    await click('Model comparison')
    expect(host.querySelector('option[value="chat-a"]')).toBeNull()
  })
})

it.each(protocols)(
  'does not replay partial native %s streams into subsequent history',
  async (protocol) => {
    await ready()
    const index = protocols.indexOf(protocol)
    await select('Comparison model 1', models[index].id)
    const partial =
      protocol === 'openai_chat'
        ? 'data: {"choices":[{"index":0,"delta":{"content":"Partial"},"finish_reason":null}]}\n\n'
        : protocol === 'openai_responses'
          ? 'event: response.output_text.delta\ndata: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Partial"}\n\n'
          : protocol === 'anthropic_messages'
            ? 'event: message_start\ndata: {"type":"message_start","message":{"type":"message","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}\n\nevent: content_block_start\ndata: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}\n\nevent: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial"}}\n\n'
            : 'data: {"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Partial"}]}}]}\n\n'
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(partial, { headers: { 'Content-Type': 'text/event-stream' } }),
    )
    await send('Partial turn')
    expect(lane(1).textContent).not.toContain('Completed')
    expect(lane(1).textContent).toContain('Partial')
    await send('New turn')
    const request = JSON.parse(vi.mocked(fetch).mock.calls[2][1]!.body as string)
    const history =
      protocol === 'gemini_generate_content'
        ? request.contents
        : protocol === 'openai_responses'
          ? request.input
          : request.messages
    expect(history).toHaveLength(1)
    expect(JSON.stringify(history)).toContain('New turn')
    expect(JSON.stringify(history)).not.toContain('Partial turn')
  },
)

it.each(['anthropic_messages', 'gemini_generate_content'] as const)(
  'keeps native %s handoff and blocking outside history',
  async (protocol) => {
    await ready()
    await select('Comparison model 1', models[protocols.indexOf(protocol)].id)
    const native = (blocked: boolean) =>
      protocol === 'anthropic_messages'
        ? `event: message_start\ndata: ${JSON.stringify({ type: 'message_start', message: { type: 'message', content: [], usage: { input_tokens: 1, output_tokens: 0 } } })}\n\nevent: message_delta\ndata: ${JSON.stringify({ type: 'message_delta', delta: { stop_reason: blocked ? 'refusal' : 'tool_use' }, usage: { output_tokens: 0 } })}\n\nevent: message_stop\ndata: {"type":"message_stop"}\n\n`
        : `data: ${JSON.stringify(blocked ? { promptFeedback: { blockReason: 'SAFETY' } } : { candidates: [{ index: 0, content: { role: 'model', parts: [{ functionCall: { name: 'next', args: {} } }] }, finishReason: 'STOP' }] })}\n\n`
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(native(false), { headers: { 'Content-Type': 'text/event-stream' } }),
    )
    await send('Handoff turn')
    expect(lane(1).textContent).toContain('Action required')
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(native(true), { headers: { 'Content-Type': 'text/event-stream' } }),
    )
    await send('Blocked turn')
    expect(lane(1).textContent).toContain('Refused')
    await send('Fresh turn')
    const request = JSON.parse(vi.mocked(fetch).mock.calls[4][1]!.body as string)
    expect(protocol === 'anthropic_messages' ? request.messages : request.contents).toHaveLength(1)
  },
)

it('clears stale Team catalogs before a fast same-target membership refresh and requires explicit model discovery', async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1780000000000)
  await ready()
  await send()
  await click('Refresh Teams')
  expect(host.querySelector('option[value="chat-a"]')).toBeNull()
  expect(host.textContent).not.toContain('Responses text')
  expect(button('Load Team models').disabled).toBe(false)
  expect(getTeamModels).toHaveBeenCalledOnce()
  await click('Load Team models')
  expect(getTeamModels).toHaveBeenCalledTimes(2)
  expect(host.querySelector('option[value="chat-a"]')).not.toBeNull()
  expect(fetch).toHaveBeenCalledTimes(2)
})

it('ignores callbacks from a removed running lane and from a changed native protocol', async () => {
  let removedLate!: (value: import('@/types/playground').MessagesResult) => void,
    removedSignal!: AbortSignal
  vi.mocked(runTeamMessages).mockImplementation((_team, _csrf, _body, signal, update) => {
    removedLate = update
    removedSignal = signal
    return new Promise(() => {})
  })
  vi.mocked(getTeamModels).mockResolvedValueOnce([
    {
      ...models[0],
      protocols: ['openai_chat', 'openai_responses'],
      input_capabilities: { openai_chat: [], openai_responses: [] },
    },
    ...models.slice(1),
  ])
  await ready()
  await click('Add comparison')
  let chatLate!: (value: ChatResult) => void, chatSignal!: AbortSignal
  vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _body, signal, update) => {
    chatLate = update
    chatSignal = signal
    return new Promise(() => {})
  })
  await send()
  await click('Remove comparison 3')
  expect(removedSignal.aborted).toBe(true)
  await select('Comparison protocol 1', 'openai_responses')
  expect(chatSignal.aborted).toBe(true)
  await act(async () => {
    removedLate({
      ...result,
      text: 'Removed private reply',
      messageStatus: 'completed',
      nonTextOutput: false,
    })
    chatLate({ ...result, text: 'Old protocol reply' })
  })
  expect(host.textContent).not.toContain('Removed private reply')
  expect(host.textContent).not.toContain('Old protocol reply')
  expect(lane(1).querySelector('article')).toBeNull()
  expect(lane(2).textContent).toContain('Responses text')
})

it('uses one real Session observer with bounded renewed reads, retained draft and no replay', async () => {
  realSession = true
  await ready()
  await send()
  await fill('comparison_prompt', 'Keep my draft')
  expect(getSession).toHaveBeenCalledOnce()
  let resolve!: (session: Awaited<ReturnType<typeof getSession>>) => void
  vi.mocked(getSession).mockReturnValueOnce(
    new Promise((done) => {
      resolve = done
    }),
  )
  let refresh!: Promise<void>
  await act(async () => {
    refresh = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  await settle()
  expect(host.querySelector('option[value="chat-a"]')).toBeNull()
  expect(host.textContent).not.toContain('Responses text')
  expect(host.querySelector<HTMLTextAreaElement>('textarea')!.value).toBe('Keep my draft')
  await act(async () => {
    resolve({
      user: { id: actor, role: 'member', name: 'Applicant', email: 'applicant@example.test' },
      csrf_token: 'fresh-csrf',
    })
    await refresh
  })
  await settle()
  await settle()
  expect(getSession).toHaveBeenCalledTimes(2)
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(getTeamModels).toHaveBeenCalledOnce()
  expect(host.querySelector('option[value="chat-a"]')).toBeNull()
  expect(host.querySelector<HTMLTextAreaElement>('textarea')!.value).toBe('Keep my draft')
  await click('Load Team models')
  expect(host.querySelector<HTMLTextAreaElement>('textarea')!.value).toBe('Keep my draft')
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  expect(vi.mocked(runTeamChat).mock.calls[1][1]).toBe('fresh-csrf')
  expect(vi.mocked(runTeamChat).mock.calls[1][2].messages).toEqual([
    { role: 'user', content: 'Keep my draft' },
  ])
  expect(getSession).toHaveBeenCalledTimes(2)
})
