import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PlaygroundPage from './index'
import client from '@/api/client'
import i18n from '@/i18n'
import { getTeamModels, runTeamChat } from '@/api/playground-team'
import { getGatewayModels, runChat } from '@/api/playground'

let actor = 'usr_one',
  csrf = 'csrf-one'
vi.mock('@/hooks/use-auth', () => ({
  useSession: () => ({ data: { user: { id: actor, role: 'member' }, csrf_token: csrf } }),
}))
vi.mock('@/api/playground-team', async (original) => ({
  ...(await original<typeof import('@/api/playground-team')>()),
  getTeamModels: vi.fn(),
  runTeamChat: vi.fn(),
}))
vi.mock('@/api/playground', async (original) => ({
  ...(await original<typeof import('@/api/playground')>()),
  getGatewayModels: vi.fn(),
  runChat: vi.fn(),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let teamFailure: boolean, requests: string[]
const nativeModel = {
  id: 'team-native-name',
  model_id: 'mdl_team',
  protocols: ['openai_chat' as const],
  input_capabilities: { openai_chat: [] },
  attachment_scope: 'team' as const,
  personal_attachments: false,
}
const result = { text: 'Team reply', requestId: 'req_team', usage: null, finishReason: 'stop' }
beforeEach(async () => {
  actor = 'usr_one'
  csrf = 'csrf-one'
  teamFailure = false
  requests = []
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config.url ?? '')
    if (config.url !== '/teams' || config.params.status !== 'active' || teamFailure)
      throw new Error('Team unavailable')
    return {
      data: {
        items: [
          { id: 'tea_one', name: 'Research Team', status: 'active' },
          { id: 'tea_two', name: 'Second Team', status: 'active' },
        ],
        next_cursor: null,
      },
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
    }
  }
  vi.mocked(getTeamModels).mockResolvedValue([nativeModel])
  vi.mocked(runTeamChat).mockImplementation(async (_team, _csrf, _body, _signal, update) => {
    update(result)
    return result
  })
  vi.mocked(getGatewayModels).mockResolvedValue([{ id: 'key-model' }])
  vi.mocked(runChat).mockResolvedValue(result)
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
  vi.resetAllMocks()
})
async function render(path = '/playground') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter initialEntries={[path]}>
          <PlaygroundPage />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await settle()
}
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5))
  })
}
function button(text: string) {
  return [...host.querySelectorAll('button')].find((item) => item.textContent === text)!
}
async function click(text: string) {
  await act(async () => button(text).click())
  await settle()
}
async function select(name: string, value: string) {
  await act(async () => {
    const item = host.querySelector<HTMLSelectElement>(`select[name="${name}"]`)!
    item.value = value
    item.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await settle()
}
async function fill(name: string, value: string) {
  await act(async () => {
    const item = host.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[name="${name}"]`)!
    Object.getOwnPropertyDescriptor(
      item instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype,
      'value',
    )!.set!.call(item, value)
    item.dispatchEvent(new Event('input', { bubbles: true }))
  })
}
async function teamReady(path = '/playground?team=tea_one&model=mdl_team') {
  await render(path)
  await click('Load Team models')
}
describe('Team Session Playground', () => {
  it('shows Team guidance and recorded partial usage in both languages without a Key', async () => {
    const partial = {
      ...result,
      usage: { prompt_tokens: 4, completion_tokens: 1, total_tokens: null },
    }
    vi.mocked(runTeamChat).mockImplementation(async (_team, _csrf, _body, _signal, update) => {
      update(partial)
      return partial
    })
    await teamReady()
    expect(host.textContent).toContain(
      'Select your Team and load its models to start a real conversation.',
    )
    expect(host.textContent).not.toContain('Verify a key and select a model')
    await fill('prompt', 'Native partial usage')
    await click('Send message')
    expect(host.textContent).toContain('Total Unknown Tokens')
    expect(host.textContent).not.toContain('The upstream returned no usage data')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('总计 未知 Tokens')
    expect(host.textContent).not.toContain('上游未返回用量数据')
  })
  it('loads the next own-Team page and keeps invocation unavailable until the selected Team is present', async () => {
    const cursors: unknown[] = []
    client.defaults.adapter = async (config) => {
      cursors.push(config.params.cursor)
      return {
        data: {
          items: [
            {
              id: config.params.cursor ? 'tea_one' : 'tea_other',
              name: config.params.cursor ? 'Research Team' : 'Other Team',
              status: 'active',
            },
          ],
          next_cursor: config.params.cursor ? null : 'page-two',
        },
        config,
        status: 200,
        statusText: 'OK',
        headers: new AxiosHeaders(),
      }
    }
    await render('/playground?team=tea_one')
    expect(button('Load Team models').disabled).toBe(true)
    await click('Load more Teams')
    expect(cursors).toEqual([undefined, 'page-two'])
    expect(button('Load Team models').disabled).toBe(false)
    await click('Load Team models')
    expect(vi.mocked(getTeamModels).mock.calls[0][0]).toBe('tea_one')
  })
  it('aborts discovery and ignores its late result after an explicit Team switch', async () => {
    await render('/playground?team=tea_one')
    let finish!: (models: (typeof nativeModel)[]) => void
    vi.mocked(getTeamModels).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    await click('Load Team models')
    const signal = vi.mocked(getTeamModels).mock.calls[0][1]
    await select('team', 'tea_two')
    expect(signal.aborted).toBe(true)
    await act(async () => finish([nativeModel]))
    expect(host.querySelector<HTMLSelectElement>('[name="model"]')!.value).toBe('')
    expect(host.textContent).not.toContain('team-native-name')
    expect(runTeamChat).not.toHaveBeenCalled()
  })
  it.each(['length', 'tool_calls', 'content_filter', null])(
    'does not replay a noncompleted Team turn with finish reason %s',
    async (finishReason) => {
      await teamReady()
      vi.mocked(runTeamChat).mockResolvedValue({ ...result, finishReason })
      await fill('prompt', 'First')
      await click('Send message')
      await fill('prompt', 'Second')
      await click('Send message')
      expect(vi.mocked(runTeamChat).mock.calls[1][2].messages).toEqual([
        { role: 'user', content: 'Second' },
      ])
    },
  )
  it('does not replay a native refusal even when its finish reason is stop', async () => {
    await teamReady()
    vi.mocked(runTeamChat).mockResolvedValue({ ...result, refused: true })
    await fill('prompt', 'First')
    await click('Send message')
    await fill('prompt', 'Second')
    await click('Send message')
    expect(vi.mocked(runTeamChat).mock.calls[1][2].messages).toHaveLength(1)
  })
  it('defaults to transient Key without reading a Team directory', async () => {
    await render()
    expect(host.querySelector<HTMLSelectElement>('[name="source"]')!.value).toBe('key')
    expect(requests).toEqual([])
    expect(host.querySelector('[name="api_key"]')).not.toBeNull()
  })
  it('selects an explicit own Team and maps a stable deep link through fresh models', async () => {
    await teamReady()
    expect(vi.mocked(getTeamModels).mock.calls[0][0]).toBe('tea_one')
    expect(host.querySelector<HTMLSelectElement>('[name="model"]')!.value).toBe('team-native-name')
    expect(host.querySelector('[name="api_key"]')).toBeNull()
    expect(
      host.querySelector<HTMLButtonElement>(
        '[aria-label="Team Sessions do not support attachments"]',
      )!.disabled,
    ).toBe(true)
    expect(button('Get code').disabled).toBe(true)
    await fill('prompt', 'Hello')
    await click('Send message')
    expect(runTeamChat).toHaveBeenCalledWith(
      'tea_one',
      'csrf-one',
      expect.objectContaining({
        model: 'team-native-name',
        messages: [{ role: 'user', content: 'Hello' }],
      }),
      expect.any(AbortSignal),
      expect.any(Function),
    )
    expect(runChat).not.toHaveBeenCalled()
    expect(host.textContent).toContain('Team reply')
  })
  it('uses current same-actor CSRF after rotation without replaying or discarding history', async () => {
    await teamReady()
    await fill('prompt', 'First')
    await click('Send message')
    csrf = 'csrf-rotated'
    await render()
    await fill('prompt', 'Second')
    await click('Send message')
    expect(vi.mocked(runTeamChat).mock.calls[1][1]).toBe('csrf-rotated')
    expect(vi.mocked(runTeamChat).mock.calls[1][2].messages).toHaveLength(3)
  })
  it('clears transient Key and Team history when source or Team changes', async () => {
    await render()
    await fill('api_key', 'secret-only-local')
    await select('source', 'team')
    await select('team', 'tea_one')
    await click('Load Team models')
    await fill('prompt', 'Hello')
    await click('Send message')
    await select('team', 'tea_two')
    expect(host.textContent).not.toContain('Team reply')
    expect(host.querySelector<HTMLSelectElement>('[name="model"]')!.value).toBe('')
    await select('source', 'key')
    expect(host.querySelector<HTMLInputElement>('[name="api_key"]')!.value).toBe('')
  })
  it('does not borrow a name or select a fallback when the expected model is absent', async () => {
    await teamReady('/playground?team=tea_one&model=mdl_removed')
    expect(host.querySelector<HTMLSelectElement>('[name="model"]')!.value).toBe('')
    expect(host.textContent).toContain('requested model is not currently callable')
    expect(runTeamChat).not.toHaveBeenCalled()
  })
  it('shows Team comparison as unsupported without dispatch or a Key fallback', async () => {
    await teamReady()
    await act(async () =>
      [...host.querySelectorAll<HTMLButtonElement>('[role="tab"]')]
        .find((item) => item.textContent === 'Model comparison')!
        .click(),
    )
    expect(host.textContent).toContain('Team Sessions do not support model comparison')
    expect(host.querySelector('[name="comparison_key"]')).toBeNull()
    expect(runChat).not.toHaveBeenCalled()
    expect(runTeamChat).not.toHaveBeenCalled()
  })
  it('hides models and transcript when current membership cannot be refreshed', async () => {
    await teamReady()
    await fill('prompt', 'Hello')
    await click('Send message')
    teamFailure = true
    await click('Refresh Teams')
    expect(host.textContent).not.toContain('Team reply')
    expect(host.querySelector<HTMLSelectElement>('[name="model"]')!.value).toBe('')
    expect(button('Load Team models').disabled).toBe(true)
  })
  it('aborts active Team work and ignores late callbacks after actor change', async () => {
    await teamReady()
    let finish!: (value: typeof result) => void
    vi.mocked(runTeamChat).mockImplementation(
      (_team, _csrf, _body, _signal, update) =>
        new Promise((resolve) => {
          finish = (value) => {
            update(value)
            resolve(value)
          }
        }),
    )
    await fill('prompt', 'Held')
    await click('Send message')
    const signal = vi.mocked(runTeamChat).mock.calls[0][3]
    actor = 'usr_other'
    await render()
    expect(signal.aborted).toBe(true)
    await act(async () => finish(result))
    expect(host.textContent).not.toContain('Team reply')
    expect(host.textContent).not.toContain('Held')
  })
  it('preserves the selected Team and drafts through live Chinese switching', async () => {
    await teamReady()
    await fill('prompt', 'Draft')
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('凭证来源')
    expect(host.textContent).toContain('Team 会话仅支持文本')
    expect(host.querySelector<HTMLTextAreaElement>('[name="prompt"]')!.value).toBe('Draft')
    expect(host.querySelector<HTMLSelectElement>('[name="team"]')!.value).toBe('tea_one')
  })
})
