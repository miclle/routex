import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import {
  memberTeamsActor,
  memberTeamsTarget,
  memberTeamsPage,
  memberTeamRow,
  emptyMemberTeamPolicy,
} from './member-teams.fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let actor: string,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  pages: ReturnType<typeof memberTeamsPage>[],
  teamStatus: number,
  sessionStatus: number,
  permissionStatus: number
const originalAdapter = client.defaults.adapter
type Gate = { promise: Promise<void>; release: () => void }
let sessionGate: Gate | undefined, teamGate: Gate | undefined, gates: Gate[]
function gate() {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  const g = { promise, release }
  gates.push(g)
  return g
}
function fail(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled unavailable', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: { message: 'Unavailable' },
  })
}
beforeEach(() => {
  actor = memberTeamsActor
  permissions = ['members.read', 'teams.read_all']
  requests = []
  pages = [memberTeamsPage()]
  gates = []
  sessionGate = teamGate = undefined
  teamStatus = sessionStatus = permissionStatus = 0
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    if (config.url === '/auth/session') {
      if (sessionStatus) throw fail(config, sessionStatus)
      data = {
        user: { id: actor, role: 'admin', name: 'Reader' },
        csrf_token: 'controlled-current-csrf',
      }
      if (sessionGate) await sessionGate.promise
    } else if (config.url === '/auth/permissions') {
      if (permissionStatus) throw fail(config, permissionStatus)
      data = { permissions: [...permissions] }
    } else if (config.url?.endsWith('/teams')) {
      const captured = structuredClone(pages[config.params?.cursor ? 1 : 0])
      if (teamGate) await teamGate.promise
      if (teamStatus) throw fail(config, teamStatus)
      data = captured
    } else if (/^\/admin\/members\/[^/]+$/.test(config.url ?? '')) {
      data = {
        id: config.url!.split('/')[3],
        name: 'Controlled member',
        email: 'member@example.invalid',
        role: 'member',
        role_ids: [],
        disabled: false,
        offboarded_at: null,
        created_at: '2026-10-01T00:00:00Z',
      }
    } else if (config.url === '/admin/roles') data = { items: [], next_cursor: null }
    else throw new Error(`Unexpected endpoint ${config.url}`)
    return {
      config,
      data: structuredClone(data),
      status: 200,
      statusText: '',
      headers: new AxiosHeaders(),
    }
  }
  router = createMemoryRouter(
    [
      { path: '/admin/members/:memberId', element: <MembersPage /> },
      { path: '/admin/teams/:teamId', element: <p>Team destination</p> },
    ],
    { initialEntries: [`/admin/members/${memberTeamsTarget}?tab=teams`] },
  )
})
afterEach(async () => {
  gates.forEach((g) => g.release())
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = originalAdapter
  vi.restoreAllMocks()
})
const until = (fn: () => void) =>
  vi.waitFor(
    async () => {
      await act(async () => {})
      fn()
    },
    { timeout: 4000, interval: 10 },
  )
async function mount() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
async function render() {
  await mount()
  await until(() => expect(host.textContent).toContain('Recorded Team c'))
}
const button = (label: string) =>
  [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.trim() === label,
  )
