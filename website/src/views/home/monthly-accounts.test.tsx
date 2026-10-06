import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { sessionKey } from '@/hooks/use-auth'
import i18n from '@/i18n'
import type { MonthlyAccount, OverviewAccountsPage, OverviewRolesPage } from '@/types/overview'
import Home from './index'
import MonthlyAccounts from './monthly-accounts'
import { homeUsageFixture } from './usage-overview-fixture'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let host: HTMLDivElement, root: Root, cache: QueryClient
let currentIdentityRole: 'admin' | 'member'
let secondRoles: OverviewRolesPage
let roles: OverviewRolesPage, rolesFailure: number, roleGate: ReturnType<typeof barrier> | null
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
function monthlyTable() {
  return host.querySelector<HTMLTableElement>(`table[aria-label="${i18n.t('overview:title')}"]`)
}
function session() {
  return {
    user: { id: actor, role: currentIdentityRole, name, email: actor + '@example.invalid' },
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
    if (monthlyTable()) return
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
  currentIdentityRole = 'member'
  actor = 'usr_member'
  name = 'Current member'
  data = page()
  second = {
    ...page(),
    teams: [{ ...page().teams[0], id: 'legacy_next', name: 'Next Team' }],
    next_cursor: null,
  }
  requests = []
  roles = {
    actor_user_id: actor,
    observed_at: data.observed_at,
    identity_role: 'member',
    roles: [],
    next_cursor: null,
  }
  secondRoles = { ...roles }
  rolesFailure = 0
  roleGate = null
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
    } else if (config.url === '/overview/roles') {
      const saved = structuredClone(config.params?.cursor ? secondRoles : roles)
      const failure = rolesFailure
      if (roleGate) await roleGate.promise
      if (failure) throw error(config, failure)
      response = saved
    } else if (config.url === '/usage') response = homeUsageFixture(true)
    else throw new Error('Unexpected API ' + config.url)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data: response }
  }
})
afterEach(async () => {
  roleGate?.release()
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
    const team = Array.from(monthlyTable()!.querySelectorAll('tbody tr'))[1]
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
      new Set(['/auth/session', '/overview/accounts', '/overview/roles', '/usage']),
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
    const row = monthlyTable()!.querySelector('tbody tr')!
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
      const row = monthlyTable()!.querySelector('tbody tr')!
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
    const row = monthlyTable()!.querySelector('tbody tr')!
    expect(row.textContent).toContain('5 / 100')
    expect(row.textContent).toContain('Held Tokens: 2')
    expect(row.textContent).toContain('Live reserved Tokens: 5')
    expect(row.querySelector('[role=progressbar]')?.getAttribute('aria-valuenow')).toBe('5')
    expect(row.textContent).not.toContain('10 / 100')
    data.personal.active_reservations = { tokens_held: '0', money_held: {} }
    await click('Refresh accounts')
    await settle()
    const refreshed = monthlyTable()!.querySelector('tbody tr')!
    expect(refreshed.textContent).toContain('Live reserved Tokens: 0')
    expect(refreshed.textContent).toContain('No live monetary reservations')
    expect(refreshed.textContent).toContain('Held Tokens: 2')
    expect(refreshed.textContent).toContain('5 / 100')
  })
  it('keeps absent local controls Not set, never labels them unlimited or zero', async () => {
    data.personal = { ...account(), tokens_month: null, money_month: null, currency: null }
    await mount()
    const row = monthlyTable()!.querySelector('tbody tr')!
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
    expect(monthlyTable()!.querySelectorAll('tbody tr')).toHaveLength(2)
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
      expect(monthlyTable()).toBeNull()
      expect(host.textContent).not.toContain('Recorded Team')
      overviewGate.release()
      await flush()
      await flush()
      expect(host.textContent).toContain('Monthly accounts could not be confirmed')
      expect(host.textContent).not.toContain('server-private')
      expect(monthlyTable()).toBeNull()
      overviewGate = null
      roles = {
        actor_user_id: actor,
        observed_at: data.observed_at,
        identity_role: 'member',
        roles: [],
        next_cursor: null,
      }
      secondRoles = { ...roles }
      rolesFailure = 0
      roleGate = null
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
    const row = monthlyTable()!.querySelector('tbody tr')!
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
    const row = monthlyTable()!.querySelector('tbody tr')!
    expect(row.textContent).toContain('No recorded monetary usage / 10.123456789012345678 USD')
    expect(row.textContent).toContain('Held money: No recorded monetary usage')
    expect(row.textContent).not.toContain('0 USD')
  })
  it('suppresses percentage while covered journal usage still has unknown Tokens', async () => {
    data.personal.usage!.tokens_unknown = '1'
    await mount()
    const row = monthlyTable()!.querySelector('tbody tr')!
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

function identityText() {
  return host.querySelector('[data-identity-labels]')?.textContent ?? ''
}
async function identityReady(check: () => void) {
  for (let n = 0; n < 60; n++) {
    await flush()
    try {
      check()
      return
    } catch (error) {
      if (n === 59) throw error
    }
  }
}
const roleCursor = (id: string) => btoa(`${actor}|${id}`).replace(/=+$/, '')
describe('fresh self identity labels', () => {
  it('shares one first account page with the monthly table and labels only explicit duties/custom Roles', async () => {
    roles.roles = [
      { id: 'rol_custom', name: '原始 Role name', builtin: false, assignment_kind: 'explicit' },
      { id: 'rol_finance', name: 'Recorded Finance', builtin: true, assignment_kind: 'explicit' },
      { id: 'rol_unknown', name: null, builtin: false, assignment_kind: 'explicit' },
    ]
    await mount()
    await identityReady(() => expect(identityText()).toContain('Finance'))
    expect(identityText()).toContain('Direct Roles: 原始 Role name, Finance, rol_unknown')
    expect(identityText()).toContain('Teams: Recorded Team')
    expect(identityText()).not.toContain('Administrator')
    expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(1)
    expect(requests.filter((r) => r.url === '/overview/roles')).toHaveLength(1)
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(1)
    const count = requests.length
    await act(async () => i18n.changeLanguage('zh'))
    expect(identityText()).toContain('直接分配的角色：原始 Role name, 财务, rol_unknown')
    expect(identityText()).toContain('所属 Team：Recorded Team')
    expect(requests).toHaveLength(count)
    await act(async () => i18n.changeLanguage('en'))
    expect(identityText()).toContain('Finance')
    expect(requests).toHaveLength(count)
  })
  it('qualifies ten first-page Teams and pages to the eleventh without duplicate account requests or accumulation', async () => {
    data.teams = Array.from({ length: 10 }, (_, n) => ({
      ...page().teams[0],
      id: `tem_${n}`,
      membership_id: `membership_${n}`,
      name: `Team ${n}`,
    }))
    data.next_cursor = 'accounts-next'
    second.teams = [{ ...second.teams[0], name: 'Eleventh Team' }]
    await mount()
    expect(identityText()).toContain('Team page 1')
    expect(identityText()).toContain('some current Team memberships')
    await click('More Team names')
    await identityReady(() => expect(identityText()).toContain('Eleventh Team'))
    expect(identityText()).not.toContain('Team 0')
    expect(identityText()).toContain('Team page 2')
    expect(monthlyTable()?.textContent).toContain('Eleventh Team')
    expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(2)
    await click('Refresh identity labels')
    await identityReady(() => expect(identityText()).toContain('Team 0'))
    expect(identityText()).toContain('Team page 1')
    expect(requests.filter((r) => r.url === '/overview/accounts')).toHaveLength(3)
  })
  it('uses fresh bounded Role pages, retains no accumulated complete-set claim, and resets on identity refresh', async () => {
    roles.roles = Array.from({ length: 10 }, (_, n) => ({
      id: `rol_${String(n).padStart(2, '0')}`,
      name: `Custom ${n}`,
      builtin: false,
      assignment_kind: 'explicit' as const,
    }))
    roles.next_cursor = roleCursor('rol_09')
    secondRoles = {
      ...roles,
      roles: [{ id: 'rol_10', name: 'Eleventh Role', builtin: false, assignment_kind: 'explicit' }],
      next_cursor: null,
    }
    await mount()
    await identityReady(() => expect(identityText()).toContain('Custom 0'))
    expect(identityText()).toContain('some direct assignments')
    await click('More Role labels')
    await identityReady(() => expect(identityText()).toContain('Eleventh Role'))
    expect(identityText()).not.toContain('Custom 0')
    expect(identityText()).toContain('Role page 2')
    expect(requests.filter((r) => r.url === '/overview/roles')[1].params.cursor).toBe(
      roles.next_cursor,
    )
    await click('Refresh identity labels')
    await identityReady(() => expect(identityText()).toContain('Custom 0'))
    expect(requests.filter((r) => r.url === '/overview/roles')).toHaveLength(3)
  })
  it('claims no current Teams only on an empty terminal first page, not an empty later page', async () => {
    data.teams = []
    await mount()
    expect(identityText()).toContain('Teams: No current Teams')
    expect(identityText()).toContain('No directly assigned Roles')
    data = page()
    data.next_cursor = 'next'
    second.teams = []
    await click('Refresh identity labels')
    await identityReady(() => expect(identityText()).toContain('Recorded Team'))
    await click('More Team names')
    await identityReady(() => expect(identityText()).toContain('Team page 2'))
    expect(identityText()).toContain('Teams: Unknown')
    expect(identityText()).not.toContain('No current Teams')
  })
  it.each([401, 403, 503])(
    'fails closed on Role admission/read status %s without directory fallback',
    async (status) => {
      rolesFailure = status
      await mount()
      await flush()
      expect(identityText()).toContain('Direct Roles: Unknown')
      expect(identityText()).not.toContain('No directly assigned Roles')
      expect(new Set(requests.map((r) => r.url))).toEqual(
        new Set(['/auth/session', '/overview/accounts', '/overview/roles', '/usage']),
      )
    },
  )
  it('hides cached Role labels throughout renewal and errors while retaining the independent fresh Team page', async () => {
    roles.roles = [
      { id: 'rol_finance', name: 'Finance', builtin: true, assignment_kind: 'explicit' },
    ]
    await mount()
    await identityReady(() => expect(identityText()).toContain('Finance'))
    roleGate = barrier()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['overview-roles'] })
    })
    await flush()
    expect(identityText()).toContain('Direct Roles: Unknown')
    expect(identityText()).not.toContain('Finance')
    rolesFailure = 503
    roleGate.release()
    roleGate = null
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['overview-roles'] })
    })
    await flush()
    expect(identityText()).not.toContain('Finance')
    expect(identityText()).toContain('Recorded Team')
  })
  it('hides both shared Team surfaces during an account refresh and cannot show malformed labels as an empty set', async () => {
    await mount()
    overviewGate = barrier()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['overview-accounts'] })
    })
    await flush()
    expect(identityText()).toContain('Teams: Unknown')
    expect(identityText()).not.toContain('Recorded Team')
    expect(monthlyTable()).toBeNull()
    overviewGate.release()
    overviewGate = null
    data.teams[0].name = 'Bad\nTeam'
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['overview-accounts'] })
    })
    await flush()
    expect(identityText()).toContain('Teams: Unknown')
    expect(identityText()).not.toContain('No current Teams')
  })
  it('requests one Session renewal on an intrinsic-role discrepancy and never mixes Role labels with it', async () => {
    roles.identity_role = 'admin'
    roles.roles = [
      { id: 'rol_finance', name: 'Finance', builtin: true, assignment_kind: 'explicit' },
    ]
    await mount()
    await identityReady(() =>
      expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2),
    )
    expect(identityText()).not.toContain('Finance')
    expect(identityText()).toContain('Direct Roles: Unknown')
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['overview-roles'] })
    })
    await flush()
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
  })
  it('discards old Role responses across renewed Session and actor changes', async () => {
    roleGate = barrier()
    roles.roles = [
      { id: 'rol_old', name: 'Private old role', builtin: false, assignment_kind: 'explicit' },
    ]
    await mount()
    expect(identityText()).not.toContain('Private old role')
    actor = 'usr_new'
    name = 'New actor'
    data = page()
    roles = {
      actor_user_id: actor,
      identity_role: 'member',
      observed_at: data.observed_at,
      roles: [{ id: 'rol_new', name: 'New role', builtin: false, assignment_kind: 'explicit' }],
      next_cursor: null,
    }
    const old = roleGate
    roleGate = null
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    await flush()
    old.release()
    await identityReady(() => expect(identityText()).toContain('New role'))
    expect(host.textContent).not.toContain('Private old role')
    expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
  })
})

