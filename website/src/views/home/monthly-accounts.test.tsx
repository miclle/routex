import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { MonthlyAccount, OverviewAccountsPage } from '@/types/overview'
import Home from './index'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let actor: string, name: string, data: OverviewAccountsPage, second: OverviewAccountsPage
let requests: InternalAxiosRequestConfig[], overviewFailure: number, sessionFailure: number
let sessionGate: ReturnType<typeof barrier> | null, overviewGate: ReturnType<typeof barrier> | null
function barrier() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  return { promise, release }
}
function account(tokens = '100'): MonthlyAccount {
  return {
    account_id: 'stable_account',
    policy_etag: 'policy_1',
    tokens_month: tokens,
    money_month: '10.123456789012345678',
    currency: 'USD',
    runtime_applied: true,
    usage_status: 'active',
    active_reservations: {
      tokens_held: '5',
      money_held: { USD: '5.000000000000000002' },
    },
    usage: {
      as_of: '2026-10-04T09:03:00Z',
      time_zone: 'UTC',
      month_start: '2026-10-01T00:00:00Z',
      month_end: '2026-11-01T00:00:00Z',
      covered: true,
      tokens_used: '5',
      tokens_held: '2',
      tokens_unknown: '0',
      money_used: { USD: '0.123456789012345678' },
      money_held: { USD: '0.000000000000000002' },
      money_unknown: '0',
    },
  }
}
function page(): OverviewAccountsPage {
  return {
    actor_user_id: actor,
    observed_at: '2026-10-04T09:03:00Z',
    platform_currency: 'USD',
    personal: account(),
    teams: [
      {
        id: 'tem_exact',
        name: 'Recorded Team',
        membership_id: 'membership_old',
        aggregate: account('1000'),
        member: account('10'),
      },
    ],
    next_cursor: null,
  }
}
function session() {
  return {
    user: { id: actor, role: 'member', name, email: actor + '@example.invalid' },
    csrf_token: 'current-proof',
  }
}
function error(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('private server failure', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'must never render server-private snapshot' },
  })
}
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 12))
  })
}
async function settle() {
  for (let i = 0; i < 12; i++) {
    await flush()
    if (host.querySelector('table')) return
  }
  throw new Error(host.textContent || 'table missing')
}
async function click(text: string) {
  const button = Array.from(host.querySelectorAll('button')).find((element) =>
    element.textContent?.includes(text),
  )
  expect(button, text).toBeDefined()
  await act(async () => button!.click())
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
  await settle()
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  actor = 'usr_member'
  name = 'Current member'
  data = page()
  second = {
    ...page(),
    teams: [{ ...page().teams[0], id: 'legacy_next', name: 'Next Team' }],
    next_cursor: null,
  }
  requests = []
  overviewFailure = 0
  sessionFailure = 0
  sessionGate = null
  overviewGate = null
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let response: unknown
    if (config.url === '/auth/session') {
      const saved = structuredClone(session())
      const failure = sessionFailure
      const gate = sessionGate
      if (gate) await gate.promise
      if (failure) throw error(config, failure)
      response = saved
    } else if (config.url === '/overview/accounts') {
      const saved = structuredClone(config.params?.cursor ? second : data)
      const failure = overviewFailure
      const gate = overviewGate
      if (gate) await gate.promise
      if (failure) throw error(config, failure)
      response = saved
    } else throw new Error('Unexpected API ' + config.url)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data: response }
  }
})
afterEach(async () => {
  sessionGate?.release()
  overviewGate?.release()
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})

