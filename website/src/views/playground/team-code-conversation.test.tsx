import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import ChatWorkbench from './chat'
import {
  getOwnActiveTeams,
  getTeamModels,
  runTeamChat,
  runTeamResponses,
  runTeamMessages,
  runTeamGemini,
} from '@/api/playground-team'
import type { GatewayModel } from '@/types/playground'
let actor = 'usr_one',
  refreshing = false,
  sessionError = false,
  csrf = 'csrf-live-never-export'
const team = 'tea_01k6kwwwwwwwwwwwwwwwwwwwww'
vi.mock('@/hooks/use-auth', () => ({
  useSession: () => ({
    data: { user: { id: actor }, csrf_token: csrf },
    isFetching: refreshing,
    isError: sessionError,
  }),
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
let root: Root, host: HTMLDivElement, cache: QueryClient
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const
const models: GatewayModel[] = [
  {
    id: 'model-one',
    protocols: [...protocols],
    attachment_scope: 'team',
    personal_attachments: false,
  },
  {
    id: 'model-two',
    protocols: ['openai_chat'],
    attachment_scope: 'team',
    personal_attachments: false,
  },
]
beforeEach(async () => {
  actor = 'usr_one'
  refreshing = false
  sessionError = false
  csrf = 'csrf-live-never-export'
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  vi.mocked(getOwnActiveTeams).mockResolvedValue({
    items: [
      {
        id: team,
        name: 'Selected Team',
        status: 'active',
        description: '',
        created_at: '',
        model_ids: [],
      },
    ],
    next_cursor: null,
  })
  vi.mocked(getTeamModels).mockResolvedValue(models)
  vi.mocked(runTeamChat).mockResolvedValue({
    text: 'Completed text',
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
})
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5))
  })
}
async function render(source: 'team' | 'key' = 'team', teamId = team) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <ChatWorkbench
          key={`${sessionError ? '' : actor}:${source}:${teamId}`}
          source={source}
          teamId={teamId}
          onTeam={vi.fn()}
          onSource={vi.fn()}
        />
      </QueryClientProvider>,
    ),
  )
  await settle()
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
  const input = host.querySelector<HTMLSelectElement>(`select[name="${label}"]`)!
  await act(async () => {
    input.value = value
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function ready() {
  await render()
  await click('Load Team models')
}
async function send(value: string) {
  await fill('prompt', value)
  await click('Send message')
}
function code() {
  return document.querySelector('pre')!.textContent!
}

it.each(protocols)('exports reviewed settings and reset defaults for Team %s', async (protocol) => {
  await ready()
  await select('protocol', protocol)
  await fill('temperature', '0.2')
  await fill('top_p', '0.4')
  await fill('max_tokens', '73')
  await fill('system', 'Reviewed system')
  await fill('prompt', 'Current draft')
  await click('Get code')
  expect(code()).toContain('/api/v1/teams/' + team)
  expect(code()).toContain('Reviewed system')
  expect(code()).toContain('Current draft')
  expect(code()).toContain('73')
  expect(code()).toContain('0.2')
  expect(code()).toContain('0.4')
  expect(code()).not.toMatch(
    /ROUTEX_API_KEY|csrf-live-never-export|Authorization|x-api-key|x-goog-api-key/,
  )
  await click('Close')
  await click('Restore default parameters')
  await click('Get code')
  expect(code()).not.toContain('Reviewed system')
  expect(code()).toContain('Current draft')
  expect(code()).toContain('2048')
  expect(code()).toContain('0.7')
  expect(runTeamChat).not.toHaveBeenCalled()
  expect(runTeamResponses).not.toHaveBeenCalled()
  expect(runTeamMessages).not.toHaveBeenCalled()
  expect(runTeamGemini).not.toHaveBeenCalled()
})
it('exports completed text history and excludes incomplete Chat turns', async () => {
  await ready()
  await send('Completed prompt')
  vi.mocked(runTeamChat).mockResolvedValue({
    text: 'Incomplete answer',
    usage: null,
    finishReason: 'length',
    requestId: 'req_incomplete',
  })
  await send('Incomplete prompt')
  await fill('prompt', 'Next draft')
  await click('Get code')
  expect(code()).toContain('Completed prompt')
  expect(code()).toContain('Completed text')
  expect(code()).toContain('Next draft')
  expect(code()).not.toMatch(/Incomplete prompt|Incomplete answer/)
})
it.each(['renewing', 'error', 'actor', 'source', 'team', 'missing-csrf'] as const)(
  'closes captured dialog after %s authority change without restoring it',
  async (kind) => {
    await ready()
    await fill('prompt', 'Captured private draft')
    await click('Get code')
    if (kind === 'renewing') refreshing = true
    if (kind === 'error') sessionError = true
    if (kind === 'actor') actor = 'usr_other'
    if (kind === 'missing-csrf') csrf = ''
    await render(kind === 'source' ? 'key' : 'team', kind === 'team' ? 'tea_other' : team)
    expect(document.querySelector('pre')).toBeNull()
    refreshing = false
    sessionError = false
    csrf = 'csrf-renewed'
    await render()
    expect(document.querySelector('pre')).toBeNull()
    if (kind === 'renewing' || kind === 'missing-csrf') {
      expect(button('Get code').disabled).toBe(true)
      expect(host.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe(
        'Captured private draft',
      )
      await click('Load Team models')
      expect(button('Get code').disabled).toBe(false)
      expect(document.querySelector('pre')).toBeNull()
    }
  },
)
it('keeps code unavailable without a current Team membership or a discovered model', async () => {
  await render()
  expect(button('Get code').disabled).toBe(true)
  vi.mocked(getTeamModels).mockResolvedValue([])
  await click('Load Team models')
  expect(button('Get code').disabled).toBe(true)
  vi.mocked(getTeamModels).mockResolvedValue(models)
  await click('Load Team models')
  await click('Get code')
  vi.mocked(getOwnActiveTeams).mockResolvedValue({ items: [], next_cursor: null })
  await act(async () => cache.invalidateQueries({ queryKey: ['playground', 'teams'] }))
  await settle()
  expect(document.querySelector('pre')).toBeNull()
  expect(button('Get code').disabled).toBe(true)
})
it('clears the capture when native protocol or model selection changes', async () => {
  await ready()
  await click('Get code')
  await select('protocol', 'openai_responses')
  expect(document.querySelector('pre')).toBeNull()
  await click('Get code')
  await select('model', 'model-two')
  expect(document.querySelector('pre')).toBeNull()
})

it.each(protocols)('captures only completed plaintext history for native %s', async (protocol) => {
  await ready()
  await select('protocol', protocol)
  const completed = {
    text: 'Native completed text',
    usage: null,
    finishReason: 'stop',
    requestId: 'req_native',
    responseStatus: 'completed' as const,
    messageStatus: 'completed' as const,
    generationStatus: 'completed' as const,
    nonTextOutput: false,
  }
  vi.mocked(runTeamChat).mockResolvedValue(completed)
  vi.mocked(runTeamResponses).mockResolvedValue(completed)
  vi.mocked(runTeamMessages).mockResolvedValue(completed)
  vi.mocked(runTeamGemini).mockResolvedValue(completed)
  await send('Native completed prompt')
  await fill('prompt', 'Native next draft')
  await click('Get code')
  expect(code()).toContain('Native completed text')
  expect(code()).toContain('Native completed prompt')
  expect(code()).toContain('Native next draft')
})
it('preserves captured Team code while bilingual source guidance switches live', async () => {
  await ready()
  await fill('prompt', 'Reviewed bilingual draft')
  await click('Get code')
  const captured = code()
  await act(async () => i18n.changeLanguage('zh'))
  expect(code()).toBe(captured)
  expect(document.body.textContent).toContain('独立登录')
})
