import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import client from '@/api/client'
import i18n from '@/i18n'
import { sessionKey } from '@/hooks/use-auth'
import type { Session } from '@/types/auth'
import type { ResourceRecord } from '@/types/resources'
import ResourceListPage from './list'
import ResourceDetailPage from './detail'

vi.mock('./settings', () => ({ ResourceSettings: () => <div>Settings panel</div> }))
vi.mock('./project-overview', () => ({ default: () => <div>Overview panel</div> }))
vi.mock('./models', () => ({ ResourceModels: () => <div>Models panel</div> }))
vi.mock('./people', () => ({ ResourcePeople: () => <div>People panel</div> }))
vi.mock('./team-roles', () => ({ default: () => <div>Roles panel</div> }))
vi.mock('@/views/project-keys', () => ({ default: () => <div>Keys panel</div> }))
vi.mock('@/views/project-requests', () => ({ default: () => <div>Requests panel</div> }))
vi.mock('@/views/team-model-requests/team-history', () => ({ default: () => <div /> }))
vi.mock('@/views/resource-limits', () => ({ default: () => <div>Limits panel</div> }))
vi.mock('@/views/usage', () => ({ ProjectUsagePanel: () => <div>Usage panel</div> }))
vi.mock('@/views/calls', () => ({ default: () => <div>Calls panel</div> }))
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const originalAdapter = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let requests: InternalAxiosRequestConfig[],
  rows: ResourceRecord[],
  permissions: string[],
  actor: string
let listStatus: number, detailStatus: number, sessionStatus: number
let heldList: Promise<void> | null,
  heldDetail: Promise<void> | null,
  heldSession: Promise<void> | null,
  heldPermissions: Promise<void> | null
