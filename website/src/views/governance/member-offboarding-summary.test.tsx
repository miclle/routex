import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider, useParams } from 'react-router'
import { AxiosError, AxiosHeaders, type InternalAxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import client from '@/api/client'
import { getPermissions } from '@/api/governance'
import { getMemberDetail } from '@/api/member-recent-login'
import { getSession } from '@/api/auth'
import { useSessionGeneration } from '@/hooks/use-session-generation'
import i18n from '@/i18n'
import type { Session } from '@/types/auth'
import MemberOffboardingSummary from './member-offboarding-summary'
import MembersPage from './members'
import { memberStateFixture, stateReviewETag } from './member-state.fixture'
import { accessSummaryFixture } from './member-access-summary.fixture'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const original = client.defaults.adapter
let root: Root,
  host: HTMLDivElement,
  cache: QueryClient,
  router: ReturnType<typeof createMemoryRouter>
let session: Session,
  permissions: string[],
  requests: InternalAxiosRequestConfig[],
  cases: ReturnType<typeof record>[],
  keyCount: number
let failure: number,
  next: ((config: InternalAxiosRequestConfig) => Promise<unknown> | undefined) | undefined
function record(
  id = 'off_saved',
  status: 'ready_to_complete' | 'completed' = 'ready_to_complete',
  mode: 'planned' | 'emergency' = 'planned',
) {
  return {
    id,
    request_id: 'original-request',
    user_id: 'usr_target',
    actor_id: 'usr_original',
    mode,
    status,
    reason: 'Original reason',
    planned_at: mode === 'planned' ? '2026-10-08T09:00:00+08:00' : null,
    created_at: '2026-10-05T00:00:00Z',
    completed_at: status === 'completed' ? '2026-10-06T00:00:00Z' : null,
    completed_by: status === 'completed' ? 'usr_other_completer' : '',
    inventory_version: 'a'.repeat(64),
    assignments: {
      project_assignments: [{ project_id: 'prj_recorded', manager_user_ids: ['usr_successor'] }],
      team_assignments: [
        { team_id: 'tea_recorded', owner_user_ids: ['usr_successor'], add_member_user_ids: [] },
      ],
    },
  }
}
function member(target: string) {
  return {
    id: target,
    name: 'Target',
    email: 'target@example.invalid',
    role: 'member' as const,
    role_ids: [],
    disabled: false,
    offboarded_at: null,
    created_at: '2026-09-23T00:00:00Z',
    last_login_at: null,
    last_login_status: 'historical_unavailable',
    registration_approval: { status: 'not_required', admission_eligible: false },
  }
}
function inventory(target = 'usr_target') {
  return {
    user_id: target,
    disabled: false,
    offboarded_at: null,
    last_administrator: false,
    inventory_version: 'a'.repeat(64),
    projects: [],
    teams: [],
    personal_keys: Array.from({ length: keyCount }, (_, i) => ({
      id: `key_${i}`,
      name: 'Current private Key',
      prefix: 'masked',
      status: 'revoked',
    })),
    cases: structuredClone(cases),
  }
}
function error(config: InternalAxiosRequestConfig, status: number) {
  return new AxiosError('Controlled failure', '', config, undefined, {
    config,
    status,
    statusText: '',
    headers: new AxiosHeaders(),
    data: {},
  })
}
function deferred() {
  let resolve!: (data: unknown) => void
  const promise = new Promise<unknown>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  session = {
    user: { id: 'usr_admin', name: 'Admin', email: 'admin@example.invalid', role: 'admin' },
    csrf_token: 'csrf_current',
  }
  permissions = ['members.read']
  requests = []
  cases = [record()]
  failure = 0
  keyCount = 17
  next = undefined
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  cache = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  client.defaults.adapter = async (config) => {
    requests.push(config)
    const target = config.url?.split('/')[3] ?? 'usr_target'
    let data: unknown
    const intercepted = next?.(config)
    if (intercepted) data = await intercepted
    else if (config.url === '/auth/session') data = structuredClone(session)
    else if (config.url === '/auth/permissions') data = { permissions: [...permissions] }
    else if (config.url?.endsWith('/offboarding')) {
      if (failure) throw error(config, failure)
      data = inventory(target)
    } else if (config.url?.endsWith('/state'))
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({
          ETag: `"${stateReviewETag}"`,
          'cache-control': 'private, no-store',
        }),
        data: memberStateFixture(member(target), session.user.id, session.user.role, permissions),
      }
    else if (config.url?.endsWith('/metadata'))
      return {
        config,
        status: 200,
        statusText: '',
        headers: new AxiosHeaders({ ETag: `"${'a'.repeat(64)}"` }),
        data: {
          user_id: target,
          name: 'Target',
          status: 'active',
          can_edit: permissions.includes('members.write'),
          etag: 'a'.repeat(64),
        },
      }
    else if (config.url?.endsWith('/access'))
      data = accessSummaryFixture(target, { roles: false, teams: false })
    else if (config.url?.startsWith('/admin/members/')) data = member(target)
    else throw new Error('Unexpected controlled endpoint ' + config.url)
    return { config, status: 200, statusText: '', headers: new AxiosHeaders(), data }
  }
})
afterEach(async () => {
  await act(async () => root.unmount())
  cache.clear()
  host.remove()
  client.defaults.adapter = original
  vi.restoreAllMocks()
})
async function until(check: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {})
    check()
  })
}
function Harness() {
  const { memberId } = useParams(),
    generation = useSessionGeneration()
  return (
    <MemberOffboardingSummary
      actor={session.user.id}
      target={memberId!}
      generation={generation}
      ready
      targetQueryKey={['admin', 'member', session.user.id, memberId, generation]}
    />
  )
}
async function authority(target = 'usr_target') {
  await cache.fetchQuery({
    queryKey: ['permissions', session.user.id],
    queryFn: ({ signal }) => getPermissions(signal),
  })
  const generation = cache.getQueryState(['auth', 'session'])!.dataUpdateCount
  await cache.fetchQuery({
    queryKey: ['admin', 'member', session.user.id, target, generation],
    queryFn: ({ signal }) => getMemberDetail(target, signal),
  })
}
async function mount(parent = false, tab = 'settings') {
  if (!parent) {
    await cache.fetchQuery({ queryKey: ['auth', 'session'], queryFn: () => getSession() })
    await authority()
  }
  router = createMemoryRouter(
    [
      { path: '/admin/members/:memberId', element: parent ? <MembersPage /> : <Harness /> },
      { path: '/admin/members/:memberId/offboarding', element: <p>Controlled handover page</p> },
    ],
    { initialEntries: [`/admin/members/usr_target?tab=${tab}`] },
  )
  await act(async () =>
    root.render(
      <QueryClientProvider client={cache}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  )
}
function card() {
  return host.querySelector('section[aria-label="Offboarding"],section[aria-label="离职交接"]')!
}
function button(label = 'Review offboarding') {
  return [...host.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent === label,
  )
}
async function loaded() {
  await until(() => expect(button()).toBeDefined())
}
it('uses independent read authority and recorded assignments rather than current Key/resource counts', async () => {
  await mount()
  await loaded()
  expect(card().textContent).toContain('1 Projects / 1 Teams')
  expect(card().textContent).toContain('Plan saved')
  expect(card().textContent).toContain('Not recorded')
  expect(card().textContent).not.toContain('17')
  expect(card().textContent).not.toContain('Current private Key')
  expect(card().querySelectorAll('dt')).toHaveLength(4)
  expect(requests.filter((r) => r.url?.endsWith('/offboarding'))).toHaveLength(1)
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(1)
  expect(requests.every((r) => r.method === 'get')).toBe(true)
  await act(async () => button()!.click())
  expect(router.state.location.pathname).toBe('/admin/members/usr_target/offboarding')
})
it('prefers the first recent saved plan and preserves server order without claiming a unique active plan', async () => {
  const completed = record('off_new', 'completed'),
    first = record('off_first'),
    second = record('off_second')
  first.assignments.project_assignments = []
  first.assignments.team_assignments = []
  second.assignments.project_assignments.push({ project_id: 'prj_second', manager_user_ids: [] })
  cases = [completed, first, second]
  await mount()
  await loaded()
  expect(card().textContent).toContain('0 Projects / 0 Teams')
  expect(card().textContent).toContain('Plan saved')
  expect(card().textContent).toContain('up to 100 recent records')
})
it('renders a completed emergency receipt after reactivation with null date and unknown historical Keys', async () => {
  cases = [record('off_completed', 'completed', 'emergency')]
  keyCount = 3
  await mount()
  await loaded()
  expect(card().textContent).toContain('Completed record')
  expect(card().textContent).toContain('Emergency')
  expect(card().textContent).toContain('Not set')
  expect(card().textContent).toContain('Not recorded')
  expect(card().textContent).not.toContain('usr_original')
  expect(card().textContent).not.toContain('usr_other_completer')
})
it('shows empty successful history with navigation, without implying no outstanding plan globally', async () => {
  cases = []
  await mount()
  await loaded()
  expect(card().textContent).toContain('No offboarding record.')
  expect(card().querySelector('dl')).toBeNull()
})
it('preserves unknown Key history and uses the most recent completed record when no plan is returned', async () => {
  const newest = record('off_new', 'completed'),
    older = record('off_old', 'completed')
  newest.assignments.project_assignments = []
  cases = [newest, older]
  await mount()
  await loaded()
  expect(card().textContent).toContain('0 Projects / 1 Teams')
  keyCount = 100
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['admin', 'member-offboarding-summary'] }),
  )
  await loaded()
  expect(card().textContent).toContain('Not recorded')
  expect(card().textContent).toContain('0 Projects / 1 Teams')
})
it('hides private facts and rejects a queued click during Session renewal, then uses new-generation reads', async () => {
  await mount()
  await loaded()
  const old = button()!,
    pending = deferred()
  next = (c) => (c.url === '/auth/session' ? pending.promise : undefined)
  let renewal!: Promise<unknown>
  await act(async () => {
    renewal = cache.fetchQuery({ queryKey: ['auth', 'session'], queryFn: () => getSession() })
    old.click()
  })
  expect(card().querySelector('dl')).toBeNull()
  expect(button()).toBeUndefined()
  expect(router.state.location.pathname).toBe('/admin/members/usr_target')
  await act(async () => {
    pending.resolve(session)
    await renewal
  })
  next = undefined
  expect(button()).toBeUndefined()
  await act(async () => authority())
  await loaded()
  expect(cache.getQueryState(['auth', 'session'])!.dataUpdateCount).toBe(2)
  expect(requests.filter((r) => r.url?.endsWith('/offboarding')).length).toBeGreaterThan(1)
})
it('discards an old matching response after same-user same-millisecond renewal', async () => {
  const pending = deferred()
  next = (c) => (c.url?.endsWith('/offboarding') ? pending.promise : undefined)
  vi.spyOn(Date, 'now').mockReturnValue(1791158400000)
  await mount()
  await until(() => expect(requests.some((r) => r.url?.endsWith('/offboarding'))).toBe(true))
  next = undefined
  cases = []
  await act(async () => {
    await cache.fetchQuery({ queryKey: ['auth', 'session'], queryFn: () => getSession() })
    await authority()
  })
  await loaded()
  expect(card().textContent).toContain('No offboarding record.')
  await act(async () => pending.resolve({ ...inventory(), cases: [record('off_obsolete')] }))
  expect(card().textContent).toContain('No offboarding record.')
  expect(card().textContent).not.toContain('Plan saved')
})
it.each(['permissions', 'parent', 'own'] as const)(
  'synchronously fences %s invalidation and old navigation',
  async (kind) => {
    await mount()
    await loaded()
    const old = button()!
    const key =
      kind === 'permissions'
        ? ['permissions', session.user.id]
        : kind === 'parent'
          ? ['admin', 'member']
          : ['admin', 'member-offboarding-summary']
    await act(async () => {
      void cache.invalidateQueries({ queryKey: key, refetchType: 'none' })
      old.click()
    })
    expect(card().querySelector('dl')).toBeNull()
    expect(button()).toBeUndefined()
    expect(router.state.location.pathname).toBe('/admin/members/usr_target')
  },
)
it('hides on revoked membership-read permission and refuses write-only authority', async () => {
  await mount()
  await loaded()
  permissions = ['members.write']
  await act(async () =>
    cache.fetchQuery({
      queryKey: ['permissions', session.user.id],
      queryFn: ({ signal }) => getPermissions(signal),
    }),
  )
  expect(button()).toBeUndefined()
  expect(card().querySelector('dl')).toBeNull()
})
it('distinguishes a read outage from empty history and explicit retry never writes', async () => {
  failure = 503
  await mount()
  await until(() => expect(button('Retry recorded offboarding')).toBeDefined())
  expect(card().textContent).not.toContain('No offboarding record.')
  failure = 0
  await act(async () => button('Retry recorded offboarding')!.click())
  await loaded()
  expect(requests.every((r) => r.method === 'get')).toBe(true)
})
it('rejects disconnected navigation after actor/target change and after unmount', async () => {
  await mount()
  await loaded()
  const old = button()!
  await act(async () => {
    await router.navigate('/admin/members/usr_other?tab=settings')
    await authority('usr_other')
  })
  await act(async () => old.click())
  expect(router.state.location.pathname).toBe('/admin/members/usr_other')
  cases = cases.map((r) => ({ ...r, user_id: 'usr_other' }))
  await act(async () =>
    cache.invalidateQueries({ queryKey: ['admin', 'member-offboarding-summary'] }),
  )
  await loaded()
  const other = button()!
  await act(async () => root.render(<p>Signed out</p>))
  await act(async () => other.click())
  expect(router.state.location.pathname).toBe('/admin/members/usr_other')
})
it.each([
  'wrong-target',
  'unknown-status',
  'unknown-mode',
  'invalid-date',
  'missing-assignments',
  'duplicate-record',
  'too-many',
  'duplicate-assignment',
] as const)('fails closed on %s without inventing facts', async (kind) => {
  const data = inventory()
  if (kind === 'wrong-target') data.cases[0].user_id = 'usr_foreign'
  if (kind === 'unknown-status') Object.assign(data.cases[0], { status: 'scheduled' })
  if (kind === 'unknown-mode') Object.assign(data.cases[0], { mode: 'automatic' })
  if (kind === 'invalid-date') data.cases[0].planned_at = '2026-02-30T00:00:00Z'
  if (kind === 'missing-assignments') Object.assign(data.cases[0], { assignments: null })
  if (kind === 'duplicate-record') data.cases.push(data.cases[0])
  if (kind === 'too-many') data.cases = Array.from({ length: 101 }, (_, i) => record(`off_${i}`))
  if (kind === 'duplicate-assignment')
    data.cases[0].assignments.team_assignments.push(data.cases[0].assignments.team_assignments[0])
  next = (c) => (c.url?.endsWith('/offboarding') ? Promise.resolve(data) : undefined)
  await mount()
  await until(() => expect(button('Retry recorded offboarding')).toBeDefined())
  expect(button()).toBeUndefined()
  expect(card().querySelector('dl')).toBeNull()
})
it('switches recorded labels live to Chinese without refetching or adding lifecycle actions', async () => {
  await mount()
  await loaded()
  const count = requests.length
  await act(async () => i18n.changeLanguage('zh'))
  expect(card().textContent).toContain('计划已保存')
  expect(card().textContent).toContain('1 个项目 / 1 个 Team')
  expect(card().textContent).toContain('未记录')
  expect(button('Review offboarding')).toBeUndefined()
  expect(button('查看离职交接')).toBeDefined()
  expect(requests).toHaveLength(count)
})
it('integrates only in existing Settings and preserves Metadata draft through real Session renewal', async () => {
  permissions = ['members.read', 'members.write']
  await mount(true)
  await loaded()
  const name = host.querySelector<HTMLInputElement>('input[name="name"]')!
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
      name,
      'Unsent custom draft',
    )
    name.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await act(async () => cache.invalidateQueries({ queryKey: ['auth', 'session'] }))
  await loaded()
  expect(host.querySelector<HTMLInputElement>('input[name="name"]')!.value).toBe(
    'Unsent custom draft',
  )
  expect(requests.filter((r) => r.url === '/auth/session')).toHaveLength(2)
  const before = requests.filter((r) => r.url?.endsWith('/offboarding')).length
  await act(async () => router.navigate('/admin/members/usr_target?tab=overview'))
  expect(host.querySelector('section[aria-label="Offboarding"]')).toBeNull()
  expect(requests.filter((r) => r.url?.endsWith('/offboarding'))).toHaveLength(before)
})
it('clears old actor navigation immediately and requires the new actor permissions and exact target read', async () => {
  await mount()
  await loaded()
  const old = button()!
  session = { ...session, user: { ...session.user, id: 'usr_other_actor' } }
  await act(async () =>
    cache.fetchQuery({ queryKey: ['auth', 'session'], queryFn: () => getSession() }),
  )
  expect(button()).toBeUndefined()
  await act(async () => old.click())
  expect(router.state.location.pathname).toBe('/admin/members/usr_target')
  await act(async () => authority())
  await loaded()
  const own = cache.getQueryCache().findAll({ queryKey: ['admin', 'member-offboarding-summary'] })
  expect(own.some((q) => q.queryKey[2] === 'usr_other_actor')).toBe(true)
})
it('does not render stale facts after an errored Session read despite retained cached Session data', async () => {
  await mount()
  await loaded()
  const old = button()!
  next = (c) => (c.url === '/auth/session' ? Promise.reject(error(c, 401)) : undefined)
  await act(async () => {
    await cache
      .fetchQuery({ queryKey: ['auth', 'session'], queryFn: () => getSession() })
      .catch(() => undefined)
    old.click()
  })
  expect(card().querySelector('dl')).toBeNull()
  expect(button()).toBeUndefined()
  expect(router.state.location.pathname).toBe('/admin/members/usr_target')
})
it('hides and denies a stale click while the own read is fetching, including an eventual failed read', async () => {
  await mount()
  await loaded()
  const old = button()!,
    pending = deferred()
  next = (c) =>
    c.url?.endsWith('/offboarding')
      ? pending.promise.then(() => {
          throw error(c, 503)
        })
      : undefined
  let refetch!: Promise<void>
  await act(async () => {
    refetch = cache.refetchQueries({ queryKey: ['admin', 'member-offboarding-summary'] })
    old.click()
  })
  expect(card().querySelector('dl')).toBeNull()
  expect(button()).toBeUndefined()
  await act(async () => {
    pending.resolve(undefined)
    await refetch
  })
  expect(button('Retry recorded offboarding')).toBeDefined()
  expect(router.state.location.pathname).toBe('/admin/members/usr_target')
})
it('supports exactly 100 recent records without asserting older-history completeness', async () => {
  cases = Array.from({ length: 100 }, (_, i) => record(`off_${i}`, 'completed'))
  await mount()
  await loaded()
  expect(card().textContent).toContain('Completed record')
  expect(card().textContent).toContain('up to 100 recent records')
})
