import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ChatWorkbench from './chat'
import i18n from '@/i18n'
import { getGatewayModels, runChat, runResponses, runMessages, runGemini } from '@/api/playground'
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
    data: { user: { id: 'usr_parameters', role: 'member' }, csrf_token: 'csrf-parameters' },
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
  text: 'Preserved completed reply',
  requestId: 'req_parameters',
  usage: null,
  finishReason: 'stop',
  responseStatus: 'completed',
  messageStatus: 'completed',
  generationStatus: 'completed',
  nonTextOutput: false,
}
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
        id: 'tea_parameters',
        name: 'Parameter Team',
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
  await i18n.changeLanguage('en')
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5))
  })
}
function button(label: string) {
  const item = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(item).toBeDefined()
  return item!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await settle()
}
function field(name: string) {
  return host.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
}
async function fill(name: string, value: string) {
  await act(async () => {
    const input = field(name)
    Object.getOwnPropertyDescriptor(
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function ready(source: 'key' | 'team', protocol: PlaygroundProtocol = 'openai_chat') {
  const model = { id: 'parameter-model', protocols: [protocol] }
  vi.mocked(getGatewayModels).mockResolvedValue([model])
  vi.mocked(getTeamModels).mockResolvedValue([{ ...model, model_id: 'mdl_parameters' }])
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ChatWorkbench source={source} teamId="tea_parameters" />
      </QueryClientProvider>,
    ),
  )
  await settle()
  if (source === 'key') await fill('api_key', 'rx_parameters-transient')
  await click(source === 'key' ? 'Verify and load models' : 'Load Team models')
}
async function customize() {
  await fill('temperature', '0.2')
  await fill('top_p', '0.4')
  await fill('max_tokens', '73')
  await fill('system', 'Custom system instruction')
}
function parameters() {
  return ['temperature', 'top_p', 'max_tokens', 'system'].map((name) => field(name).value)
}
async function submit() {
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
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
function payload(source: 'key' | 'team', protocol: PlaygroundProtocol, index: number) {
  return transport(source, protocol).mock.calls[index][
    source === 'key' ? 1 : 2
  ] as unknown as Record<string, unknown>
}

describe.each(['key', 'team'] as const)('%s conversation parameter reset', (source) => {
  it.each([
    'openai_chat',
    'openai_responses',
    'anthropic_messages',
    'gemini_generate_content',
  ] as const)(
    'restores only parameter defaults in the next %s request while keeping local context',
    async (protocol) => {
      await ready(source, protocol)
      await customize()
      await act(async () => field('stream').click())
      await fill('prompt', 'Preserved completed turn')
      await submit()
      await fill('prompt', 'Preserved unsent draft')
      const oldPayload = structuredClone(payload(source, protocol, 0))
      const discoveryCount =
        source === 'key'
          ? vi.mocked(getGatewayModels).mock.calls.length
          : vi.mocked(getTeamModels).mock.calls.length
      await click('Restore default parameters')
      expect(parameters()).toEqual(['0.7', '1', '2048', ''])
      expect(field('prompt').value).toBe('Preserved unsent draft')
      expect((field('stream') as HTMLInputElement).checked).toBe(false)
      expect(host.querySelector<HTMLSelectElement>('[name="source"]')!.value).toBe(source)
      expect(host.querySelector<HTMLSelectElement>('[name="model"]')!.value).toBe('parameter-model')
      expect(host.querySelector<HTMLSelectElement>('[name="protocol"]')!.value).toBe(protocol)
      expect(host.querySelector('[role="log"]')!.textContent).toContain('Preserved completed turn')
      expect(host.querySelector('[role="log"]')!.textContent).toContain(completed.text)
      expect(host.textContent).toContain('Model parameters reset to defaults.')
      expect(transport(source, protocol)).toHaveBeenCalledTimes(1)
      expect(payload(source, protocol, 0)).toEqual(oldPayload)
      expect(source === 'key' ? getGatewayModels : getTeamModels).toHaveBeenCalledTimes(
        discoveryCount,
      )
      if (source === 'key') {
        expect(field('api_key').value).toBe('rx_parameters-transient')
        await click('Get code')
        const code = document.querySelector('pre')!.textContent!
        expect(code).toMatch(/"temperature":\s*0\.7/)
        expect(code).toMatch(
          protocol === 'gemini_generate_content' ? /"topP":\s*1/ : /"top_p":\s*1/,
        )
        expect(code).toMatch(
          new RegExp(
            `"${protocol === 'gemini_generate_content' ? 'maxOutputTokens' : protocol === 'openai_responses' ? 'max_output_tokens' : protocol === 'anthropic_messages' ? 'max_tokens' : 'max_completion_tokens'}":\\s*2048`,
          ),
        )
        expect(code).toContain('Preserved completed turn')
        expect(code).toContain('Preserved unsent draft')
        expect(code).not.toContain('Custom system instruction')
        expect(code).not.toContain('rx_parameters-transient')
        await click('Close')
      } else {
        expect(host.querySelector('[name="api_key"]')).toBeNull()
        expect(getGatewayModels).not.toHaveBeenCalled()
      }
      await submit()
      expect(transport(source, protocol)).toHaveBeenCalledTimes(2)
      const body = payload(source, protocol, 1)
      expect(body.model).toBe('parameter-model')
      expect(body.stream).toBe(false)
      if (protocol === 'gemini_generate_content') {
        expect(body.generationConfig).toEqual({
          temperature: 0.7,
          topP: 1,
          maxOutputTokens: 2048,
          candidateCount: 1,
        })
        expect(body.systemInstruction).toBeUndefined()
      } else {
        expect(body.temperature).toBe(0.7)
        expect(body.top_p).toBe(1)
        expect(
          body[
            protocol === 'openai_responses'
              ? 'max_output_tokens'
              : protocol === 'anthropic_messages'
                ? 'max_tokens'
                : 'max_completion_tokens'
          ],
        ).toBe(2048)
        expect(body.instructions).toBeUndefined()
        expect(body.system).toBeUndefined()
      }
      const nativeHistory = JSON.stringify(body.contents ?? body.input ?? body.messages)
      expect(nativeHistory).toContain('Preserved completed turn')
      expect(nativeHistory).toContain(completed.text)
      expect(nativeHistory).toContain('Preserved unsent draft')
      expect(nativeHistory).not.toContain('Custom system instruction')
      if (source === 'team')
        expect(transport(source, protocol).mock.calls[1].slice(0, 2)).toEqual([
          'tea_parameters',
          'csrf-parameters',
        ])
    },
  )

  it('disables reset during an in-flight request without changing its captured parameters', async () => {
    let finish!: (result: NativeResult) => void
    transport(source, 'openai_chat').mockImplementationOnce(
      () =>
        new Promise<NativeResult>((resolve) => {
          finish = resolve
        }),
    )
    await ready(source)
    await customize()
    await fill('prompt', 'Captured pending request')
    await submit()
    const captured = structuredClone(payload(source, 'openai_chat', 0))
    expect(button('Restore default parameters').disabled).toBe(true)
    await click('Restore default parameters')
    expect(parameters()).toEqual(['0.2', '0.4', '73', 'Custom system instruction'])
    expect(payload(source, 'openai_chat', 0)).toEqual(captured)
    expect(transport(source, 'openai_chat')).toHaveBeenCalledTimes(1)
    await act(async () => finish(completed))
    await click('Restore default parameters')
    expect(parameters()).toEqual(['0.7', '1', '2048', ''])
    expect(payload(source, 'openai_chat', 0)).toEqual(captured)
  })
})

it('updates reset feedback on language change without clearing a draft or other selections', async () => {
  await ready('key')
  await customize()
  await fill('prompt', 'Unsent bilingual draft')
  await click('Restore default parameters')
  await act(async () => i18n.changeLanguage('zh'))
  expect(button('恢复默认参数').title).toBe('恢复默认参数')
  expect(host.textContent).toContain('模型参数已恢复默认值。')
  expect(host.textContent).not.toContain('Model parameters reset to defaults.')
  expect(field('prompt').value).toBe('Unsent bilingual draft')
  expect(field('api_key').value).toBe('rx_parameters-transient')
  expect(parameters()).toEqual(['0.7', '1', '2048', ''])
  expect(runChat).not.toHaveBeenCalled()
  await fill('temperature', '0.3')
  expect(host.textContent).not.toContain('模型参数已恢复默认值。')
})