let releaseAll: (() => void)[]
const manager = {
  id: 'pmg_one',
  user_id: 'usr_one',
  name: 'Project manager',
  email: 'manager@example.invalid',
}
function session(): Session {
  return {
    user: { id: actor, name: 'Current actor', role: 'member', email: 'member@example.invalid' },
    csrf_token: 'current-csrf',
  }
}
function project(id: string, extra: Partial<ResourceRecord> = {}): ResourceRecord {
  return {
    id,
    name: `Private ${id}`,
    description: 'Private description',
    created_at: '2026-10-04T00:00:00Z',
    status: 'active',
    model_ids: ['mdl_one', 'mdl_two', 'mdl_three'],
    managers: [manager],
    key_count: 2,
    limits: null,
    ...extra,
  }
}
function held(): [Promise<void>, () => void] {
  let release!: () => void
  const promise = new Promise<void>((resolve) => {
    release = resolve
  })
  releaseAll.push(release)
  return [promise, release]
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  actor = 'usr_one'
  permissions = ['projects.read_all', 'projects.write']
  rows = [project('prj_one')]
  listStatus = detailStatus = sessionStatus = 200
  heldList = heldDetail = heldSession = heldPermissions = null
  releaseAll = []
  requests = []
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60000 }, mutations: { retry: false } },
  })
  cache.setQueryData(sessionKey, session())
  cache.setQueryData(['permissions', actor], permissions)
  client.defaults.adapter = async (config) => {
    requests.push(config)
    let status = 200,
      data: unknown
    if (config.url === '/auth/session') {
      const wait = heldSession
      const current = session()
      status = sessionStatus
      if (wait) await wait
      data = current
    } else if (config.url === '/auth/permissions') {
      const wait = heldPermissions
      if (wait) await wait
      data = { permissions }
    } else if (
      ['/projects', '/admin/projects', '/teams', '/admin/teams'].includes(config.url ?? '')
    ) {
      const wait = heldList,
        captured = [...rows]
      status = listStatus
      if (wait) await wait
      data = { items: captured, next_cursor: null }
    } else if (config.url?.endsWith('/roles'))
      data = {
        team_id: 'tea_one',
        actor_team_actions: [],
        effective_team_actions: [],
        role_ids: [],
        roles: [],
        can_assign_roles: false,
        etag: 'a'.repeat(64),
      }
    else if (
      config.url?.startsWith('/projects/') ||
      config.url?.startsWith('/admin/projects/') ||
      config.url === '/teams/tea_one'
    ) {
      const wait = heldDetail
      status = detailStatus
      if (wait) await wait
      data = config.url.includes('tea_one')
        ? { ...project('tea_one'), members: [{ ...manager, role: 'owner', status: 'active' }] }
        : rows.find((row) => config.url?.endsWith(row.id))
    } else throw new Error('Unexpected endpoint ' + config.url)
    const response = { config, status, statusText: '', data, headers: new AxiosHeaders() }
    if (status >= 400)
      throw new AxiosError('Current authority unavailable', '', config, undefined, response)
    return response
  }
})
afterEach(async () => {
  await act(async () => {
    releaseAll.forEach((release) => release())
    root.unmount()
  })
  router?.dispose()
  cache.clear()
  client.defaults.adapter = originalAdapter
  host.remove()
})
async function until(assertion: () => void) {
  for (let n = 0; n < 80; n++) {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 5)))
    try {
      assertion()
      return
    } catch (error) {
      if (n === 79) throw error
    }
  }
}
async function mount(path = '/projects', kind: 'projects' | 'teams' = 'projects') {
  const admin = path.startsWith('/admin')
  router = createMemoryRouter(
    [
      { path: '/projects', element: <ResourceListPage kind="projects" /> },
      { path: '/admin/projects', element: <ResourceListPage kind="projects" admin /> },
      { path: '/teams', element: <ResourceListPage kind="teams" /> },
      {
        path: `${admin ? '/admin' : ''}/${kind}/:resourceId`,
        element: <ResourceDetailPage kind={kind} admin={admin} />,
      },
    ],
    { initialEntries: [path] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
function table() {
  return host.querySelector('table')!
}
function tableRows() {
  return [...table().querySelectorAll('tbody tr')]
}
function listReads() {
  return requests.filter((item) => item.url === '/projects' || item.url === '/admin/projects')
}
async function click(label: string) {
  const found = [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (item) => item.textContent === label || item.getAttribute('aria-label') === label,
  )!
  expect(found, label).toBeDefined()
  await act(async () => found.click())
}

describe('Project list projections', () => {
  it('shows retained Key count in the member table without Overview, limits or global directory reads', async () => {
    await mount()
    await until(() => expect(tableRows()).toHaveLength(1))
    expect(table().textContent).toContain('Project Keys')
    expect(table().textContent).not.toContain('Monthly token policy')
    expect(tableRows()[0].children[2].textContent).toBe('2')
    expect(table().textContent).not.toContain('3 models')
    expect(requests.map((item) => item.url)).not.toContain('/admin/projects')
    expect(
      requests.some((item) => item.url?.includes('/overview') || item.url?.includes('/limits')),
    ).toBe(false)
    expect(host.querySelector('input[name="q"]')).toBeNull()
  })
  it('keeps exact stored zero, absent controls, absent policy and unavailable projections distinct', async () => {
    rows = [
      project('prj_zero', {
        key_count: 0,
        limits: {
          stored: true,
          tokens_month: 0,
          money_month: '0.0000',
          currency: 'USD',
          rpm: 0,
          tpm: 0,
        },
      }),
      project('prj_precision', {
        key_count: 4,
        limits: {
          stored: true,
          tokens_month: 1000001,
          money_month: '999999999999999999.000001',
          currency: 'JPY',
          rpm: null,
          tpm: 2000,
        },
      }),
      project('prj_absent', {
        key_count: null,
        limits: {
          stored: false,
          tokens_month: null,
          money_month: null,
          currency: '',
          rpm: null,
          tpm: null,
        },
      }),
      project('prj_unavailable', { key_count: null, limits: null }),
      project('prj_missing', { key_count: undefined, limits: undefined }),
    ]
    await mount('/admin/projects')
    await until(() => expect(tableRows()).toHaveLength(5))
    expect(tableRows()[0].children[2].textContent).toBe('0')
    expect(tableRows()[0].textContent).toContain('0.0000 USD')
    expect(tableRows()[0].textContent).toContain('0 RPM · 0 TPM')
    expect(tableRows()[1].textContent).toContain('1,000,001')
    expect(tableRows()[1].textContent).toContain('999999999999999999.000001 JPY')
    expect(tableRows()[1].textContent).toContain('Not set RPM · 2,000 TPM')
    expect(tableRows()[2].textContent).toContain('Unknown')
    expect(tableRows()[2].textContent).toContain('No stored policy')
    expect(tableRows()[3].textContent).toContain('Unavailable')
    expect(tableRows()[4].textContent).toContain('Unknown')
    expect(tableRows()[4].children[3].textContent).toBe('Unknown')
    expect(tableRows()[3].children[3].textContent).toBe('Unavailable')
    expect(
      requests.some((item) => item.url?.includes('/overview') || item.url?.includes('/limits')),
    ).toBe(false)
    await act(async () => i18n.changeLanguage('zh'))
    expect(table().textContent).toContain('月度 Token 规则')
    expect(tableRows()[1].textContent).toContain('未设置 RPM')
    expect(tableRows()[1].textContent).toContain('999999999999999999.000001 JPY')
  })
  it('shows unavailable counts for read_all-only reviewers without pretending zero or fetching Keys', async () => {
    permissions = ['projects.read_all']
    cache.setQueryData(['permissions', actor], permissions)
    rows = [
      project('prj_one', {
        key_count: null,
        limits: {
          stored: true,
          tokens_month: null,
          money_month: null,
          currency: '',
          rpm: null,
          tpm: null,
        },
      }),
    ]
    await mount('/admin/projects')
    await until(() => expect(tableRows()).toHaveLength(1))
    expect(tableRows()[0].children[2].textContent).toBe('Unknown')
    expect(requests.some((item) => item.url?.includes('/keys'))).toBe(false)
  })
  it('submits literal Project name or ID query and status without row lookups', async () => {
    await mount('/admin/projects')
    await until(() => expect(tableRows()).toHaveLength(1))
    const input = host.querySelector<HTMLInputElement>('input[name="q"]')!
    expect(input.placeholder).toBe('Search Project name or ID')
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        '  project_%literal  ',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
      const status = host.querySelector<HTMLSelectElement>('select[name="status"]')!
      status.value = 'disabled'
      status.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await click('Filter')
    await until(() =>
      expect(listReads().at(-1)!.params).toMatchObject({
        q: 'project_%literal',
        status: 'disabled',
      }),
    )
    expect(requests.some((item) => item.url?.includes('/overview'))).toBe(false)
  })
  it.each([403, 404])(
    'hides rows and actions during renewed list reads and after %i',
    async (status) => {
      await mount()
      await until(() => expect(tableRows()).toHaveLength(1))
      let release!: () => void
      ;[heldList, release] = held()
      listStatus = status
      await act(async () => {
        void cache.invalidateQueries({ queryKey: ['resources', 'projects', false] })
      })
      await until(() => expect(tableRows()).toHaveLength(0))
      expect(host.textContent).not.toContain('Private prj_one')
      await act(async () => release())
      await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
      expect(tableRows()).toHaveLength(0)
      expect(host.textContent).not.toContain('Project manager')
    },
  )
  it('reauthorizes after Session renewal and suppresses a late old actor list', async () => {
    await mount()
    await until(() => expect(tableRows()).toHaveLength(1))
    let release!: () => void
    ;[heldList, release] = held()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: ['resources', 'projects', false] })
    })
    await until(() => expect(tableRows()).toHaveLength(0))
    actor = 'usr_two'
    listStatus = 403
    heldList = null
    await act(async () => cache.setQueryData(sessionKey, session()))
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    await act(async () => release())
    expect(tableRows()).toHaveLength(0)
    expect(host.textContent).not.toContain('Private prj_one')
  })
  it('hides stored projections during same-actor Session renewal and fetches them again afterwards', async () => {
    await mount('/admin/projects')
    await until(() => expect(tableRows()).toHaveLength(1))
    const before = listReads().length
    let release!: () => void
    ;[heldSession, release] = held()
    await act(async () => {
      void cache.invalidateQueries({ queryKey: sessionKey })
    })
    await until(() => expect(tableRows()).toHaveLength(0))
    rows = [project('prj_new', { key_count: null })]
    await act(async () => release())
    await until(() => expect(tableRows()[0]?.textContent).toContain('Private prj_new'))
    expect(listReads().length).toBeGreaterThan(before)
    expect(host.textContent).not.toContain('Private prj_one')
  })
  it('preserves Team model counts and existing member navigation', async () => {
    rows = [{ ...project('tea_one'), members: [{ ...manager, role: 'owner', status: 'active' }] }]
    await mount('/teams', 'teams')
    await until(() => expect(tableRows()).toHaveLength(1))
    expect(table().textContent).toContain('3 models')
    expect(table().textContent).not.toContain('Project Keys')
  })
})

