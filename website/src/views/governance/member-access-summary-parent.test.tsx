import { rolesWorkspace, roleSummary } from './member-roles.fixture'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import MembersPage from './members'
import { accessSummaryFixture } from './member-access-summary.fixture'
import { effectiveModelsPage } from './member-effective-models.fixture'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let host: HTMLDivElement,
  root: Root,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let permissions: string[], requests: InternalAxiosRequestConfig[], failure: number
beforeEach(async () => {
  permissions = ['members.read', 'roles.read', 'teams.read_all']
  requests = []
  failure = 0
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await i18n.changeLanguage('en')
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let data: unknown
    const target = config.url?.split('/')[3] ?? 'usr_target'
    if (config.url === '/auth/session')
      data = {
        user: { id: 'usr_reader', name: 'Reader', role: 'admin' },
        csrf_token: 'current-csrf',
      }
    else if (config.url === '/auth/permissions') data = { permissions }
    else if (/^\/admin\/members\/[^/]+$/.test(config.url ?? ''))
      data = {
        id: target,
        name: 'Target',
        email: 'target@example.invalid',
        role: 'member',
        disabled: false,
        offboarded_at: null,
        created_at: '2026-10-01T00:00:00Z',
        role_ids: ['rol_selected'],
        registration_approval: { status: 'not_required', admission_eligible: false },
        last_login_at: null,
        last_login_status: 'historical_unavailable',
      }
    else if (config.url?.endsWith('/roles/candidates'))
      data = {
        items: [roleSummary('rol_unassigned', 'Unassigned directory role')],
        next_cursor: null,
        etag: 'a'.repeat(64),
      }
    else if (config.url?.endsWith('/roles')) {
      const page = rolesWorkspace(target)
      page.assigned_roles = [roleSummary('rol_selected', 'Recorded role')]
      data = page
    } else if (config.url?.endsWith('/access')) {
      if (failure)
        throw new AxiosError('Unavailable', '', config, undefined, {
          config,
          status: failure,
          statusText: '',
          headers: new AxiosHeaders(),
          data: {},
        })
      data = accessSummaryFixture(target, {
        roles: permissions.includes('roles.read'),
        teams: permissions.includes('teams.read_all'),
      })
    } else if (config.url?.endsWith('/overview'))
      data = {
        user_id: target,
        observed_at: '2026-10-05T00:00:00Z',
        platform_currency: 'USD',
        total_personal_keys: '0',
        personal: {
          account_id: `user:${target}`,
          policy_etag: '0',
          tokens_month: null,
          money_month: null,
          currency: null,
          runtime_applied: false,
          usage_status: 'inactive',
          usage: null,
          active_reservations: null,
        },
      }
    else if (config.url?.endsWith('/effective-models')) {
      const page = effectiveModelsPage(target, {
        teams: permissions.includes('teams.read_all'),
        providers: false,
        prices: false,
      })
      page.items.forEach((row) =>
        row.sources.forEach((source) => {
          if (source.kind === 'team') source.team_name = 'Effective source Team'
        }),
      )
      data = page
    } else if (config.url === '/admin/roles')
      data = {
        items: [
          {
            id: 'rol_selected',
            name: 'Recorded role',
            builtin: false,
            permissions: ['providers.read'],
          },
          {
            id: 'rol_unassigned',
            name: 'Unassigned directory role',
            builtin: false,
            permissions: [],
          },
          { id: 'rol_member', name: 'Member', builtin: true, permissions: [] },
        ],
        available_permissions: ['providers.read'],
      }
    else throw new Error('Unexpected endpoint ' + config.url)
    return {
      config,
      status: 200,
      statusText: '',
      headers: new AxiosHeaders({
        'cache-control': 'private, no-store',
        ...(config.url?.includes('/roles') ? { etag: `"${'a'.repeat(64)}"` } : {}),
      }),
      data,
    }
  }
  router = createMemoryRouter([{ path: '/admin/members/:memberId', element: <MembersPage /> }], {
    initialEntries: ['/admin/members/usr_target'],
  })
})
afterEach(async () => {
  await act(async () => root.unmount())
  router.dispose()
  cache.clear()
  host.remove()
  client.defaults.adapter = original
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
  await until(() => expect(host.textContent).toContain('Recorded Team'))
}
it('uses one resource summary for Overview header and eight cells without global role/Team reads', async () => {
  await mount()
  expect(requests.filter((r) => r.url?.endsWith('/access'))).toHaveLength(1)
  expect(requests.some((r) => r.url === '/admin/roles' || r.url === '/admin/teams')).toBe(false)
  expect(host.textContent).not.toContain('Unassigned directory role')
  const card = host.querySelector<HTMLElement>('section[aria-label="Access status"]')!
  expect([...card.querySelectorAll('dt')].map((cell) => cell.textContent)).toEqual([
    'Roles',
    'Teams',
    'Recent login',
    'Created',
    'Updated',
    'Status',
    'Registration approval',
    'Current admission',
  ])
  const cards = [...host.querySelectorAll('section')]
  expect(cards.indexOf(card)).toBeLessThan(
    cards.findIndex((section) => section.getAttribute('aria-label') === 'Effective models'),
  )
  expect(host.textContent).toContain('Roles: Member · Recorded role')
  expect(requests.every((r) => r.method === 'get')).toBe(true)
})
it('uses scoped assigned-role review and preserves the direct saved permission card without a global directory', async () => {
  await mount()
  await act(async () => router.navigate('/admin/members/usr_target?tab=roles'))
  await until(() =>
    expect(host.querySelector('table[aria-label="Assigned member roles"]')?.textContent).toContain(
      'Recorded role',
    ),
  )
  expect(requests.filter((r) => r.url === '/admin/members/usr_target/roles')).toHaveLength(1)
  expect(
    host.querySelector('table[aria-label="Assigned member roles"]')?.textContent,
  ).not.toContain('Unassigned directory role')
  expect(host.querySelector('[aria-label="Remove Recorded role"]')).not.toBeNull()
  expect(host.textContent).toContain('Effective member permissions')
  await act(async () => router.navigate('/admin/members/usr_target'))
  expect(host.textContent).not.toContain('Unassigned directory role')
  expect(requests.filter((r) => r.url === '/admin/roles')).toHaveLength(0)
  expect(requests.every((request) => request.method === 'get')).toBe(true)
})
it('clears header, Access cells and tooltip authority on target invalidation and failed renewed reads', async () => {
  await mount()
  const query = cache
    .getQueryCache()
    .findAll({ queryKey: ['admin', 'member', 'usr_reader', 'usr_target'] })[0]
  await act(async () => {
    void cache.invalidateQueries({ queryKey: query.queryKey, refetchType: 'none' })
  })
  expect(document.body.textContent).not.toContain('Recorded Team')
  expect(document.body.textContent).not.toContain('Recorded role')
  failure = 503
  await act(async () => {
    await cache.refetchQueries({ queryKey: query.queryKey })
  })
  await until(() => expect(host.textContent).toContain('The action failed.'))
  expect(host.textContent).not.toContain('Recorded Team')
  expect(host.textContent).not.toContain('Recorded role')
})
it('keeps denied independent Role/Team sections explicit without a directory request', async () => {
  permissions = ['members.read']
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
  await until(() => expect(host.textContent).toContain('Not authorized'))
  expect(host.textContent).not.toContain('Recorded Team')
  expect(host.textContent).not.toContain('Recorded role')
  expect(host.textContent).not.toContain('No Teams')
  expect(requests.some((r) => r.url === '/admin/roles' || r.url === '/admin/teams')).toBe(false)
})
