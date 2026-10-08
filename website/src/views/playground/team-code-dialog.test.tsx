import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import CodeDialog from './code-dialog'
import { buildTeamPlaygroundSnippet, type TeamSnippetInput } from '@/lib/playground-team-snippet'
import type { SnippetLanguage } from '@/lib/playground-snippet'
const request: TeamSnippetInput = {
  source: 'team',
  teamId: 'tea_01k6kwwwwwwwwwwwwwwwwwwwww',
  origin: 'https://gateway.example.test',
  protocol: 'anthropic_messages',
  model: 'native',
  stream: true,
  temperature: 0.7,
  topP: 1,
  maxTokens: 2048,
  system: 'Captured system',
  messages: [
    { role: 'user', content: 'Completed prompt' },
    { role: 'assistant', content: 'Completed answer' },
    { role: 'user', content: "Draft\nDon't expand $HOME" },
  ],
}
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root, host: HTMLDivElement, copy: ReturnType<typeof vi.fn>
const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.stubGlobal('fetch', vi.fn())
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  copy = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
  await act(async () => root.render(<CodeDialog request={request} onClose={vi.fn()} />))
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
  if (original) Object.defineProperty(navigator, 'clipboard', original)
  else Reflect.deleteProperty(navigator, 'clipboard')
})
async function click(label: string) {
  const button = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
  expect(button).toBeDefined()
  await act(async () => button.click())
}
it('copies independently authenticated Team examples in all three languages without live credentials or Key headers', async () => {
  for (const label of ['cURL', 'Python', 'JavaScript']) {
    await click(label)
    const code = document.querySelector('pre')!.textContent!
    expect(code).toBe(
      buildTeamPlaygroundSnippet(
        request,
        label === 'cURL' ? 'curl' : (label.toLowerCase() as SnippetLanguage),
      ),
    )
    expect(document.querySelector('pre code span[class]')).not.toBeNull()
    expect(code).toContain('/api/v1/teams/' + request.teamId + '/messages')
    expect(code).toContain('/api/v1/auth/login')
    expect(code).toContain('ROUTEX_EMAIL')
    expect(code).toContain('ROUTEX_PASSWORD')
    expect(code).toContain('202')
    expect(code).toContain('X-CSRF-Token')
    expect(code).toContain('Completed answer')
    expect(code).toContain('Captured system')
    expect(code).not.toMatch(
      /ROUTEX_API_KEY|Authorization|x-api-key|x-goog-api-key|csrf-playground|secret-live/,
    )
    await click('Copy code')
    expect(copy).toHaveBeenLastCalledWith(code)
  }
  expect(document.body.textContent).toContain('Python 3')
})
it('switches source-specific guidance live without replacing the captured request', async () => {
  const code = document.querySelector('pre')!.textContent
  expect(document.body.textContent).toContain('ROUTEX_EMAIL')
  expect(document.body.textContent).toContain('ROUTEX_PASSWORD')
  await act(async () => i18n.changeLanguage('zh'))
  expect(document.body.textContent).toContain('独立登录')
  expect(document.body.textContent).toContain('Python 3')
  expect(document.querySelector('pre')!.textContent).toBe(code)
})
it('blocks invalid Team identity without fabricating a Key or Team endpoint', async () => {
  await act(async () =>
    root.render(<CodeDialog request={{ ...request, teamId: '../other' }} onClose={vi.fn()} />),
  )
  expect(document.querySelector('pre')).toBeNull()
  expect(
    [...document.querySelectorAll<HTMLButtonElement>('button')].find(
      (item) => item.textContent === 'Copy code',
    )!.disabled,
  ).toBe(true)
})

it('preserves opaque Team login heredoc and inert multiline authentication programs without dispatch', async () => {
  const captured = {
    ...request,
    system: '<script>never()</script>雪🧩',
    messages: [
      { role: 'user' as const, content: 'Draft\n<img src=x onerror=never()>\n${never()}' },
    ],
  }
  await act(async () => root.render(<CodeDialog request={captured} onClose={vi.fn()} />))
  for (const [label, language] of [
    ['cURL', 'curl'],
    ['Python', 'python'],
    ['JavaScript', 'javascript'],
  ] as const) {
    await click(label)
    const expected = buildTeamPlaygroundSnippet(captured, language)
    const pre = document.querySelector('pre')!
    expect(pre.textContent).toBe(expected)
    expect(pre.querySelector('script, img, iframe')).toBeNull()
    expect(pre.querySelector('span[class]')).not.toBeNull()
    await click('Copy code')
    expect(copy).toHaveBeenLastCalledWith(expected)
    if (language === 'curl') {
      const body = [...pre.querySelectorAll('span')].find((token) =>
        token.textContent?.includes('import http.cookiejar'),
      )!
      expect(body).toBeDefined()
      expect(body.className).toBe('')
    }
  }
  expect(fetch).not.toHaveBeenCalled()
})
