import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChatWorkbench from './chat'
import i18n from '@/i18n'
import {
  getGatewayModels,
  readChatResponse,
  readResponsesResponse,
  runChat,
  runResponses,
  runMessages,
  runGemini,
} from '@/api/playground'
import {
  getOwnActiveTeams,
  getTeamModels,
  runTeamChat,
  runTeamResponses,
  runTeamMessages,
  runTeamGemini,
} from '@/api/playground-team'
import type {
  ChatResult,
  ResponsesResult,
  MessagesResult,
  GeminiResult,
  PlaygroundProtocol,
} from '@/types/playground'

vi.mock('@/hooks/use-auth', () => ({
  useSession: () => ({
    data: { user: { id: 'usr_finality', role: 'member' }, csrf_token: 'csrf-finality' },
  }),
}))
vi.mock('@/api/playground', async (original) => ({
  ...(await original<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
  runResponses: vi.fn(),
  runMessages: vi.fn(),
  runGemini: vi.fn(),
}))
vi.mock('@/api/playground-team', async (original) => ({
  ...(await original<typeof import('@/api/playground-team')>()),
  getOwnActiveTeams: vi.fn(),
  getTeamModels: vi.fn(),
  runTeamChat: vi.fn(),
  runTeamResponses: vi.fn(),
  runTeamMessages: vi.fn(),
  runTeamGemini: vi.fn(),
}))

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
type NativeResult = ChatResult & ResponsesResult & MessagesResult & GeminiResult
const completed: NativeResult = {
  text: 'Completed text',
  requestId: 'req_finality',
  usage: null,
  finishReason: 'stop',
  responseStatus: 'completed',
  messageStatus: 'completed',
  generationStatus: 'completed',
  nonTextOutput: false,
}
const knownUsage = { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 }
let root: Root, host: HTMLDivElement, cache: QueryClient
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.mocked(getOwnActiveTeams).mockResolvedValue({
    items: [
      {
        id: 'tem_finality',
        name: 'Finality Team',
        status: 'active',
        description: '',
        created_at: '2026-10-04T00:00:00Z',
        model_ids: [],
      },
    ],
    next_cursor: null,
  })
  for (const execute of [runChat, runResponses, runMessages, runGemini])
    vi.mocked(execute).mockResolvedValue(completed)
  for (const execute of [runTeamChat, runTeamResponses, runTeamMessages, runTeamGemini])
    vi.mocked(execute).mockResolvedValue(completed)
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  vi.resetAllMocks()
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5))
  })
}
async function fill(name: string, value: string) {
  await act(async () => {
    const input = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
    const prototype =
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function click(text: string) {
  await act(async () => {
    const button = [...host.querySelectorAll('button')].find((item) => item.textContent === text)!
    expect(button.disabled).toBe(false)
    button.click()
  })
  await settle()
}
async function ready(source: 'key' | 'team', protocol: PlaygroundProtocol) {
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'native-model', protocols: [protocol] }])
  vi.mocked(getTeamModels).mockResolvedValue([
    { id: 'native-model', model_id: 'mdl_native', protocols: [protocol] },
  ])
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ChatWorkbench source={source} teamId="tem_finality" />
      </QueryClientProvider>,
    ),
  )
  await settle()
  if (source === 'key') await fill('api_key', 'rx_finality-transient')
  await click(source === 'key' ? 'Verify and load models' : 'Load Team models')
}
async function submit(prompt: string) {
  await fill('prompt', prompt)
  await act(async () => {
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
function transport(source: 'key' | 'team', protocol: PlaygroundProtocol) {
  const key = {
    openai_chat: runChat,
    openai_responses: runResponses,
    anthropic_messages: runMessages,
    gemini_generate_content: runGemini,
  }
  const team = {
    openai_chat: runTeamChat,
    openai_responses: runTeamResponses,
    anthropic_messages: runTeamMessages,
    gemini_generate_content: runTeamGemini,
  }
  return vi.mocked(source === 'key' ? key[protocol] : team[protocol])
}
function submittedHistory(source: 'key' | 'team', protocol: PlaygroundProtocol) {
  const body = transport(source, protocol).mock.calls[1][
    source === 'key' ? 1 : 2
  ] as unknown as Record<string, unknown>
  if (protocol === 'gemini_generate_content') return body.contents
  return protocol === 'openai_responses' ? body.input : body.messages
}
async function codeHistory(source: 'key' | 'team', expected: string[], excluded: string[]) {
  const button = [...host.querySelectorAll('button')].find(
    (item) => item.textContent === 'Get code',
  )!
  if (source === 'team') {
    expect(button.disabled).toBe(true)
    return
  }
  await click('Get code')
  const code = document.querySelector('pre')!.textContent!
  for (const value of expected) expect(code).toContain(value)
  for (const value of excluded) expect(code).not.toContain(value)
  expect(code).not.toContain('rx_finality-transient')
}

const rejected: {
  protocol: PlaygroundProtocol
  name: string
  status: string
  patch: Partial<NativeResult>
}[] = [
  ...(['length', 'tool_calls', 'function_call', 'content_filter', null] as const).map(
    (finishReason) => ({
      protocol: 'openai_chat' as const,
      name: `Chat ${finishReason}`,
      status:
        finishReason === 'content_filter'
          ? 'Refused'
          : finishReason === 'tool_calls' || finishReason === 'function_call'
            ? 'Action required'
            : 'Incomplete',
      patch: { finishReason },
    }),
  ),
  {
    protocol: 'openai_chat',
    name: 'Chat refusal with stop',
    status: 'Refused',
    patch: { refused: true },
  },
  ...(['failed', 'incomplete', 'queued', 'in_progress'] as const).map((responseStatus) => ({
    protocol: 'openai_responses' as const,
    name: `Responses ${responseStatus}`,
    status:
      responseStatus === 'failed'
        ? 'Call failed'
        : responseStatus === 'incomplete'
          ? 'Incomplete'
          : 'Accepted, not completed',
    patch: { responseStatus },
  })),
  {
    protocol: 'openai_responses',
    name: 'Responses failed refusal',
    status: 'Call failed',
    patch: { responseStatus: 'failed', refused: true },
  },
  ...(['openai_responses', 'anthropic_messages', 'gemini_generate_content'] as const).flatMap(
    (protocol) => [
      {
        protocol,
        name: `${protocol} non-text completion`,
        status: 'Action required',
        patch: { nonTextOutput: true },
      },
      {
        protocol,
        name: `${protocol} refusal completion`,
        status: 'Refused',
        patch: { refused: true },
      },
    ],
  ),
  ...(
    ['openai_chat', 'openai_responses', 'anthropic_messages', 'gemini_generate_content'] as const
  ).map((protocol) => ({
    protocol,
    name: `${protocol} empty completion`,
    status: 'Incomplete',
    patch: { text: '   ' },
  })),
]

describe.each(['key', 'team'] as const)('%s conversation native finality', (source) => {
  it.each(rejected)(
    'keeps $name visible with usage while excluding it from history and code',
    async ({ protocol, patch, status }) => {
      const result = { ...completed, text: 'Non-completed answer', usage: knownUsage, ...patch }
      transport(source, protocol).mockResolvedValueOnce(result)
      await ready(source, protocol)
      await submit('Excluded turn')
      expect(host.textContent).toContain(status)
      expect(host.textContent).toContain('req_finality')
      expect(host.textContent).toContain('Input 3 · Output 2 · Total 5 Tokens')
      if (result.text.trim()) expect(host.textContent).toContain(result.text)
      await submit('Completed turn')
      expect(transport(source, protocol)).toHaveBeenCalledTimes(2)
      expect(submittedHistory(source, protocol)).toEqual(
        protocol === 'gemini_generate_content'
          ? [{ role: 'user', parts: [{ text: 'Completed turn' }] }]
          : [{ role: 'user', content: 'Completed turn' }],
      )
      await fill('prompt', 'Next draft')
      await codeHistory(
        source,
        ['Completed turn', 'Completed text', 'Next draft'],
        ['Excluded turn', 'Non-completed answer'],
      )
    },
  )
  it.each([
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
    'gemini_generate_content',
  ] as const)('retains completed %s text without inventing missing usage', async (protocol) => {
    await ready(source, protocol)
    await submit('Completed turn')
    expect(host.textContent).toContain('Completed')
    expect(host.textContent).toContain('The upstream returned no usage data')
    await submit('Follow-up')
    expect(submittedHistory(source, protocol)).toEqual(
      protocol === 'gemini_generate_content'
        ? [
            { role: 'user', parts: [{ text: 'Completed turn' }] },
            { role: 'model', parts: [{ text: 'Completed text' }] },
            { role: 'user', parts: [{ text: 'Follow-up' }] },
          ]
        : [
            { role: 'user', content: 'Completed turn' },
            { role: 'assistant', content: 'Completed text' },
            { role: 'user', content: 'Follow-up' },
          ],
    )
    await fill('prompt', 'Next draft')
    await codeHistory(source, ['Completed turn', 'Completed text', 'Follow-up', 'Next draft'], [])
  })
})

it.each([false, true])(
  'excludes a parsed native Chat length result (stream=%s) despite known usage',
  async (stream) => {
    vi.mocked(runChat).mockImplementationOnce((_key, request, signal, update) => {
      const body = {
        choices: [
          {
            index: 0,
            [stream ? 'delta' : 'message']: { content: 'Truncated native' },
            finish_reason: 'length',
          },
        ],
        usage: knownUsage,
      }
      const response = new Response(
        stream ? `data: ${JSON.stringify(body)}\n\ndata: [DONE]\n\n` : JSON.stringify(body),
        {
          headers: {
            'Content-Type': stream ? 'text/event-stream' : 'application/json',
            'X-Request-ID': 'req_native_length',
          },
        },
      )
      return readChatResponse(response, request, signal, update)
    })
    await ready('key', 'openai_chat')
    if (!stream)
      await act(async () => host.querySelector<HTMLInputElement>('[name="stream"]')!.click())
    await submit('Excluded native')
    expect(host.textContent).toContain('Incomplete')
    expect(host.textContent).toContain('Truncated native')
    expect(host.textContent).toContain('Input 3 · Output 2 · Total 5 Tokens')
    await submit('Completed turn')
    expect(submittedHistory('key', 'openai_chat')).toEqual([
      { role: 'user', content: 'Completed turn' },
    ])
    await codeHistory('key', ['Completed turn'], ['Excluded native', 'Truncated native'])
  },
)

it('excludes a parsed native Responses completed tool item from the next input and generated code', async () => {
  vi.mocked(runResponses).mockImplementationOnce((_key, request, signal, update) =>
    readResponsesResponse(
      new Response(
        JSON.stringify({
          status: 'completed',
          output: [
            { type: 'message', content: [{ type: 'output_text', text: 'Tool handoff text' }] },
            { type: 'function_call', name: 'opaque', arguments: '{}' },
          ],
          usage: { input_tokens: 3, output_tokens: 2, total_tokens: 5 },
        }),
        { headers: { 'Content-Type': 'application/json' } },
      ),
      request,
      signal,
      update,
    ),
  )
  await ready('key', 'openai_responses')
  await act(async () => host.querySelector<HTMLInputElement>('[name="stream"]')!.click())
  await submit('Excluded native')
  expect(host.textContent).toContain('Action required')
  expect(host.textContent).toContain('Tool handoff text')
  expect(host.textContent).toContain('Input 3 · Output 2 · Total 5 Tokens')
  await submit('Completed turn')
  expect(submittedHistory('key', 'openai_responses')).toEqual([
    { role: 'user', content: 'Completed turn' },
  ])
  await codeHistory('key', ['Completed turn'], ['Excluded native', 'Tool handoff text'])
})