describe('monthly member Overview', () => {
  it('renders the identity and exact monthly table without a directory or old editable Limits flow', async () => {
    await mount()
    expect(host.textContent).toContain('Current member')
    expect(host.textContent).toContain('This month’s resource accounts')
    expect(host.textContent).toContain('Personal resource account')
    expect(host.textContent).toContain('Recorded Team')
    const team = Array.from(host.querySelectorAll('tbody tr'))[1]
    expect(team.textContent).toContain('Team aggregate')
    expect(team.textContent).toContain('Your member account')
    expect(team.textContent).toContain('5 / 1,000')
    expect(team.textContent).toContain('5 / 10')
    expect(team.textContent).toContain('0.123456789012345678 USD')
    expect(team.textContent).toContain('Held money: 0.000000000000000002 USD')
    expect(team.textContent).toContain('Held Tokens: 2')
    expect(team.textContent).toContain('Live reserved Tokens: 5')
    expect(team.textContent).toContain('Live reserved money: 5.000000000000000002 USD')
    expect(team.textContent).toContain('Oct 1, 2026')
    expect(team.textContent).not.toContain('1,010')
    expect(Array.from(host.querySelectorAll('a')).map((a) => a.getAttribute('href'))).toContain(
      '/usage?team=tem_exact',
    )
    expect(new Set(requests.map((r) => r.url))).toEqual(
      new Set(['/auth/session', '/overview/accounts']),
    )
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(1)
  })
  it('switches existing facts and date labels live to Chinese without reloading private APIs', async () => {
    await mount()
    const count = requests.length
    await act(async () => i18n.changeLanguage('zh'))
    expect(host.textContent).toContain('本月资源账户额度')
    expect(host.textContent).toContain('Team 总体')
    expect(host.textContent).toContain('您的成员账户')
    expect(host.textContent).toContain('2026年10月1日')
    expect(host.textContent).toContain('0.123456789012345678 USD')
    expect(host.textContent).toContain('进行中预留 Token：5')
    expect(host.textContent).toContain('进行中预留金额：5.000000000000000002 USD')
    expect(requests).toHaveLength(count)
  })
  it('shows uncovered known subtotals, huge unknown counters and historical currencies without remaining allowance', async () => {
    const usage = data.personal.usage!
    usage.covered = false
    usage.tokens_unknown = '9007199254740993'
    usage.money_unknown = '9007199254740994'
    usage.money_used = { EUR: '0.123456789012345678', USD: '9007199254740993.000000000000000002' }
    data.personal.runtime_applied = false
    await mount()
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('Known subtotal')
    expect(row.textContent).toContain('Monthly coverage incomplete')
    expect(row.textContent).toContain('Unknown Token usage: 9,007,199,254,740,993')
    expect(row.textContent).toContain('Unknown money usage: 9,007,199,254,740,994')
    expect(row.textContent).toContain('9,007,199,254,740,993.000000000000000002 USD')
    expect(row.textContent).toContain('0.123456789012345678 EUR')
    expect(row.textContent).toContain('Current runtime application unconfirmed')
    expect(row.querySelector('[role=progressbar]')).toBeNull()
    expect(row.textContent).not.toMatch(/remaining|allowance available/i)
  })
  it.each(['inactive', 'unavailable'] as const)(
    'preserves %s null usage and finite zero caps instead of zero usage/percent',
    async (status) => {
      data.personal = {
        ...account('0'),
        usage_status: status,
        usage: null,
        active_reservations: null,
        money_month: '0',
        currency: 'USD',
      }
      await mount()
      const row = host.querySelector('tbody tr')!
      expect(row.textContent).toContain('Unknown / 0')
      expect(row.textContent).toContain('Unknown / 0 USD')
      expect(row.textContent).toContain(
        status === 'inactive' ? 'Accounting inactive' : 'Usage unavailable',
      )
      expect(row.querySelector('[role=progressbar]')).toBeNull()
      expect(row.textContent).not.toContain('Live reserved Tokens: 0')
      expect(row.textContent).not.toContain('No live monetary reservations')
    },
  )
  it('keeps live reservations separate from monthly settled usage, retained holds and the usage rate', async () => {
    await mount()
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('5 / 100')
    expect(row.textContent).toContain('Held Tokens: 2')
    expect(row.textContent).toContain('Live reserved Tokens: 5')
    expect(row.querySelector('[role=progressbar]')?.getAttribute('aria-valuenow')).toBe('5')
    expect(row.textContent).not.toContain('10 / 100')
    data.personal.active_reservations = { tokens_held: '0', money_held: {} }
    await click('Refresh accounts')
    await settle()
    const refreshed = host.querySelector('tbody tr')!
    expect(refreshed.textContent).toContain('Live reserved Tokens: 0')
    expect(refreshed.textContent).toContain('No live monetary reservations')
    expect(refreshed.textContent).toContain('Held Tokens: 2')
    expect(refreshed.textContent).toContain('5 / 100')
  })
  it('keeps absent local controls Not set, never labels them unlimited or zero', async () => {
    data.personal = { ...account(), tokens_month: null, money_month: null, currency: null }
    await mount()
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('5 / Not set')
    expect(row.textContent).toContain('USD / Not set')
    expect(row.querySelector('[role=progressbar]')).toBeNull()
    expect(row.textContent).not.toContain('Unlimited')
  })
  it('paginates at ten Teams with Personal repeated once and disables absent next page', async () => {
    data.next_cursor = 'exact-cursor'
    await mount()
    await click('Next Teams')
    await settle()
    expect(host.textContent).toContain('Next Team')
    expect(host.textContent).not.toContain('Recorded Team')
    expect(host.querySelectorAll('tbody tr')).toHaveLength(2)
    expect(requests.filter((r) => r.url === '/overview/accounts').at(-1)?.params).toEqual({
      cursor: 'exact-cursor',
      limit: 10,
    })
    expect(
      Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Next Teams')
        ?.disabled,
    ).toBe(true)
    await click('Previous Teams')
    await settle()
    expect(host.textContent).toContain('Recorded Team')
  })
  it.each([403, 404, 503])(
    'hides old accounts during renewed reads and HTTP%s errors with explicit retry',
    async (status) => {
      await mount()
      overviewGate = barrier()
      overviewFailure = status
      await click('Refresh accounts')
      await flush()
      expect(host.querySelector('table')).toBeNull()
      expect(host.textContent).not.toContain('Recorded Team')
      overviewGate.release()
      await flush()
      await flush()
      expect(host.textContent).toContain('Monthly accounts could not be confirmed')
      expect(host.textContent).not.toContain('server-private')
      expect(host.querySelector('table')).toBeNull()
      overviewGate = null
      overviewFailure = 0
      data.teams = []
      await click('Refresh accounts')
      await settle()
      expect(host.textContent).toContain('No current active Team accounts')
    },
  )
  it('hides identity and old accounts during real Session renewal then reauthorizes once without a subscriber loop', async () => {
    await mount()
    sessionGate = barrier()
    let refreshing!: Promise<unknown>
    await act(async () => {
      refreshing = cache.refetchQueries({ queryKey: sessionKey })
    })
    await flush()
    expect(host.textContent).toContain('Confirming your current account')
    expect(host.textContent).not.toContain('Current member')
    expect(host.textContent).not.toContain('Recorded Team')
    data.teams[0].membership_id = 'membership_rejoined'
    sessionGate.release()
    await act(async () => refreshing)
    await settle()
    await flush()
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
    expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(2)
    expect(host.textContent).toContain('Recorded Team')
  })
  it('reauthorizes after a fast same-millisecond structurally identical Session response', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(1_700_000_000_000)
    await mount()
    const timestamp = cache.getQueryState(sessionKey)?.dataUpdatedAt
    const version = cache.getQueryState(sessionKey)?.dataUpdateCount
    data.teams = []
    await act(async () => cache.refetchQueries({ queryKey: sessionKey }))
    await settle()
    expect(cache.getQueryState(sessionKey)?.dataUpdatedAt).toBe(timestamp)
    expect(cache.getQueryState(sessionKey)?.dataUpdateCount).toBe((version ?? 0) + 1)
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
    expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(2)
    expect(host.querySelector('tbody')?.textContent).not.toContain('Recorded Team')
    expect(host.textContent).toContain('No current active Team accounts')
  })
  it('preserves explicit zero settled/held values and zero caps without fabricating a rate', async () => {
    data.personal.tokens_month = '0'
    data.personal.money_month = '0'
    data.personal.usage!.tokens_used = '0'
    data.personal.usage!.tokens_held = '0'
    data.personal.usage!.money_used = { USD: '0' }
    data.personal.usage!.money_held = { USD: '0' }
    await mount()
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('0 / 0')
    expect(row.textContent).toContain('0 USD / 0 USD')
    expect(row.textContent).toContain('Held Tokens: 0')
    expect(row.textContent).toContain('Held money: 0 USD')
    expect(row.textContent).toContain('Usage rate unavailable')
    expect(row.querySelector('[role=progressbar]')).toBeNull()
  })
  it('does not convert missing monetary records into zero in any currency', async () => {
    data.personal.usage!.money_used = {}
    data.personal.usage!.money_held = {}
    await mount()
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('No recorded monetary usage / 10.123456789012345678 USD')
    expect(row.textContent).toContain('Held money: No recorded monetary usage')
    expect(row.textContent).not.toContain('0 USD')
  })
  it('suppresses percentage while covered journal usage still has unknown Tokens', async () => {
    data.personal.usage!.tokens_unknown = '1'
    await mount()
    const row = host.querySelector('tbody tr')!
    expect(row.textContent).toContain('Complete monthly coverage')
    expect(row.textContent).toContain('Unknown Token usage: 1')
    expect(row.textContent).toContain('Usage rate unavailable')
    expect(row.querySelector('[role=progressbar]')).toBeNull()
  })
  it('ignores a late old-generation account response after same-actor Session renewal', async () => {
    overviewGate = barrier()
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <Home />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    )
    await flush()
    await flush()
    expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(1)
    const old = overviewGate
    overviewGate = null
    data.teams = []
    await act(async () => cache.refetchQueries({ queryKey: sessionKey }))
    await settle()
    expect(requests.filter((r) => r.url === '/overview/accounts')[0].signal?.aborted).toBe(true)
    old.release()
    await flush()
    await flush()
    expect(host.textContent).not.toContain('Recorded Team')
    expect(host.textContent).toContain('No current active Team accounts')
  })
  it('actor switch resets pagination and ignores older actor detail even when the adapter resolves late', async () => {
    data.next_cursor = 'old-cursor'
    await mount()
    overviewGate = barrier()
    await click('Next Teams')
    await flush()
    const old = overviewGate
    overviewGate = null
    actor = 'usr_other'
    name = 'Other actor'
    data = { ...page(), teams: [] }
    await act(async () => cache.refetchQueries({ queryKey: sessionKey }))
    await settle()
    old.release()
    await flush()
    expect(host.textContent).toContain('Other actor')
    expect(host.querySelector('tbody')?.textContent).not.toContain('Next Team')
    expect(host.textContent).not.toContain('Recorded Team')
    expect(
      requests.filter((r) => r.url === '/overview/accounts').at(-1)?.params.cursor,
    ).toBeUndefined()
  })
  it.each([401, 403, 503])(
    'a failed Session refresh HTTP%s hides all private facts and dispatches no accounts read',
    async (status) => {
      await mount()
      const before = requests.filter((r) => r.url === '/overview/accounts').length
      sessionFailure = status
      await act(async () => cache.refetchQueries({ queryKey: sessionKey }))
      await flush()
      expect(host.textContent).toContain('Your current account could not be confirmed')
      expect(host.textContent).not.toContain('Current member')
      expect(host.textContent).not.toContain('Recorded Team')
      expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(before)
    },
  )
  it('rejects an exact server actor mismatch without rendering another account snapshot', async () => {
    data.actor_user_id = 'usr_wrong'
    await act(async () =>
      root.render(
        <QueryClientProvider client={cache}>
          <MemoryRouter>
            <Home />
          </MemoryRouter>
        </QueryClientProvider>,
      ),
    )
    await flush()
    await flush()
    expect(host.textContent).toContain('Monthly accounts could not be confirmed')
    expect(host.textContent).not.toContain('Recorded Team')
  })
})
