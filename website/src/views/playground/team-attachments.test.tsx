import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/i18n'
import ChatWorkbench from './chat'
import CompareWorkbench from './compare'
import { uploadAttachment, deleteAttachment, AttachmentError } from '@/api/attachments'
import type { TeamAttachment } from '@/types/attachments'
import {
  uploadTeamAttachment,
  deleteTeamAttachment,
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
  uploadTeamAttachment: vi.fn(),
  deleteTeamAttachment: vi.fn(),
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
let root: Root, host: HTMLDivElement, cache: QueryClient
const protocols = [
  'openai_chat',
  'openai_responses',
  'anthropic_messages',
  'gemini_generate_content',
] as const
const membership = 'tmb_original'
const object: TeamAttachment = {
  id: 'obj_01k6kwwwwwwwwwwwwwwwwwwwww',
  name: 'private.png',
  mime: 'image/png',
  size: 3,
  state: 'ready',
  created_at: '2026-10-04T00:00:00Z',
  expires_at: '2026-10-04T01:00:00Z',
  attachment_team_id: team,
  attachment_membership_id: membership,
  creator_user_id: 'usr_one',
}
const models: GatewayModel[] = [
  {
    id: 'model-one',
    protocols: [...protocols],
    attachment_scope: 'team',
    personal_attachments: false,
    attachment_team_id: team,
    attachment_membership_id: membership,
    input_capabilities: Object.fromEntries(
      protocols.map((protocol) => [protocol, ['image', 'pdf']]),
    ),
  },
]
let comparison = false
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
beforeEach(async () => {
  comparison = false
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
  vi.mocked(uploadTeamAttachment).mockResolvedValue(object)
  vi.mocked(deleteTeamAttachment).mockResolvedValue({ ...object, state: 'deleted' })
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
        {comparison ? (
          <CompareWorkbench source={source} teamId={teamId} onSource={vi.fn()} onTeam={vi.fn()} />
        ) : (
          <ChatWorkbench
            key={`${sessionError ? '' : actor}:${source}:${teamId}`}
            source={source}
            teamId={teamId}
            onTeam={vi.fn()}
            onSource={vi.fn()}
          />
        )}
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
async function upload(name = 'private.png') {
  const input = host.querySelector<HTMLInputElement>('input[type="file"]')!
  expect(input.disabled).toBe(false)
  await act(async () => {
    Object.defineProperty(input, 'files', {
      configurable: true,
      value: [new File(['png'], name, { type: 'image/png' })],
    })
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await settle()
}
async function compareSend(value = 'Shared media prompt') {
  const input = host.querySelector<HTMLTextAreaElement>('[name="comparison_prompt"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () =>
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })),
  )
  await settle()
}
it.each(protocols)(
  'uses one creator-private attachment in native %s and excludes its reference from completed history',
  async (protocol) => {
    await ready()
    await select('protocol', protocol)
    vi.mocked(runTeamResponses).mockResolvedValue({
      text: 'Completed text',
      usage: null,
      responseStatus: 'completed',
      finishReason: null,
      nonTextOutput: false,
      requestId: 'req_one',
    })
    vi.mocked(runTeamMessages).mockResolvedValue({
      text: 'Completed text',
      usage: null,
      messageStatus: 'completed',
      nonTextOutput: false,
      finishReason: 'end_turn',
      requestId: 'req_one',
    })
    vi.mocked(runTeamGemini).mockResolvedValue({
      text: 'Completed text',
      usage: null,
      generationStatus: 'completed',
      nonTextOutput: false,
      finishReason: 'STOP',
      requestId: 'req_one',
    })
    await upload()
    expect(button('Get code').disabled).toBe(true)
    await send('Describe private image')
    const run =
      protocol === 'openai_chat'
        ? runTeamChat
        : protocol === 'openai_responses'
          ? runTeamResponses
          : protocol === 'anthropic_messages'
            ? runTeamMessages
            : runTeamGemini
    expect(JSON.stringify(vi.mocked(run).mock.calls[0][2])).toContain(
      'routex://attachments/' + object.id,
    )
    expect(uploadTeamAttachment).toHaveBeenCalledWith(
      team,
      expect.any(File),
      csrf,
      expect.any(AbortSignal),
    )
    expect(deleteTeamAttachment).toHaveBeenCalledWith(team, object.id, csrf)
    await send('Next text only')
    const next = JSON.stringify(vi.mocked(run).mock.calls[1][2])
    expect(next).toContain('Describe private image')
    expect(next).toContain('Completed text')
    expect(next).not.toContain('routex://')
    expect(uploadAttachment).not.toHaveBeenCalled()
    expect(deleteAttachment).not.toHaveBeenCalled()
  },
)
it.each([
  { attachment_team_id: 'tea_other' },
  { attachment_membership_id: 'tmb_rejoined' },
  { creator_user_id: 'usr_other' },
])('never adopts or deletes unexpectedly owned upload %j', async (change) => {
  vi.mocked(uploadTeamAttachment).mockResolvedValue({ ...object, ...change })
  await ready()
  await upload()
  expect(host.querySelector('[aria-label="Remove private.png"]')).toBeNull()
  expect(deleteTeamAttachment).not.toHaveBeenCalled()
  expect(runTeamChat).not.toHaveBeenCalled()
})
it('clears media and aborts upload on renewed Session without clearing text or replaying', async () => {
  const pending = deferred<TeamAttachment>()
  vi.mocked(uploadTeamAttachment).mockReturnValue(pending.promise)
  await ready()
  await fill('prompt', 'Unsent text')
  await upload()
  const signal = vi.mocked(uploadTeamAttachment).mock.calls[0][3]!
  refreshing = true
  await render()
  expect(signal.aborted).toBe(true)
  expect(host.textContent).not.toContain('private.png')
  expect(host.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe('Unsent text')
  await act(async () => pending.resolve(object))
  await settle()
  expect(host.querySelector('[aria-label="Remove private.png"]')).toBeNull()
  refreshing = false
  csrf = 'rotated-csrf'
  await render()
  await click('Load Team models')
  expect(uploadTeamAttachment).toHaveBeenCalledTimes(1)
  expect(runTeamChat).not.toHaveBeenCalled()
  await upload('new.png')
  expect(vi.mocked(uploadTeamAttachment).mock.calls[1][2]).toBe('rotated-csrf')
})
it('does not offer media after leave/rejoin returns a different exact membership', async () => {
  await ready()
  await upload()
  await click('Refresh Teams')
  vi.mocked(getTeamModels).mockResolvedValue(
    models.map((model) => ({ ...model, attachment_membership_id: 'tmb_rejoined' })),
  )
  await click('Load Team models')
  expect(host.querySelector('[aria-label="Remove private.png"]')).toBeNull()
  expect(deleteTeamAttachment).toHaveBeenCalledWith(team, object.id, csrf)
  expect(runTeamChat).not.toHaveBeenCalled()
})
it('shares one upload and retains it until every comparison lane settles, including independent cancellation', async () => {
  comparison = true
  await ready()
  await upload()
  const pending = deferred<Awaited<ReturnType<typeof runTeamChat>>>()
  vi.mocked(runTeamChat).mockImplementationOnce(() => pending.promise)
  await compareSend()
  expect(runTeamChat).toHaveBeenCalledTimes(2)
  expect(deleteTeamAttachment).not.toHaveBeenCalled()
  for (const call of vi.mocked(runTeamChat).mock.calls)
    expect(JSON.stringify(call[2])).toContain('routex://attachments/' + object.id)
  await click('Stop comparison 1')
  expect(vi.mocked(runTeamChat).mock.calls[0][3].aborted).toBe(true)
  expect(deleteTeamAttachment).not.toHaveBeenCalled()
  await act(async () =>
    pending.resolve({
      text: 'Late canceled output',
      usage: null,
      finishReason: 'stop',
      requestId: 'req_late',
    }),
  )
  await settle()
  expect(deleteTeamAttachment).toHaveBeenCalledTimes(1)
  expect(host.textContent).not.toContain('Late canceled output')
  await compareSend('Next turn')
  const next = vi
    .mocked(runTeamChat)
    .mock.calls.slice(2)
    .map((call) => JSON.stringify(call[2]))
  expect(next.every((body) => !body.includes('routex://'))).toBe(true)
  expect(next[0]).not.toContain('Shared media prompt')
  expect(next[1]).toContain('Shared media prompt')
})
it('unmount aborts lanes but defers submitted object deletion until their transports settle', async () => {
  comparison = true
  await ready()
  await upload()
  const first = deferred<Awaited<ReturnType<typeof runTeamChat>>>(),
    second = deferred<Awaited<ReturnType<typeof runTeamChat>>>()
  vi.mocked(runTeamChat).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
  await compareSend()
  await act(async () => root.render(null))
  await settle()
  expect(vi.mocked(runTeamChat).mock.calls.every((call) => call[3].aborted)).toBe(true)
  expect(deleteTeamAttachment).not.toHaveBeenCalled()
  await act(async () =>
    first.resolve({ text: 'Ignored', usage: null, finishReason: 'stop', requestId: 'first' }),
  )
  await settle()
  expect(deleteTeamAttachment).not.toHaveBeenCalled()
  await act(async () =>
    second.resolve({ text: 'Ignored', usage: null, finishReason: 'stop', requestId: 'second' }),
  )
  await settle()
  expect(deleteTeamAttachment).toHaveBeenCalledTimes(1)
})
it('intersects every selected comparison protocol and fails closed with missing capabilities', async () => {
  comparison = true
  vi.mocked(getTeamModels).mockResolvedValue([
    {
      ...models[0],
      input_capabilities: {
        openai_chat: ['image'],
        openai_responses: ['pdf'],
        anthropic_messages: [],
        gemini_generate_content: [],
      },
    },
  ])
  await ready()
  expect(host.querySelector<HTMLInputElement>('input[type="file"]')!.accept).toBe(
    'image/png,image/jpeg',
  )
  const second = host.querySelectorAll<HTMLSelectElement>(
    'select[aria-label="Comparison protocol 2"]',
  )[0]
  await act(async () => {
    second.value = 'openai_responses'
    second.dispatchEvent(new Event('change', { bubbles: true }))
  })
  expect(host.querySelector<HTMLInputElement>('input[type="file"]')!.disabled).toBe(true)
  expect(uploadTeamAttachment).not.toHaveBeenCalled()
})
it('changes bilingual media guidance live without losing the private draft', async () => {
  await ready()
  await upload()
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('一小时后过期')
  expect(host.textContent).toContain('private.png')
  expect(button('获取代码').disabled).toBe(true)
})
it('uses one shared media object across four independently selected native lanes', async () => {
  comparison = true
  await ready()
  await click('Add comparison')
  await click('Add comparison')
  for (let index = 0; index < protocols.length; index++) {
    const input = host.querySelector<HTMLSelectElement>(
      `select[aria-label="Comparison protocol ${index + 1}"]`,
    )!
    await act(async () => {
      input.value = protocols[index]
      input.dispatchEvent(new Event('change', { bubbles: true }))
    })
  }
  vi.mocked(runTeamResponses).mockResolvedValue({
    text: 'Responses text',
    usage: null,
    responseStatus: 'completed',
    finishReason: null,
    nonTextOutput: false,
    requestId: 'res',
  })
  vi.mocked(runTeamMessages).mockResolvedValue({
    text: 'Messages text',
    usage: null,
    messageStatus: 'completed',
    finishReason: 'end_turn',
    nonTextOutput: false,
    requestId: 'msg',
  })
  vi.mocked(runTeamGemini).mockResolvedValue({
    text: 'Gemini text',
    usage: null,
    generationStatus: 'completed',
    finishReason: 'STOP',
    nonTextOutput: false,
    requestId: 'gem',
  })
  await upload()
  for (let index = 1; index <= 4; index++)
    expect(button(`Get code for comparison ${index}`).disabled).toBe(true)
  await compareSend()
  for (const run of [runTeamChat, runTeamResponses, runTeamMessages, runTeamGemini]) {
    expect(run).toHaveBeenCalledTimes(1)
    const call = vi.mocked(run).mock.calls[0]
    expect(call[0]).toBe(team)
    expect(call[1]).toBe(csrf)
    expect(JSON.stringify(call[2])).toContain('routex://attachments/' + object.id)
  }
  expect(uploadTeamAttachment).toHaveBeenCalledTimes(1)
  expect(deleteTeamAttachment).toHaveBeenCalledTimes(1)
  expect(uploadAttachment).not.toHaveBeenCalled()
})
it.each(['actor', 'team', 'source'] as const)(
  'clears a Team media draft when %s changes',
  async (change) => {
    await ready()
    await upload()
    await fill('prompt', 'Draft')
    if (change === 'actor') {
      actor = 'usr_two'
      await render()
    }
    if (change === 'team') await render('team', 'tea_other')
    if (change === 'source') await render('key')
    expect(host.querySelector('[aria-label="Remove private.png"]')).toBeNull()
    expect(deleteTeamAttachment).toHaveBeenCalledWith(team, object.id, csrf)
    expect(runTeamChat).not.toHaveBeenCalled()
    expect(uploadAttachment).not.toHaveBeenCalled()
  },
)
it.each([
  { attachment_team_id: undefined },
  { attachment_membership_id: undefined },
  { attachment_scope: 'user' as const },
  { input_capabilities: { openai_chat: [] } },
])('fails closed on missing or non-Team discovery context %j', async (change) => {
  vi.mocked(getTeamModels).mockResolvedValue([{ ...models[0], ...change }])
  await ready()
  expect(host.querySelector<HTMLInputElement>('input[type="file"]')!.disabled).toBe(true)
  expect(uploadTeamAttachment).not.toHaveBeenCalled()
  expect(uploadAttachment).not.toHaveBeenCalled()
})
it('a protocol change discards media that the newly selected native protocol cannot accept', async () => {
  vi.mocked(getTeamModels).mockResolvedValue([
    {
      ...models[0],
      input_capabilities: {
        openai_chat: ['image'],
        openai_responses: [],
        anthropic_messages: [],
        gemini_generate_content: [],
      },
    },
  ])
  await ready()
  await upload()
  await select('protocol', 'openai_responses')
  expect(host.querySelector('[aria-label="Remove private.png"]')).toBeNull()
  expect(host.querySelector<HTMLInputElement>('input[type="file"]')!.disabled).toBe(true)
  expect(deleteTeamAttachment).toHaveBeenCalledWith(team, object.id, csrf)
})

it('an upload authority denial clears the draft and requires explicit Team reconfirmation', async () => {
  await ready()
  await upload()
  vi.mocked(uploadTeamAttachment).mockRejectedValueOnce(new AttachmentError(403))
  await upload('second.png')
  expect(host.querySelector('[aria-label="Remove private.png"]')).toBeNull()
  expect(button('Load Team models').disabled).toBe(true)
  expect(runTeamChat).not.toHaveBeenCalled()
  expect(uploadAttachment).not.toHaveBeenCalled()
})
it('a storage budget denial leaves the existing draft available without dispatch or replay', async () => {
  await ready()
  await upload()
  vi.mocked(uploadTeamAttachment).mockRejectedValueOnce(new AttachmentError(429))
  await upload('second.png')
  expect(host.querySelector('[aria-label="Remove private.png"]')).not.toBeNull()
  expect(button('Load Team models').disabled).toBe(false)
  expect(runTeamChat).not.toHaveBeenCalled()
  expect(uploadTeamAttachment).toHaveBeenCalledTimes(2)
})
it('does not treat failed object cleanup as confirmed deletion or repeat inference', async () => {
  await ready()
  await upload()
  vi.mocked(deleteTeamAttachment).mockRejectedValueOnce(new AttachmentError(403))
  await send('Describe')
  expect(runTeamChat).toHaveBeenCalledTimes(1)
  expect(deleteTeamAttachment).toHaveBeenCalledTimes(1)
  expect(host.querySelector('[role="alert"]')).not.toBeNull()
})