describe('legacy Project tab navigation', () => {
  it.each([
    ['managers', 'settings'],
    ['models', 'resources'],
    ['limits', 'resources'],
  ])('canonicalizes %s to %s after a fresh exact resource read', async (legacy, canonical) => {
    const [wait, release] = held()
    heldDetail = wait
    await mount(`/projects/prj_one?tab=${legacy}&keep=literal`)
    await until(() => expect(requests.some((item) => item.url === '/projects/prj_one')).toBe(true))
    expect(router.state.location.search).toBe(`?tab=${legacy}&keep=literal`)
    expect(host.textContent).not.toContain('Private prj_one')
    await act(async () => release())
    await until(() => expect(router.state.location.search).toBe(`?tab=${canonical}&keep=literal`))
    expect(router.state.historyAction).toBe('REPLACE')
  })
  it.each([403, 404])('does not canonicalize an unauthorized Project (%i)', async (status) => {
    detailStatus = status
    await mount('/projects/prj_one?tab=models')
    await until(() => expect(host.querySelector('[role="alert"]')).not.toBeNull())
    expect(router.state.location.search).toBe('?tab=models')
    expect(host.textContent).not.toContain('Private prj_one')
  })
  it('waits for current Session authority before replacing a legacy URL', async () => {
    const [wait, release] = held()
    heldSession = wait
    await mount('/projects/prj_one?tab=managers&keep=yes')
    await until(() => expect(requests.some((item) => item.url === '/projects/prj_one')).toBe(true))
    expect(router.state.location.search).toBe('?tab=managers&keep=yes')
    await act(async () => release())
    await until(() => expect(router.state.location.search).toBe('?tab=settings&keep=yes'))
  })
  it('keeps Team models and limits URLs unchanged', async () => {
    await mount('/teams/tea_one?tab=models', 'teams')
    await until(() => expect(host.textContent).toContain('Models panel'))
    expect(router.state.location.search).toBe('?tab=models')
    await act(async () => router.navigate('/teams/tea_one?tab=limits'))
    expect(router.state.location.search).toBe('?tab=limits')
  })
})

