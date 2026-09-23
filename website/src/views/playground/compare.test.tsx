import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GatewayError, getGatewayModels, runChat, runResponses } from '@/api/playground'
import i18n from '@/i18n'
import CompareWorkbench from './compare'
import PlaygroundPage from './index'
vi.mock('@/api/playground', async (original) => ({
  ...(await original<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
  runResponses: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement
const chatResult = {
  text: 'Chat answer',
  requestId: 'req_chat',
  usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 },
  finishReason: 'stop',
}
const nativeResult = {
  text: 'Native answer',
  requestId: 'req_native',
  usage: { prompt_tokens: 7, completion_tokens: 8, total_tokens: 15 },
  finishReason: 'completed',
  responseStatus: 'completed' as const,
  nonTextOutput: false,
}
beforeEach(async () => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  vi.mocked(getGatewayModels).mockResolvedValue([
    { id: 'chat-a', protocols: ['openai_chat'] },
    { id: 'native-b', protocols: ['openai_responses'] },
    { id: 'both-c', protocols: ['openai_chat', 'openai_responses'] },
    { id: 'unavailable', protocols: [] },
  ])
  vi.mocked(runChat).mockResolvedValue(chatResult)
  vi.mocked(runResponses).mockResolvedValue(nativeResult)
  await act(async () => root.render(<CompareWorkbench />))
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.resetAllMocks()
})
function button(label: string) {
  const found = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
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
async function select(label: string, value: string) {
  const input = host.querySelector<HTMLSelectElement>(`select[aria-label="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function ready() {
  await fill('comparison_key', 'rx_comparison_only')
  await click('Verify and load models')
}
async function send(text = 'Same prompt') {
  await fill('comparison_prompt', text)
  await act(async () => {
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  })
}
function lane(n: number) {
  return host.querySelector<HTMLElement>(`section[aria-label="Comparison ${n}"]`)!
}
describe('model comparison workbench', () => {
  it('requires verification and keeps two to four independent model columns', async () => {
    expect(host.querySelectorAll('section')).toHaveLength(2)
    expect(button('Add comparison').disabled).toBe(true)
    await ready()
    expect(host.querySelector('option[value="unavailable"]')).toBeNull()
    await click('Add comparison')
    await click('Add comparison')
    expect(host.querySelectorAll('section')).toHaveLength(4)
    expect(button('Add comparison').disabled).toBe(true)
    await click('Remove comparison 4')
    await click('Remove comparison 3')
    expect(host.querySelectorAll('section')).toHaveLength(2)
    expect(host.querySelector('[aria-label^="Remove comparison"]')).toBeNull()
  })
  it('dispatches identical captured text to native protocols and isolates successful histories', async () => {
    await ready()
    await send()
    expect(vi.mocked(runChat).mock.calls[0][1]).toMatchObject({
      model: 'chat-a',
      messages: [{ role: 'user', content: 'Same prompt' }],
      stream: true,
      stream_options: { include_usage: true },
    })
    expect(vi.mocked(runResponses).mock.calls[0][1]).toMatchObject({
      model: 'native-b',
      input: [{ role: 'user', content: 'Same prompt' }],
      max_output_tokens: 2048,
      stream: true,
    })
    expect(lane(1).textContent).toContain('req_chat')
    expect(lane(1).textContent).toContain('Total 5 Tokens')
    expect(lane(2).textContent).toContain('req_native')
    expect(lane(2).textContent).toContain('Total 15 Tokens')
    await send('Follow-up')
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Same prompt' },
      { role: 'assistant', content: 'Chat answer' },
      { role: 'user', content: 'Follow-up' },
    ])
    expect(vi.mocked(runResponses).mock.calls[1][1].input).toEqual([
      { role: 'user', content: 'Same prompt' },
      { role: 'assistant', content: 'Native answer' },
      { role: 'user', content: 'Follow-up' },
    ])
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })
  it('keeps a failed lane independent and excludes its incomplete output from later history', async () => {
    vi.mocked(runChat).mockRejectedValueOnce(new GatewayError('Rejected', 'req_failure', 429))
    await ready()
    await send()
    expect(lane(1).textContent).toContain('Rejected')
    expect(lane(2).textContent).toContain('Native answer')
    await send('Retry')
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([
      { role: 'user', content: 'Retry' },
    ])
    expect(vi.mocked(runResponses).mock.calls[1][1].input).toHaveLength(3)
    vi.mocked(runResponses).mockResolvedValueOnce({
      ...nativeResult,
      responseStatus: 'incomplete',
      text: 'Incomplete native',
    })
    await send('Incomplete')
    expect(lane(2).textContent).toContain('Incomplete')
    await send('After incomplete')
    expect(
      vi.mocked(runResponses).mock.calls[3][1].input.map((item) => item.content),
    ).not.toContain('Incomplete native')
  })
  it('prevents double dispatch and stops one stream without aborting its sibling', async () => {
    let firstSignal: AbortSignal | undefined,
      secondSignal: AbortSignal | undefined,
      complete!: () => void
    vi.mocked(runChat).mockImplementation(
      (_key, _request, signal, update) =>
        new Promise((_resolve, reject) => {
          firstSignal = signal
          update({ ...chatResult, text: 'Partial chat', usage: null })
          signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    vi.mocked(runResponses).mockImplementation(async (_key, _request, signal, update) => {
      secondSignal = signal
      update({ ...nativeResult, text: 'Partial native', responseStatus: null, usage: null })
      await new Promise<void>((resolve) => {
        complete = resolve
      })
      return nativeResult
    })
    await ready()
    await fill('comparison_prompt', 'Capture once')
    await act(async () => {
      const form = host.querySelector('form')!
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    })
    expect(runChat).toHaveBeenCalledTimes(1)
    expect(runResponses).toHaveBeenCalledTimes(1)
    await click('Stop comparison 1')
    expect(firstSignal?.aborted).toBe(true)
    expect(secondSignal?.aborted).toBe(false)
    expect(lane(1).textContent).toContain('Stopped')
    expect(lane(1).textContent).toContain('Partial chat')
    await act(async () => complete())
    expect(lane(2).textContent).toContain('Native answer')
  })
  it('resets only the changed model/protocol history and clears all secrets on key reset', async () => {
    await ready()
    await send()
    await select('Comparison model 1', 'both-c')
    expect(lane(1).textContent).not.toContain('Chat answer')
    expect(lane(2).textContent).toContain('Native answer')
    await select('Comparison protocol 1', 'openai_responses')
    await send('Native now')
    expect(vi.mocked(runResponses).mock.calls.at(-2)?.[1]).toMatchObject({
      model: 'both-c',
      input: [{ role: 'user', content: 'Native now' }],
    })
    await click('Clear key')
    expect(host.querySelector<HTMLInputElement>('[name="comparison_key"]')!.value).toBe('')
    expect(host.textContent).not.toContain('Native answer')
    expect(button('Send to all models').disabled).toBe(true)
  })
  it('aborts hidden workbench requests and discards its key when switching tabs', async () => {
    await act(async () => root.render(<PlaygroundPage />))
    await click('Model comparison')
    let signal: AbortSignal | undefined
    vi.mocked(runChat).mockImplementation(
      (_key, _request, current) =>
        new Promise((_resolve, reject) => {
          signal = current
          current.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
    )
    await ready()
    await send()
    await click('Model conversation')
    expect(signal?.aborted).toBe(true)
    await click('Model comparison')
    expect(host.querySelector<HTMLInputElement>('[name="comparison_key"]')!.value).toBe('')
  })
  it('updates comparison accessibility and status copy without losing the shared draft', async () => {
    await ready()
    await fill('comparison_prompt', 'Keep this draft')
    await act(async () => {
      await i18n.changeLanguage('zh')
    })
    expect(host.querySelector('[aria-label="对比模型 1"]')).not.toBeNull()
    expect(host.textContent).toContain('添加对比')
    expect(host.querySelector<HTMLTextAreaElement>('[name="comparison_prompt"]')!.value).toBe(
      'Keep this draft',
    )
    expect(JSON.stringify(localStorage)).not.toContain('rx_comparison_only')
  })
})
