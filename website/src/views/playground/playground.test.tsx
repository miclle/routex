import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PlaygroundPage from './index'
import i18n from '@/i18n'
import {
  GatewayError,
  getGatewayModels,
  runChat,
  runResponses,
  runMessages,
} from '@/api/playground'

vi.mock('@/api/playground', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
  runResponses: vi.fn(),
  runMessages: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
beforeEach(async () => {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'key-authorized-model' }])
  vi.mocked(runChat).mockImplementation(async (_key, _request, _signal, update) => {
    const result = {
      text: 'Real response',
      requestId: 'req_ui',
      usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
      finishReason: 'stop',
    }
    update(result)
    return result
  })
  await act(async () => {
    root.render(<PlaygroundPage />)
  })
})
afterEach(async () => {
  await act(async () => {
    root.unmount()
  })
  container.remove()
  vi.resetAllMocks()
})
function button(text: string) {
  return [...container.querySelectorAll('button')].find((el) => el.textContent === text)!
}
async function click(text: string) {
  await act(async () => {
    button(text).click()
  })
}
async function fill(name: string, value: string) {
  const input = container.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
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
async function ready() {
  await fill('api_key', 'rx_transient')
  await click('Verify and load models')
  await fill('prompt', 'Hello')
}
async function submit() {
  await act(async () => {
    container
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}

describe('Playground user behavior', () => {
  it('requires Key verification, sends native parameters, and carries successful chat context forward', async () => {
    expect(button('Send message').disabled).toBe(true)
    await ready()
    await submit()
    expect(container.textContent).toContain('Real response')
    expect(container.textContent).toContain('req_ui')
    expect(container.textContent).toContain('Input 2 · Output 3 · Total 5 Tokens')
    const [key, request] = vi.mocked(runChat).mock.calls[0]
    expect(key).toBe('rx_transient')
    expect(request).toMatchObject({
      model: 'key-authorized-model',
      stream: true,
      stream_options: { include_usage: true },
      messages: [{ role: 'user', content: 'Hello' }],
    })
    await fill('prompt', 'Continue')
    await submit()
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Hello' },
      { role: 'assistant', content: 'Real response' },
      { role: 'user', content: 'Continue' },
    ])
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
    await click('Clear key')
    expect(container.querySelector<HTMLInputElement>('[name="api_key"]')!.value).toBe('')
    expect(container.textContent).not.toContain('Real response')
  })
  it('can select ordinary responses and displays gateway failures with the request ID', async () => {
    await ready()
    await act(async () => {
      container.querySelector<HTMLInputElement>('[name="stream"]')!.click()
    })
    vi.mocked(runChat).mockRejectedValue(new GatewayError('Model access denied', 'req_denied', 403))
    await submit()
    expect(vi.mocked(runChat).mock.calls[0][1].stream).toBe(false)
    expect(container.textContent).toContain('Model access denied')
    expect(container.textContent).toContain('req_denied')
    expect(button('Clear conversation').disabled).toBe(false)
  })
  it('shows partial stream content, cancels the request, and excludes the cancelled turn from context', async () => {
    vi.mocked(runChat).mockImplementation(
      (_key, _request, signal, update) =>
        new Promise((_resolve, reject) => {
          update({
            text: 'Partial stream',
            requestId: 'req_stream',
            usage: null,
            finishReason: null,
          })
          signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    await ready()
    await submit()
    expect(container.textContent).toContain('Partial stream')
    expect(button('Request in progress…').disabled).toBe(true)
    await click('Stop generation')
    expect(container.textContent).toContain('Stopped')
    expect(container.textContent).toContain('Partial stream')
    vi.mocked(runChat).mockResolvedValue({
      text: 'New answer',
      requestId: 'req_new',
      usage: null,
      finishReason: 'stop',
    })
    await fill('prompt', 'Try again')
    await submit()
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Try again' },
    ])
  })
  it('shows recoverable Key verification errors without enabling send', async () => {
    vi.mocked(getGatewayModels).mockRejectedValue(new GatewayError('Key is revoked', '', 401))
    await fill('api_key', 'revoked-key')
    await click('Verify and load models')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('Key is revoked')
    expect(button('Send message').disabled).toBe(true)
    expect(button('Verify and load models').disabled).toBe(false)
  })
  it('aborts an active request when leaving the page', async () => {
    let requestSignal: AbortSignal | undefined
    vi.mocked(runChat).mockImplementation((_key, _request, signal) => {
      requestSignal = signal
      return new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
      })
    })
    await ready()
    await submit()
    await act(async () => {
      root.render(null)
    })
    expect(requestSignal?.aborted).toBe(true)
  })
})

it('offers eligible native protocols and excludes explicitly unusable models', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'responses-only', protocols: ['openai_responses'] },
    { id: 'unavailable', protocols: [] },
    { id: 'both', protocols: ['openai_chat', 'openai_responses'] },
  ])
  await fill('api_key', 'rx_transient')
  await click('Verify and load models')
  const options = [...container.querySelectorAll('option')].map((option) => option.value)
  expect(options).toContain('both')
  expect(options).toContain('responses-only')
  expect(options).not.toContain('unavailable')
})

