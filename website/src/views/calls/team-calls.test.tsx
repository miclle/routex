import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CallsPage from './index'
import client from '@/api/client'
import i18n from '@/i18n'
let actor = 'usr_member'
vi.mock('@/hooks/use-auth', () => ({
  useSession: () => ({ data: { user: { id: actor, role: 'admin' }, csrf_token: 'csrf' } }),
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const adapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  requests: { url?: string; params: unknown }[],
  failure: boolean
const call = {
  request_id: 'req_team_own',
  model_id: 'mdl_one',
  model_name: 'Team model',
  key_id: '',
  protocol: 'openai_chat',
  status: 'success',
  stream: true,
  started_at: '2026-10-02T00:00:00Z',
  completed_at: '2026-10-02T00:00:01Z',
  duration_ms: 1000,
  input_tokens: 2,
  output_tokens: 3,
}
beforeEach(async () => {
  actor = 'usr_member'
  failure = false
  requests = []
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push({ url: config.url, params: config.params })
    if (failure)
      throw new AxiosError('Membership removed', '', config, undefined, {
        data: { message: 'Forbidden' },
        config,
        status: 403,
        statusText: 'Forbidden',
        headers: new AxiosHeaders(),
      })
    return {
      data: config.url?.endsWith('/req_team_own') ? call : { items: [call], next_cursor: 'next' },
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
    }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = adapter
})
async function render(teamId = 'tea_one') {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <CallsPage teamId={teamId} />
      </QueryClientProvider>,
    ),
  )
  await settle()
}
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 10))
  })
}
async function click(text: string) {
  await act(async () =>
    [...host.querySelectorAll<HTMLButtonElement>('button')]
      .find((item) => item.textContent === text)!
      .click(),
  )
  await settle()
}
describe('own-actor Team calls', () => {
  it('reauthorizes a remounted Team list despite a fresh 60-second application cache', async () => {
    cache.setDefaultOptions({ queries: { retry: false, staleTime: 60_000 } })
    await render()
    expect(host.textContent).toContain('req_team_own')
    await act(async () => root.render(null))
    cache.setQueryData(['calls', 'team', actor, 'tea_one', {}], {
      pages: [{ items: [call], next_cursor: null }],
      pageParams: [null],
    })
    failure = true
    const before = requests.length
    await render()
    expect(requests.slice(before).map((item) => item.url)).toEqual(['/teams/tea_one/calls'])
    expect(host.textContent).not.toContain('req_team_own')
    expect(host.querySelector('[role="alert"]')).not.toBeNull()
  })
  it('reauthorizes cached Team detail instead of trusting a fresh recorded response', async () => {
    cache.setDefaultOptions({ queries: { retry: false, staleTime: 60_000 } })
    await render()
    cache.setQueryData(['call', 'team', actor, 'tea_one', 'req_team_own'], call)
    failure = true
    await click('Details')
    expect(requests.at(-1)?.url).toBe('/teams/tea_one/calls/req_team_own')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Team model')
  })
  it('uses only the exact Team endpoint and independent member cache, without Key filters or export', async () => {
    await render()
    expect(host.textContent).toContain('Your Team call records')
    expect(host.textContent).toContain('req_team_own')
    expect(host.textContent).toContain('Only your own calls')
    expect(host.querySelector('[name="key_id"]')).toBeNull()
    expect(host.querySelector('[name="user_id"]')).toBeNull()
    expect(host.textContent).not.toContain('Export CSV')
    expect(requests.every((item) => item.url === '/teams/tea_one/calls')).toBe(true)
    expect(
      cache.getQueryCache().findAll({ queryKey: ['calls', 'team', 'usr_member', 'tea_one'] }),
    ).toHaveLength(1)
    await click('Details')
    expect(requests.at(-1)?.url).toBe('/teams/tea_one/calls/req_team_own')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Attempts')
  })
  it('hides cached rows and detail after a failed membership refresh', async () => {
    await render()
    await click('Details')
    failure = true
    await click('Refresh')
    expect(host.textContent).not.toContain('req_team_own')
    expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Team model')
  })
  it('does not preserve cached rows after pagination authorization failure', async () => {
    await render()
    failure = true
    await click('Load more')
    expect(host.textContent).not.toContain('req_team_own')
    expect(requests.at(-1)?.params).toMatchObject({ cursor: 'next' })
  })
  it('keeps each actor and Team in an independent query resource and clears selected details', async () => {
    await render()
    await click('Details')
    actor = 'usr_other'
    await render('tea_two')
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(requests.at(-1)?.url).toBe('/teams/tea_two/calls')
    expect(
      cache.getQueryCache().findAll({ queryKey: ['calls', 'team', 'usr_other', 'tea_two'] }),
    ).toHaveLength(1)
    expect(requests.some((item) => item.url === '/admin/calls')).toBe(false)
  })
  it('switches existing explanations to Chinese without broadening scope', async () => {
    await render()
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('你的 Team 调用记录')
    expect(host.textContent).toContain('仅展示你在此 Team 中的调用')
    expect(requests.every((item) => item.url === '/teams/tea_one/calls')).toBe(true)
  })
})
