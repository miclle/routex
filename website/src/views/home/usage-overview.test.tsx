import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import Home from './index'
import { homeUsageFixture } from './usage-overview-fixture'
import type { UsageReport } from '@/types/usage'
vi.mock('./monthly-accounts', () => ({
  default: () => <div data-monthly-placeholder>Monthly accounts retained</div>,
}))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root, host: HTMLDivElement, cache: QueryClient
let report: UsageReport, actor: string, role: 'admin' | 'member', failure: number
let requests: InternalAxiosRequestConfig[], gates: ReturnType<typeof barrier>[]
let sessionGate: ReturnType<typeof barrier> | undefined,
  usageGate: ReturnType<typeof barrier> | undefined
function barrier(): { promise: Promise<void>; release: () => void } {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const gate = { promise, release: () => release() }
  gates.push(gate)
  return gate
}
function session() {
  return {
    user: { id: actor, name: actor, email: actor + '@example.test', role },
    csrf_token: 'csrf-test',
  }
}
function error(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('server-private detail', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'Never render private error' },
  })
}
beforeEach(async () => {
  vi.spyOn(Date, 'now').mockReturnValue(1_800_000_000_000)
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_one'
  role = 'member'
  report = homeUsageFixture()
  failure = 0
  requests = []
  gates = []
  sessionGate = undefined
  usageGate = undefined
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/auth/session') {
      data = structuredClone(session())
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/usage') {
      data = structuredClone(report)
      if (usageGate) await usageGate.promise
      if (failure) throw error(config, failure)
    } else throw new Error('Unexpected API request')
    return { data, config, status: 200, statusText: '', headers: new AxiosHeaders() }
  }
})
afterEach(async () => {
  gates.forEach((gate) => gate.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
  await i18n.changeLanguage('en')
})
async function flush() {
  await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
}
async function until(check: () => void) {
  for (let n = 0; n < 100; n++) {
    await flush()
    try {
      check()
      return
    } catch (error) {
      if (n === 99) throw error
    }
  }
}
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <Home />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.querySelector('table')).not.toBeNull())
}
function button(label: string) {
  return [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label,
  )!
}
async function click(label: string) {
  await act(async () => button(label).click())
}
function usageCalls() {
  return requests.filter((request) => request.url === '/usage')
}
it.each(['member', 'admin'] as const)(
  'keeps monthly composition and requests one Personal report for %s cards/trend/tabs',
  async (value) => {
    role = value
    await mount()
    expect(
      host
        .querySelector('[data-monthly-placeholder]')!
        .compareDocumentPosition(host.querySelector('[data-summary-cards]')!) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(host.querySelector('[data-summary-cards]')!.children).toHaveLength(3)
    expect(host.textContent).toContain('33.3%')
    expect(host.textContent).toContain('including canceled calls and admission failures')
    expect(host.textContent).toContain('9,007,199,254,740,993')
    expect(host.textContent).toContain('Historical model')
    expect(host.querySelector('svg[role="img"] path[d*="C"]')).not.toBeNull()
    expect(usageCalls()).toHaveLength(1)
    expect(new Set(requests.map((request) => request.url))).toEqual(
      new Set(['/auth/session', '/usage']),
    )
    expect(usageCalls()[0].params).toEqual({
      period: '30d',
      timezone: 'UTC',
      granularity: 'day',
      compare: false,
    })
    await act(async () =>
      host.querySelector<HTMLButtonElement>('[role="tab"][aria-selected="true"]')!.focus(),
    )
    const keyTab = [...host.querySelectorAll<HTMLElement>('[role="tab"]')].find(
      (tab) => tab.textContent === 'API Keys',
    )!
    await act(async () => keyTab.click())
    await until(() => expect(host.textContent).toContain('key_revoked'))
    expect(usageCalls()).toHaveLength(1)
    expect(host.querySelector('select')).toBeNull()
    expect(host.textContent).not.toMatch(/remaining allowance|Provider diagnostics|Export/)
  },
)
it('preserves exact unknown subtotal, gaps and unavailable shares', async () => {
  for (const stats of [
    report.current.summary,
    report.current.trend[0].stats,
    report.current.models[0].stats,
    report.current.keys[0].stats,
  ]) {
    stats.tokens.input.value = null
    stats.tokens.input.unknown_calls = 1
    stats.tokens.total.value = null
    stats.tokens.total.unknown_calls = 1
  }
  await mount()
  expect(host.textContent).toContain('Unknown')
  expect(host.textContent).toContain('9,007,199,254,740,993')
  expect(host.querySelectorAll('[data-known-segment]')).toHaveLength(1)
  expect(host.querySelector('[data-summary-cards]')!.textContent).toContain('1 call')
  expect(host.querySelector('tbody')!.textContent).not.toContain('100.0%')
})
it('renders known empty data while leaving success and shares unknown', async () => {
  report = homeUsageFixture(true)
  await mount()
  expect(host.textContent).toContain('No recorded calls')
  expect(host.querySelector('[data-summary-cards]')!.textContent).toContain('Unknown')
  expect(host.textContent).not.toContain('100%')
})
it.each([401, 403, 404, 422, 503])(
  'hides every old usage fact during refresh and after HTTP %i without automatic retry',
  async (status) => {
    await mount()
    usageGate = barrier()
    failure = status
    await click('Refresh 30-day usage')
    await until(() => expect(host.textContent).toContain('Confirming Personal usage'))
    expect(host.textContent).not.toContain('Historical model')
    expect(host.querySelector('[data-summary-cards]')).toBeNull()
    expect(host.querySelector('table')).toBeNull()
    await act(async () => usageGate!.release())
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(host.textContent).not.toContain('Never render private error')
    expect(host.textContent).not.toContain('Historical model')
    expect(usageCalls()).toHaveLength(2)
    expect(host.querySelector('a[href="/usage"]')).not.toBeNull()
    if (status === 422) expect(host.textContent).toContain('narrow the range')
    failure = 0
    usageGate = undefined
    await click('Refresh 30-day usage')
    await until(() => expect(host.textContent).toContain('Historical model'))
    expect(usageCalls()).toHaveLength(3)
  },
)
it('hides all private Home facts while Session is renewed and cancels an old report', async () => {
  await mount()
  usageGate = barrier()
  await click('Refresh 30-day usage')
  const signal = usageCalls().at(-1)!.signal!
  sessionGate = barrier()
  await act(async () => {
    void cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(host.textContent).toContain('Confirming your current account'))
  expect(host.textContent).not.toContain('usr_one')
  expect(signal.aborted).toBe(true)
  const old = usageGate
  usageGate = undefined
  await act(async () => sessionGate!.release())
  await until(() => expect(host.textContent).toContain('Historical model'))
  await act(async () => old.release())
  expect(usageCalls()).toHaveLength(3)
})
it('renews an identical same-ms Session response and ignores late prior-generation facts', async () => {
  await mount()
  usageGate = barrier()
  await click('Refresh 30-day usage')
  const oldGate = usageGate,
    signal = usageCalls().at(-1)!.signal!
  report = homeUsageFixture(true)
  usageGate = undefined
  const before = cache.getQueryState(sessionKey)!
  await act(async () => {
    await cache.refetchQueries({ queryKey: sessionKey })
  })
  await until(() => expect(host.textContent).toContain('No recorded calls'))
  const after = cache.getQueryState(sessionKey)!
  expect(after.data).toBe(before.data)
  expect(after.dataUpdatedAt).toBe(before.dataUpdatedAt)
  expect(after.dataUpdateCount).toBe(before.dataUpdateCount + 1)
  expect(signal.aborted).toBe(true)
  await act(async () => oldGate.release())
  expect(host.textContent).not.toContain('Historical model')
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
})
it('changes actor without allowing older responses or global usage scope', async () => {
  await mount()
  usageGate = barrier()
  await click('Refresh 30-day usage')
  const oldGate = usageGate,
    signal = usageCalls().at(-1)!.signal!
  actor = 'usr_two'
  report = homeUsageFixture(true)
  usageGate = undefined
  await act(async () => cache.setQueryData(sessionKey, session()))
  await until(() => expect(host.textContent).toContain('No recorded calls'))
  await act(async () => oldGate.release())
  expect(signal.aborted).toBe(true)
  expect(host.textContent).not.toContain('Historical model')
  expect(host.textContent).toContain('usr_two')
  expect(
    usageCalls().every(
      (request) => request.url === '/usage' && request.params.user_id === undefined,
    ),
  ).toBe(true)
})
it('switches live language without another report or changing historical identifiers', async () => {
  await mount()
  const count = usageCalls().length
  await act(async () => i18n.changeLanguage('zh'))
  expect(host.textContent).toContain('个人用量')
  expect(host.textContent).toContain('近 30 天')
  expect(host.textContent).toContain('成功调用 / 全部已记录请求')
  expect(host.textContent).toContain('Historical model')
  expect(usageCalls()).toHaveLength(count)
})

it('uses authoritative zero success rate when every recorded call was canceled', async () => {
  for (const stats of [
    report.current.summary,
    report.current.trend[0].stats,
    report.current.models[0].stats,
    report.current.keys[0].stats,
  ]) {
    stats.successes = 0
    stats.errors = 0
    stats.canceled = stats.requests
    stats.success_rate = 0
  }
  await mount()
  expect(host.querySelector('[data-summary-cards]')!.textContent).toContain('0%')
  expect(host.textContent).toContain('including canceled calls and admission failures')
})