it('hides administrative policy projections while permissions renew and receives narrower Key authority afterwards', async () => {
  await mount('/admin/projects')
  await until(() => expect(tableRows()).toHaveLength(1))
  let release!: () => void
  ;[heldPermissions, release] = held()
  await act(async () => {
    void cache.invalidateQueries({ queryKey: ['permissions', actor] })
  })
  await until(() => expect(tableRows()).toHaveLength(0))
  permissions = ['projects.read_all']
  rows = [project('prj_one', { key_count: null })]
  await act(async () => release())
  await until(() => expect(tableRows()).toHaveLength(1))
  expect(tableRows()[0].children[2].textContent).toBe('Unknown')
  expect(
    requests.some((item) => item.url?.includes('/keys') || item.url?.includes('/overview')),
  ).toBe(false)
})

it('renders malformed or inexact policy projections as unknown without monetary coercion', async () => {
  rows = [
    project('prj_one', {
      key_count: -1,
      limits: {
        stored: true,
        tokens_month: Number.MAX_SAFE_INTEGER + 1,
        money_month: '1e3',
        currency: 'USD',
        rpm: -1,
        tpm: 0,
      },
    }),
  ]
  await mount('/admin/projects')
  await until(() => expect(tableRows()).toHaveLength(1))
  expect(tableRows()[0].children[2].textContent).toBe('Unknown')
  expect(tableRows()[0].children[3].textContent).toBe('Unknown')
  expect(tableRows()[0].children[4].textContent).toBe('Unknown')
  expect(tableRows()[0].children[5].textContent).toBe('Unknown RPM · 0 TPM')
})

it('does not let a late Project authorization redirect the current resource target', async () => {
  rows = [project('prj_one'), project('prj_two')]
  const [wait, release] = held()
  heldDetail = wait
  await mount('/projects/prj_one?tab=models')
  await until(() => expect(requests.some((item) => item.url === '/projects/prj_one')).toBe(true))
  heldDetail = null
  await act(async () => router.navigate('/projects/prj_two?tab=managers&keep=second'))
  await until(() =>
    expect(router.state.location.pathname + router.state.location.search).toBe(
      '/projects/prj_two?tab=settings&keep=second',
    ),
  )
  await act(async () => release())
  expect(router.state.location.pathname + router.state.location.search).toBe(
    '/projects/prj_two?tab=settings&keep=second',
  )
  expect(host.textContent).not.toContain('Private prj_one')
})

it('canonicalizes administrative legacy URLs using the scoped administrative detail endpoint', async () => {
  await mount('/admin/projects/prj_one?tab=limits')
  await until(() => expect(router.state.location.search).toBe('?tab=resources'))
  expect(requests.some((item) => item.url === '/admin/projects/prj_one')).toBe(true)
  expect(requests.some((item) => item.url === '/projects/prj_one')).toBe(false)
})
