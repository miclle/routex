import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import CodeDialog from './code-dialog'
import type { SnippetInput } from '@/lib/playground-snippet'
const request: SnippetInput = {
  origin: 'https://gateway.example.test',
  protocol: 'anthropic_messages',
  model: 'native',
  stream: true,
  temperature: 0.7,
  topP: 1,
  maxTokens: 2048,
  system: 'System',
  messages: [{ role: 'user', content: "你好\nDon't substitute $HOME" }],
}
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
let root: Root,
  host: HTMLDivElement,
  copy: ReturnType<typeof vi.fn<(value: string) => Promise<void>>>,
  close: ReturnType<typeof vi.fn<() => void>>
beforeEach(async () => {
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  copy = vi.fn().mockResolvedValue(undefined)
  close = vi.fn()
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
  await act(async () => root.render(<CodeDialog request={request} onClose={close} />))
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  if (original) Object.defineProperty(navigator, 'clipboard', original)
  else Reflect.deleteProperty(navigator, 'clipboard')
})
async function click(label: string) {
  await act(async () => {
    const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (item) => item.textContent === label,
    )!
    expect(button).toBeDefined()
    button.click()
  })
}
it('copies all three functional language tabs with environment-key references and matching native shapes', async () => {
  for (const [label, marker] of [
    ['cURL', '$ROUTEX_API_KEY'],
    ['Python', 'os.environ["ROUTEX_API_KEY"]'],
    ['JavaScript', 'process.env.ROUTEX_API_KEY'],
  ]) {
    await click(label)
    const code = document.querySelector('pre')!.textContent!
    expect(code).toContain('/v1/messages')
    expect(code).toContain('x-api-key')
    expect(code).toContain('anthropic-version')
    expect(code).toContain(marker)
    expect(document.querySelector('[role="tabpanel"]')).not.toBeNull()
    await click('Copy code')
    expect(copy).toHaveBeenLastCalledWith(code)
  }
  expect(document.body.textContent).toContain('Request code copied')
  await click('Close')
  expect(close).toHaveBeenCalled()
})
it('preserves the captured code through language switching and offers manual copy on clipboard rejection', async () => {
  copy.mockRejectedValue(new Error('denied'))
  await click('Python')
  const captured = document.querySelector('pre')!.textContent
  await click('Copy code')
  expect(document.body.textContent).toContain('Select and copy the code manually')
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(document.body.textContent).toContain('复制失败')
  expect(document.querySelector('pre')!.textContent).toBe(captured)
})
it('does not produce or copy a fake Gemini path', async () => {
  await act(async () =>
    root.render(
      <CodeDialog
        request={{ ...request, protocol: 'gemini_generate_content', model: 'vendor/model' }}
        onClose={close}
      />,
    ),
  )
  expect(document.querySelector('pre')).toBeNull()
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === 'Copy code',
  )!
  expect(button.disabled).toBe(true)
  expect(document.body.textContent).toContain('compatible public name')
})