it('preserves standalone MonthlyAccounts with one own account request and no Session/Role observer', async () => {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <MemoryRouter>
          <MonthlyAccounts actorId={actor} />
        </MemoryRouter>
      </QueryClientProvider>,
    ),
  )
  await settle()
  expect(new Set(requests.map((request) => request.url))).toEqual(new Set(['/overview/accounts']))
  expect(requests).toHaveLength(1)
  expect(monthlyTable()?.textContent).toContain('Recorded Team')
})

it('localizes only the finite assigned duty names, leaving intrinsic identity separate', async () => {
  roles.roles = ['rol_finance', 'rol_operations', 'rol_procurement'].map((id) => ({
    id,
    name: null,
    builtin: true,
    assignment_kind: 'explicit',
  }))
  await mount()
  await identityReady(() => expect(identityText()).toContain('Finance, Operations, Procurement'))
  await act(async () => i18n.changeLanguage('zh'))
  expect(identityText()).toContain('财务, 运维, 采购')
  expect(requests.filter((request) => request.url === '/overview/roles')).toHaveLength(1)
})
it('recovers coherent labels only after one successful intrinsic Session renewal', async () => {
  await mount()
  roles.identity_role = 'admin'
  currentIdentityRole = 'admin'
  roles.roles = [{ id: 'rol_finance', name: 'Finance', builtin: true, assignment_kind: 'explicit' }]
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['overview-roles'] })
  })
  await identityReady(() => expect(identityText()).toContain('Finance'))
  expect(cache.getQueryData<{ user: { role: string } }>(sessionKey)?.user.role).toBe('admin')
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2)
})
it('discards a held old second Role page when a fresh Session starts a new collection', async () => {
  roles.roles = [{ id: 'rol_a', name: 'First Role', builtin: false, assignment_kind: 'explicit' }]
  roles.next_cursor = roleCursor('rol_a')
  secondRoles = {
    ...roles,
    roles: [
      { id: 'rol_z', name: 'Obsolete private page', builtin: false, assignment_kind: 'explicit' },
    ],
    next_cursor: null,
  }
  await mount()
  await identityReady(() => expect(identityText()).toContain('First Role'))
  roleGate = barrier()
  await click('More Role labels')
  await flush()
  expect(identityText()).not.toContain('First Role')
  const old = roleGate
  roleGate = null
  roles = { ...roles, roles: [], next_cursor: null }
  await act(async () => {
    void cache.invalidateQueries({ queryKey: sessionKey })
  })
  await identityReady(() => expect(identityText()).toContain('No directly assigned Roles'))
  old.release()
  await flush()
  expect(identityText()).not.toContain('Obsolete private page')
  expect(identityText()).not.toContain('Role page 2')
})

it('permits one new explicit discrepancy check without an automatic Session retry loop', async () => {
  roles.identity_role = 'admin'
  await mount()
  await identityReady(() =>
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(2),
  )
  await click('Refresh identity labels')
  await identityReady(() =>
    expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(3),
  )
  await flush()
  expect(requests.filter((request) => request.url === '/auth/session')).toHaveLength(3)
})
