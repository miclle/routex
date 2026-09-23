import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PlaygroundPage from './index'
import { GatewayError, getGatewayModels, runChat } from '@/api/playground'

vi.mock('@/api/playground', async (importOriginal) => ({ ...await importOriginal<typeof import('@/api/playground')>(), getGatewayModels: vi.fn(), runChat: vi.fn() }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root
let container: HTMLDivElement
beforeEach(async () => {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'key-authorized-model' }])
  vi.mocked(runChat).mockImplementation(async (_key, _request, _signal, update) => {
    const result = { text: 'Real response', requestId: 'req_ui', usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 }, finishReason: 'stop' }
    update(result)
    return result
  })
  await act(async () => { root.render(<PlaygroundPage />) })
})
afterEach(async () => { await act(async () => { root.unmount() }); container.remove(); vi.resetAllMocks() })
function button(text: string) { return [...container.querySelectorAll('button')].find((el) => el.textContent === text)! }
async function click(text: string) { await act(async () => { button(text).click() }) }
async function fill(name: string, value: string) {
  const input = container.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
  await act(async () => { Object.getOwnPropertyDescriptor(input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')!.set!.call(input, value); input.dispatchEvent(new Event('input', { bubbles: true })) })
}
async function ready() { await fill('api_key', 'rx_transient'); await click('验证并加载模型'); await fill('prompt', 'Hello') }
async function submit() { await act(async () => { container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) }) }

describe('Playground user behavior', () => {
  it('requires Key verification, sends native parameters, and carries successful chat context forward', async () => {
    expect(button('发送消息').disabled).toBe(true)
    await ready()
    await submit()
    expect(container.textContent).toContain('Real response')
    expect(container.textContent).toContain('req_ui')
    expect(container.textContent).toContain('输入 2 · 输出 3 · 总计 5 Tokens')
    const [key, request] = vi.mocked(runChat).mock.calls[0]
    expect(key).toBe('rx_transient')
    expect(request).toMatchObject({ model: 'key-authorized-model', stream: true, stream_options: { include_usage: true }, messages: [{ role: 'user', content: 'Hello' }] })
    await fill('prompt', 'Continue')
    await submit()
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([{ role: 'user', content: 'Hello' }, { role: 'assistant', content: 'Real response' }, { role: 'user', content: 'Continue' }])
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
    await click('清除密钥')
    expect(container.querySelector<HTMLInputElement>('[name="api_key"]')!.value).toBe('')
    expect(container.textContent).not.toContain('Real response')
  })
  it('can select ordinary responses and displays gateway failures with the request ID', async () => {
    await ready()
    await act(async () => { container.querySelector<HTMLInputElement>('[name="stream"]')!.click() })
    vi.mocked(runChat).mockRejectedValue(new GatewayError('Model access denied', 'req_denied', 403))
    await submit()
    expect(vi.mocked(runChat).mock.calls[0][1].stream).toBe(false)
    expect(container.textContent).toContain('Model access denied')
    expect(container.textContent).toContain('req_denied')
    expect(button('清空对话').disabled).toBe(false)
  })
  it('shows partial stream content, cancels the request, and excludes the cancelled turn from context', async () => {
    vi.mocked(runChat).mockImplementation((_key, _request, signal, update) => new Promise((_resolve, reject) => {
      update({ text: 'Partial stream', requestId: 'req_stream', usage: null, finishReason: null })
      signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    await ready()
    await submit()
    expect(container.textContent).toContain('Partial stream')
    expect(button('请求进行中…').disabled).toBe(true)
    await click('停止生成')
    expect(container.textContent).toContain('已停止')
    expect(container.textContent).toContain('Partial stream')
    vi.mocked(runChat).mockResolvedValue({ text: 'New answer', requestId: 'req_new', usage: null, finishReason: 'stop' })
    await fill('prompt', 'Try again')
    await submit()
    expect(vi.mocked(runChat).mock.calls[1][1].messages).toEqual([{ role: 'user', content: 'Try again' }])
  })
  it('shows recoverable Key verification errors without enabling send', async () => {
    vi.mocked(getGatewayModels).mockRejectedValue(new GatewayError('Key is revoked', '', 401))
    await fill('api_key', 'revoked-key')
    await click('验证并加载模型')
    expect(container.querySelector('[role="alert"]')?.textContent).toContain('Key is revoked')
    expect(button('发送消息').disabled).toBe(true)
    expect(button('验证并加载模型').disabled).toBe(false)
  })
  it('aborts an active request when leaving the page', async () => {
    let requestSignal: AbortSignal | undefined
    vi.mocked(runChat).mockImplementation((_key, _request, signal) => { requestSignal = signal; return new Promise((_resolve, reject) => { signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))) }) })
    await ready()
    await submit()
    await act(async () => { root.render(null) })
    expect(requestSignal?.aborted).toBe(true)
  })
})
