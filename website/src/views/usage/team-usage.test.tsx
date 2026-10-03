import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosHeaders, AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { beforeEach, afterEach, describe, it, expect } from 'vitest'
import i18n from '@/i18n'
import client from '@/api/client'
import UsagePage from './index'
import { usageFixture, teamUsageFixture } from './fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>,
  requests: InternalAxiosRequestConfig[]
let actor: string,
  teamsError: number,
  reportError: number,
  malformed: boolean,
  paged: boolean,
  hold: Promise<void> | undefined
const original = client.defaults.adapter
const session = () => ({ user: { id: actor, role: 'member' }, csrf_token: 'csrf' })
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  teamsError = 0
  reportError = 0
  malformed = false
  paged = false
  hold = undefined
  requests = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 60000 } } })
  cache.setQueryData(['auth', 'session'], session())
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const response = {
      config,
      data: {} as unknown,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
    if (config.url === '/auth/session') response.data = session()
    else if (config.url === '/teams') {
      if (teamsError) {
        response.status = teamsError
        throw new AxiosError('Denied', '', config, undefined, response)
      }
      response.data = {
        items: [
          {
            id: config.params.cursor ? 'tea_second' : 'tea_one',
            name: config.params.cursor ? 'Design' : 'Research',
            status: 'active',
          },
        ],
        next_cursor: paged && !config.params.cursor ? 'next' : null,
      }
    } else if (config.url === '/usage') response.data = usageFixture()
    else if (config.url?.endsWith('/usage')) {
      if (hold) await hold
      if (reportError) {
        response.status = reportError
        throw new AxiosError('Denied', '', config, undefined, response)
      }
      response.data = malformed
        ? teamUsageFixture('tea_wrong')
        : teamUsageFixture(config.url.split('/')[2])
    } else throw new Error(`Unexpected request ${config.url}`)
    return response
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  router?.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
})
async function until(check: () => void) {
  for (let i = 0; i < 100; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10))
    })
    try {
      check()
      return
    } catch (error) {
      if (i === 99) throw error
    }
  }
}
async function mount(path = '/usage') {
  router = createMemoryRouter([{ path: '/usage', element: <UsagePage /> }], {
    initialEntries: [path],
  })
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('[aria-label="Resource account"]')).toBeTruthy())
}
async function selectTeam(team: string) {
  const select = host.querySelector<HTMLSelectElement>('[aria-label="Resource account"]')!
  await act(async () => {
    select.value = team
    select.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function click(label: string) {
  const button = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (button) => button.textContent === label,
  )!
  expect(button).toBeDefined()
  await act(async () => button.click())
}
const usageRequests = () => requests.filter((request) => request.url?.endsWith('/usage'))
describe('member Team aggregate usage', () => {
  it('defaults to Personal and selects only a named own Team while removing Key controls', async () => {
    await mount()
    await until(() => expect(host.textContent).toContain('Research'))
    expect(usageRequests()[0].url).toBe('/usage')
    await selectTeam('tea_one')
    await until(() => expect(host.textContent).toContain('Shared aggregate usage'))
    await until(() => expect(host.textContent).toContain('Historical model'))
    expect(router.state.location.search).toBe('?team=tea_one')
    expect(usageRequests().at(-1)?.url).toBe('/teams/tea_one/usage')
    expect(host.querySelector('input[name="key_id"]')).toBeNull()
    expect(host.textContent).not.toContain('key_usage_resource')
    expect(host.textContent).not.toContain('Historical provider')
    expect(host.textContent).not.toContain('API Key ranking')
    expect(requests.some((request) => request.url?.startsWith('/admin'))).toBe(false)
  })
  it('reads an expected Team route directly even outside the currently loaded page', async () => {
    paged = true
    await mount('/usage?team=tea_second')
    await until(() => expect(host.textContent).toContain('Historical model'))
    expect(usageRequests().at(-1)?.url).toBe('/teams/tea_second/usage')
    expect(host.textContent).toContain('tea_second')
    await click('More Teams')
    await until(() => expect(host.textContent).toContain('Design'))
    expect(
      requests.find((request) => request.url === '/teams' && request.params.cursor)?.params.cursor,
    ).toBe('next')
  })
  it('hides cached Team facts throughout fresh authorization and after membership denial', async () => {
    await mount('/usage?team=tea_one')
    await until(() => expect(host.textContent).toContain('Historical model'))
    let release!: () => void
    hold = new Promise<void>((resolve) => {
      release = resolve
    })
    reportError = 404
    await click('Refresh')
    expect(host.textContent).not.toContain('Historical model')
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('You no longer have access'))
    expect(host.textContent).not.toContain('0.123456789012345678')
    expect(host.querySelector('svg[aria-label="Model usage"]')).toBeNull()
  })
  it('hides facts on scoped Team-list authorization loss without reading global directories', async () => {
    await mount('/usage?team=tea_one')
    await until(() => expect(host.textContent).toContain('Historical model'))
    teamsError = 403
    await click('Refresh Teams')
    await until(() => expect(host.textContent).toContain('could not be authorized'))
    expect(host.textContent).not.toContain('Historical model')
    expect(requests.some((request) => request.url?.startsWith('/admin'))).toBe(false)
  })
  it('rejects a mismatched Team report rather than showing another resource', async () => {
    malformed = true
    await mount('/usage?team=tea_one')
    await until(() => expect(host.textContent).toContain('unavailable'))
    expect(host.textContent).not.toContain('Historical model')
  })
  it('switches language without losing the selected Team or exact recorded amounts', async () => {
    await mount('/usage?team=tea_one')
    await until(() => expect(host.textContent).toContain('0.123456789012345678'))
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('团队用量')
    expect(host.querySelector<HTMLSelectElement>('[aria-label="资源账户"]')?.value).toBe('tea_one')
    expect(host.textContent).toContain('0.123456789012345678')
    expect(host.textContent).toContain('已知')
  })
  it('clears Team facts and filters on source switch and cancels the old query', async () => {
    await mount('/usage?team=tea_one')
    await until(() => expect(host.textContent).toContain('Historical model'))
    let release!: () => void
    hold = new Promise<void>((resolve) => {
      release = resolve
    })
    await click('Refresh')
    const old = usageRequests().at(-1)!
    await selectTeam('')
    expect(old.signal?.aborted).toBe(true)
    await act(async () => release())
    await until(() => expect(host.textContent).toContain('key_usage_resource'))
    expect(router.state.location.search).toBe('')
    expect(usageRequests().at(-1)?.url).toBe('/usage')
  })
  it('does not reuse a prior actor’s Team report after actor replacement', async () => {
    await mount('/usage?team=tea_one')
    await until(() => expect(host.textContent).toContain('Historical model'))
    actor = 'usr_other'
    teamsError = 403
    await act(async () => cache.setQueryData(['auth', 'session'], session()))
    await until(() => expect(host.textContent).toContain('could not be authorized'))
    expect(host.textContent).not.toContain('Historical model')
    expect(
      cache.getQueryData([
        'usage',
        'usr_other',
        ['team', 'tea_one'],
        { period: 'month', timezone: 'UTC', granularity: 'auto', compare: false },
      ]),
    ).toBeUndefined()
  })
})
