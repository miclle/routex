import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { getSession } from '@/api/auth'
import { sessionKey } from '@/hooks/use-auth'
import { getOwnActiveTeams, getTeamModels, runTeamChat } from '@/api/playground-team'
import i18n from '@/i18n'
import ChatWorkbench from './chat'
import CompareWorkbench from './compare'

vi.mock('@/api/auth', () => ({ getSession: vi.fn(), getSetup: vi.fn() }))
vi.mock('@/api/playground-team', async (original) => ({
  ...(await original<typeof import('@/api/playground-team')>()),
  getOwnActiveTeams: vi.fn(),
  getTeamModels: vi.fn(),
  runTeamChat: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const team = 'tea_01k6kwwwwwwwwwwwwwwwwwwwww'
const session = {
  user: { id: 'usr_one', role: 'member' as const, name: 'Member', email: 'member@example.test' },
  csrf_token: 'csrf-original',
}
let root: Root, host: HTMLDivElement, cache: QueryClient
beforeEach(async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.mocked(getSession).mockImplementation(async () => structuredClone(session))
  vi.mocked(getOwnActiveTeams).mockResolvedValue({
    items: [
      { id: team, name: 'Team', status: 'active', description: '', created_at: '', model_ids: [] },
    ],
    next_cursor: null,
  })
  vi.mocked(getTeamModels).mockResolvedValue([
    {
      id: 'model-one',
      protocols: ['openai_chat'],
      attachment_scope: 'team',
      personal_attachments: false,
    },
  ])
  vi.mocked(runTeamChat).mockResolvedValue({
    text: 'Completed private text',
    usage: null,
    finishReason: 'stop',
    requestId: 'req_completed',
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  vi.resetAllMocks()
  vi.restoreAllMocks()
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5))
  })
}
function button(label: string) {
  const value = [...document.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
  expect(value, label).toBeDefined()
  return value
}
async function click(label: string) {
  await act(async () => button(label).click())
  await settle()
}
async function fill(value: string) {
  const input = host.querySelector<HTMLTextAreaElement>(
    'textarea[name="prompt"], textarea[name="comparison_prompt"]',
  )!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function ready(mode: 'chat' | 'compare') {
  const Component = mode === 'chat' ? ChatWorkbench : CompareWorkbench
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <Component source="team" teamId={team} onTeam={vi.fn()} onSource={vi.fn()} />
      </QueryClientProvider>,
    ),
  )
  await settle()
  await settle()
  await click('Load Team models')
  expect(getSession).toHaveBeenCalledOnce()
}
async function send() {
  await fill('Submitted prompt')
  if (host.querySelector('[name="prompt"]')) await click('Send message')
  else
    await act(async () =>
      host
        .querySelector('form')!
        .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
    )
  await settle()
}
async function renew() {
  const old = cache.getQueryState(sessionKey)!
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await settle()
  const current = cache.getQueryState(sessionKey)!
  expect(current.data).toBe(old.data)
  expect(current.dataUpdatedAt).toBe(old.dataUpdatedAt)
  expect(current.dataUpdateCount).toBe(old.dataUpdateCount + 1)
  expect(getSession).toHaveBeenCalledTimes(2)
}
it.each(['chat', 'compare'] as const)(
  '%s clears completed history and captured code on an identical batched Session renewal',
  async (mode) => {
    await ready(mode)
    await send()
    await fill('Keep unsent draft')
    await click(mode === 'chat' ? 'Get code' : 'Get code for comparison 1')
    expect(document.querySelector('pre')!.textContent).toContain('Completed private text')
    const dispatched = vi.mocked(runTeamChat).mock.calls.length
    await renew()
    expect(document.querySelector('pre')).toBeNull()
    expect(host.textContent).not.toContain('Completed private text')
    expect(host.querySelector('option[value="model-one"]')).toBeNull()
    expect(
      host.querySelector<HTMLTextAreaElement>(
        'textarea[name="prompt"], textarea[name="comparison_prompt"]',
      )!.value,
    ).toBe('Keep unsent draft')
    expect(runTeamChat).toHaveBeenCalledTimes(dispatched)
    expect(getTeamModels).toHaveBeenCalledOnce()
    await click('Load Team models')
    await send()
    expect(vi.mocked(runTeamChat).mock.calls.at(-1)![2].messages).toEqual([
      { role: 'user', content: 'Submitted prompt' },
    ])
  },
)
it.each(['chat', 'compare'] as const)(
  '%s aborts pending lanes on an identical batched Session renewal',
  async (mode) => {
    await ready(mode)
    const signals: AbortSignal[] = []
    vi.mocked(runTeamChat).mockImplementation((_team, _csrf, _payload, signal) => {
      signals.push(signal)
      return new Promise((_resolve, reject) =>
        signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError'))),
      )
    })
    await send()
    await fill('Keep unsent draft')
    expect(signals).toHaveLength(mode === 'chat' ? 1 : 2)
    await renew()
    expect(signals.every((signal) => signal.aborted)).toBe(true)
    expect(host.querySelector('option[value="model-one"]')).toBeNull()
    expect(
      host.querySelector<HTMLTextAreaElement>(
        'textarea[name="prompt"], textarea[name="comparison_prompt"]',
      )!.value,
    ).toBe('Keep unsent draft')
  },
)
it.each(['chat', 'compare'] as const)(
  '%s preserves history on a manual same-actor CSRF cache replacement',
  async (mode) => {
    await ready(mode)
    await send()
    const dispatched = vi.mocked(runTeamChat).mock.calls.length
    await act(async () =>
      cache.setQueryData(sessionKey, { ...session, csrf_token: 'csrf-replaced' }),
    )
    await settle()
    expect(host.textContent).toContain('Completed private text')
    expect(host.querySelector('option[value="model-one"]')).not.toBeNull()
    await send()
    expect(vi.mocked(runTeamChat).mock.calls[dispatched][1]).toBe('csrf-replaced')
    expect(vi.mocked(runTeamChat).mock.calls[dispatched][2].messages).toHaveLength(3)
    expect(getSession).toHaveBeenCalledOnce()
  },
)
