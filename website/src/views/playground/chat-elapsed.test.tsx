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
import { deleteAttachment, uploadAttachment } from '@/api/attachments'
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

vi.mock('@/api/attachments', async (original) => ({
  ...(await original<typeof import('@/api/attachments')>()),
  uploadAttachment: vi.fn(),
  deleteAttachment: vi.fn(),
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
let clock = 1000
beforeEach(async () => {
  await i18n.changeLanguage('en')
  clock = 1000
  vi.spyOn(performance, 'now').mockImplementation(() => clock)
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
  vi.restoreAllMocks()
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
async function ready(
  source: 'key' | 'team',
  protocol: PlaygroundProtocol = 'openai_chat',
  media = false,
) {
  const model = {
    id: 'parameter-model',
    protocols: [protocol],
    ...(media
      ? {
          attachment_scope: 'user' as const,
          input_capabilities: { openai_chat: ['image' as const] },
        }
      : {}),
  }
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
function elapsed() {
  return [...host.querySelectorAll<HTMLSpanElement>('span[title]')].find(
    (item) => item.title === i18n.t('playground:browserElapsedHelp'),
  )
}
function pending(source: 'key' | 'team' = 'key', protocol: PlaygroundProtocol = 'openai_chat') {
  let finish!: (result: NativeResult) => void
  transport(source, protocol).mockImplementationOnce(
    () =>
      new Promise<NativeResult>((resolve) => {
        finish = resolve
      }),
  )
  return (result = completed) => act(async () => finish(result))
}

describe.each(['key', 'team'] as const)('%s browser elapsed', (source) => {
  for (const stream of [true, false]) {
    it.each([
      'openai_chat',
      'openai_responses',
      'anthropic_messages',
      'gemini_generate_content',
    ] as const)(
      `measures %s ${stream ? 'stream' : 'ordinary'} settlement without changing native request`,
      async (protocol) => {
        const finish = pending(source, protocol)
        await ready(source, protocol)
        if (!stream) await act(async () => field('stream').click())
        await fill('prompt', 'Elapsed request')
        clock = 2500
        await submit()
        expect(elapsed()).toBeUndefined()
        expect(transport(source, protocol)).toHaveBeenCalledTimes(1)
        const args = transport(source, protocol).mock.calls[0]
        const body = args[source === 'key' ? 1 : 2] as unknown as Record<string, unknown>
        expect(body.model).toBe('parameter-model')
        expect(body.stream).toBe(stream)
        expect(body.elapsedMs).toBeUndefined()
        if (source === 'team')
          expect(args.slice(0, 2)).toEqual(['tea_parameters', 'csrf-parameters'])
        else expect(args[0]).toBe('rx_parameters-transient')
        clock = 2845
        await finish()
        expect(elapsed()?.textContent).toBe('Browser elapsed: 345 ms')
        expect(elapsed()?.title).toContain('not Provider latency or time to first token')
        expect(host.querySelector('[role="log"]')!.textContent).toContain('Completed')
        clock = 9000
        await fill('prompt', 'Unsent draft')
        await click('Restore default parameters')
        expect(elapsed()?.textContent).toBe('Browser elapsed: 345 ms')
        expect(field('prompt').value).toBe('Unsent draft')
        expect(transport(source, protocol)).toHaveBeenCalledTimes(1)
      },
    )
  }
})

it('does not invent a running duration from streamed updates', async () => {
  let progress!: (value: ChatResult) => void
  let finish!: (result: ChatResult) => void
  vi.mocked(runChat).mockImplementationOnce((_key, _body, _signal, onUpdate) => {
    progress = onUpdate!
    return new Promise((resolve) => {
      finish = resolve
    })
  })
  await ready('key')
  await fill('prompt', 'Streaming')
  await submit()
  clock = 1500
  await act(async () => progress({ ...completed, text: 'Partial native content' }))
  expect(host.textContent).toContain('Partial native content')
  expect(elapsed()).toBeUndefined()
  clock = 1800
  await act(async () => finish(completed))
  expect(elapsed()?.textContent).toBe('Browser elapsed: 800 ms')
})

it('ends timing at cancellation and does not replace it with a late settlement', async () => {
  const finish = pending()
  await ready('key')
  await fill('prompt', 'Cancel')
  await submit()
  clock = 1127
  await click('Stop generation')
  expect(elapsed()?.textContent).toBe('Browser elapsed: 127 ms')
  clock = 4000
  await finish()
  expect(elapsed()?.textContent).toBe('Browser elapsed: 127 ms')
  expect(host.querySelector('[role="log"]')!.textContent).toContain('Stopped')
  expect(host.querySelector('[role="log"]')!.textContent).not.toContain('Preserved completed reply')
  expect(runChat).toHaveBeenCalledTimes(1)
})

it.each([
  ['zero', 1000, 'Browser elapsed: 0 ms'],
  ['invalid', Number.NaN, null],
  ['backwards', 900, null],
] as const)(
  'keeps %s observation distinct from a fabricated duration',
  async (_kind, end, expected) => {
    const finish = pending()
    await ready('key')
    await fill('prompt', 'Clock boundary')
    await submit()
    clock = end
    await finish()
    expect(elapsed()?.textContent ?? null).toBe(expected)
  },
)

it.each([
  ['incomplete', { finishReason: 'length' }, 'Incomplete'],
  ['refused', { refused: true }, 'Refused'],
  ['handoff', { finishReason: 'tool_calls' }, 'Action required'],
] as const)('keeps %s native evidence independent of elapsed', async (_kind, patch, status) => {
  const finish = pending()
  await ready('key')
  await fill('prompt', 'Native boundary')
  await submit()
  clock = 1234
  await finish({ ...completed, ...patch })
  expect(elapsed()?.textContent).toBe('Browser elapsed: 234 ms')
  expect(host.querySelector('[role="log"]')!.textContent).toContain(status)
  expect(host.querySelector('[role="log"]')!.textContent).not.toContain('Completed')
})

it('records a rejected transport without claiming native completion or retrying it', async () => {
  let fail!: (error: Error) => void
  vi.mocked(runChat).mockImplementationOnce(
    () =>
      new Promise((_resolve, reject) => {
        fail = reject
      }),
  )
  await ready('key')
  await fill('prompt', 'Failed')
  await submit()
  clock = 1300
  await act(async () => fail(new Error('controlled failure')))
  expect(elapsed()?.textContent).toBe('Browser elapsed: 300 ms')
  expect(host.querySelector('[role="log"]')!.textContent).toContain('Call failed')
  expect(runChat).toHaveBeenCalledTimes(1)
})

it('clears finalized observations with conversation reset and never reuses them for another request', async () => {
  const finish = pending()
  await ready('key')
  await fill('prompt', 'First')
  await submit()
  clock = 1321
  await finish()
  expect(elapsed()?.textContent).toBe('Browser elapsed: 321 ms')
  await click('Clear conversation')
  expect(elapsed()).toBeUndefined()
  const second = pending()
  clock = 5000
  await fill('prompt', 'Second')
  await submit()
  expect(elapsed()).toBeUndefined()
  clock = 5040
  await second()
  expect(elapsed()?.textContent).toBe('Browser elapsed: 40 ms')
  expect(runChat).toHaveBeenCalledTimes(2)
})

it('cannot restore a cleared model generation from a late response', async () => {
  const finish = pending()
  await ready('key')
  await fill('prompt', 'Old generation')
  await submit()
  await act(async () => {
    const select = host.querySelector<HTMLSelectElement>('[name="model"]')!
    select.value = ''
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
  clock = 1800
  await finish()
  expect(elapsed()).toBeUndefined()
  expect(host.textContent).not.toContain('Old generation')
  expect(host.textContent).not.toContain('Preserved completed reply')
})

it('does not resurrect an observation after unmount', async () => {
  const finish = pending()
  await ready('key')
  await fill('prompt', 'Unmounted')
  await submit()
  await act(async () => root.render(null))
  clock = 1800
  await finish()
  expect(host.textContent).toBe('')
})

it('localizes the recorded observation without clearing history or draft', async () => {
  const finish = pending()
  await ready('key')
  await fill('prompt', 'Recorded request')
  await submit()
  clock = 1250
  await finish()
  await fill('prompt', 'Bilingual draft')
  await act(async () => i18n.changeLanguage('zh'))
  expect(elapsed()?.textContent).toBe('浏览器耗时：250 毫秒')
  expect(elapsed()?.title).toContain('这不是 Provider 延迟')
  expect(field('prompt').value).toBe('Bilingual draft')
  expect(host.textContent).toContain('Recorded request')
  expect(host.textContent).not.toContain('Browser elapsed')
  expect(runChat).toHaveBeenCalledTimes(1)
})

it('excludes upload and asynchronous attachment cleanup from elapsed observation', async () => {
  const attachment = {
    id: 'obj_elapsed',
    name: 'elapsed.png',
    mime: 'image/png',
    size: 3,
    state: 'ready' as const,
    created_at: '2026-10-06T00:00:00Z',
  }
  vi.mocked(uploadAttachment).mockImplementationOnce(async () => {
    clock = 5000
    return attachment
  })
  let release!: (value: typeof attachment) => void
  vi.mocked(deleteAttachment).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve
      }),
  )
  const finish = pending()
  await ready('key', 'openai_chat', true)
  const input = host.querySelector<HTMLInputElement>('input[type="file"]')!
  await act(async () => {
    Object.defineProperty(input, 'files', {
      configurable: true,
      value: [new File(['png'], 'elapsed.png', { type: 'image/png' })],
    })
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await settle()
  expect(uploadAttachment).toHaveBeenCalledTimes(1)
  clock = 9000
  await fill('prompt', 'Media elapsed')
  await submit()
  expect(elapsed()).toBeUndefined()
  clock = 9100
  await finish()
  expect(deleteAttachment).toHaveBeenCalledTimes(1)
  expect(elapsed()?.textContent).toBe('Browser elapsed: 100 ms')
  clock = 20000
  await act(async () => release(attachment))
  await settle()
  expect(elapsed()?.textContent).toBe('Browser elapsed: 100 ms')
  expect(runChat).toHaveBeenCalledTimes(1)
})
