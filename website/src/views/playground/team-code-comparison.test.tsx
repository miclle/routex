import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import CompareWorkbench from './compare'
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
        <CompareWorkbench source={source} teamId={teamId} onTeam={vi.fn()} onSource={vi.fn()} />
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
async function fill(value: string) {
  const input = host.querySelector<HTMLTextAreaElement>('[name="comparison_prompt"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
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
  await render()
  await click('Load Team models')
}
async function send(value: string) {
  await fill(value)
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await settle()
}
function code() {
  return document.querySelector('pre')!.textContent!
}
it.each(protocols)(
  'captures %s through an independently logged-in Team endpoint without dispatching',
  async (protocol) => {
    await ready()
    await select('Comparison protocol 1', protocol)
    await fill('Current draft')
    await click('Get code for comparison 1')
    expect(code()).toContain('/api/v1/teams/' + team)
    expect(code()).toContain('Current draft')
    expect(code()).not.toMatch(
      /ROUTEX_API_KEY|csrf-live-never-export|Authorization|x-api-key|x-goog-api-key/,
    )
    expect(runTeamChat).not.toHaveBeenCalled()
    expect(runTeamResponses).not.toHaveBeenCalled()
    expect(runTeamMessages).not.toHaveBeenCalled()
    expect(runTeamGemini).not.toHaveBeenCalled()
  },
)
it('includes only completed plain-text turns and preserves their original prompt', async () => {
  await ready()
  await send('Completed prompt')
  vi.mocked(runTeamChat).mockResolvedValue({
    text: 'Truncated answer',
    usage: null,
    finishReason: 'length',
    requestId: 'req_incomplete',
  })
  await send('Truncated prompt')
  vi.mocked(runTeamChat).mockResolvedValue({
    text: 'Tool handoff',
    usage: null,
    finishReason: 'tool_calls',
    requestId: 'req_handoff',
  })
  await send('Handoff prompt')
  await fill('Next draft')
  await click('Get code for comparison 1')
  expect(code()).toContain('Completed prompt')
  expect(code()).toContain('Completed text')
  expect(code()).toContain('Next draft')
  expect(code()).not.toMatch(/Truncated prompt|Truncated answer|Handoff prompt|Tool handoff/)
})
it.each(['renewing', 'error', 'actor', 'source', 'team', 'missing-csrf'] as const)(
  'hides and destroys capture after %s authority change',
  async (kind) => {
    await ready()
    await fill('Private captured draft')
    await click('Get code for comparison 1')
    if (kind === 'renewing') refreshing = true
    if (kind === 'error') sessionError = true
    if (kind === 'actor') actor = 'usr_other'
    if (kind === 'missing-csrf') csrf = ''
    await render(kind === 'source' ? 'key' : 'team', kind === 'team' ? 'tea_other' : team)
    expect(document.querySelector('pre')).toBeNull()
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    refreshing = false
    sessionError = false
    csrf = 'csrf-renewed'
    await render()
    expect(document.querySelector('pre')).toBeNull()
  },
)
it('removes capture when membership is rechecked and requires fresh model discovery', async () => {
  await ready()
  await click('Get code for comparison 1')
  vi.mocked(getOwnActiveTeams).mockResolvedValue({ items: [], next_cursor: null })
  await click('Refresh Teams')
  expect(document.querySelector('pre')).toBeNull()
  expect(button('Get code for comparison 1').disabled).toBe(true)
})
it('clears captured code on model/protocol/lane changes', async () => {
  await ready()
  await click('Get code for comparison 1')
  await select('Comparison model 1', 'model-two')
  expect(document.querySelector('pre')).toBeNull()
  await click('Get code for comparison 1')
  expect(code()).toContain('model-two')
  expect(code()).not.toContain('model-one')
  await select('Comparison model 1', 'model-one')
  expect(document.querySelector('pre')).toBeNull()
  await click('Get code for comparison 1')
  await select('Comparison protocol 1', 'openai_responses')
  expect(document.querySelector('pre')).toBeNull()
  await click('Add comparison')
  await click('Get code for comparison 3')
  await act(async () => button('Remove comparison 3').click())
  expect(document.querySelector('pre')).toBeNull()
})

it.each([
  { responseStatus: 'completed' as const, refused: true },
  { responseStatus: 'completed' as const, nonTextOutput: true },
  { responseStatus: 'incomplete' as const },
  { responseStatus: 'failed' as const },
  { responseStatus: 'in_progress' as const },
])('excludes noncompleted/nontext Responses history %j', async (state) => {
  await ready()
  await select('Comparison protocol 1', 'openai_responses')
  vi.mocked(runTeamResponses).mockResolvedValue({
    text: 'Excluded native answer',
    nonTextOutput: false,
    usage: null,
    finishReason: null,
    requestId: 'req_noncompleted',
    ...state,
  })
  await send('Excluded native prompt')
  await fill('Reviewed next prompt')
  await click('Get code for comparison 1')
  expect(code()).toContain('Reviewed next prompt')
  expect(code()).not.toContain('Excluded native')
})
it('never captures a running or canceled lane and ignores its late completion', async () => {
  await ready()
  let resolve!: (value: Awaited<ReturnType<typeof runTeamChat>>) => void
  const pending = new Promise<Awaited<ReturnType<typeof runTeamChat>>>((done) => {
    resolve = done
  })
  vi.mocked(runTeamChat).mockImplementationOnce(() => pending)
  await send('Canceled prompt')
  expect(button('Get code for comparison 1').disabled).toBe(true)
  await click('Stop comparison 1')
  await act(async () =>
    resolve({
      text: 'Late canceled text',
      usage: null,
      finishReason: 'stop',
      requestId: 'req_late',
    }),
  )
  await fill('New draft')
  await click('Get code for comparison 1')
  expect(code()).not.toMatch(/Canceled prompt|Late canceled text/)
  expect(code()).toContain('New draft')
})
it('does not expose capture before successful model discovery or for an empty catalog', async () => {
  await render()
  expect(button('Get code for comparison 1').disabled).toBe(true)
  vi.mocked(getTeamModels).mockResolvedValue([])
  await act(async () => button('Load Team models').click())
  await settle()
  expect(document.querySelector('pre')).toBeNull()
  expect(button('Get code for comparison 1').disabled).toBe(true)
})