const reads = () => requests.filter((r) => r.url?.endsWith('/teams'))
it('renders six approved columns with exact distinct facts and no directory requests', async () => {
  await render()
  expect([...host.querySelectorAll('thead th')].map((th) => th.textContent)).toEqual([
    'Team',
    'Status',
    'Token usage / quota',
    'Budget used / total',
    'Rate limits',
    'Joined',
  ])
  expect(host.textContent).toContain('9,007,199,254,740,993 / 0')
  expect(host.textContent).toContain('5.000000000000000002 USD')
  expect(host.textContent).toContain('0.000000000000000001 USD')
  expect(host.textContent).toContain('Live reservations: 5')
  expect(host.textContent).toContain('Retained monthly reservations: 0')
  expect(host.textContent).toContain('Unknown')
  expect(host.textContent).toContain('Application unconfirmed')
  expect(requests.every((r) => r.method === 'get')).toBe(true)
  expect(requests.map((r) => r.url)).not.toContain('/admin/teams')
  expect(
    requests.some(
      (r) =>
        r.url?.includes('/limits') || r.url?.includes('/models') || r.url?.includes('/overview'),
    ),
  ).toBe(false)
  const link = host.querySelector<HTMLAnchorElement>('a[href^="/admin/teams/"]')!
  expect(link.getAttribute('href')).toBe(`/admin/teams/${pages[0].items[0].id}`)
})
it.each([{ grants: ['members.read'] }, { grants: ['teams.read_all'] }, { grants: [] }])(
  'deep link with permissions %j stays denied without a Team fetch',
  async ({ grants }) => {
    permissions = grants
    await mount()
    await until(() =>
      expect(host.textContent).toContain('Current member and Team read authority is required.'),
    )
    expect(reads()).toHaveLength(0)
    expect(router.state.location.search).toBe('?tab=teams')
    expect(host.querySelector('[role="tabpanel"]')?.getAttribute('aria-label')).toBe('Teams')
  },
)
it('uses the approved trigger order without changing existing tab URLs', async () => {
  permissions.push('roles.read', 'members.models.write')
  await render()
  expect([...host.querySelectorAll('[role="tab"]')].map((e) => e.textContent)).toEqual([
    'Overview',
    'Teams',
    'API Keys',
    'Models',
    'Budgets, quotas and limits',
    'Roles and permissions',
    'Settings',
  ])
})
it('preserves null policy and unavailable usage distinctly from explicit zero and retained statuses', async () => {
  const row = memberTeamRow('d')
  row.status = 'archived'
  row.membership_status = 'disabled'
  row.limits.policy_recorded = false
  row.limits.policy_etag = '0'
  row.limits.stored = emptyMemberTeamPolicy()
  row.limits.usage_status = 'unavailable'
  row.limits.usage = null
  row.limits.active_reservations = null
  pages = [memberTeamsPage([row])]
  await mount()
  await until(() => expect(host.textContent).toContain('Recorded Team d'))
  expect(host.textContent).toContain('No member policy recorded')
  expect(host.textContent).toContain('Unknown / Not set')
  expect(host.textContent).toContain('Archived')
  expect(host.textContent).toContain('Disabled membership')
  expect(host.textContent).not.toContain('No recorded charges')
})
it('updates help and precise values live in Chinese without another data request', async () => {
  await render()
  const n = reads().length
  const trigger = host.querySelector<HTMLButtonElement>(
    '[aria-label="About member token usage and quota"]',
  )!
  await act(async () => trigger.focus())
  await until(() => expect(document.querySelector('[role="tooltip"]')).not.toBeNull())
  await act(async () => {
    await i18n.changeLanguage('zh')
  })
  expect(host.textContent).toContain('Token 用量 / 配额')
  expect(document.querySelector('[role="tooltip"]')!.textContent).toContain('团队与成员数值不相加')
  expect(reads()).toHaveLength(n)
})
it('hides rows and private portals during same-millisecond genuine Session renewal, then reauthorizes', async () => {
  await render()
  const trigger = host.querySelector<HTMLButtonElement>(
    '[aria-label="Recorded context for Recorded Team c"]',
  )!
  await act(async () => trigger.focus())
  await until(() => expect(document.querySelector('[role="tooltip"]')).not.toBeNull())
  const g = gate()
  sessionGate = g
  const before = cache.getQueryState(['auth', 'session'])!.dataUpdateCount
  const time = cache.getQueryState(['auth', 'session'])!.dataUpdatedAt
  vi.spyOn(Date, 'now').mockReturnValue(time)
  let renewed!: Promise<unknown>
  await act(async () => {
    renewed = cache.refetchQueries({ queryKey: ['auth', 'session'] })
  })
  expect(host.textContent).not.toContain('Recorded Team c')
  expect(document.querySelector('[role="tooltip"]')).toBeNull()
  expect(host.querySelector('a[href^="/admin/teams/"]')).toBeNull()
  await act(async () => {
    g.release()
    await renewed
  })
  sessionGate = undefined
  await until(() => expect(host.textContent).toContain('Recorded Team c'))
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdateCount).toBeGreaterThan(before)
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdatedAt).toBe(time)
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
})
it('denies a captured Team link and tooltip event before React paints revoked permissions', async () => {
  await render()
  const link = host.querySelector<HTMLAnchorElement>('a[href^="/admin/teams/"]')!,
    help = host.querySelector<HTMLButtonElement>(
      '[aria-label="Recorded context for Recorded Team c"]',
    )!
  let allowed = true
  await act(async () => {
    cache.setQueryData(['permissions', actor], ['members.read'])
    const event = new MouseEvent('click', { bubbles: true, cancelable: true })
    allowed = link.dispatchEvent(event)
    help.focus()
  })
  expect(allowed).toBe(false)
  expect(router.state.location.pathname).toContain('/admin/members/')
  expect(document.querySelector('[role="tooltip"]')).toBeNull()
  expect(host.textContent).not.toContain('Recorded Team c')
})
it('does not retain rows after permission-read errors or Team-read errors', async () => {
  await render()
  permissionStatus = 503
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions', actor] })
  })
  expect(host.textContent).not.toContain('Recorded Team c')
  expect(host.querySelector('a[href^="/admin/teams/"]')).toBeNull()
  permissionStatus = 0
  await act(async () => {
    await cache.refetchQueries({ queryKey: ['permissions', actor] })
  })
  await until(() => expect(host.textContent).toContain('Recorded Team c'))
  teamStatus = 503
  await act(async () => button('Refresh')!.click())
  await until(() => expect(host.textContent).not.toContain('Recorded Team c'))
  expect(document.querySelector('[role="tooltip"]')).toBeNull()
})
it.each(['actor', 'target', 'tab', 'logout'])(
  'discards a late page when %s changes and aborts the exact request',
  async (change) => {
    const g = gate()
    teamGate = g
    await mount()
    await until(() => expect(reads()).toHaveLength(1))
    const signal = reads()[0].signal
    await act(async () => {
      if (change === 'actor') {
        pages = [memberTeamsPage([memberTeamRow('d')])]
        actor = 'usr_01dddddddddddddddddddddddd'
        cache.setQueryData(['auth', 'session'], {
          user: { id: actor, role: 'admin', name: 'Other' },
          csrf_token: 'other',
        })
      }
      if (change === 'target')
        await router.navigate('/admin/members/usr_01dddddddddddddddddddddddd?tab=teams')
      if (change === 'tab')
        await router.navigate(`/admin/members/${memberTeamsTarget}?tab=settings`)
      if (change === 'logout') cache.setQueryData(['auth', 'session'], null)
    })
    await until(() => expect(signal?.aborted).toBe(true))
    await act(async () => {
      g.release()
    })
    if (change === 'actor') await until(() => expect(host.textContent).toContain('Recorded Team d'))
    expect(host.textContent).not.toContain('Recorded Team c')
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
  },
)
it('loads bounded next pages and rejects overlapping identities instead of deduplicating', async () => {
  pages[0].next_cursor = btoa('canonical')
  pages.push(memberTeamsPage([memberTeamRow('d')]))
  await render()
  await act(async () => button('Load more')!.click())
  await until(() => expect(host.textContent).toContain('Recorded Team d'))
  expect(host.querySelectorAll('tbody tr')).toHaveLength(2)
  expect(reads()[1].params).toEqual({ limit: 20, cursor: btoa('canonical') })
  expect(button('Load more')).toBeUndefined()
})
it('hides all prior pages on malformed overlapping pagination', async () => {
  pages[0].next_cursor = btoa('canonical')
  pages.push(memberTeamsPage())
  await render()
  await act(async () => button('Load more')!.click())
  await until(() =>
    expect(host.textContent).toContain(
      'The action failed. Check the service connection and retry.',
    ),
  )
  expect(host.textContent).not.toContain('Recorded Team c')
  expect(host.querySelector('a[href^="/admin/teams/"]')).toBeNull()
})
it('blocks captured pagination before renewed authority paints', async () => {
  pages[0].next_cursor = btoa('canonical')
  pages.push(memberTeamsPage([memberTeamRow('d')]))
  await render()
  const next = button('Load more')!
  await act(async () => {
    cache.setQueryData(['permissions', actor], ['members.read'])
    next.click()
  })
  expect(reads()).toHaveLength(1)
})

it('renders known zero usage and empty holds distinctly from unknown journal facts', async () => {
  const l = pages[0].items[0].limits
  l.usage = {
    ...l.usage!,
    covered: true,
    tokens_used: '0',
    tokens_unknown: '0',
    money_unknown: '0',
    money_used: {},
    money_held: {},
  }
  l.active_reservations = { tokens_held: '0', money_held: {} }
  await render()
  expect(host.textContent).toContain('0 / 0')
  expect(host.textContent).toContain('No recorded charges / 0.000000000000000001 USD')
  expect(host.textContent).toContain('Live reservations: No recorded reservations')
  expect(host.textContent).toContain('Live reservations: 0')
  expect(host.textContent).not.toContain('Known subtotal; coverage is incomplete')
})