async function select(name: string, value: string) {
  await act(async () => {
    const input = container.querySelector<HTMLSelectElement>(`select[name="${name}"]`)!
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
it('uses selected native Responses parameters and resets history when changing protocols', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'native', protocols: ['openai_chat', 'openai_responses'] },
  ])
  vi.mocked(runResponses).mockResolvedValue({
    text: 'Response text',
    requestId: 'req_native',
    usage: null,
    finishReason: 'completed',
    responseStatus: 'completed',
    nonTextOutput: false,
  })
  await ready()
  await select('protocol', 'openai_responses')
  await fill('system', 'Be precise')
  await submit()
  expect(vi.mocked(runChat)).not.toHaveBeenCalled()
  expect(vi.mocked(runResponses).mock.calls[0][1]).toEqual({
    model: 'native',
    input: [{ role: 'user', content: 'Hello' }],
    instructions: 'Be precise',
    temperature: 0.7,
    top_p: 1,
    max_output_tokens: 2048,
    stream: true,
  })
  expect(container.textContent).toContain('POST /v1/responses')
  await fill('prompt', 'Continue')
  await submit()
  expect(vi.mocked(runResponses).mock.calls[1][1].input).toEqual([
    { role: 'user', content: 'Hello' },
    { role: 'assistant', content: 'Response text' },
    { role: 'user', content: 'Continue' },
  ])
  await select('protocol', 'openai_chat')
  expect(container.textContent).not.toContain('Response text')
  expect(container.textContent).toContain('POST /v1/chat/completions')
})
it.each(['incomplete', 'failed', 'queued'] as const)(
  'keeps Responses %s outside completed conversation history',
  async (status) => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      { id: 'native', protocols: ['openai_responses'] },
    ])
    vi.mocked(runResponses).mockResolvedValue({
      text: 'Partial native',
      requestId: 'req_native',
      usage: null,
      finishReason: status,
      responseStatus: status,
      nonTextOutput: false,
    })
    await ready()
    await submit()
    expect(container.textContent).toContain(
      status === 'queued'
        ? 'Accepted, not completed'
        : status === 'failed'
          ? 'Call failed'
          : 'Incomplete',
    )
    expect(container.textContent).toContain('Partial native')
    await fill('prompt', 'Retry')
    await submit()
    expect(vi.mocked(runResponses).mock.calls[1][1].input).toEqual([
      { role: 'user', content: 'Retry' },
    ])
    expect(runChat).not.toHaveBeenCalled()
  },
)

it('switches native labels without losing draft or protocol and prevents duplicate dispatch', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'native', protocols: ['openai_responses'] }])
  let release!: () => void
  vi.mocked(runResponses).mockImplementation(async (_key, _request, _signal, update) => {
    const result = {
      text: 'Native partial',
      requestId: 'req_native',
      usage: null,
      finishReason: null,
      responseStatus: null,
      nonTextOutput: false,
    }
    update(result)
    await new Promise<void>((resolve) => {
      release = resolve
    })
    return { ...result, responseStatus: 'completed' }
  })
  await ready()
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(container.textContent).toContain('协议类型')
  expect(container.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe('Hello')
  expect(container.querySelector<HTMLSelectElement>('[name="protocol"]')!.value).toBe(
    'openai_responses',
  )
  await act(async () => {
    const form = container.querySelector('form')!
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
  expect(runResponses).toHaveBeenCalledTimes(1)
  await act(async () => release())
  expect(container.textContent).toContain('Native partial')
  expect(document.documentElement.lang).toBe('zh')
  expect(JSON.stringify(localStorage)).not.toContain('rx_transient')
})

it('sends Messages-only models through the native endpoint with top-level system and no Chat fallback', async () => {
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'messages-native', protocols: ['anthropic_messages'] },
  ])
  vi.mocked(runMessages).mockResolvedValue({
    text: 'Messages answer',
    requestId: 'req_messages',
    usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
    finishReason: 'end_turn',
    messageStatus: 'completed',
    nonTextOutput: false,
  })
  await ready()
  await fill('system', 'Be precise')
  await fill('max_tokens', '0')
  await submit()
  expect(container.textContent).toContain('Anthropic Messages')
  expect(container.textContent).toContain('POST /v1/messages')
  expect(vi.mocked(runMessages).mock.calls[0][1]).toEqual({
    model: 'messages-native',
    messages: [{ role: 'user', content: 'Hello' }],
    system: 'Be precise',
    stream: true,
    temperature: 0.7,
    top_p: 1,
    max_tokens: 0,
  })
  expect(runChat).not.toHaveBeenCalled()
  expect(runResponses).not.toHaveBeenCalled()
  await fill('prompt', 'Continue')
  await submit()
  expect(vi.mocked(runMessages).mock.calls[1][1].messages).toHaveLength(3)
  expect(JSON.stringify(localStorage)).not.toContain('rx_transient')
})
it.each(['handoff', 'refused', 'incomplete'] as const)(
  'keeps Messages %s explicit and out of follow-up history',
  async (status) => {
    vi.mocked(getGatewayModels).mockResolvedValue([
      { id: 'messages-native', protocols: ['anthropic_messages'] },
    ])
    vi.mocked(runMessages).mockResolvedValue({
      text: 'Partial native',
      requestId: 'req_messages',
      usage: null,
      finishReason: 'tool_use',
      messageStatus: status,
      nonTextOutput: false,
    })
    await ready()
    await submit()
    expect(container.textContent).toContain(
      status === 'handoff' ? 'Action required' : status === 'refused' ? 'Refused' : 'Incomplete',
    )
    await fill('prompt', 'Retry')
    await submit()
    expect(vi.mocked(runMessages).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Retry' },
    ])
  },
)
